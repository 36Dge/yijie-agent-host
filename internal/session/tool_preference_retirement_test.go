package session

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

func TestRetiredPreferenceMigrationPreservesUnrelatedSessionBytes(t *testing.T) {
	home := t.TempDir()
	db, err := bolt.Open(filepath.Join(home, "sessions.db"), 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	old := Record{TaskID: uuid.NewString(), AgentSessionID: uuid.NewString(), Cwd: home, Purpose: purposeOrdinary, State: StateIdle}
	raw, _ := json.Marshal(old)
	retired := old
	retired.TaskID, retired.AgentSessionID = uuid.NewString(), uuid.NewString()
	other, _ := json.Marshal(retired)
	var legacy map[string]json.RawMessage
	_ = json.Unmarshal(other, &legacy)
	legacy["tool_profile"] = json.RawMessage(`"light"`)
	other, _ = json.Marshal(legacy)
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{metadataBucket, sessionsBucket, tasksBucket, threadsBucket, modelOperationsV2Bucket} {
			if _, e := tx.CreateBucketIfNotExists(name); e != nil {
				return e
			}
		}
		m := tx.Bucket(metadataBucket)
		if e := m.Put(storeSchemaVersionKey, []byte("12")); e != nil {
			return e
		}
		if e := m.Put(modelBaseVersionKey, []byte("7")); e != nil {
			return e
		}
		if e := tx.Bucket(sessionsBucket).Put([]byte(old.AgentSessionID), raw); e != nil {
			return e
		}
		return tx.Bucket(sessionsBucket).Put([]byte(retired.AgentSessionID), other)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(home, WithChatModelV2Storage())
	if err != nil {
		t.Fatal(err)
	}
	err = store.db.View(func(tx *bolt.Tx) error {
		if string(tx.Bucket(metadataBucket).Get(storeSchemaVersionKey)) != "13" {
			t.Fatal("retirement version not recorded")
		}
		if string(tx.Bucket(sessionsBucket).Get([]byte(old.AgentSessionID))) != string(raw) {
			t.Fatal("unrelated session bytes changed")
		}
		var v map[string]json.RawMessage
		_ = json.Unmarshal(tx.Bucket(sessionsBucket).Get([]byte(retired.AgentSessionID)), &v)
		if _, found := v["tool_profile"]; found {
			t.Fatal("withdrawn preference survived")
		}
		delete(legacy, "tool_profile")
		a, _ := json.Marshal(v)
		b, _ := json.Marshal(legacy)
		if string(a) != string(b) {
			t.Fatal("retirement changed unrelated fields")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(home, WithChatModelV2Storage())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err = reopened.Get(old.AgentSessionID); err != nil {
		t.Fatal(err)
	}
}
