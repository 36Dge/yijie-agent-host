package session

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/google/uuid"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestSyntheticArtifactsPublishCanonicalV3LifecycleAndResources(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
		t.Fatal(err)
	}
	spool, err := artifact.OpenStore(filepath.Join(home, "artifact-spool"), artifact.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = spool.Close() })
	v3 := NewEventHubVersion(EventSchemaVersionV3, 64, 16)
	service := NewService(&fakeRuntime{}, store, NewEventHub(16, 8), nil, WithV3Artifacts(v3, spool, true))
	if err := service.publishSyntheticArtifacts(testSessionID, testTurnID); err != nil {
		t.Fatal(err)
	}
	_, replay, _, cancel, err := service.SubscribeEventsV3(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != 12 {
		t.Fatalf("expected four three-event lifecycles, got %d", len(replay))
	}
	contract := compileAgentSessionEventV3Contract(t)
	seenKinds := make(map[string]bool)
	for index, event := range replay {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		if err := contract.Validate(instance); err != nil {
			t.Fatalf("v3 event %d violates contract: %v\n%s", index, err, encoded)
		}
		if event.SchemaVersion != 3 || event.Terminal {
			t.Fatalf("unexpected v3 lifecycle identity: %+v", event)
		}
		seenKinds[event.Payload.Kind] = true
		if event.EventType != EventItemArtifactCompleted {
			continue
		}
		resource, err := service.ReadArtifact(testSessionID, event.Payload.ArtifactID, ArtifactContent)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(resource.Bytes)
		if event.Payload.SizeBytes == nil || *event.Payload.SizeBytes != int64(len(resource.Bytes)) || event.Payload.SHA256 != bytesToHex(digest[:]) {
			t.Fatalf("completed manifest mismatch: %+v resource=%+v", event.Payload, resource)
		}
		if event.Payload.Kind == "video" {
			if _, err := service.ReadArtifact(testSessionID, event.Payload.ArtifactID, ArtifactPoster); err != nil {
				t.Fatalf("video poster unavailable: %v", err)
			}
		}
		if event.Payload.Kind == "report" {
			instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(resource.Bytes))
			if err != nil {
				t.Fatal(err)
			}
			if err := compileReportDocumentV1Contract(t).Validate(instance); err != nil {
				t.Fatalf("synthetic report violates ReportDocumentV1: %v\n%s", err, resource.Bytes)
			}
		}
	}
	for _, kind := range []string{"image", "video", "file", "report"} {
		if !seenKinds[kind] {
			t.Fatalf("synthetic lifecycle omitted %s", kind)
		}
	}
	if _, err := service.ReadArtifact("019fbd88-cbc3-7bf1-934d-7b05cd693f54", replay[2].Payload.ArtifactID, ArtifactContent); !errors.Is(err, artifact.ErrNotFound) {
		t.Fatalf("cross-session read leaked resource: %v", err)
	}
}

func TestArtifactAcknowledgementDelegatesToEncryptedStore(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
		t.Fatal(err)
	}
	spool, err := artifact.OpenStore(filepath.Join(home, "artifact-spool"), artifact.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = spool.Close() })
	service := NewService(&fakeRuntime{}, store, NewEventHub(8, 4), nil,
		WithV3Artifacts(NewEventHubVersion(EventSchemaVersionV3, 32, 8), spool, true))
	if err := service.publishSyntheticArtifacts(testSessionID, testTurnID); err != nil {
		t.Fatal(err)
	}
	_, replay, _, cancel, err := service.SubscribeEventsV3(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	completed := replay[2]
	receipt, err := service.AcknowledgeArtifact(testSessionID, completed.Payload.ArtifactID, ArtifactAcknowledgement{
		AckID: uuid.NewString(), SizeBytes: *completed.Payload.SizeBytes, SHA256: completed.Payload.SHA256,
		LocalCommittedAt: time.Now().UTC(),
	})
	if err != nil || receipt.CleanupStatus != "completed" {
		t.Fatalf("acknowledgement mismatch: %+v err=%v", receipt, err)
	}
	if _, err := service.ReadArtifact(testSessionID, completed.Payload.ArtifactID, ArtifactContent); !errors.Is(err, artifact.ErrExpired) {
		t.Fatalf("acknowledged content remained readable: %v", err)
	}
}

func TestV3PreservesV2ReasoningAndLifecycleVariants(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
		t.Fatal(err)
	}
	v3 := NewEventHubVersion(EventSchemaVersionV3, 32, 8)
	service := NewService(&fakeRuntime{}, store, NewEventHub(16, 8), nil, WithV3Artifacts(v3, nil, false))
	service.HandleNotification(RuntimeNotificationReasoningTextDelta, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID, "itemId": "reasoning-v3", "contentIndex": 0, "delta": "safe reasoning",
	}))
	service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{"id": "reasoning-v3", "type": "reasoning", "content": []string{"safe reasoning"}},
	}))
	_, replay, _, cancel, err := service.SubscribeEventsV3(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != 3 || replay[0].EventType != EventItemReasoningTextDelta || replay[1].EventType != EventItemReasoningFinalized || replay[2].EventType != EventItemCompleted {
		t.Fatalf("v3 did not preserve v2 reasoning lifecycle: %+v", replay)
	}
	contract := compileAgentSessionEventV3Contract(t)
	for _, event := range replay {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		if err := contract.Validate(instance); err != nil {
			t.Fatalf("preserved v3 event violates contract: %v\n%s", err, encoded)
		}
	}
}

func compileAgentSessionEventV3Contract(t *testing.T) *jsonschema.Schema {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate Agent session v3 contract test source")
	}
	contract, err := jsonschema.NewCompiler().Compile(filepath.Clean(filepath.Join(
		filepath.Dir(sourceFile), "..", "..", "api", "jsonschema", "agent-session-event-v3.schema.json",
	)))
	if err != nil {
		t.Fatalf("compile AgentSessionEventV3 JSON Schema: %v", err)
	}
	return contract
}

func compileReportDocumentV1Contract(t *testing.T) *jsonschema.Schema {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate report v1 contract test source")
	}
	contract, err := jsonschema.NewCompiler().Compile(filepath.Clean(filepath.Join(
		filepath.Dir(sourceFile), "..", "..", "api", "jsonschema", "report-document-v1.schema.json",
	)))
	if err != nil {
		t.Fatalf("compile ReportDocumentV1 JSON Schema: %v", err)
	}
	return contract
}

func bytesToHex(value []byte) string {
	const digits = "0123456789abcdef"
	encoded := make([]byte, len(value)*2)
	for index, item := range value {
		encoded[index*2] = digits[item>>4]
		encoded[index*2+1] = digits[item&0x0f]
	}
	return string(encoded)
}
