package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
)

var (
	sessionsBucket = []byte("sessions")
	tasksBucket    = []byte("task_index")
	threadsBucket  = []byte("thread_index")
)

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
	db *bolt.DB
}

func OpenStore(hostHome string) (*Store, error) {
	if hostHome == "" || !filepath.IsAbs(hostHome) {
		return nil, errors.New("Agent Host home must be an absolute path")
	}
	if err := os.MkdirAll(hostHome, 0o700); err != nil {
		return nil, fmt.Errorf("create Agent Host home: %w", err)
	}
	if err := os.Chmod(hostHome, 0o700); err != nil {
		return nil, fmt.Errorf("protect Agent Host home: %w", err)
	}
	dbPath := filepath.Join(hostHome, "sessions.db")
	db, err := bolt.Open(dbPath, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("open Agent Host session store: %w", err)
	}
	if err := os.Chmod(dbPath, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("protect Agent Host session store: %w", err)
	}
	store := &Store{db: db}
	if err := db.Update(func(tx *bolt.Tx) error {
		for _, bucket := range [][]byte{sessionsBucket, tasksBucket, threadsBucket} {
			if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize Agent Host session store: %w", err)
	}
	return store, nil
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
