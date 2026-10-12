package codex

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// Six normal synthetic turns observe the actual fixed Runtime's Provider tools.
// The only HTTP destination is the existing local verification route; responses
// contain ordinary final text and never request a tool or a remote operation.
func TestFEAT158PinnedRuntimeExpertWebSearchLifecycle(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	listener, err := net.Listen("tcp", "127.0.0.1:18083")
	if err != nil {
		t.Fatal("local qualification port occupied; no process stopped")
	}
	type observation struct {
		Model  string   `json:"model"`
		Tools  string   `json:"tools_sha256"`
		Search []string `json:"search_tools"`
	}
	var mu sync.Mutex
	var observed []observation
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string            `json:"model"`
			Tools []json.RawMessage `json:"tools"`
		}
		if r.Method != "POST" || r.URL.Path != "/v1/responses" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("unexpected local qualification request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		value := observation{Model: body.Model, Search: []string{}}
		tools, _ := json.Marshal(body.Tools)
		value.Tools = fmt.Sprintf("%x", sha256.Sum256(tools))
		for _, raw := range body.Tools {
			var tool struct{ Type, Name string }
			if json.Unmarshal(raw, &tool) != nil {
				t.Error("invalid ordinary native tool definition")
			}
			if strings.HasPrefix(tool.Type, "web_search") || tool.Name == "web.run" || tool.Name == "web" {
				value.Search = append(value.Search, string(raw))
			}
		}
		mu.Lock()
		observed = append(observed, value)
		n := len(observed)
		mu.Unlock()
		if n > 6 {
			t.Error("unexpected extra qualification turn request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id := fmt.Sprintf("local_expert_web_%d", n)
		item := map[string]any{"id": "message_" + id, "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "LOCAL_REFERENCE_OK", "annotations": []any{}}}}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []any{
			map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}},
			map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item},
			map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item},
			map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}},
		} {
			raw, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", raw)
			w.(http.Flusher).Flush()
		}
	})}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if server.Shutdown(ctx) != nil {
			t.Error("normal local observer shutdown pending")
		}
	}()
	config := integrationConfig(binary, manifest, integrationPrivateTempDir(t), "local-minimax-reference")
	config.RequestTimeout = 20 * time.Second
	config.ChatModelsEnabled, config.RuntimePermissionsEnabled = true, true
	config.KimiAPIKey = "local-kimi-reference"
	config.PermissionVerificationBaseURL = "http://127.0.0.1:18083/v1"
	start := func() (*Manager, *integrationNotifications) {
		m, events := NewManager(config, nil), newIntegrationNotifications()
		if err := m.SetNotificationHandler(events.handle); err != nil {
			t.Fatal(err)
		}
		if err := m.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { shutdownIntegrationManager(t, m, config.ShutdownTimeout) })
		return m, events
	}
	ctxFor := func(profile string, mode PermissionMode, role bool, explicitNone bool) context.Context {
		ctx := WithPermissionMode(WithModelProfile(context.Background(), profile), mode)
		if role || explicitNone {
			roleText, index := "", ""
			if role {
				roleText, index = "Organize benign local reference notes.", "Local reference index."
			}
			var err error
			ctx, err = WithExpertInstructions(ctx, roleText, index)
			if err != nil {
				t.Fatal(err)
			}
		}
		servers, err := disabledMarketServers(nil, "http://127.0.0.1:18083")
		if err != nil {
			t.Fatal(err)
		}
		return WithMarketPrepared(ctx, &MarketPrepared{Config: map[string]any{"mcp_servers": servers}})
	}
	turn := func(m *Manager, events *integrationNotifications, ctx context.Context, thread string) {
		input, _ := TextUserInput("Return the ordinary local qualification text without using tools.")
		p, _ := ModelProfile(ModelProfileID(ctx))
		actual, err := m.StartTurnV2(ctx, thread, []UserInput{input}, p.Effort)
		if err != nil {
			t.Fatal(err)
		}
		if status, _, err := events.waitForTurn(actual.ID, 30*time.Second); err != nil || status != "completed" {
			t.Fatal("normal synthetic turn did not complete", status, err)
		}
	}
	m, events := start()
	ordinary := ctxFor("kimi-k3-max-v1", PermissionAsk, false, false)
	baseline, err := m.StartThread(ordinary, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	turn(m, events, ordinary, baseline.ID)
	expert := ctxFor("kimi-k3-max-v1", PermissionAsk, true, false)
	thread, err := m.StartThread(expert, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	turn(m, events, expert, thread.ID)
	for _, ctx := range []context.Context{
		ctxFor("minimax-m3-high-v1", PermissionAuto, true, false),
		ctxFor("kimi-k3-max-v1", PermissionFull, true, false),
	} {
		if changed, err := m.SwitchThread(ctx, thread.ID); err != nil || changed.ID != thread.ID {
			t.Fatal("normal model/permission switch failed", err)
		}
		turn(m, events, ctx, thread.ID)
	}
	shutdownIntegrationManager(t, m, config.ShutdownTimeout)
	m, events = start()
	if resumed, err := m.SwitchThread(expert, thread.ID); err != nil || resumed.ID != thread.ID {
		t.Fatal("normal reopen failed", err)
	}
	turn(m, events, expert, thread.ID)
	removed := ctxFor("kimi-k3-max-v1", PermissionAsk, false, true)
	if resumed, err := m.SwitchThread(removed, thread.ID); err != nil || resumed.ID != thread.ID {
		t.Fatal("explicit removal failed", err)
	}
	turn(m, events, removed, thread.ID)
	mu.Lock()
	defer mu.Unlock()
	if len(observed) != 6 || len(observed[0].Search) == 0 || !reflect.DeepEqual(observed[0].Search, observed[5].Search) {
		t.Fatal("ordinary web-search configuration was not observed and restored", observed)
	}
	for i := 1; i <= 4; i++ {
		if len(observed[i].Search) != 0 {
			t.Fatal("expert exposed builtin search", i, observed[i])
		}
	}
	for i, observation := range observed {
		raw, _ := json.Marshal(observation)
		t.Logf("turn=%d %s", i+1, raw)
	}
	t.Log("6 normal loopback turns, 6 HTTP requests, 0 real Provider calls, 0 tool calls; same expert thread across switch/reopen/removal")
}
