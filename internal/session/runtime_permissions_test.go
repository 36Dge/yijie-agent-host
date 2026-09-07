package session

import (
	"errors"
	"strings"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

func TestRuntimeApprovalReviewPreservesOrdinaryOperationAndTarget(t *testing.T) {
	approval := codex.RuntimeApproval{
		Summary: "printf 'sample' > /Users/demo/project/outside/approved.txt",
		Scope:   "/Users/demo/project/workspace",
		Reason:  "Write an ordinary sample file outside the workspace",
	}
	if got := safeRuntimeApproval(approval); got != approval {
		t.Fatalf("ordinary operation was hidden from its reviewer: %+v", got)
	}
}

func TestRuntimePermissionAdmissionPreservesAcceptedReplay(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", "MiniMax-M3", "minimax"); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	blocked := func(threadID string) error {
		if threadID != testThreadID {
			t.Fatalf("admission used a different thread: %s", threadID)
		}
		return ErrTurnActive
	}
	if _, _, err := store.PrepareTurnOperation(testSessionID, testTurnOperationID, digest, TraceContext{}, blocked); !errors.Is(err, ErrTurnActive) {
		t.Fatalf("pending approval did not prevent reservation: %v", err)
	}
	// Once the approval is resolved, the same operation can be admitted. The
	// denied attempt must not have reserved an operation or changed the task.
	if _, _, err := store.PrepareTurnOperation(testSessionID, testTurnOperationID, digest, TraceContext{}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcceptTurnOperation(testSessionID, testTurnOperationID, digest, testTurnID); err != nil {
		t.Fatal(err)
	}
	// A normal retry after acceptance returns the original turn, even while
	// that turn is waiting for its own approval. It never starts another turn.
	replayed, _, err := store.PrepareTurnOperation(testSessionID, testTurnOperationID, digest, TraceContext{}, blocked)
	if err != nil || replayed.State != TurnOperationStateAccepted || replayed.TurnID != testTurnID {
		t.Fatalf("accepted replay was lost while approval pending: %+v, %v", replayed, err)
	}
}
