package session

import (
	"context"
	"errors"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodels"
	"testing"
)

type modelTestRuntime struct {
	multimodalTestRuntime
	switches int
}

func (*modelTestRuntime) ChatModelsEnabled() bool       { return true }
func (*modelTestRuntime) ModelAvailable(id string) bool { _, ok := codex.ModelProfile(id); return ok }
func (r *modelTestRuntime) SwitchThread(ctx context.Context, id string) (codex.ThreadInfo, error) {
	r.switches++
	p, _ := codex.ModelProfile(codex.ModelProfileID(ctx))
	return codex.ThreadInfo{ID: id, Model: p.Model, ModelProvider: p.Provider, ReasoningEffort: p.Effort}, nil
}
func modelService(t *testing.T) (*Service, *modelTestRuntime) {
	t.Helper()
	store, e := OpenStore(t.TempDir(), WithChatModelStorage())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = store.Close() })
	reserveBoundSession(t, store)
	rt := &modelTestRuntime{}
	return NewService(rt, store, NewEventHub(16, 8), nil, WithNativeReasoning()), rt
}
func TestChatModelSelectionRevisionReplayAndLegacyProtection(t *testing.T) {
	s, rt := modelService(t)
	ctx := context.Background()
	initial, e := s.ReadChatModel(testSessionID)
	if e != nil || initial.Revision != 0 || initial.ProfileId == nil || *initial.ProfileId != wire.ProfileIdMinimaxM3HighV1 {
		t.Fatal(initial, e)
	}
	request := wire.SelectRequest{SchemaVersion: 1, OperationId: testTurnOperationID, ExpectedRevision: 0, ProfileId: wire.ProfileIdKimiK3MaxV1}
	chosen, e := s.SelectChatModel(ctx, testSessionID, request)
	if e != nil || chosen.Revision != 1 || chosen.State != wire.SelectionStateReady {
		t.Fatal(chosen, e)
	}
	replay, e := s.SelectChatModel(ctx, testSessionID, request)
	if e != nil || replay.Revision != 1 || rt.switches != 1 {
		t.Fatal(replay, e, rt.switches)
	}
	request.OperationId = "00000000-0000-4000-8000-000000000156"
	request.ProfileId = wire.ProfileIdMinimaxM3HighV1
	if _, e = s.SelectChatModel(ctx, testSessionID, request); !errors.Is(e, ErrModelRevision) {
		t.Fatal(e)
	}
	record, _ := s.store.Get(testSessionID)
	if e = s.checkModelRequest(ctx, record); !errors.Is(e, ErrModelUnavailable) {
		t.Fatal("legacy write accepted", e)
	}
}
func TestChatModelAcceptedTurnKeepsOriginalSnapshotAfterSelectionChange(t *testing.T) {
	s, rt := modelService(t)
	calls := 0
	rt.startV2 = func(_ string, _ []codex.UserInput, effort string) (codex.TurnInfo, error) {
		calls++
		if effort != "max" {
			t.Fatal(effort)
		}
		return codex.TurnInfo{ID: testTurnID}, nil
	}
	_, e := s.SelectChatModel(context.Background(), testSessionID, wire.SelectRequest{SchemaVersion: 1, OperationId: "00000000-0000-4000-8000-000000000156", ProfileId: wire.ProfileIdKimiK3MaxV1})
	if e != nil {
		t.Fatal(e)
	}
	ctx := codex.WithModelProfile(context.Background(), "kimi-k3-max-v1")
	input := StartTurnV2Input{AgentSessionID: testSessionID, OperationID: testTurnOperationID, ContentBlocks: []TurnContentBlock{{Type: ContentBlockText, Text: "normal text"}}}
	if _, e = s.StartTurnV2(ctx, input); e != nil {
		t.Fatal(e)
	}
	op, e := s.store.TurnOperation(testSessionID, testTurnOperationID)
	if e != nil || op.ModelProfile != "kimi-k3-max-v1" {
		t.Fatal(op, e)
	}
	// A normal terminal notification releases the turn; no crash/fault injection.
	s.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{"threadId": testThreadID, "turn": map[string]any{"id": testTurnID, "status": "completed"}}))
	_, e = s.SelectChatModel(context.Background(), testSessionID, wire.SelectRequest{SchemaVersion: 1, OperationId: "00000000-0000-4000-8000-000000000157", ExpectedRevision: 1, ProfileId: wire.ProfileIdMinimaxM3HighV1})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.StartTurnV2(ctx, input); e != nil || calls != 1 {
		t.Fatal(e, calls)
	}
	if _, e = s.StartTurnV2(codex.WithModelProfile(context.Background(), "minimax-m3-high-v1"), input); !errors.Is(e, ErrTurnOperationConflict) {
		t.Fatal(e)
	}
}

func TestChatModelStorePreservesDraftPurposeInVersionSeven(t *testing.T) {
	store, err := OpenStore(t.TempDir(), WithChatModelStorage())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	record := Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir(),
		Purpose: purposeDraft, DraftWorkspaceID: "00000000-0000-4000-8000-000000000156", DraftSchemaVersion: 1, DraftPolicyVersion: 1,
		ModelProfile: "kimi-k3-max-v1", ModelRevision: 1, ModelSelectionState: "ready"}
	if err := store.Reserve(record); err != nil {
		t.Fatal("model-aware draft reservation rejected", err)
	}
	bound, err := store.BindThread(testSessionID, testThreadID, "", "kimi-k3", "kimi")
	if err != nil || bound.Purpose != purposeDraft || bound.ModelProfile != "kimi-k3-max-v1" {
		t.Fatal("model-aware draft binding differs", err)
	}
	loaded, err := store.Get(testSessionID)
	if err != nil || loaded.DraftWorkspaceID != record.DraftWorkspaceID || loaded.DraftPolicyVersion != 1 {
		t.Fatal("draft purpose lost", err)
	}
}
