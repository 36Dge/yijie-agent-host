package session

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/artifactmedia"
)

// Normal local files and real Store/Spool/EventHub; no attack/fault fixtures.
func TestArtifactMediaNormalRegistrationAndACK(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host-home")
	workspace := t.TempDir()
	store, e := OpenStore(home)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	if e = store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: workspace}); e != nil {
		t.Fatal(e)
	}
	if _, e = store.BindThread(testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID); e != nil {
		t.Fatal(e)
	}
	if _, e = store.BindTurn(testSessionID, testTurnID); e != nil {
		t.Fatal(e)
	}
	spool, e := artifact.OpenStore(filepath.Join(home, "artifact-spool"), artifact.Options{})
	if e != nil {
		t.Fatal(e)
	}
	defer spool.Close()
	legacy := NewEventHubVersion(3, 64, 16)
	service := NewService(&fakeRuntime{}, store, NewEventHub(16, 8), nil, WithV3Artifacts(legacy, spool, false), WithArtifactMedia())
	files := map[string][]byte{"report.html": []byte("<!doctype html><html><meta charset=utf-8><h1>报告</h1></html>"), "drawing.svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="80" height="60"><rect width="80" height="60" fill="teal"/></svg>`)}
	files["prefixed.svg"] = []byte(`<s:svg xmlns:s="http://www.w3.org/2000/svg" width="80" height="60"><s:rect width="80" height="60" fill="teal"/></s:svg>`)
	if sample := os.Getenv("YIJIE_TEST_MEDIA_FILE"); sample != "" {
		data, e := os.ReadFile(sample)
		if e != nil {
			t.Fatal(e)
		}
		files["sample.mp4"] = data
	}
	for name, data := range files {
		if e = os.WriteFile(filepath.Join(workspace, name), data, 0600); e != nil {
			t.Fatal(e)
		}
		request := wire.PublishRequest{ThreadId: testThreadID, TurnId: testTurnID, Input: wire.PublishInput{RelativePath: name, DisplayName: name}}
		receipt, e := service.PublishArtifact(context.Background(), request)
		if e != nil {
			t.Fatal(name, e)
		}
		repeat, e := service.PublishArtifact(context.Background(), request)
		if e != nil || repeat != receipt {
			t.Fatal("registration was not idempotent", e)
		}
		resource, e := service.ReadArtifact(testSessionID, receipt.ArtifactId, ArtifactContent)
		if e != nil || !bytes.Equal(resource.Bytes, data) {
			t.Fatal("resource changed", e)
		}
		if _, e = service.AcknowledgeArtifact(testSessionID, receipt.ArtifactId, ArtifactAcknowledgement{AckID: receipt.ArtifactId, SizeBytes: int64(len(data)), SHA256: resource.SHA256, LocalCommittedAt: time.Now().UTC()}); e != nil {
			t.Fatal(e)
		}
	}
	_, events, _, cancel, e := service.SubscribeMediaEvents(testSessionID, "", 0)
	if e != nil {
		t.Fatal(e)
	}
	cancel()
	if len(events) != len(files)*2 {
		t.Fatalf("event count %d", len(events))
	}
	for i, event := range events {
		if event.SchemaVersion != 9 || event.Payload.Provenance != "tool" || event.TurnID != testTurnID || *event.Payload.Ordinal != i/2 {
			t.Fatal("event identity/version/ordinal")
		}
	}
	_, old, _, cancel, e := service.SubscribeEventsV3(testSessionID, "", 0)
	if e != nil {
		t.Fatal(e)
	}
	cancel()
	if len(old) != 0 {
		t.Fatal("new media leaked into legacy stream")
	}
	// A legacy producer still starts at ordinal zero on v3. The companion
	// stream places it after files already registered in this same turn.
	record, e := store.Get(testSessionID)
	if e != nil {
		t.Fatal(e)
	}
	id, e := newUUID()
	if e != nil {
		t.Fatal(e)
	}
	zero := 0
	e = service.publishV3(record, Event{TurnID: testTurnID, ItemID: "artifact-" + id, EventType: EventItemArtifactStarted, Payload: EventPayload{ArtifactID: id, Kind: "image", Provenance: "tool", Status: "in_progress", Ordinal: &zero, DisplayName: "image.png"}})
	if e != nil {
		t.Fatal(e)
	}
	_, merged, _, cancel, e := service.SubscribeMediaEvents(testSessionID, "", 0)
	if e != nil {
		t.Fatal(e)
	}
	cancel()
	if *merged[len(merged)-1].Payload.Ordinal != len(files) {
		t.Fatal("mixed producer ordinal collided")
	}
	_, old, _, cancel, e = service.SubscribeEventsV3(testSessionID, "", 0)
	if e != nil {
		t.Fatal(e)
	}
	cancel()
	if len(old) != 1 || *old[0].Payload.Ordinal != 0 {
		t.Fatal("legacy ordinal changed")
	}
	if e = service.completeTerminal(record, syntheticTerminal{turnID: testTurnID, status: "completed", payload: EventPayload{Status: "completed"}}); e != nil {
		t.Fatal(e)
	}
	if len(service.media.registrations) != 0 || len(service.media.ordinals) != 0 || len(service.media.order) != 0 {
		t.Fatal("completed turn retained registration authority")
	}
	if _, e = service.PublishArtifact(context.Background(), wire.PublishRequest{ThreadId: testThreadID, TurnId: testTurnID, Input: wire.PublishInput{RelativePath: "report.html", DisplayName: "report.html"}}); e == nil {
		t.Fatal("completed turn accepted another registration")
	}
}

func TestArtifactMediaNormalTerminalStopsRegistration(t *testing.T) {
	// The current turn assertion is covered with a normal missing/idle session.
	home := filepath.Join(t.TempDir(), "host-home")
	store, e := OpenStore(home)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	service := NewService(&fakeRuntime{}, store, NewEventHub(16, 8), nil, WithArtifactMedia())
	if _, e = service.PublishArtifact(context.Background(), wire.PublishRequest{ThreadId: testThreadID, TurnId: testTurnID, Input: wire.PublishInput{RelativePath: "report.html", DisplayName: "report.html"}}); e == nil {
		t.Fatal("missing active scope must reject")
	}
}
