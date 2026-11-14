package session

import (
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

// Shares the existing bbolt ownership boundary with the normal Host. An active
// writer prevents maintenance; this reader neither migrates nor edits history.
func OpenMaintenanceReader(hostHome string) (*bolt.DB, error) {
	if _, err := validateExactOwnerDirectory(hostHome, "Host maintenance home"); err != nil {
		return nil, err
	}
	path := filepath.Join(hostHome, "sessions.db")
	if err := validatePrivateFileExact(path); err != nil {
		return nil, err
	}
	return bolt.Open(path, 0600, &bolt.Options{ReadOnly: true, Timeout: time.Second})
}
