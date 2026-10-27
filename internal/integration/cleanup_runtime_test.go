package integration

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/fakeresponses"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/google/uuid"
)

// A normal streaming response that remains open until the consumer cancels it.
// All emitted data comes from the existing ordinary complete-response fixture.
// There is no transport break, process signal, altered executable, or fault data.
type cleanupStreamingWriter struct {
	http.ResponseWriter
	ctx       context.Context
	streaming chan struct{}
	once      sync.Once
}

func (w *cleanupStreamingWriter) Flush() { w.ResponseWriter.(http.Flusher).Flush() }

func (w *cleanupStreamingWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if bytes.Contains(p, []byte("response.output_text.delta")) {
		w.Flush()
		w.once.Do(func() { close(w.streaming) })
		select {
		case <-w.ctx.Done():
		case <-time.After(10 * time.Second): // ordinary response can finish on its own
		}
	}
	return n, err
}

func TestPinnedRuntimeCleanupCompleteInterruptAndNormalReopen(t *testing.T) {
	if os.Getenv("YIJIE_RUN_CLEANUP_INTEGRATION") != "1" {
		t.Skip("explicit normal-lifecycle integration opt-in required")
	}
	binary, manifest := os.Getenv("YIJIE_CODEX_INTEGRATION_BINARY"), os.Getenv("YIJIE_CODEX_INTEGRATION_MANIFEST")
	if binary == "" || manifest == "" {
		t.Fatal("canonical Runtime paths are required")
	}
	const runID = "123e4567-e89b-42d3-a456-426614174000"
	provider, err := fakeresponses.New(fakeresponses.Config{RunID: runID, FixtureID: codex.FEAT126FakeFixtureID, Mode: fakeresponses.ModeComplete, MaxCalls: 2})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:18082")
	if err != nil {
		t.Fatal("fixed test provider endpoint unavailable", err)
	}
	var slow atomic.Bool
	streaming := make(chan struct{})
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if slow.Load() && r.Method == http.MethodPost {
			provider.Handler().ServeHTTP(&cleanupStreamingWriter{ResponseWriter: w, ctx: r.Context(), streaming: streaming}, r)
		} else {
			provider.Handler().ServeHTTP(w, r)
		}
	})}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Error("provider normal shutdown", err)
		}
		select {
		case err := <-done:
			if !errors.Is(err, http.ErrServerClosed) {
				t.Error(err)
			}
		case <-ctx.Done():
			t.Error("provider not stopped")
		}
	})
	hostHome := filepath.Join(t.TempDir(), "host-home")
	runtimeHome, workspace := exactPrivateTempDir(t), t.TempDir()
	config := codex.DefaultConfig()
	config.BinaryPath, config.ManifestPath, config.CodexHome = binary, manifest, runtimeHome
	config.StartupTimeout, config.RequestTimeout, config.ShutdownTimeout = 30*time.Second, 10*time.Second, 10*time.Second
	config.FakeResponses = codex.FakeResponsesConfig{Enabled: true, BaseURL: codex.FEAT126FakeBaseURL, RunID: runID, FixtureID: codex.FEAT126FakeFixtureID}
	open := func() (*codex.Manager, *session.Store, *session.Service) {
		store, err := session.OpenStore(hostHome)
		if err != nil {
			t.Fatal(err)
		}
		manager := codex.NewManager(config, nil)
		service := session.NewService(manager, store, session.NewEventHub(512, 64), nil)
		if err := manager.SetNotificationHandler(service.HandleNotification); err != nil {
			t.Fatal(err)
		}
		if err := manager.Start(context.Background()); err != nil {
			t.Fatal("canonical Runtime start", err)
		}
		t.Cleanup(func() {
			shutdownManager(t, manager, config.ShutdownTimeout)
			if err := store.Close(); err != nil {
				t.Error(err)
			}
		})
		return manager, store, service
	}
	manager, store, service := open()
	type deleted struct{ sessionID, threadID, operation string }
	var deletedSessions []deleted
	for _, interrupt := range []bool{false, true} {
		slow.Store(interrupt)
		record, err := service.StartSession(context.Background(), session.StartSessionInput{TaskID: uuid.NewString(), Cwd: workspace})
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.StartTurn(context.Background(), session.StartTurnInput{AgentSessionID: record.AgentSessionID, Input: "ordinary synthetic cleanup verification", ReasoningEffort: "high"})
		if err != nil {
			t.Fatal(err)
		}
		if interrupt {
			select {
			case <-streaming:
			case <-time.After(15 * time.Second):
				t.Fatal("normal streaming response did not start")
			}
		} else {
			deadline := time.Now().Add(15 * time.Second)
			for {
				current, err := store.Get(record.AgentSessionID)
				if err != nil {
					t.Fatal(err)
				}
				if current.LastTurnStatus == "completed" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("ordinary turn did not complete")
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		operation := uuid.NewString()
		deadline := time.Now().Add(10 * time.Second)
		for {
			result, err := service.CleanupSession(context.Background(), record.AgentSessionID, operation)
			if err != nil {
				t.Fatal(err)
			}
			if result.Outcome == "complete" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("cleanup remained incomplete", result)
			}
			time.Sleep(50 * time.Millisecond)
		}
		deletedSessions = append(deletedSessions, deleted{record.AgentSessionID, record.CodexThreadID, operation})
		if _, err := store.Get(record.AgentSessionID); !errors.Is(err, session.ErrNotFound) {
			t.Fatal("Host mapping retained", err)
		}
	}
	shutdownManager(t, manager, config.ShutdownTimeout)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	manager, store, service = open()
	for _, record := range deletedSessions {
		if _, err := store.Get(record.sessionID); !errors.Is(err, session.ErrNotFound) {
			t.Fatal("Host mapping returned after reopen", err)
		}
		if _, err := manager.ReadThread(context.Background(), record.threadID); err == nil {
			t.Fatal("deleted Runtime thread readable after reopen")
		}
		if _, err := manager.ResumeThread(context.Background(), record.threadID); err == nil {
			t.Fatal("deleted Runtime thread resumable after reopen")
		}
		result, err := service.CleanupSession(context.Background(), record.sessionID, record.operation)
		if err != nil || result.Outcome != "complete" {
			t.Fatal("receipt replay after reopen", result, err)
		}
	}
	if provider.Snapshot().AcceptedCalls != 2 {
		t.Fatal("unexpected model request count", provider.Snapshot().AcceptedCalls)
	}
	t.Log("two local synthetic provider calls; normal completion, interruption, delete confirmation and normal reopen verified; no external calls")
}
