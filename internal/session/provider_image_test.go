package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/imagegen"
)

type imageGeneratorFunc func(context.Context, imagegen.Request) (imagegen.Result, error)

func (function imageGeneratorFunc) Generate(ctx context.Context, request imagegen.Request) (imagegen.Result, error) {
	return function(ctx, request)
}

func TestProviderImagePublishesTextAndSingleCharacterReferenceArtifacts(t *testing.T) {
	pngBytes, err := syntheticPNG()
	if err != nil {
		t.Fatal(err)
	}
	reference := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
	tests := []struct {
		name       string
		arguments  map[string]any
		references []imageReference
		wantRef    string
	}{
		{name: "text to image", arguments: map[string]any{"prompt": "a clean poster", "mode": imagegen.ModeTextToImage, "aspect_ratio": "16:9"}},
		{name: "single character reference", arguments: map[string]any{"prompt": "the same character in a park", "mode": imagegen.ModeSubject}, references: []imageReference{{mediaType: "image/png", dataURL: reference}}, wantRef: reference},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var request imagegen.Request
			service, spool := newProviderImageService(t, imageGeneratorFunc(func(_ context.Context, received imagegen.Request) (imagegen.Result, error) {
				request = received
				return imagegen.Result{Bytes: pngBytes, MediaType: "image/png", Width: 1, Height: 1}, nil
			}), test.references)
			result := service.HandleDynamicToolCall(context.Background(), providerImageCall(t, test.arguments))
			if !result.Success || request.ReferenceDataURL != test.wantRef {
				t.Fatalf("unexpected provider result=%+v request=%+v", result, request)
			}
			_, replay, _, cancel, err := service.SubscribeEventsV3(testSessionID, "", 0)
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			if len(replay) != 3 || replay[0].EventType != EventItemArtifactStarted ||
				replay[1].EventType != EventItemArtifactProgress || replay[2].EventType != EventItemArtifactCompleted ||
				replay[2].Payload.Provenance != "provider" || replay[2].Payload.MediaType != "image/png" {
				t.Fatalf("unexpected provider artifact lifecycle: %+v", replay)
			}
			resource, err := spool.Get(testSessionID, replay[2].Payload.ArtifactID, artifact.Content)
			if err != nil || !bytes.Equal(resource.Bytes, pngBytes) {
				t.Fatalf("provider artifact was not staged: %+v err=%v", resource, err)
			}
		})
	}
}

func TestProviderImageAllowsOnlyOneConcurrentPaidCallAndInterruptCancelsIt(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	var calls int
	var callsMu sync.Mutex
	service, _ := newProviderImageService(t, imageGeneratorFunc(func(ctx context.Context, _ imagegen.Request) (imagegen.Result, error) {
		callsMu.Lock()
		calls++
		callsMu.Unlock()
		once.Do(func() { close(entered) })
		<-ctx.Done()
		return imagegen.Result{}, &imagegen.ProviderError{Code: imagegen.ErrorRequestCanceled}
	}), nil)
	call := providerImageCall(t, map[string]any{"prompt": "one image", "mode": imagegen.ModeTextToImage})
	first := make(chan codex.DynamicToolResult, 1)
	go func() { first <- service.HandleDynamicToolCall(context.Background(), call) }()
	<-entered
	duplicate := service.HandleDynamicToolCall(context.Background(), call)
	if duplicate.Success || !strings.Contains(duplicate.Text, "protocol_error") {
		t.Fatalf("duplicate provider call was not rejected: %+v", duplicate)
	}
	if err := service.InterruptTurn(context.Background(), testSessionID, testTurnID, TraceContext{}); err != nil {
		t.Fatal(err)
	}
	result := <-first
	if result.Success || !strings.Contains(result.Text, "turn_interrupted") {
		t.Fatalf("interrupted provider call returned %+v", result)
	}
	callsMu.Lock()
	defer callsMu.Unlock()
	if calls != 1 {
		t.Fatalf("provider called %d times, want one", calls)
	}
}

func TestProviderImageFailureIsContentFreeAndPublishesStableFailure(t *testing.T) {
	service, _ := newProviderImageService(t, imageGeneratorFunc(func(context.Context, imagegen.Request) (imagegen.Result, error) {
		return imagegen.Result{}, errors.New("PROVIDER-SECRET-CANARY")
	}), nil)
	result := service.HandleDynamicToolCall(context.Background(), providerImageCall(t, map[string]any{
		"prompt": "safe prompt", "mode": imagegen.ModeTextToImage,
	}))
	if result.Success || strings.Contains(result.Text, "CANARY") {
		t.Fatalf("raw provider failure escaped: %+v", result)
	}
	_, replay, _, cancel, err := service.SubscribeEventsV3(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != 3 || replay[2].EventType != EventItemArtifactFailed ||
		replay[2].Payload.ErrorCode != "generation_failed" || replay[2].Payload.Message == nil ||
		strings.Contains(*replay[2].Payload.Message, "CANARY") {
		t.Fatalf("unstable provider failure lifecycle: %+v", replay)
	}
}

func newProviderImageService(t *testing.T, generator imagegen.Generator, references []imageReference) (*Service, *artifact.Store) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	record, err := store.BindThread(testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID)
	if err != nil {
		t.Fatal(err)
	}
	record, err = store.PrepareTurn(testSessionID, TraceContext{})
	if err != nil {
		t.Fatal(err)
	}
	spool, err := artifact.OpenStore(filepath.Join(home, "artifact-spool"), artifact.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = spool.Close() })
	service := NewService(&fakeRuntime{}, store, NewEventHub(16, 8), nil,
		WithV3Artifacts(NewEventHubVersion(EventSchemaVersionV3, 32, 8), spool, false),
		WithImageGenerator(generator),
	)
	if err := service.beginImageTurn(record, references); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindTurn(testSessionID, testTurnID); err != nil {
		t.Fatal(err)
	}
	if err := service.bindImageTurn(testThreadID, testTurnID); err != nil {
		t.Fatal(err)
	}
	return service, spool
}

func providerImageCall(t *testing.T, arguments map[string]any) codex.DynamicToolCall {
	t.Helper()
	raw, err := json.Marshal(arguments)
	if err != nil {
		t.Fatal(err)
	}
	return codex.DynamicToolCall{
		ThreadID: testThreadID, TurnID: testTurnID, CallID: "provider-call-1",
		Tool: codex.DynamicToolGenerateImage, Arguments: raw,
	}
}
