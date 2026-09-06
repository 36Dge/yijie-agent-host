package skills

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	bolt "go.etcd.io/bbolt"
)

const operationsDatabaseName = "operations.v2.db"

var operationsBucket = []byte("operations-v2")

// The v2 marker is published only after importing and syncing every v1 record.
// Old Hosts reject it, rather than reopening a partial idempotency history.
func (s *Service) openOperationDatabase(create bool) (*bolt.DB, error) {
	info, err := s.state.Lstat(operationsDatabaseName)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return nil, errors.New("Skill operation database is not owner-only")
		}
	} else if !create || !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return bolt.Open(filepath.Join(s.managedRoot, stateDirectoryName, operationsDatabaseName), 0o600, &bolt.Options{
		Timeout: time.Second,
		OpenFile: func(_ string, flags int, mode os.FileMode) (*os.File, error) {
			return s.state.OpenFile(operationsDatabaseName, flags|syscall.O_NOFOLLOW, mode)
		},
	})
}

func operationFromPersisted(p persistedOperation, current catalog) *operationRecord {
	r := &operationRecord{
		ID: p.ID, Fingerprint: p.Fingerprint, Kind: p.Kind, SkillID: p.SkillID,
		Status: p.Status, Phase: p.Phase, HadPrevious: p.HadPrevious,
		PreviousEnabled: p.PreviousEnabled, State: stateFromPersistedV1(p.State),
		Snapshot: snapshotFromPersistedV1(p.Snapshot), ErrorCode: p.ErrorCode,
		CompletedAt: dereferenceTime(p.CompletedAt), done: make(chan struct{}),
	}
	backfillLegacyBlockedReason(r.State, current)
	if r.Snapshot != nil {
		for i := range r.Snapshot.Skills {
			backfillLegacyBlockedReason(&r.Snapshot.Skills[i], current)
		}
	}
	close(r.done)
	return r
}

func decodeDatabaseOperation(id, raw []byte) (persistedOperation, error) {
	var p persistedOperation
	if len(raw) == 0 || len(raw) > maxOperationsFileBytes || strictJSON(raw, &p) != nil ||
		p.ID != string(id) || validatePersistedOperation(p) != nil {
		return p, errors.New("Skill operation database record is invalid")
	}
	return p, nil
}

func (s *Service) loadOperationDatabase() error {
	db, err := s.openOperationDatabase(false)
	if err != nil {
		return err
	}
	s.operationDB = db
	// Completed transactions can still have cleanup directories. Their commit
	// decisions must be present when the existing recovery code examines them.
	referenced := make(map[string]bool)
	entries, err := fs.ReadDir(s.managed.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, id, ok := parseSkillTransactionName(entry.Name()); ok {
			referenced[id] = true
		}
	}
	return db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(operationsBucket)
		if bucket == nil {
			return errors.New("Skill operation database is not initialized")
		}
		return bucket.ForEach(func(id, raw []byte) error {
			p, err := decodeDatabaseOperation(id, raw)
			if err != nil {
				return err
			}
			if p.Status != "complete" || referenced[p.ID] {
				if len(s.operations) >= maxOperations {
					return errors.New("too many unfinished Skill operations")
				}
				s.operations[p.ID] = operationFromPersisted(p, s.operationCatalog)
			}
			return nil
		})
	})
}

func (s *Service) cachePersistedOperation(id string) error {
	if s.operations[id] != nil {
		return nil
	}
	return s.operationDB.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(operationsBucket).Get([]byte(id))
		if raw == nil {
			return nil
		}
		p, err := decodeDatabaseOperation([]byte(id), raw)
		if err != nil {
			return err
		}
		s.operations[id] = operationFromPersisted(p, s.operationCatalog)
		return nil
	})
}

func (s *Service) releaseCompletedOperationCache() {
	for id, record := range s.operations {
		if record.Status == "complete" && channelClosed(record.done) {
			delete(s.operations, id) // The database still owns this exact replay value.
		}
	}
}

func (s *Service) persistOperationDatabase(stored operationStore) error {
	values := make(map[string][]byte, len(stored.Operations))
	for _, operation := range stored.Operations {
		raw, err := json.Marshal(operation)
		if err != nil {
			return err
		}
		if len(raw) > maxOperationsFileBytes {
			return errors.New("Skill operation result is too large")
		}
		values[operation.ID] = raw
	}
	changed := false
	if err := s.operationDB.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(operationsBucket)
		for id := range s.operationDeletes {
			changed = changed || bucket.Get([]byte(id)) != nil
		}
		for id, raw := range values {
			changed = changed || !bytes.Equal(bucket.Get([]byte(id)), raw)
		}
		return nil
	}); err != nil {
		return err
	}
	if !changed {
		clear(s.operationDeletes)
		s.operationStoreNeedsV1Rewrite = false
		return nil
	}
	err := s.operationDB.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(operationsBucket)
		for id := range s.operationDeletes {
			if err := bucket.Delete([]byte(id)); err != nil {
				return err
			}
		}
		for id, raw := range values {
			if err := bucket.Put([]byte(id), raw); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		clear(s.operationDeletes)
		s.operationStoreNeedsV1Rewrite = false
	}
	return err
}

func (s *Service) migrateOperationDatabase() (err error) {
	legacy, err := s.state.ReadFile(operationsFileName)
	if err != nil {
		return err
	}
	// Keep a content-addressed exact backup even if a previous pre-marker attempt
	// was interrupted and v1 subsequently accepted more completed operations.
	backup := "operations.v1-" + operationFingerprint(string(legacy)) + ".json"
	if _, err := s.state.Lstat(backup); errors.Is(err, os.ErrNotExist) {
		if err := writeOwnerOnlyFile(s.state, backup, legacy); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	db, err := s.openOperationDatabase(true)
	if err != nil {
		return err
	}
	s.operationDB = db
	published := false
	defer func() {
		if !published {
			_ = db.Close()
			s.operationDB = nil
		}
	}()
	// While operations.json is v1, only that file is authoritative. A database
	// left by an interrupted import never contains an accepted v2 operation.
	if err := db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket(operationsBucket) != nil {
			if err := tx.DeleteBucket(operationsBucket); err != nil {
				return err
			}
		}
		_, err := tx.CreateBucket(operationsBucket)
		return err
	}); err != nil {
		return err
	}
	clear(s.operationDeletes)
	if err := s.persistOperationsLocked(); err != nil {
		return err
	}
	if err := syncRoot(s.state); err != nil {
		return err
	}
	const temporary = "operations.v2-marker.tmp"
	_ = s.state.Remove(temporary)
	if err := writeOwnerOnlyFile(s.state, temporary, []byte("{\"schema_version\":2,\"operations\":[]}\n")); err != nil {
		return err
	}
	if err := s.state.Rename(temporary, operationsFileName); err != nil {
		return err
	}
	// Once renamed, retain the database authority even if directory sync fails.
	// Reverting to JSON here could overwrite the already-published v2 marker.
	published = true
	return syncRoot(s.state)
}
