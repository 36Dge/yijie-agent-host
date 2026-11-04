package session

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	agenthost "github.com/36Dge/yijie-agent-host/internal/contracts"
	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

type toolProfileRuntime struct {
	fakeRuntime
	observed []agenthost.ToolProfile
}

func (*toolProfileRuntime) ToolProfilesEnabled() bool { return true }
func (r *toolProfileRuntime) StartThread(ctx context.Context, cwd string) (codex.ThreadInfo, error) {
	r.observed = append(r.observed, codex.ToolProfile(ctx))
	return r.fakeRuntime.StartThread(ctx, cwd)
}
func (r *toolProfileRuntime) ResumeThread(ctx context.Context, id string) (codex.ThreadInfo, error) {
	r.observed = append(r.observed, codex.ToolProfile(ctx))
	return r.fakeRuntime.ResumeThread(ctx, id)
}

func TestToolProfileNormalReopenPreservesChoiceAndLegacyRecord(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "host")
	old, err := OpenStore(home, WithChatModelV2Storage())
	if err != nil {
		t.Fatal(err)
	}
	legacy := Record{TaskID: uuid.NewString(), AgentSessionID: uuid.NewString(), Cwd: root}
	if err = old.Reserve(legacy); err != nil {
		t.Fatal(err)
	}
	var original []byte
	if err = old.db.View(func(tx *bolt.Tx) error {
		original = append([]byte(nil), tx.Bucket(sessionsBucket).Get([]byte(legacy.AgentSessionID))...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	open := func() *Store {
		s, e := OpenStore(home, WithChatModelV2Storage(), WithToolProfileStorage())
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	store := open()
	runtime := &toolProfileRuntime{fakeRuntime: fakeRuntime{startThread: func(string) (codex.ThreadInfo, error) {
		return codex.ThreadInfo{ID: testThreadID, Model: codex.MiniMaxModel, ModelProvider: codex.MiniMaxProviderID}, nil
	}}}
	service := NewService(runtime, store, NewEventHub(16, 4), nil)
	record, err := service.StartSession(context.Background(), StartSessionInput{TaskID: testTaskID, Cwd: root, ToolProfile: "light"})
	if err != nil || record.ToolProfile != "light" || len(runtime.observed) != 1 || runtime.observed[0] != "light" {
		t.Fatalf("new light profile: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if rejected, e := OpenStore(home, WithChatModelV2Storage()); e == nil {
		rejected.Close()
		t.Fatal("old reader accepted new profile format")
	}
	store = open()
	defer store.Close()
	runtime = &toolProfileRuntime{fakeRuntime: fakeRuntime{resume: func(id string) (codex.ThreadInfo, error) {
		return codex.ThreadInfo{ID: id, Model: codex.MiniMaxModel, ModelProvider: codex.MiniMaxProviderID}, nil
	}}}
	service = NewService(runtime, store, NewEventHub(16, 4), nil)
	if _, err = service.ResumeSession(context.Background(), record.AgentSessionID, TraceContext{}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.observed) != 1 || runtime.observed[0] != "light" {
		t.Fatal("normal resume lost immutable product choice")
	}
	if err = store.db.View(func(tx *bolt.Tx) error {
		if string(tx.Bucket(sessionsBucket).Get([]byte(legacy.AgentSessionID))) != string(original) {
			t.Fatal("legacy session bytes changed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
