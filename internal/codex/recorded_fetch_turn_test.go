package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
	"github.com/google/uuid"
)

// The coordinator serves recorded Responses items, then optionally forwards
// the answer-only request under its own explicit model-call allowance.
func TestHangzhouRecordedFetchTurn(t *testing.T) {
	dir := os.Getenv("HANGZHOU_REPLAY_DIRECTORY")
	origin := os.Getenv("HANGZHOU_REPLAY_MODEL")
	manifest := os.Getenv("FEAT175_PRODUCT_WORKER_MANIFEST")
	if dir == "" || origin == "" || manifest == "" {
		t.Skip("requires the local recorded weather driver")
	}
	dest, err := url.Parse(origin)
	if err != nil || dest.Scheme != "http" || dest.Hostname() != "127.0.0.1" {
		t.Fatal("only a local regression driver is supported")
	}
	var grant struct {
		Binding, Grant        uuid.UUID
		Bearer                string
		Revision, Credentials int
	}
	data, err := os.ReadFile(filepath.Join(dir, "test-grant.json"))
	if err != nil || json.Unmarshal(data, &grant) != nil {
		t.Fatal("isolated grant unavailable")
	}
	var recorded struct{ Request, Commentary, RecordedAt string }
	data, err = os.ReadFile(filepath.Join(dir, "source.json"))
	if err != nil || json.Unmarshal(data, &recorded) != nil {
		t.Fatal("weather recording unavailable")
	}
	var manager *Manager
	scope := uuid.New()
	admToken := strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
	admission := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q wire.WebAdmissionRequest
		if r.Header.Get("Authorization") != "Bearer "+admToken || json.NewDecoder(r.Body).Decode(&q) != nil || manager == nil {
			w.WriteHeader(403)
			return
		}
		root, turn, err := manager.BuiltinWebOwner(r.Context(), q.ThreadId, q.TurnId)
		if err != nil {
			w.WriteHeader(403)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"root_thread_id": root, "root_turn_id": turn, "operation_id": q.OperationId, "scope_id": scope, "deadline_unix_ms": time.Now().Add(45 * time.Second).UnixMilli()})
	}))
	defer admission.Close()
	h := newNativeToolsHarnessWithSetup(t, func(_ *nativeToolsHarness, _ map[string]any, _ int64) map[string]any {
		t.Error("unexpected model route")
		return feat164AuxText("end")
	}, func(m *Manager) {
		manager = m
		if err := m.ConfigureBuiltinWebProfile(wire.LocalAPIOrigin, admission.URL, admToken, "local_demo", nil); err != nil {
			t.Fatal(err)
		}
	}, func(c *Config) {
		feat175Config(c)
		c.PermissionVerificationBaseURL = origin
		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		home := filepath.Join(root, "host-home")
		if err = os.Mkdir(home, 0700); err != nil {
			t.Fatal(err)
		}
		c.Market = MarketConfig{Manifest: manifest, LibraryHome: filepath.Join(home, "market-worker", "library-home"), HostInstanceID: uuid.NewString()}
	})
	type observed struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	var notices []observed
	h.notifications.mu.Lock()
	h.notifications.observe = func(method string, params json.RawMessage) {
		switch method {
		case "turn/started", "item/started", "item/completed", "turn/completed":
			notices = append(notices, observed{method, append(json.RawMessage(nil), params...)})
		}
	}
	h.notifications.mu.Unlock()
	ins := wire.WebCapabilityInstall{Method: "web_capability_install", ProcessEpoch: uuid.New(), BindingEpoch: uuid.New(), Delegation: wire.Delegation{BindingId: grant.Binding, GrantId: grant.Grant, Bearer: grant.Bearer, Audience: "builtin-web-retrieval/1", AuthorizationRevision: grant.Revision, CredentialRevision: grant.Credentials, ExpiresAtUnixMs: int(time.Now().Add(299 * time.Second).UnixMilli())}}
	raw, _ := json.Marshal(ins)
	ack, err := h.manager.BuiltinWebControl(context.Background(), "WebCapabilityInstall", raw)
	if err != nil || ack.State != "applied" {
		t.Fatal("scoped web gateway unavailable", err)
	}
	profile := "kimi-k3-1-pro-high-v1"
	ctx := WithPermissionMode(WithModelProfile(context.Background(), profile), PermissionAsk)
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	// The recording crossed midnight. Preserve the original user wording and
	// supply its real timestamp as replay context instead of querying for today.
	turn := feat175Start(t, h, profile, thread.ID, recorded.Request+"\n\n原始请求的当地时间为 "+recorded.RecordedAt+"，相对日期按该时间解读。")
	status, _, waitErr := h.notifications.waitForTurn(turn.ID, 230*time.Second)
	if waitErr != nil {
		_ = h.manager.InterruptTurn(context.Background(), thread.ID, turn.ID)
	}
	h.notifications.mu.Lock()
	observedBytes, marshalErr := json.Marshal(notices)
	h.notifications.mu.Unlock()
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if err = os.WriteFile(filepath.Join(dir, "native-notifications.json"), observedBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || waitErr != nil {
		t.Fatalf("recorded turn ended %s: %v", status, waitErr)
	}
	history, err := h.manager.ReadThread(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Turns []struct {
			Items []struct {
				Type, Text, Phase, Tool string
				Result                  json.RawMessage
			}
		}
	}
	if json.Unmarshal(history, &saved) != nil || len(saved.Turns) != 1 {
		t.Fatal("missing native turn")
	}
	var final string
	var fetches, updates int
	for _, item := range saved.Turns[0].Items {
		if item.Type == "mcpToolCall" && item.Tool == "kimi_web_fetch" {
			fetches++
		}
		if item.Type == "agentMessage" && item.Phase == "commentary" && item.Text == recorded.Commentary {
			updates++
		}
		if item.Type == "agentMessage" && item.Phase == "final_answer" {
			final = item.Text
		}
	}
	if err = os.WriteFile(filepath.Join(dir, "completed-thread.json"), history, 0600); err != nil {
		t.Fatal(err)
	}
	if len(final) == 0 || fetches != 3 || updates != 3 {
		t.Fatalf("missing final or recorded work: fetches=%d updates=%d final chars=%d", fetches, updates, len([]rune(final)))
	}
	if err = os.WriteFile(filepath.Join(dir, "final-answer.md"), []byte(final+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	metrics, _ := json.Marshal(map[string]any{"scope": scope, "thread": thread.ID, "turn": turn.ID, "status": "completed", "fetchCalls": fetches, "rawRepeatedUpdates": updates, "finalChars": len([]rune(final))})
	if err = os.WriteFile(filepath.Join(dir, "completed-turn-metrics.json"), metrics, 0600); err != nil {
		t.Fatal(err)
	}
}
