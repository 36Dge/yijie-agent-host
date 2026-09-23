package session

import (
	"errors"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

// GetByTask reads the existing unique index and record in one snapshot. Missing
// includes normal cleanup; it does not prove that the task never executed.
func (s *Store) GetByTask(taskID string) (Record, error) {
	if !isCanonicalNonZeroUUID(taskID) {
		return Record{}, fmt.Errorf("%w: task_id must be a canonical non-zero UUID", ErrInvalidArgument)
	}
	var record Record
	err := s.db.View(func(tx *bolt.Tx) error {
		sessionID := tx.Bucket(tasksBucket).Get([]byte(taskID))
		if sessionID == nil {
			return ErrNotFound
		}
		loaded, err := s.loadRecord(tx, string(sessionID))
		if err != nil {
			return err
		}
		if loaded.TaskID != taskID || loaded.AgentSessionID != string(sessionID) {
			return errors.New("persisted task mapping is inconsistent")
		}
		record = loaded
		return nil
	})
	return record, err
}

// LookupSessionMapping and LookupTurnOperation deliberately do not inspect the
// Runtime, refresh trace fields, resume sessions or repair submission state.
func (s *Service) LookupSessionMapping(taskID string) (Record, error) {
	return s.store.GetByTask(taskID)
}

func (s *Service) LookupTurnOperation(sessionID, operationID string) (TurnOperation, error) {
	if !isCanonicalNonZeroUUID(sessionID) {
		return TurnOperation{}, fmt.Errorf("%w: agent_session_id must be a canonical non-zero UUID", ErrInvalidArgument)
	}
	return s.store.TurnOperation(sessionID, operationID)
}
