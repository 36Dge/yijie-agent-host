package session

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/artifactmedia"
)

// Real Store/Spool/EventHub, ordinary source files only. Runtime generation and
// App/native playback are separate acceptance steps, not simulated by this test.
func TestFEAT165NormalFilesRegistrationACKAndLegacyIsolation(t *testing.T) {
	workspace := os.Getenv("YIJIE_FEAT165_NORMAL_FILES")
	if workspace == "" {
		t.Skip("normal fixture directory not provided")
	}
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: workspace}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.BindThread(testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.BindTurn(testSessionID, testTurnID); err != nil {
		t.Fatal(err)
	}
	spool, err := artifact.OpenStore(filepath.Join(home, "artifact-spool"), artifact.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	service := NewService(&fakeRuntime{}, store, NewEventHub(16, 8), nil, WithV3Artifacts(NewEventHubVersion(3, 64, 16), spool, false), WithV4Events(NewEventHubVersion(4, 64, 64)), WithArtifactMedia())
	request := func(name string) wire.PublishRequest {
		return wire.PublishRequest{ThreadId: testThreadID, TurnId: testTurnID, Input: wire.PublishInput{RelativePath: name, DisplayName: name}}
	}
	if _, err = service.PublishArtifact(context.Background(), request("sample.md")); err == nil {
		t.Fatal("extended file admitted without its reader")
	}
	_, _, _, cancel, err := service.SubscribeFileMediaEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	service.HandleNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{"id": "normal-commentary", "type": "agentMessage", "text": "Preparing ordinary files", "phase": "commentary"},
	}))
	for _, ext := range []string{"csv", "docx", "pptx", "md", "txt", "jpg", "png", "jpeg", "svg", "html", "pdf", "mp4"} {
		name := "sample." + ext
		data, err := os.ReadFile(filepath.Join(workspace, name))
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := service.PublishArtifact(context.Background(), request(name))
		if err != nil {
			t.Fatal(name, err)
		}
		resource, err := service.ReadArtifact(testSessionID, receipt.ArtifactId, ArtifactContent)
		if err != nil || !bytes.Equal(resource.Bytes, data) {
			t.Fatal(name, "resource changed", err)
		}
		if _, err = service.AcknowledgeArtifact(testSessionID, receipt.ArtifactId, ArtifactAcknowledgement{AckID: receipt.ArtifactId, SizeBytes: int64(len(data)), SHA256: resource.SHA256, LocalCommittedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
		repeat, err := service.PublishArtifact(context.Background(), request(name))
		if err != nil || repeat != receipt {
			t.Fatal(name, "duplicate registration after ACK", err)
		}
	}
	service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{"id": "normal-final", "type": "agentMessage", "text": "Ordinary files registered", "phase": "final_answer"},
	}))
	service.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID,
		"turn":     map[string]any{"id": testTurnID, "status": "completed"},
	}))
	_, current, _, closeCurrent, err := service.SubscribeFileMediaEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	closeCurrent()
	_, legacy, _, closeLegacy, err := service.SubscribeMediaEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	closeLegacy()
	if len(current) != 27 || len(legacy) != 21 {
		t.Fatalf("current/legacy lifecycle counts: %d/%d", len(current), len(legacy))
	}
	contract := compileAgentSessionEventV3Contract(t)
	for _, stream := range [][]Event{current, legacy} {
		for _, index := range []int{0, len(stream) - 2, len(stream) - 1} {
			event := stream[index]
			if event.Payload.Phase != nil {
				t.Fatal("newer native phase leaked into v3-shaped media stream")
			}
			event.SchemaVersion = 3
			validateFEAT134Event(t, contract, event)
		}
	}
	for i, event := range current[1 : len(current)-2] {
		if event.SchemaVersion != 12 || *event.Payload.Ordinal != i/2 {
			t.Fatal("v12 version/order", event.SchemaVersion)
		}
	}
	for i, event := range legacy[1 : len(legacy)-2] {
		if event.SchemaVersion != 9 || *event.Payload.Ordinal != i/2 {
			t.Fatal("legacy version/order")
		}
		if event.EventType == EventItemArtifactCompleted && !wire.LegacyMediaAllowed(event.Payload.Kind, event.Payload.MediaType) {
			t.Fatal("new MIME leaked into v9")
		}
	}
	if output := os.Getenv("YIJIE_FEAT165_NORMAL_EVENTS"); output != "" {
		data, err := json.Marshal(current)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	cancel()
	if len(service.media.subscribers) != 0 {
		t.Fatal("normal unsubscribe retained admission")
	}
}
