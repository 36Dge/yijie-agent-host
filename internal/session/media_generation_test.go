package session

import (
	"context"
	document "github.com/36Dge/yijie-agent-host/internal/contracts/documentruntime"
	"testing"
	"time"

	media "github.com/36Dge/yijie-agent-host/internal/contracts/mediageneration"
	"github.com/36Dge/yijie-agent-host/internal/imagegen"
	bolt "go.etcd.io/bbolt"
)

func mediaFixture(t *testing.T, expert bool) (*Service, *int) {
	t.Helper()
	count := 0
	image, err := syntheticPNG()
	if err != nil {
		t.Fatal(err)
	}
	service, _ := newProviderImageService(t, imageGeneratorFunc(func(context.Context, imagegen.Request) (imagegen.Result, error) {
		count++
		return imagegen.Result{Bytes: image, MediaType: "image/png", Width: 1, Height: 1}, nil
	}), nil)
	if expert {
		service.imageTurns[testThreadID].route = media.ExpertImageRoutes[0].Route
	}
	return service, &count
}

func TestMediaGenerationReusesOneArtifactForDuplicateCalls(t *testing.T) {
	service, calls := mediaFixture(t, false)
	request := providerMediaCall(map[string]any{"prompt": "A blue ceramic cup"})
	first := service.GenerateImage(context.Background(), request)
	second := service.GenerateImage(context.Background(), request)
	if first.Code != media.GenerateCodeOk || first.ArtifactId == nil || second.ArtifactId == nil || *first.ArtifactId != *second.ArtifactId || *calls != 1 {
		t.Fatalf("duplicate generation: %+v %+v calls=%d", first, second, *calls)
	}
	request.Input.Prompt = "A green cup"
	if service.GenerateImage(context.Background(), request).Code != media.GenerateCodeOperationConflict || *calls != 1 {
		t.Fatal("changed input created a second image")
	}
}

func TestMediaGenerationSelectedExpertNeverUsesBuiltin(t *testing.T) {
	for _, route := range []string{media.ExpertImageRoutes[0].Route, "expert-only"} {
		t.Run(route, func(t *testing.T) {
			service, calls := mediaFixture(t, true)
			service.imageTurns[testThreadID].route = route
			request := providerMediaCall(map[string]any{"prompt": "A studio lamp"})
			for _, reference := range []bool{false, true} {
				request.Input.UseCurrentReference = reference
				result := service.GenerateImage(context.Background(), request)
				if result.Code != media.GenerateCodeExpertRequired || *calls != 0 {
					t.Fatalf("selected expert changed provider: %+v, calls=%d", result, *calls)
				}
			}
		})
	}
}

func TestMediaGenerationStoredSelectionBlocksStaleBuiltinTool(t *testing.T) {
	service, calls := mediaFixture(t, false)
	// A normal stored expert binding remains authoritative even if old native
	// history still contains the ordinary conversation's builtin tool.
	err := service.store.db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(expertBindingsBucket); err != nil {
			return err
		}
		return putExpertBinding(tx, expertBinding{Version: 1, AgentSessionID: testSessionID, Revision: 1, Expert: feat158Ref()})
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		result := service.GenerateImage(context.Background(), providerMediaCall(map[string]any{"prompt": "A cup"}))
		if result.Code != media.GenerateCodeExpertRequired || *calls != 0 {
			t.Fatalf("stored selection bypassed: %+v", result)
		}
	}
	binding, err := service.store.expertBinding(testSessionID)
	if err != nil || binding.Expert == nil {
		t.Fatal("selected expert was removed")
	}
}

func TestMediaGenerationCancellationStopsBuiltin(t *testing.T) {
	service, calls := mediaFixture(t, false)
	service.cancelImageTurn(testThreadID, testTurnID)
	if service.GenerateImage(context.Background(), providerMediaCall(map[string]any{"prompt": "A cup"})).Code != media.GenerateCodeCancelled || *calls != 0 {
		t.Fatal("cancelled builtin admitted")
	}
}

func TestMediaGenerationLegacyCallsNeverDispatch(t *testing.T) {
	service, calls := mediaFixture(t, false)
	if service.HandleDynamicToolCall(context.Background(), providerImageCall(t, map[string]any{"prompt": "A cup", "mode": imagegen.ModeTextToImage})).Success || *calls != 0 {
		t.Fatal("retired producer dispatched")
	}
}

func TestMediaGenerationStatusUsesExistingACKWithoutDocuments(t *testing.T) {
	service, calls := mediaFixture(t, false)
	result := service.GenerateImage(context.Background(), providerMediaCall(map[string]any{"prompt": "A blue ceramic cup"}))
	if result.ArtifactId == nil || result.Status != media.GenerateStatusStaged {
		t.Fatal(result)
	}
	req := document.StatusRequest{ThreadId: testThreadID, TurnId: testTurnID, Input: document.StatusInput{ArtifactId: *result.ArtifactId}}
	status, err := service.DocumentArtifactStatus(context.Background(), req)
	if err != nil || status.Status != "staged" || service.documentEnvironment != nil {
		t.Fatal(status, err)
	}
	content, err := service.ReadArtifact(testSessionID, *result.ArtifactId, ArtifactContent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.AcknowledgeArtifact(testSessionID, *result.ArtifactId, ArtifactAcknowledgement{AckID: *result.ArtifactId, SizeBytes: int64(len(content.Bytes)), SHA256: content.SHA256, LocalCommittedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	status, err = service.DocumentArtifactStatus(context.Background(), req)
	if err != nil || status.Status != "committed" || *calls != 1 {
		t.Fatal(status, err)
	}
	repeated := service.GenerateImage(context.Background(), providerMediaCall(map[string]any{"prompt": "A blue ceramic cup"}))
	if repeated.Status != media.GenerateStatusCommitted || *calls != 1 {
		t.Fatal(repeated)
	}
}

func TestMediaGenerationTurnEndClosesArtifactBeforeProviderReturns(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	image, err := syntheticPNG()
	if err != nil {
		t.Fatal(err)
	}
	service, _ := newProviderImageService(t, imageGeneratorFunc(func(context.Context, imagegen.Request) (imagegen.Result, error) {
		close(entered)
		<-release
		return imagegen.Result{Bytes: image, MediaType: "image/png", Width: 1, Height: 1}, nil
	}), nil)
	finished := make(chan media.GenerateResult, 1)
	go func() {
		finished <- service.GenerateImage(context.Background(), providerMediaCall(map[string]any{"prompt": "A ceramic cup"}))
	}()
	<-entered
	// A normal native cancellation closes the Artifact even before an external
	// operation has returned. No process or credential fixture is altered.
	service.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{"threadId": testThreadID, "turn": map[string]any{"id": testTurnID, "status": "interrupted"}}))
	_, events, _, cancel, err := service.SubscribeEventsV3(testSessionID, "", 0)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	cancel()
	failed := 0
	for _, event := range events {
		if event.EventType == EventItemArtifactFailed {
			failed++
			if event.Payload.ErrorCode != "turn_interrupted" {
				t.Error("wrong cancellation result")
			}
		}
	}
	if failed != 1 {
		t.Error("pending Artifact did not close with its turn")
	}
	close(release)
	if result := <-finished; result.Code != media.GenerateCodeCancelled {
		t.Fatal(result)
	}
	_, events, _, cancel, err = service.SubscribeEventsV3(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	failed = 0
	for _, event := range events {
		if event.EventType == EventItemArtifactCompleted {
			t.Error("late provider result published")
		}
		if event.EventType == EventItemArtifactFailed {
			failed++
		}
	}
	if failed != 1 {
		t.Fatal("duplicate terminal", failed)
	}
}
