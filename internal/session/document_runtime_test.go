package session

import (
	"context"
	"github.com/36Dge/yijie-agent-host/internal/artifact"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	media "github.com/36Dge/yijie-agent-host/internal/contracts/artifactmedia"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/documentruntime"
	"github.com/36Dge/yijie-agent-host/internal/documents"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDocumentCapabilitiesUseCurrentSessionAndExistingACK(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host-home")
	workspace := t.TempDir()
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
	service := NewService(&fakeRuntime{}, store, NewEventHub(16, 8), nil, WithV3Artifacts(NewEventHubVersion(3, 64, 16), spool, false), WithArtifactMedia())
	request := wire.EnvironmentRequest{ThreadId: testThreadID, TurnId: testTurnID}
	if _, err = service.DocumentEnvironment(context.Background(), request); err == nil {
		t.Fatal("unconfigured service advertised capability")
	}
	service.SetDocumentEnvironment(documents.New("", ""))
	result, err := service.DocumentEnvironment(context.Background(), request)
	if err != nil || result.Code != "missing" {
		t.Fatal("configured service did not report real dependency state", err)
	}
	request.TurnId = "019fbd88-cbc3-7bf1-934d-7b05cd693f99"
	if _, err = service.DocumentEnvironment(context.Background(), request); err == nil {
		t.Fatal("unbound turn queried capability")
	}
	if err = os.WriteFile(filepath.Join(workspace, "document.txt"), []byte("Normal committed document."), 0600); err != nil {
		t.Fatal(err)
	}
	registered, err := service.PublishArtifact(context.Background(), media.PublishRequest{ThreadId: testThreadID, TurnId: testTurnID, Input: media.PublishInput{RelativePath: "document.txt", DisplayName: "document.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	statusRequest := wire.StatusRequest{ThreadId: testThreadID, TurnId: testTurnID, Input: wire.StatusInput{ArtifactId: registered.ArtifactId}}
	status, err := service.DocumentArtifactStatus(context.Background(), statusRequest)
	if err != nil || status.Status != "staged" {
		t.Fatal(status, err)
	}
	content, err := service.ReadArtifact(testSessionID, registered.ArtifactId, ArtifactContent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.AcknowledgeArtifact(testSessionID, registered.ArtifactId, ArtifactAcknowledgement{AckID: registered.ArtifactId, SizeBytes: int64(len(content.Bytes)), SHA256: content.SHA256, LocalCommittedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	status, err = service.DocumentArtifactStatus(context.Background(), statusRequest)
	if err != nil || status.Status != "committed" {
		t.Fatal(status, err)
	}
	if samples := os.Getenv("YIJIE_DOCUMENT_TEST_SAMPLES"); samples != "" {
		_, _, _, cancel, err := service.SubscribeFileMediaEvents(testSessionID, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		defer cancel()
		for _, format := range []string{"txt", "md", "html", "csv", "json", "docx", "pptx", "xlsx", "pdf"} {
			name := "sample." + format
			bytes, err := os.ReadFile(filepath.Join(samples, name))
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(workspace, name), bytes, 0600); err != nil {
				t.Fatal(err)
			}
			registered, err := service.PublishArtifact(context.Background(), media.PublishRequest{ThreadId: testThreadID, TurnId: testTurnID, Input: media.PublishInput{RelativePath: name, DisplayName: name}})
			if err != nil {
				t.Fatal(format, err)
			}
			content, err := service.ReadArtifact(testSessionID, registered.ArtifactId, ArtifactContent)
			if err != nil || len(content.Bytes) != len(bytes) {
				t.Fatal(format, err)
			}
			if _, err = service.AcknowledgeArtifact(testSessionID, registered.ArtifactId, ArtifactAcknowledgement{AckID: registered.ArtifactId, SizeBytes: int64(len(bytes)), SHA256: content.SHA256, LocalCommittedAt: time.Now()}); err != nil {
				t.Fatal(format, err)
			}
		}
	}
}
