package session

import (
	"context"
	"testing"
	"time"

	media "github.com/36Dge/yijie-agent-host/internal/contracts/mediageneration"
	"github.com/36Dge/yijie-agent-host/internal/imagegen"
)

const nextImageTurnID = "019fbd88-cbc3-7bf1-934d-7b05cd693f99"

func prepareNextImageTurn(t *testing.T, service *Service) {
	t.Helper()
	service.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turn": map[string]any{"id": testTurnID, "status": "completed"},
	}))
	record, err := service.store.PrepareTurn(testSessionID, TraceContext{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.beginImageTurn(context.Background(), record, nil); err != nil {
		t.Fatal(err)
	}
}

func bindNextImageTurn(t *testing.T, service *Service) {
	t.Helper()
	if _, err := service.store.BindTurn(testSessionID, nextImageTurnID); err != nil {
		t.Fatal(err)
	}
	if err := service.bindImageTurn(testThreadID, nextImageTurnID); err != nil {
		t.Fatal(err)
	}
}

func nextImageRequest() media.GenerateRequest {
	request := providerMediaCall(map[string]any{"prompt": "A blue ceramic cup"})
	request.TurnId = nextImageTurnID
	return request
}

func TestMediaGenerationOldTerminalPreservesNextTurn(t *testing.T) {
	for _, bound := range []bool{false, true} {
		t.Run(map[bool]string{false: "starting", true: "active"}[bound], func(t *testing.T) {
			service, calls := mediaFixture(t, false)
			prepareNextImageTurn(t, service)
			if bound {
				bindNextImageTurn(t, service)
			}
			service.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{
				"threadId": testThreadID, "turn": map[string]any{"id": testTurnID, "status": "completed"},
			}))
			if !bound {
				bindNextImageTurn(t, service)
			}
			result := service.GenerateImage(context.Background(), nextImageRequest())
			if result.Code != media.GenerateCodeOk || *calls != 1 {
				t.Fatalf("old terminal cancelled the next image turn: %+v calls=%d", result, *calls)
			}
		})
	}
}

func TestMediaGenerationWaitsForNativeTurnBinding(t *testing.T) {
	service, calls := mediaFixture(t, false)
	prepareNextImageTurn(t, service)
	result := make(chan media.GenerateResult, 1)
	go func() { result <- service.GenerateImage(context.Background(), nextImageRequest()) }()
	select {
	case early := <-result:
		t.Fatalf("MCP arrived before native binding and was rejected: %+v", early)
	case <-time.After(30 * time.Millisecond):
	}
	bindNextImageTurn(t, service)
	select {
	case got := <-result:
		if got.Code != media.GenerateCodeOk || *calls != 1 {
			t.Fatalf("native binding did not admit the request: %+v calls=%d", got, *calls)
		}
	case <-time.After(time.Second):
		t.Fatal("native binding did not release the MCP request")
	}
}

func TestMediaGenerationDelayedInterruptPreservesNextTurn(t *testing.T) {
	service, calls := mediaFixture(t, false)
	prepareNextImageTurn(t, service)
	// The interrupt handler read the previous active turn, then native terminal
	// processing and the next submission ran before it reached cancellation.
	service.cancelImageTurn(testThreadID, testTurnID)
	bindNextImageTurn(t, service)
	if got := service.GenerateImage(context.Background(), nextImageRequest()); got.Code != media.GenerateCodeOk || *calls != 1 {
		t.Fatalf("delayed interrupt cancelled the next turn: %+v", got)
	}
}

func TestMediaGenerationInterruptBeforeImageBindingCancelsOwnedTurn(t *testing.T) {
	service, calls := mediaFixture(t, false)
	prepareNextImageTurn(t, service)
	if _, err := service.store.BindTurn(testSessionID, nextImageTurnID); err != nil {
		t.Fatal(err)
	}
	service.cancelImageTurn(testThreadID, nextImageTurnID)
	if err := service.bindImageTurn(testThreadID, nextImageTurnID); err != nil {
		t.Fatal(err)
	}
	if got := service.GenerateImage(context.Background(), nextImageRequest()); got.Code != media.GenerateCodeCancelled || *calls != 0 {
		t.Fatalf("owned interrupt did not cancel the turn: %+v", got)
	}
}

func TestMediaGenerationCompletedBeforeBindingLeavesNoPendingTurn(t *testing.T) {
	service, calls := mediaFixture(t, false)
	prepareNextImageTurn(t, service)
	service.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turn": map[string]any{"id": nextImageTurnID, "status": "completed"},
	}))
	bindNextImageTurn(t, service)
	if service.imageTurns[testThreadID] != nil {
		t.Fatal("late turn/start response retained an already completed image turn")
	}
	if service.GenerateImage(context.Background(), nextImageRequest()).Code != media.GenerateCodeCancelled || *calls != 0 {
		t.Fatal("completed turn dispatched an image")
	}
}

func TestMediaGenerationBindingWaitDoesNotReserveOrDispatchEarly(t *testing.T) {
	for _, action := range []string{"caller cancelled", "turn cancelled", "other turn", "timeout"} {
		t.Run(action, func(t *testing.T) {
			service, calls := mediaFixture(t, false)
			prepareNextImageTurn(t, service)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan media.GenerateResult, 1)
			go func() { finished <- service.GenerateImage(ctx, nextImageRequest()) }()
			select {
			case got := <-finished:
				t.Fatalf("request did not wait for native binding: %+v", got)
			case <-time.After(30 * time.Millisecond):
			}
			want := media.GenerateCodeCancelled
			switch action {
			case "caller cancelled":
				cancel()
			case "turn cancelled":
				service.clearImageTurn(testThreadID)
			case "other turn":
				other := "019fbd88-cbc3-7bf1-934d-7b05cd693f98"
				if _, err := service.store.BindTurn(testSessionID, other); err != nil {
					t.Fatal(err)
				}
				if err := service.bindImageTurn(testThreadID, other); err != nil {
					t.Fatal(err)
				}
			case "timeout":
				want = media.GenerateCodeStatePending
			}
			select {
			case got := <-finished:
				if got.Code != want || *calls != 0 {
					t.Fatalf("unexpected binding outcome: %+v calls=%d", got, *calls)
				}
			case <-time.After(time.Duration(media.BindingWaitMilliseconds)*time.Millisecond + time.Second):
				t.Fatal("binding wait was not bounded")
			}
			if action == "timeout" || action == "caller cancelled" {
				bindNextImageTurn(t, service)
				if got := service.GenerateImage(context.Background(), nextImageRequest()); got.Code != media.GenerateCodeOk || *calls != 1 {
					t.Fatalf("unreserved request could not retry after native binding: %+v", got)
				}
			}
		})
	}
}

func TestMediaGenerationPublishedReceiptSurvivesLaterStop(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "interrupt", true: "terminal"}[remove], func(t *testing.T) {
			service, calls := mediaFixture(t, false)
			turn := service.imageTurns[testThreadID]
			record, err := service.store.Get(testSessionID)
			if err != nil {
				t.Fatal(err)
			}
			result := service.executeBuiltinImage(context.Background(), turn.context, record, testTurnID,
				imagegen.Request{Prompt: "A blue ceramic cup", Mode: imagegen.ModeTextToImage}, turn)
			if result.Code != media.GenerateCodeOk || result.ArtifactId == nil {
				t.Fatal(result)
			}
			if remove {
				service.clearImageTurnForID(testThreadID, testTurnID)
			} else {
				service.cancelImageTurn(testThreadID, testTurnID)
			}
			got := service.completeImageCall(testThreadID, turn, result)
			if got.Code != media.GenerateCodeOk || got.Status != media.GenerateStatusStaged || got.ArtifactId != result.ArtifactId || *calls != 1 {
				t.Fatalf("later stop rewrote a published receipt: %+v", got)
			}
			_, events, _, cancel, err := service.SubscribeEventsV3(testSessionID, "", 0)
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			if len(events) != 2 || events[1].EventType != EventItemArtifactCompleted {
				t.Fatal("later stop added a conflicting artifact terminal")
			}
		})
	}
}
