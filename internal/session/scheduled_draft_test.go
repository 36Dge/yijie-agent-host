package session

import (
	"context"
	"errors"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/scheduledraft"
	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type draftTestRuntime struct {
	multimodalTestRuntime
	ready bool
}

func (r *draftTestRuntime) ScheduledDraftReady() bool { return r.ready }
func TestFEAT155DraftStoreForwardReaderAndImmutablePurpose(t *testing.T) {
	// Native candidate options must not combine with the older isolated Store profile.
	if _, err := OpenStore(t.TempDir(), WithScheduledDraftStorage(), func(s *Store) error { s.feat126ProjectDirectory = "declared-other-profile"; return nil }); !errors.Is(err, ErrDraftStorageDisabled) {
		t.Fatal(err)
	}
	home, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	s, e := OpenStore(home)
	if e != nil {
		t.Fatal(e)
	}
	ordinary := Record{TaskID: uuid.NewString(), AgentSessionID: uuid.NewString(), Cwd: home}
	if e = s.Reserve(ordinary); e != nil {
		t.Fatal(e)
	}
	version := func(want string) {
		t.Helper()
		e := s.db.View(func(tx *bolt.Tx) error {
			if string(tx.Bucket(metadataBucket).Get(storeSchemaVersionKey)) != want {
				t.Fatal("version")
			}
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
	}
	version("5")
	s.Close()
	s, e = OpenStore(home, WithScheduledDraftStorage())
	if e != nil {
		t.Fatal(e)
	}
	version("6")
	old, e := s.Get(ordinary.AgentSessionID)
	if e != nil || old.Purpose != purposeOrdinary {
		t.Fatalf("legacy: %+v %v", old, e)
	}
	draft := Record{TaskID: uuid.NewString(), AgentSessionID: uuid.NewString(), Cwd: home, Purpose: purposeDraft, DraftWorkspaceID: uuid.NewString(), DraftPolicyVersion: 1, DraftSchemaVersion: 1}
	if e = s.Reserve(draft); e != nil {
		t.Fatal(e)
	}
	e = s.db.Update(func(tx *bolt.Tx) error {
		r, err := s.loadRecord(tx, draft.AgentSessionID)
		if err != nil {
			return err
		}
		r.Purpose = purposeOrdinary
		r.DraftWorkspaceID = ""
		r.DraftPolicyVersion = 0
		r.DraftSchemaVersion = 0
		return s.saveRecord(tx, r)
	})
	if !errors.Is(e, ErrDraftPurpose) {
		t.Fatalf("purpose mutation: %v", e)
	}
	s.Close()
	s, e = OpenStore(home)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	version("6")
	got, e := s.Get(draft.AgentSessionID)
	if e != nil || got.DraftWorkspaceID != draft.DraftWorkspaceID {
		t.Fatal(e)
	}
	service := NewService(nil, s, NewEventHub(8, 8), nil)
	if service.ScheduledDraftCapability().Available {
		t.Fatal("reader gained producer")
	}
	if _, e = service.ResumeSession(context.Background(), draft.AgentSessionID, TraceContext{}); !errors.Is(e, ErrDraftPurpose) {
		t.Fatal(e)
	}
	for _, purpose := range []string{"", "future-purpose"} {
		v := draft
		v.Purpose = purpose
		e = s.db.View(func(tx *bolt.Tx) error { return validateStoredPurpose(tx, &v) })
		if !errors.Is(e, ErrDraftPurpose) {
			t.Fatalf("unknown format accepted: %v", e)
		}
	}
}
func TestFEAT155DraftServicePurposeReplayAndUnknown(t *testing.T) {
	home, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	s, e := OpenStore(home, WithScheduledDraftStorage())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	workspace, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	starts := 0
	thread := uuid.NewString()
	turn := uuid.NewString()
	runtime := &draftTestRuntime{ready: true}
	runtime.startThread = func(cwd string) (codex.ThreadInfo, error) {
		starts++
		if cwd != workspace {
			t.Fatal("cwd")
		}
		return codex.ThreadInfo{ID: thread, Model: codex.MiniMaxModel, ModelProvider: codex.MiniMaxProviderID}, nil
	}
	runtime.resume = func(id string) (codex.ThreadInfo, error) {
		return codex.ThreadInfo{ID: id, Model: codex.MiniMaxModel, ModelProvider: codex.MiniMaxProviderID}, nil
	}
	runtime.startV2 = func(id string, inputs []codex.UserInput, effort string) (codex.TurnInfo, error) {
		calls++
		if len(inputs) != 1 {
			t.Fatal("not text only")
		}
		return codex.TurnInfo{ID: turn, Status: "inProgress"}, nil
	}
	service := NewService(runtime, s, NewEventHub(8, 8), nil, WithScheduledDraftDirectory(func(string) (string, error) { return workspace, nil }))
	input := wire.CreateRequest{SchemaVersion: 1, PolicyVersion: 1, TaskId: uuid.NewString(), WorkspaceId: uuid.NewString()}
	receipt, e := service.CreateScheduledDraft(context.Background(), input)
	if e != nil {
		t.Fatal(e)
	}
	if e = wire.Validate("SessionReceipt", receipt); e != nil {
		t.Fatal(e)
	}
	replay, e := service.CreateScheduledDraft(context.Background(), input)
	if e != nil || receipt != replay || starts != 1 {
		t.Fatalf("create replay %v", e)
	}
	wrong := input
	wrong.WorkspaceId = uuid.NewString()
	if _, e = service.CreateScheduledDraft(context.Background(), wrong); !errors.Is(e, ErrTurnOperationConflict) {
		t.Fatal(e)
	}
	if _, e = service.ResumeSession(context.Background(), receipt.AgentSessionId, TraceContext{}); !errors.Is(e, ErrDraftPurpose) {
		t.Fatal(e)
	}
	if _, e = service.StartTurn(context.Background(), StartTurnInput{AgentSessionID: receipt.AgentSessionId, Input: "ordinary"}); !errors.Is(e, ErrDraftPurpose) {
		t.Fatal(e)
	}
	for _, mode := range []codex.PermissionMode{"", "ask", "auto", "full"} {
		_, e = service.StartTurnV2(context.Background(), StartTurnV2Input{AgentSessionID: receipt.AgentSessionId, OperationID: uuid.NewString(), PermissionMode: mode, ContentBlocks: []TurnContentBlock{{Type: ContentBlockText, Text: "ordinary"}}})
		if !errors.Is(e, ErrDraftPurpose) {
			t.Fatal(e)
		}
	}
	q := wire.TurnRequest{SchemaVersion: 1, PolicyVersion: 1, OperationId: uuid.NewString(), Text: "每天九点总结"}
	first, e := service.StartScheduledDraftTurn(context.Background(), receipt.AgentSessionId, q)
	if e != nil {
		t.Fatal(e)
	}
	second, e := service.StartScheduledDraftTurn(context.Background(), receipt.AgentSessionId, q)
	if e != nil || first != second || calls != 1 {
		t.Fatal("duplicate", e)
	}
	q.Text = "changed"
	if _, e = service.StartScheduledDraftTurn(context.Background(), receipt.AgentSessionId, q); !errors.Is(e, ErrTurnOperationConflict) {
		t.Fatal(e)
	}
	if _, e = s.CompleteTurn(receipt.AgentSessionId, turn, "completed"); e != nil {
		t.Fatal(e)
	}
	if _, e = service.ResumeScheduledDraft(context.Background(), receipt.AgentSessionId, wire.ResumeRequest{SchemaVersion: 1, PolicyVersion: 1}); e != nil {
		t.Fatal(e)
	}
	unknown := uuid.NewString()
	if _, _, e = s.PrepareTurnOperation(receipt.AgentSessionId, unknown, strings.Repeat("a", 64), TraceContext{}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.MarkTurnOperationUncertain(receipt.AgentSessionId, unknown, strings.Repeat("a", 64), "runtime_request_failed"); e != nil {
		t.Fatal(e)
	}
	runtime.ready = false
	if _, e = service.StartScheduledDraftTurn(context.Background(), receipt.AgentSessionId, wire.TurnRequest{SchemaVersion: 1, PolicyVersion: 1, OperationId: unknown, Text: "again"}); !errors.Is(e, ErrDraftPolicy) {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal("unknown replay sent")
	}
}

func TestFEAT155DraftNativeWorkspaceRootIsExistingEmptyAndCanonical(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	id := uuid.NewString()
	dir := filepath.Join(root, id)
	if e = os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	s := &Service{}
	WithScheduledDraftWorkspaceRoot(root)(s)
	got, e := s.resolveDraftCwd(id)
	if e != nil || got != dir {
		t.Fatal(got, e)
	}
	if _, e = s.resolveDraftCwd(uuid.NewString()); !errors.Is(e, ErrDraftPolicy) {
		t.Fatal(e)
	}
	// A normal file in the candidate directory makes it unsuitable; no permission
	// fault, path substitution or malicious resource is constructed.
	if e = os.WriteFile(filepath.Join(dir, "ordinary-note.txt"), []byte("ordinary note"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = s.resolveDraftCwd(id); !errors.Is(e, ErrDraftPolicy) {
		t.Fatal(e)
	}
}
