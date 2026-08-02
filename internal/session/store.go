package session

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	bolt "go.etcd.io/bbolt"
)

const (
	StateStarting = "starting"
	StateIdle     = "idle"
	StateActive   = "active"
	StateFailed   = "failed"
)

var (
	ErrNotFound         = errors.New("agent session not found")
	ErrTaskExists       = errors.New("task already has an agent session")
	ErrThreadExists     = errors.New("Codex thread is already mapped")
	ErrTurnActive       = errors.New("agent session already has an active turn")
	ErrTurnNotActive    = errors.New("turn is not active for this agent session")
	ErrSessionNotUsable = errors.New("agent session is not ready for this operation")
	ErrInvalidArgument  = errors.New("invalid argument")
	ErrRuntimeRequest   = errors.New("Codex Runtime request failed")
	ErrCleanupConflict  = errors.New("cleanup operation conflicts with another session")
)

var (
	sessionsBucket        = []byte("sessions")
	tasksBucket           = []byte("task_index")
	threadsBucket         = []byte("thread_index")
	metadataBucket        = []byte("metadata")
	cleanupReceiptsBucket = []byte("cleanup_receipts_v2")
	storeSchemaVersionKey = []byte("schema_version")
)

const storeSchemaVersion = "2"

type TraceContext struct {
	TraceID   string `json:"trace_id,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	TenantID  string `json:"tenant_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
}

type Record struct {
	TaskID           string       `json:"task_id"`
	AgentSessionID   string       `json:"agent_session_id"`
	CodexThreadID    string       `json:"codex_thread_id,omitempty"`
	RuntimeSessionID string       `json:"runtime_session_id,omitempty"`
	ActiveTurnID     string       `json:"active_turn_id,omitempty"`
	LastTurnID       string       `json:"last_turn_id,omitempty"`
	LastTurnStatus   string       `json:"last_turn_status,omitempty"`
	State            string       `json:"state"`
	Cwd              string       `json:"cwd"`
	Model            string       `json:"model,omitempty"`
	ModelProvider    string       `json:"model_provider,omitempty"`
	FailureCode      string       `json:"failure_code,omitempty"`
	Trace            TraceContext `json:"trace"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
}

type Store struct {
	db         *bolt.DB
	receiptKey [32]byte
}

func OpenStore(hostHome string) (*Store, error) {
	if hostHome == "" || !filepath.IsAbs(hostHome) {
		return nil, errors.New("Agent Host home must be an absolute path")
	}
	if err := preparePrivateDirectory(hostHome); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(hostHome, "sessions.db")
	if err := preparePrivateFileIfExists(dbPath); err != nil {
		return nil, err
	}
	db, err := bolt.Open(dbPath, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("open Agent Host session store: %w", err)
	}
	if err := os.Chmod(dbPath, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("protect Agent Host session store: %w", err)
	}
	if err := validatePrivateFile(dbPath); err != nil {
		_ = db.Close()
		return nil, err
	}
	receiptKey, err := loadOrCreateReceiptKey(hostHome)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	store := &Store{db: db, receiptKey: receiptKey}
	if err := db.Update(func(tx *bolt.Tx) error {
		for _, bucket := range [][]byte{sessionsBucket, tasksBucket, threadsBucket, metadataBucket, cleanupReceiptsBucket} {
			if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
				return err
			}
		}
		metadata := tx.Bucket(metadataBucket)
		version := metadata.Get(storeSchemaVersionKey)
		if version != nil && string(version) != "1" && string(version) != storeSchemaVersion {
			return fmt.Errorf("unsupported Agent Host store schema version %q", version)
		}
		if version == nil || string(version) == "1" {
			if err := metadata.Put(storeSchemaVersionKey, []byte(storeSchemaVersion)); err != nil {
				return err
			}
		}
		return purgeExpiredCleanupReceipts(tx, time.Now().UTC())
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize Agent Host session store: %w", err)
	}
	return store, nil
}

func purgeExpiredCleanupReceipts(tx *bolt.Tx, now time.Time) error {
	bucket := tx.Bucket(cleanupReceiptsBucket)
	cursor := bucket.Cursor()
	for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
		var receipt CleanupReceipt
		if err := json.Unmarshal(value, &receipt); err != nil {
			return fmt.Errorf("decode cleanup receipt: %w", err)
		}
		if !receipt.ExpiresAt.After(now) {
			if err := cursor.Delete(); err != nil {
				return err
			}
		}
	}
	return nil
}

type CleanupReceipt struct {
	SchemaVersion int       `json:"schema_version"`
	OperationID   string    `json:"operation_id"`
	SessionHash   string    `json:"keyed_session_hash"`
	Outcome       string    `json:"outcome"`
	RuntimeTree   string    `json:"runtime_thread_tree"`
	HostMapping   string    `json:"host_mapping"`
	HostReplay    string    `json:"host_replay"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

func (s *Store) CleanupReceipt(operationID, sessionID string) (CleanupReceipt, error) {
	var receipt CleanupReceipt
	err := s.db.View(func(tx *bolt.Tx) error {
		value := tx.Bucket(cleanupReceiptsBucket).Get([]byte(operationID))
		if value == nil {
			return ErrNotFound
		}
		if err := json.Unmarshal(value, &receipt); err != nil {
			return fmt.Errorf("decode cleanup receipt: %w", err)
		}
		if receipt.ExpiresAt.Before(time.Now().UTC()) {
			return ErrNotFound
		}
		if !hmac.Equal([]byte(receipt.SessionHash), []byte(s.keyedSessionHash(sessionID))) {
			return ErrCleanupConflict
		}
		return nil
	})
	return receipt, err
}

func (s *Store) DeleteSessionWithReceipt(operationID, sessionID string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		record, err := loadRecord(tx, sessionID)
		if err != nil {
			return err
		}
		if record.ActiveTurnID != "" {
			return ErrTurnActive
		}
		if existing := tx.Bucket(cleanupReceiptsBucket).Get([]byte(operationID)); existing != nil {
			var receipt CleanupReceipt
			if json.Unmarshal(existing, &receipt) != nil || !hmac.Equal([]byte(receipt.SessionHash), []byte(s.keyedSessionHash(sessionID))) {
				return ErrCleanupConflict
			}
			return nil
		}
		if err := tx.Bucket(tasksBucket).Delete([]byte(record.TaskID)); err != nil {
			return err
		}
		if err := tx.Bucket(threadsBucket).Delete([]byte(record.CodexThreadID)); err != nil {
			return err
		}
		if err := tx.Bucket(sessionsBucket).Delete([]byte(sessionID)); err != nil {
			return err
		}
		now := time.Now().UTC()
		receipt := CleanupReceipt{
			SchemaVersion: 1, OperationID: operationID, SessionHash: s.keyedSessionHash(sessionID),
			Outcome: "complete", RuntimeTree: "complete", HostMapping: "complete", HostReplay: "complete",
			CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour),
		}
		encoded, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		return tx.Bucket(cleanupReceiptsBucket).Put([]byte(operationID), encoded)
	})
}

func (s *Store) keyedSessionHash(sessionID string) string {
	hash := hmac.New(sha256.New, s.receiptKey[:])
	_, _ = hash.Write([]byte(sessionID))
	return hex.EncodeToString(hash.Sum(nil))
}

func loadOrCreateReceiptKey(hostHome string) ([32]byte, error) {
	var key [32]byte
	path := filepath.Join(hostHome, "cleanup-receipt.key")
	if err := preparePrivateFileIfExists(path); err != nil {
		return key, err
	}
	content, err := os.ReadFile(path)
	if err == nil {
		if len(content) != len(key) {
			return key, errors.New("cleanup receipt key has invalid length")
		}
		copy(key[:], content)
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return key, fmt.Errorf("read cleanup receipt key: %w", err)
	}
	if _, err := rand.Read(key[:]); err != nil {
		return key, fmt.Errorf("generate cleanup receipt key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return key, fmt.Errorf("create cleanup receipt key: %w", err)
	}
	if _, err := file.Write(key[:]); err != nil {
		_ = file.Close()
		return key, fmt.Errorf("write cleanup receipt key: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return key, fmt.Errorf("sync cleanup receipt key: %w", err)
	}
	if err := file.Close(); err != nil {
		return key, fmt.Errorf("close cleanup receipt key: %w", err)
	}
	if err := validatePrivateFile(path); err != nil {
		return key, err
	}
	return key, nil
}

func preparePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create Agent Host home: %w", err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return fmt.Errorf("inspect Agent Host home: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || int(stat.Uid) != os.Geteuid() {
		return errors.New("Agent Host home is not a safe owner directory")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("protect Agent Host home: %w", err)
	}
	return nil
}

func preparePrivateFileIfExists(path string) error {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect private Host file: %w", err)
	}
	if err := validatePrivateFileIdentity(path); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("protect private Host file: %w", err)
	}
	return validatePrivateFile(path)
}

func validatePrivateFile(path string) error {
	if err := validatePrivateFileIdentity(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect private Host file: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("private Host file is accessible to group or other users")
	}
	return nil
}

func validatePrivateFileIdentity(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect private Host file: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || int(stat.Uid) != os.Geteuid() || stat.Nlink != 1 {
		return errors.New("private Host file is not a safe owner-only regular file")
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Reserve(record Record) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		tasks := tx.Bucket(tasksBucket)
		if tasks.Get([]byte(record.TaskID)) != nil {
			return ErrTaskExists
		}
		sessions := tx.Bucket(sessionsBucket)
		if sessions.Get([]byte(record.AgentSessionID)) != nil {
			return errors.New("agent session id collision")
		}
		now := time.Now().UTC()
		record.State = StateStarting
		record.CreatedAt = now
		record.UpdatedAt = now
		encoded, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if err := sessions.Put([]byte(record.AgentSessionID), encoded); err != nil {
			return err
		}
		return tasks.Put([]byte(record.TaskID), []byte(record.AgentSessionID))
	})
}

func (s *Store) BindThread(sessionID, threadID, runtimeSessionID, model, provider string) (Record, error) {
	var updated Record
	err := s.db.Update(func(tx *bolt.Tx) error {
		threads := tx.Bucket(threadsBucket)
		if existing := threads.Get([]byte(threadID)); existing != nil && string(existing) != sessionID {
			return ErrThreadExists
		}
		record, err := loadRecord(tx, sessionID)
		if err != nil {
			return err
		}
		if record.CodexThreadID != "" && record.CodexThreadID != threadID {
			return ErrThreadExists
		}
		record.CodexThreadID = threadID
		record.RuntimeSessionID = runtimeSessionID
		record.Model = model
		record.ModelProvider = provider
		record.State = StateIdle
		record.FailureCode = ""
		record.UpdatedAt = time.Now().UTC()
		if err := saveRecord(tx, record); err != nil {
			return err
		}
		if err := threads.Put([]byte(threadID), []byte(sessionID)); err != nil {
			return err
		}
		updated = record
		return nil
	})
	return updated, err
}

func (s *Store) PrepareTurn(sessionID string, trace TraceContext) (Record, error) {
	return s.update(sessionID, func(record *Record) error {
		if record.CodexThreadID == "" || record.State == StateFailed {
			return ErrSessionNotUsable
		}
		if record.ActiveTurnID != "" || record.State == StateActive || record.State == StateStarting {
			return ErrTurnActive
		}
		record.State = StateStarting
		record.Trace = trace
		return nil
	})
}

func (s *Store) BindTurn(sessionID, turnID string) (Record, error) {
	return s.update(sessionID, func(record *Record) error {
		// A very short turn can emit turn/completed while the turn/start
		// response is still being delivered to the caller. Do not reactivate a
		// turn whose terminal notification already won that race.
		if record.ActiveTurnID == "" && record.LastTurnID == turnID && record.LastTurnStatus != "" {
			return nil
		}
		if record.ActiveTurnID != "" && record.ActiveTurnID != turnID {
			return ErrTurnActive
		}
		record.ActiveTurnID = turnID
		record.State = StateActive
		return nil
	})
}

func (s *Store) TurnStartFailed(sessionID, failureCode string) (Record, error) {
	return s.update(sessionID, func(record *Record) error {
		record.ActiveTurnID = ""
		record.State = StateIdle
		record.FailureCode = failureCode
		return nil
	})
}

func (s *Store) CompleteTurn(sessionID, turnID, status string) (Record, error) {
	return s.update(sessionID, func(record *Record) error {
		if record.ActiveTurnID != "" && record.ActiveTurnID != turnID {
			return ErrTurnNotActive
		}
		record.ActiveTurnID = ""
		record.LastTurnID = turnID
		record.LastTurnStatus = status
		record.State = StateIdle
		record.FailureCode = ""
		return nil
	})
}

func (s *Store) UpdateTrace(sessionID string, trace TraceContext) (Record, error) {
	return s.update(sessionID, func(record *Record) error {
		record.Trace = trace
		return nil
	})
}

func (s *Store) MarkFailed(sessionID, failureCode string) (Record, error) {
	return s.update(sessionID, func(record *Record) error {
		record.State = StateFailed
		record.FailureCode = failureCode
		return nil
	})
}

func (s *Store) Resume(
	sessionID string,
	trace TraceContext,
	runtimeSessionID, activeTurnID, lastTurnID, lastStatus string,
) (Record, error) {
	return s.update(sessionID, func(record *Record) error {
		record.Trace = trace
		if runtimeSessionID != "" {
			record.RuntimeSessionID = runtimeSessionID
		}
		record.ActiveTurnID = activeTurnID
		record.LastTurnID = lastTurnID
		record.LastTurnStatus = lastStatus
		if activeTurnID == "" {
			record.State = StateIdle
		} else {
			record.State = StateActive
		}
		record.FailureCode = ""
		return nil
	})
}

func (s *Store) Get(sessionID string) (Record, error) {
	var record Record
	err := s.db.View(func(tx *bolt.Tx) error {
		loaded, err := loadRecord(tx, sessionID)
		if err != nil {
			return err
		}
		record = loaded
		return nil
	})
	return record, err
}

func (s *Store) GetByThread(threadID string) (Record, error) {
	var record Record
	err := s.db.View(func(tx *bolt.Tx) error {
		sessionID := tx.Bucket(threadsBucket).Get([]byte(threadID))
		if sessionID == nil {
			return ErrNotFound
		}
		loaded, err := loadRecord(tx, string(sessionID))
		if err != nil {
			return err
		}
		record = loaded
		return nil
	})
	return record, err
}

func (s *Store) update(sessionID string, mutate func(*Record) error) (Record, error) {
	var updated Record
	err := s.db.Update(func(tx *bolt.Tx) error {
		record, err := loadRecord(tx, sessionID)
		if err != nil {
			return err
		}
		if err := mutate(&record); err != nil {
			return err
		}
		record.UpdatedAt = time.Now().UTC()
		if err := saveRecord(tx, record); err != nil {
			return err
		}
		updated = record
		return nil
	})
	return updated, err
}

func loadRecord(tx *bolt.Tx, sessionID string) (Record, error) {
	encoded := tx.Bucket(sessionsBucket).Get([]byte(sessionID))
	if encoded == nil {
		return Record{}, ErrNotFound
	}
	var record Record
	if err := json.Unmarshal(encoded, &record); err != nil {
		return Record{}, fmt.Errorf("decode persisted agent session: %w", err)
	}
	return record, nil
}

func saveRecord(tx *bolt.Tx, record Record) error {
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return tx.Bucket(sessionsBucket).Put([]byte(record.AgentSessionID), encoded)
}
