package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

// One explicitly selected ordinary turn. The local cap is applied before every
// supplier HTTP request, including requests made by the completion loop.
func TestPublicMessageLiveHostTurn(t *testing.T) {
	model := os.Getenv("YIJIE_PUBLICATION_LIVE_MODEL")
	if model == "" {
		t.Skip("explicit real-provider acceptance only")
	}
	endpoint, keyFile, profile, effort := "", "", "", ""
	switch model {
	case "MiniMax-M3":
		endpoint = codex.MiniMaxChinaBaseURL
		keyFile = "minimax-api-key"
		profile = "minimax-m3-high-v1"
		effort = "high"
	case "kimi-k3.1-pro":
		endpoint = "https://api.moonshot.cn/v1"
		keyFile = "kimi-api-key"
		profile = codex.KimiK31ProfileID
		effort = "high"
	default:
		t.Fatal("unsupported profile")
	}
	dir := os.Getenv("YIJIE_PUBLICATION_EXCHANGE")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute evidence path required")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(os.Getenv("YIJIE_PUBLICATION_KEY_DIRECTORY"), keyFile))
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	supplier := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	meter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.URL.Path != "/v1/responses" || r.Method != "POST" || json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&body) != nil || body["model"] != model {
			http.Error(w, "invalid acceptance request", 400)
			return
		}
		if calls.Add(1) > 3 {
			http.Error(w, "acceptance request limit reached", 429)
			return
		}
		body["max_output_tokens"] = 2048
		// Only the arithmetic prompt from this test goes to the provider. Local
		// execution and hosted web access are unnecessary for that prompt.
		body["tools"] = []any{}
		delete(body, "tool_choice")
		delete(body, "parallel_tool_calls")
		delete(body, "include")
		data, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(r.Context(), "POST", endpoint+"/responses", bytes.NewReader(data))
		if err != nil {
			http.Error(w, "request unavailable", 500)
			return
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(key)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		response, err := supplier.Do(req)
		if err != nil {
			http.Error(w, "provider transport unavailable", 502)
			return
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			var rejected struct {
				Error struct {
					Code    string
					Message string
				}
			}
			_ = json.NewDecoder(io.LimitReader(response.Body, 8192)).Decode(&rejected)
			if strings.Contains(rejected.Error.Message, strings.TrimSpace(string(key))) {
				rejected.Error.Message = ""
			}
			if len(rejected.Error.Message) > 300 {
				rejected.Error.Message = ""
			}
			t.Logf("supplier HTTP %d %s %s", response.StatusCode, rejected.Error.Code, rejected.Error.Message)
			http.Error(w, fmt.Sprintf("supplier HTTP %d", response.StatusCode), response.StatusCode)
			return
		}
		w.Header().Set("Content-Type", response.Header.Get("Content-Type"))
		w.WriteHeader(200)
		buf := make([]byte, 8192)
		for {
			n, e := response.Body.Read(buf)
			if n > 0 {
				if _, err := w.Write(buf[:n]); err != nil {
					return
				}
				w.(http.Flusher).Flush()
			}
			if e != nil {
				return
			}
		}
	}))
	defer meter.Close()
	cfg := codex.DefaultConfig()
	cfg.BinaryPath = os.Getenv("YIJIE_CODEX_INTEGRATION_BINARY")
	cfg.ManifestPath = os.Getenv("YIJIE_CODEX_INTEGRATION_MANIFEST")
	cfg.CodexHome = filepath.Join(dir, "runtime")
	cfg.MiniMax = codex.MiniMaxConfig{Enabled: true, APIKey: "only-local-provider-meter"}
	cfg.KimiAPIKey = "only-local-provider-meter"
	cfg.ChatModelsEnabled, cfg.KimiK31Candidate, cfg.RuntimePermissionsEnabled, cfg.NativeToolsEnabled, cfg.InteractionToolsEnabled, cfg.ToolSearchEnabled = true, true, true, true, true, true
	cfg.PermissionVerificationBaseURL = meter.URL + "/v1"
	cfg.StartupTimeout = 45 * time.Second
	if err = os.Mkdir(cfg.CodexHome, 0700); err != nil {
		t.Fatal(err)
	}
	manager := codex.NewManager(cfg, nil)
	store, err := session.OpenStore(filepath.Join(dir, "host"), session.WithChatModelV2Storage())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := session.NewService(manager, store, session.NewEventHub(512, 64), nil)
	if err = manager.SetNotificationHandler(svc.HandleNotification); err != nil {
		t.Fatal(err)
	}
	if err = manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := manager.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	const token = "ordinary-local-publication-test"
	host := httptest.NewServer(NewHandler(Config{Environment: "local", Runtime: cfg, MultimodalV2Enabled: true}, manager, svc, token))
	defer host.Close()
	do := func(method, path string, input any) *http.Response {
		var body io.Reader
		if input != nil {
			b, _ := json.Marshal(input)
			body = bytes.NewReader(b)
		}
		r, _ := http.NewRequest(method, host.URL+path, body)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Yijie-Publication", "2")
		r.Header.Set("X-Yijie-Model-Profile", profile)
		r.Header.Set("Content-Type", "application/json")
		res, e := (&http.Client{Timeout: 4 * time.Minute}).Do(r)
		if e != nil {
			t.Fatal(e)
		}
		if res.StatusCode >= 300 {
			raw, _ := io.ReadAll(io.LimitReader(res.Body, 512))
			res.Body.Close()
			t.Fatalf("Host %s HTTP %d %s", method, res.StatusCode, raw)
		}
		if res.Header.Get("X-Yijie-Publication") != "2" {
			t.Fatal("publication not negotiated")
		}
		return res
	}
	cwd := filepath.Join(dir, "workspace")
	if err = os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	resp := do("POST", "/v2/model-chat/tasks/"+feat137AppTaskID+"/agent-sessions", map[string]any{"cwd": cwd})
	var created struct {
		Session struct {
			ID string `json:"agent_session_id"`
		}
	}
	err = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if err != nil || created.Session.ID == "" {
		t.Fatal("missing session binding", err)
	}
	sid := created.Session.ID
	prompt := "请先单独发一条公开的进度说明，然后用一句正式答复给出 90 分钟等于多少小时，以及 2 小时等于多少分钟。无需工具。"
	resp = do("POST", "/v2/model-chat/agent-sessions/"+sid+"/permission-turns", map[string]any{"permission_mode": "ask", "turn": map[string]any{"operation_id": feat137AppDecisionID, "reasoning_effort": effort, "content_blocks": []any{map[string]any{"type": "text", "text": prompt}}}})
	resp.Body.Close()
	resp = do("GET", "/v13/agent-sessions/"+sid+"/events", nil)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 8192), 4<<20)
	var frames bytes.Buffer
	var status string
	for scanner.Scan() {
		line := scanner.Bytes()
		frames.Write(line)
		frames.WriteByte('\n')
		if bytes.HasPrefix(line, []byte("data: ")) {
			var event session.Event
			if json.Unmarshal(line[6:], &event) != nil {
				t.Fatal("invalid SSE")
			}
			if n := event.Payload.Native; n != nil && n.Method == "turn/completed" && n.Turn != nil && n.Turn.Status != nil {
				status = string(*n.Turn.Status)
			}
		}
		if len(line) == 0 && status != "" {
			break
		}
	}
	resp.Body.Close()
	if err = os.WriteFile(filepath.Join(dir, "host.sse"), frames.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	resp = do("GET", "/v6/agent-sessions/"+sid+"/native-thread", nil)
	history, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil {
		t.Fatal(e)
	}
	if err = os.WriteFile(filepath.Join(dir, "host-history.json"), history, 0600); err != nil {
		t.Fatal(err)
	}
	receipt, _ := json.Marshal(map[string]any{"model": model, "requests": calls.Load(), "status": status, "outputTokenCap": 2048, "toolsEnabled": false})
	if err = os.WriteFile(filepath.Join(dir, "live-result.json"), receipt, 0600); err != nil {
		t.Fatal(err)
	}
	if status != "completed" {
		t.Fatalf("terminal=%s, supplier requests=%d", status, calls.Load())
	}
}
