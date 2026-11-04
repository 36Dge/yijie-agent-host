package session

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	agenthostcontract "github.com/36Dge/yijie-agent-host/internal/contracts"
)

type ordinarySelectedSkillResources struct {
	calls       int
	unavailable bool
}

func (r *ordinarySelectedSkillResources) ResolveSelectedSkills(_ context.Context, refs []agenthostcontract.SelectedSkillRef) ([]codex.UserInput, error) {
	r.calls++
	if r.unavailable || len(refs) != 1 || refs[0].Id != "ordinary-sample" || refs[0].Version != "1.0.0" {
		return nil, errors.New("ordinary selection unavailable")
	}
	input, err := codex.SkillUserInput("same-name", "/ordinary/managed/sample/SKILL.md")
	return []codex.UserInput{input}, err
}

func TestSelectedSkillAcceptedReplayDoesNotResolveOrDispatchAgain(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reserveBoundSession(t, store)
	resources := &ordinarySelectedSkillResources{}
	calls := 0
	runtime := &multimodalTestRuntime{startV2: func(_ string, inputs []codex.UserInput, _ string) (codex.TurnInfo, error) {
		calls++
		if len(inputs) != 2 {
			t.Fatalf("expected original text plus one native Skill, got %d", len(inputs))
		}
		return codex.TurnInfo{ID: testTurnID, Status: "inProgress"}, nil
	}}
	service := NewService(runtime, store, NewEventHub(16, 8), slog.New(slog.NewTextHandler(io.Discard, nil)), WithSelectedSkillResources(resources))
	input := StartTurnV2Input{AgentSessionID: testSessionID, OperationID: testTurnOperationID,
		ContentBlocks: []TurnContentBlock{{Type: ContentBlockText, Text: "$same-name\n\nExplain six times seven.", Skills: []agenthostcontract.SelectedSkillRef{{Id: "ordinary-sample", Version: "1.0.0"}}}}}
	if _, err := service.StartTurnV2(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	resources.unavailable = true
	replayed, err := service.StartTurnV2(context.Background(), input)
	if err != nil || replayed.ID != testTurnID || calls != 1 || resources.calls != 1 {
		t.Fatalf("accepted replay changed after resource removal: error=%v calls=%d resolves=%d", err, calls, resources.calls)
	}
	changed := input
	changed.ContentBlocks = []TurnContentBlock{{Type: ContentBlockText, Text: input.ContentBlocks[0].Text, Skills: []agenthostcontract.SelectedSkillRef{{Id: "ordinary-sample", Version: "2.0.0"}}}}
	if _, err := service.StartTurnV2(context.Background(), changed); !errors.Is(err, ErrTurnOperationConflict) || calls != 1 {
		t.Fatalf("changed frozen Skill version was not rejected: %v", err)
	}
}
