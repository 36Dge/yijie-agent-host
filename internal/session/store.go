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

	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

const (
	StateStarting = "starting"
	StateIdle     = "idle"
	StateActive   = "active"
	StateCleaning = "cleaning"
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
	sessionsBucket          = []byte("sessions")
	tasksBucket             = []byte("task_index")
	threadsBucket           = []byte("thread_index")
	metadataBucket          = []byte("metadata")
	cleanupReceiptsBucket   = []byte("cleanup_receipts_v2")
	cleanupOperationsBucket = []byte("cleanup_operations_v3")
	storeSchemaVersionKey   = []byte("schema_version")
	feat126CwdEncodingKey   = []byte("feat126_cwd_encoding")
	feat126RunIDKey         = []byte("feat126_run_id")
)

const (
	storeSchemaVersion        = "3"
	feat126CwdEncodingVersion = "opaque-project-v1"
	feat126OpaqueProjectCwd   = "feat126-s10-project"
)

type TraceContext struct {
	TraceID   string `json:"trace_id,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	TenantID  string `json:"tenant_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
}

type Record struct {
	TaskID             string       `json:"task_id"`
	AgentSessionID     string       `json:"agent_session_id"`
	CodexThreadID      string       `json:"codex_thread_id,omitempty"`
	RuntimeSessionID   string       `json:"runtime_session_id,omitempty"`
	ActiveTurnID       string       `json:"active_turn_id,omitempty"`
	LastTurnID         string       `json:"last_turn_id,omitempty"`
	LastTurnStatus     string       `json:"last_turn_status,omitempty"`
	State              string       `json:"state"`
	Cwd                string       `json:"cwd"`
	Model              string       `json:"model,omitempty"`
	ModelProvider      string       `json:"model_provider,omitempty"`
	FailureCode        string       `json:"failure_code,omitempty"`
	CleanupOperationID string       `json:"cleanup_operation_id,omitempty"`
	Trace              TraceContext `json:"trace"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
}

type Store struct {
	db                      *bolt.DB
	receiptKey              [32]byte
	feat126ProjectDirectory string
	feat126RunID            string
	feat126RunRoot          string
}

type StoreOption func(*Store) error

func WithFEAT126Authority(projectDirectory, runID string) StoreOption {
	return func(store *Store) error {
		canonical, err := canonicalFEAT126ProjectDirectory(projectDirectory)
		if err != nil {
			return err
		}
		runRoot, err := validateExactOwnerDirectory(filepath.Dir(canonical), "FEAT-126 run root")
		if err != nil {
			return err
		}
		parsed, err := uuid.Parse(runID)
		if err != nil || parsed == uuid.Nil || parsed.String() != runID || filepath.Base(runRoot) != runID {
			return errors.New("FEAT-126 run authority is invalid")
		}
		store.feat126ProjectDirectory = canonical
		store.feat126RunID = runID
		store.feat126RunRoot = runRoot
		return nil
	}
}

func OpenStore(hostHome string, options ...StoreOption) (*Store, error) {
	if hostHome == "" || !filepath.IsAbs(hostHome) {
		return nil, errors.New("Agent Host home must be an absolute path")
	}
	store := &Store{}
	for _, option := range options {
		if option != nil {
			if err := option(store); err != nil {
				return nil, err
			}
		}
	}
	featureProfile := store.feat126ProjectDirectory != ""
	if featureProfile {
		canonicalHostHome, err := validateExactOwnerDirectory(hostHome, "FEAT-126 Host home")
		if err != nil {
			return nil, err
		}
		if filepath.Base(canonicalHostHome) != "host-home" ||
			store.feat126RunRoot != filepath.Dir(canonicalHostHome) {
			return nil, errors.New("FEAT-126 project directory is outside the Host run authority")
		}
		hostHome = canonicalHostHome
	} else if err := preparePrivateDirectory(hostHome); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(hostHome, "sessions.db")
	createdByOpen := false
	if featureProfile {
		var err error
		createdByOpen, err = prepareFEAT126StoreFile(dbPath)
		if err != nil {
			return nil, err
		}
	} else if err := preparePrivateFileIfExists(dbPath); err != nil {
		return nil, err
	}
	db, err := bolt.Open(dbPath, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("open Agent Host session store: %w", err)
	}
	if !featureProfile {
		if err := os.Chmod(dbPath, 0o600); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("protect Agent Host session store: %w", err)
		}
	}
	if err := validatePrivateFileExact(dbPath); err != nil {
		_ = db.Close()
		return nil, err
	}
	store.db = db
	if err := db.Update(func(tx *bolt.Tx) error {
		if featureProfile && !createdByOpen {
			if err := store.validateExistingFEAT126Store(tx); err != nil {
				return err
			}
			return purgeExpiredCleanupReceipts(tx, time.Now().UTC())
		}
		for _, bucket := range [][]byte{sessionsBucket, tasksBucket, threadsBucket, metadataBucket, cleanupReceiptsBucket, cleanupOperationsBucket} {
			if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
				return err
			}
		}
		metadata := tx.Bucket(metadataBucket)
		version := metadata.Get(storeSchemaVersionKey)
		if version != nil && string(version) != "1" && string(version) != "2" && string(version) != storeSchemaVersion {
			return fmt.Errorf("unsupported Agent Host store schema version %q", version)
		}
		if version == nil || string(version) == "1" || string(version) == "2" {
			if err := metadata.Put(storeSchemaVersionKey, []byte(storeSchemaVersion)); err != nil {
				return err
			}
		}
		if featureProfile {
			if err := store.initializeNewFEAT126Store(tx); err != nil {
				return err
			}
		} else if err := validateDefaultStoreEncoding(tx); err != nil {
			return err
		}
		return purgeExpiredCleanupReceipts(tx, time.Now().UTC())
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize Agent Host session store: %w", err)
	}
	receiptKey, err := loadOrCreateReceiptKey(hostHome)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	store.receiptKey = receiptKey
	return store, nil
}

func prepareFEAT126StoreFile(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		file, createErr := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if createErr != nil {
			return false, fmt.Errorf("exclusively create FEAT-126 session store: %w", createErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			return false, fmt.Errorf("close new FEAT-126 session store: %w", closeErr)
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect FEAT-126 session store: %w", err)
	}
	if info.Size() == 0 {
		return false, errors.New("pre-existing FEAT-126 session store is uninitialized")
	}
	if err := validatePrivateFileExact(path); err != nil {
		return false, err
	}
	return false, nil
}

func canonicalFEAT126ProjectDirectory(projectDirectory string) (string, error) {
	if projectDirectory == "" || !filepath.IsAbs(projectDirectory) || filepath.Base(projectDirectory) != "project" {
		return "", errors.New("FEAT-126 project directory authority is invalid")
	}
	return validateExactOwnerDirectory(projectDirectory, "FEAT-126 project directory authority")
}

func validateExactOwnerDirectory(path, authority string) (string, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", fmt.Errorf("%s is not canonical", authority)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("%s cannot be inspected", authority)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return "", fmt.Errorf("%s must be an owner-only non-symlink directory", authority)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return "", fmt.Errorf("%s is not canonical", authority)
	}
	return resolved, nil
}

func (s *Store) initializeNewFEAT126Store(tx *bolt.Tx) error {
	metadata := tx.Bucket(metadataBucket)
	if metadata.Get(feat126CwdEncodingKey) != nil || metadata.Get(feat126RunIDKey) != nil || tx.Bucket(sessionsBucket).Stats().KeyN != 0 {
		return errors.New("new FEAT-126 session store is not empty")
	}
	if err := metadata.Put(feat126CwdEncodingKey, []byte(feat126CwdEncodingVersion)); err != nil {
		return err
	}
	return metadata.Put(feat126RunIDKey, []byte(s.feat126RunID))
}

func (s *Store) validateExistingFEAT126Store(tx *bolt.Tx) error {
	for _, name := range [][]byte{sessionsBucket, tasksBucket, threadsBucket, metadataBucket, cleanupReceiptsBucket, cleanupOperationsBucket} {
		if tx.Bucket(name) == nil {
			return errors.New("pre-existing FEAT-126 session store is unmarked")
		}
	}
	metadata := tx.Bucket(metadataBucket)
	if string(metadata.Get(storeSchemaVersionKey)) != storeSchemaVersion {
		return errors.New("pre-existing FEAT-126 session store has an unsupported schema")
	}
	if string(metadata.Get(feat126CwdEncodingKey)) != feat126CwdEncodingVersion ||
		string(metadata.Get(feat126RunIDKey)) != s.feat126RunID {
		return errors.New("pre-existing FEAT-126 session store authority mismatch")
	}
	return tx.Bucket(sessionsBucket).ForEach(func(_, encoded []byte) error {
		var record Record
		if err := json.Unmarshal(encoded, &record); err != nil {
			return fmt.Errorf("decode persisted agent session: %w", err)
		}
		if record.Cwd != feat126OpaqueProjectCwd {
			return errors.New("FEAT-126 session store contains a non-opaque project reference")
		}
		return nil
	})
}

func validateDefaultStoreEncoding(tx *bolt.Tx) error {
	metadata := tx.Bucket(metadataBucket)
	if metadata.Get(feat126CwdEncodingKey) != nil || metadata.Get(feat126RunIDKey) != nil {
		return errors.New("FEAT-126 session store requires exact feature authority")
	}
	return tx.Bucket(sessionsBucket).ForEach(func(_, encoded []byte) error {
		var record Record
		if err := json.Unmarshal(encoded, &record); err != nil {
			return fmt.Errorf("decode persisted agent session: %w", err)
		}
		if record.Cwd == feat126OpaqueProjectCwd {
			return errors.New("opaque FEAT-126 project reference requires exact feature authority")
		}
		return nil
	})
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
	expired := false
	err := s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(cleanupReceiptsBucket)
		value := bucket.Get([]byte(operationID))
		if value == nil {
			return ErrNotFound
		}
		if err := json.Unmarshal(value, &receipt); err != nil {
			return fmt.Errorf("decode cleanup receipt: %w", err)
		}
		if !receipt.ExpiresAt.After(time.Now().UTC()) {
			if err := bucket.Delete([]byte(operationID)); err != nil {
				return err
			}
			expired = true
			return nil
		}
		if !hmac.Equal([]byte(receipt.SessionHash), []byte(s.keyedSessionHash(sessionID))) {
			return ErrCleanupConflict
		}
		return nil
	})
	if err == nil && expired {
		return CleanupReceipt{}, ErrNotFound
	}
	return receipt, err
}

const (
	CleanupStateRuntimeDeletePending   = "runtime_delete_pending"
	CleanupStateRuntimeDeleteConfirmed = "runtime_delete_confirmed"
)

type CleanupOperation struct {
	SchemaVersion int       `json:"schema_version"`
	OperationID   string    `json:"operation_id"`
	SessionHash   string    `json:"keyed_session_hash"`
	ThreadID      string    `json:"thread_id"`
	State         string    `json:"state"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (s *Store) BeginCleanup(operationID, sessionID string) (CleanupOperation, error) {
	var operation CleanupOperation
	err := s.db.Update(func(tx *bolt.Tx) error {
		if err := purgeExpiredCleanupReceipts(tx, time.Now().UTC()); err != nil {
			return err
		}
		operations := tx.Bucket(cleanupOperationsBucket)
		if encoded := operations.Get([]byte(operationID)); encoded != nil {
			if err := json.Unmarshal(encoded, &operation); err != nil {
				return fmt.Errorf("decode cleanup operation: %w", err)
			}
			if !hmac.Equal([]byte(operation.SessionHash), []byte(s.keyedSessionHash(sessionID))) {
				return ErrCleanupConflict
			}
			return nil
		}
		if encoded := tx.Bucket(cleanupReceiptsBucket).Get([]byte(operationID)); encoded != nil {
			var receipt CleanupReceipt
			if json.Unmarshal(encoded, &receipt) != nil || !hmac.Equal([]byte(receipt.SessionHash), []byte(s.keyedSessionHash(sessionID))) {
				return ErrCleanupConflict
			}
			return ErrNotFound
		}
		record, err := s.loadRecord(tx, sessionID)
		if err != nil {
			return err
		}
		if record.ActiveTurnID != "" || record.State == StateActive || record.State == StateStarting || record.CleanupOperationID != "" {
			return ErrTurnActive
		}
		now := time.Now().UTC()
		operation = CleanupOperation{
			SchemaVersion: 1, OperationID: operationID, SessionHash: s.keyedSessionHash(sessionID),
			ThreadID: record.CodexThreadID, State: CleanupStateRuntimeDeletePending,
			CreatedAt: now, UpdatedAt: now,
		}
		encoded, err := json.Marshal(operation)
		if err != nil {
			return err
		}
		if err := operations.Put([]byte(operationID), encoded); err != nil {
			return err
		}
		record.CleanupOperationID = operationID
		record.State = StateCleaning
		record.UpdatedAt = now
		return s.saveRecord(tx, record)
	})
	return operation, err
}

func (s *Store) MarkCleanupRuntimeDeleted(operationID, sessionID string) (CleanupOperation, error) {
	var operation CleanupOperation
	err := s.db.Update(func(tx *bolt.Tx) error {
		operations := tx.Bucket(cleanupOperationsBucket)
		encoded := operations.Get([]byte(operationID))
		if encoded == nil {
			return ErrNotFound
		}
		if err := json.Unmarshal(encoded, &operation); err != nil {
			return fmt.Errorf("decode cleanup operation: %w", err)
		}
		if !hmac.Equal([]byte(operation.SessionHash), []byte(s.keyedSessionHash(sessionID))) {
			return ErrCleanupConflict
		}
		operation.State = CleanupStateRuntimeDeleteConfirmed
		operation.UpdatedAt = time.Now().UTC()
		encoded, err := json.Marshal(operation)
		if err != nil {
			return err
		}
		return operations.Put([]byte(operationID), encoded)
	})
	return operation, err
}

func (s *Store) DeleteSessionWithReceipt(operationID, sessionID string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		operations := tx.Bucket(cleanupOperationsBucket)
		encodedOperation := operations.Get([]byte(operationID))
		if encodedOperation == nil {
			return ErrSessionNotUsable
		}
		var operation CleanupOperation
		if json.Unmarshal(encodedOperation, &operation) != nil || !hmac.Equal([]byte(operation.SessionHash), []byte(s.keyedSessionHash(sessionID))) {
			return ErrCleanupConflict
		}
		if operation.State != CleanupStateRuntimeDeleteConfirmed {
			return ErrSessionNotUsable
		}
		record, err := s.loadRecord(tx, sessionID)
		if err != nil {
			return err
		}
		if record.ActiveTurnID != "" || record.CleanupOperationID != operationID || record.State != StateCleaning {
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
		if err := tx.Bucket(cleanupReceiptsBucket).Put([]byte(operationID), encoded); err != nil {
			return err
		}
		return operations.Delete([]byte(operationID))
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

func validatePrivateFileExact(path string) error {
	if err := validatePrivateFileIdentity(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect private Host file: %w", err)
	}
	if info.Mode().Perm() != 0o600 {
		return errors.New("private Host file must have exact 0600 permissions")
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
		if err := s.saveRecord(tx, record); err != nil {
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
		record, err := s.loadRecord(tx, sessionID)
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
		if err := s.saveRecord(tx, record); err != nil {
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
		if record.CodexThreadID == "" || record.State == StateFailed || record.State == StateCleaning || record.CleanupOperationID != "" {
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
		if record.State == StateCleaning || record.CleanupOperationID != "" {
			return ErrSessionNotUsable
		}
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
		if record.State == StateCleaning || record.CleanupOperationID != "" {
			return ErrSessionNotUsable
		}
		record.ActiveTurnID = ""
		record.State = StateIdle
		record.FailureCode = failureCode
		return nil
	})
}

func (s *Store) CompleteTurn(sessionID, turnID, status string) (Record, error) {
	return s.update(sessionID, func(record *Record) error {
		if record.State == StateCleaning || record.CleanupOperationID != "" {
			return ErrSessionNotUsable
		}
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
		if record.State == StateCleaning || record.CleanupOperationID != "" {
			return ErrSessionNotUsable
		}
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
		if record.State == StateCleaning || record.CleanupOperationID != "" {
			return ErrSessionNotUsable
		}
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
		loaded, err := s.loadRecord(tx, sessionID)
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
		loaded, err := s.loadRecord(tx, string(sessionID))
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
		record, err := s.loadRecord(tx, sessionID)
		if err != nil {
			return err
		}
		if err := mutate(&record); err != nil {
			return err
		}
		record.UpdatedAt = time.Now().UTC()
		if err := s.saveRecord(tx, record); err != nil {
			return err
		}
		updated = record
		return nil
	})
	return updated, err
}

func (s *Store) loadRecord(tx *bolt.Tx, sessionID string) (Record, error) {
	encoded := tx.Bucket(sessionsBucket).Get([]byte(sessionID))
	if encoded == nil {
		return Record{}, ErrNotFound
	}
	var record Record
	if err := json.Unmarshal(encoded, &record); err != nil {
		return Record{}, fmt.Errorf("decode persisted agent session: %w", err)
	}
	if s.feat126ProjectDirectory != "" {
		if record.Cwd != feat126OpaqueProjectCwd {
			return Record{}, errors.New("FEAT-126 session project reference is not opaque")
		}
		record.Cwd = s.feat126ProjectDirectory
	} else if record.Cwd == feat126OpaqueProjectCwd {
		return Record{}, errors.New("opaque FEAT-126 project reference requires exact feature authority")
	}
	return record, nil
}

func (s *Store) saveRecord(tx *bolt.Tx, record Record) error {
	if s.feat126ProjectDirectory != "" {
		if record.Cwd != s.feat126ProjectDirectory {
			return errors.New("FEAT-126 session cwd is outside the authorized project")
		}
		record.Cwd = feat126OpaqueProjectCwd
	} else if record.Cwd == feat126OpaqueProjectCwd {
		return errors.New("opaque FEAT-126 project reference requires exact feature authority")
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return tx.Bucket(sessionsBucket).Put([]byte(record.AgentSessionID), encoded)
}
