// Local cross-process acceptance coordinator. The Native reader is the only ACK producer.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/app"
	"github.com/36Dge/yijie-agent-host/internal/artifact"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/security"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/google/uuid"
)

func untilFile(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if b, e := os.ReadFile(path); e == nil && len(b) > 0 {
			return true
		}
		if _, e := os.Stat(filepath.Join(filepath.Dir(path), "receiver-finished")); e == nil {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func run() (result error) {
	if len(os.Args) < 7 {
		return fmt.Errorf("expected state, workspace, runtime, adapter, loopback port and file names")
	}
	dir, files, rt, adapterBin := os.Args[1], os.Args[2], os.Args[3], os.Args[4]
	port, err := strconv.ParseUint(os.Args[5], 10, 16)
	if err != nil || port == 0 {
		return fmt.Errorf("invalid local verification port")
	}
	expected := os.Args[6:]
	for _, name := range expected {
		if name != filepath.Base(name) || name == "." || name == ".." {
			return fmt.Errorf("invalid sample name")
		}
	}
	hostHome := filepath.Join(dir, "host")
	for _, name := range []string{"host", "codex", "native-secrets", "native-saved"} {
		if e := os.Mkdir(filepath.Join(dir, name), 0700); e != nil {
			return e
		}
	}
	ctx := codex.WithPermissionMode(codex.WithModelProfile(context.Background(), "minimax-m3-high-v1"), codex.PermissionAsk)
	cfg := codex.DefaultConfig()

	cfg.BinaryPath = filepath.Join(rt, "codex")
	cfg.ManifestPath = filepath.Join(rt, "runtime-manifest.json")
	cfg.CodexHome = filepath.Join(dir, "codex")
	cfg.ChatModelsEnabled = true
	cfg.KimiK31Candidate = true
	cfg.RuntimePermissionsEnabled = true
	cfg.MiniMax = codex.MiniMaxConfig{Enabled: true, APIKey: "local-fixture-only"}
	cfg.KimiAPIKey = "local-fixture-only"
	cfg.PermissionVerificationBaseURL = fmt.Sprintf("http://127.0.0.1:%d/v1", port)
	cfg.StartupTimeout = 35 * time.Second
	cfg.RequestTimeout = 35 * time.Second
	cfg.ShutdownTimeout = 30 * time.Second
	runtime := codex.NewManager(cfg, nil)
	store, e := session.OpenStore(hostHome, session.WithChatModelV2Storage())
	if e != nil {
		return e
	}
	defer func() { result = errors.Join(result, store.Close()) }()
	spool, e := artifact.OpenStore(filepath.Join(hostHome, "artifact-spool"), artifact.Options{})
	if e != nil {
		return e
	}
	defer func() { result = errors.Join(result, spool.Close()) }()
	serv := session.NewService(runtime, store, session.NewEventHub(128, 64), nil, session.WithV3Artifacts(session.NewEventHubVersion(3, 128, 64), spool, false), session.WithArtifactMedia())
	done := make(chan string, 1)
	e = runtime.SetNotificationHandler(func(method string, raw json.RawMessage) {
		serv.HandleNotification(method, raw)
		if method == "turn/completed" {
			var v struct{ Turn struct{ Status string } }
			if json.Unmarshal(raw, &v) == nil {
				select {
				case done <- v.Turn.Status:
				default:
				}
			}
		}
	})
	if e != nil {
		return e
	}
	token, e := security.LoadOrCreateAPIToken(hostHome)
	if e != nil {
		return e
	}
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return e
	}
	nonce := uuid.NewString()
	server := &http.Server{Handler: app.NewHandler(app.Config{Environment: "local", Runtime: cfg, HostHome: hostHome, InstanceNonce: nonce, ArtifactV3Enabled: true, ArtifactMediaEnabled: true}, runtime, serv, token), ReadHeaderTimeout: 4 * time.Second}
	go server.Serve(ln)
	defer func() {
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		result = errors.Join(result, server.Shutdown(c))
	}()
	a, e := artifact.StartAdapter(adapterBin, "http://"+ln.Addr().String(), token)
	if e != nil {
		return e
	}
	defer func() {
		c, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		result = errors.Join(result, a.Stop(c))
	}()
	if e = runtime.SetArtifactMCP(a.URL, a.Token); e != nil {
		return e
	}
	if e = runtime.Start(ctx); e != nil {
		return e
	}
	defer func() {
		c, cancel := context.WithTimeout(ctx, 32*time.Second)
		defer cancel()
		result = errors.Join(result, runtime.Shutdown(c))
	}()
	record, e := serv.StartSession(ctx, session.StartSessionInput{TaskID: uuid.NewString(), Cwd: files})
	if e != nil {
		return fmt.Errorf("start session: %w", e)
	}
	binding, _ := json.Marshal(map[string]any{"agent": record.AgentSessionID, "port": ln.Addr().(*net.TCPAddr).Port, "nonce": nonce, "files": expected})
	if e = os.WriteFile(filepath.Join(dir, "connection.json"), binding, 0600); e != nil {
		return e
	}
	fmt.Println("Real Host, Runtime and MCP ready. Waiting for the native receiver.")
	if !untilFile(filepath.Join(dir, "native-reader-ready"), 3*time.Minute) {
		return fmt.Errorf("native reader did not subscribe")
	}
	started, e := serv.StartTurn(ctx, session.StartTurnInput{AgentSessionID: record.AgentSessionID, Input: "YJ-PUBLISH-1123 工作区登记验收，仅使用本地样本和内置产物工具。", ReasoningEffort: "high"})
	if e != nil {
		return fmt.Errorf("start turn: %w", e)
	}
	if e = os.WriteFile(filepath.Join(dir, "turn-id"), []byte(started.ID), 0600); e != nil {
		return e
	}
	select {
	case status := <-done:
		fmt.Println("Actual runtime terminal:", status)
		if status != "completed" {
			return fmt.Errorf("runtime terminal %s", status)
		}
	case <-time.After(100 * time.Second):
		return fmt.Errorf("ordinary turn timeout")
	}
	if !untilFile(filepath.Join(dir, "native-saved.json"), 30*time.Second) {
		return fmt.Errorf("native save/reopen evidence missing")
	}
	return nil
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
