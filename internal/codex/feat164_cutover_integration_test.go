package codex

// Ordinary native protocol conformance, not fault or attack injection. These
// tests never replace executables, alter existing history, or call a provider.
import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type feat164CutoverCall struct {
	Model, Effort, Kind, Digest string
	InputBytes                  int
}
type feat164Cutover struct {
	t                    *testing.T
	root, home, cwd, url string
	catalog              string
	manager              *Manager
	notifications        *integrationNotifications
	server               *http.Server
	mu                   sync.Mutex
	calls                []feat164CutoverCall
	errors               []string
	allowedModel         string
	reply                func(map[string]any, int) map[string]any
	stream               func(http.ResponseWriter, *http.Request, map[string]any, int) bool
	keep                 bool
}

func newFEAT164Cutover(t *testing.T, reply func(map[string]any, int) map[string]any) *feat164Cutover {
	t.Helper()
	if os.Getenv("YIJIE_FEAT164_S4D_OFFLINE") != "true" {
		t.Skip("explicit FEAT-164 S4d offline qualification required")
	}
	root, err := os.MkdirTemp("", "feat164-cutover-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	h := &feat164Cutover{t: t, root: root, home: filepath.Join(root, "runtime"), cwd: filepath.Join(root, "workspace"), reply: reply}
	for _, p := range []string{h.home, h.cwd} {
		if err = os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h.url = "http://" + listener.Addr().String() + "/v1"
	h.server = &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		var body map[string]any
		if e != nil || r.Method != "POST" || r.URL.Path != "/v1/responses" || json.Unmarshal(raw, &body) != nil {
			t.Error("unexpected loopback route")
			w.WriteHeader(400)
			return
		}
		reasoning, _ := body["reasoning"].(map[string]any)
		model, _ := body["model"].(string)
		effort, _ := reasoning["effort"].(string)
		if (model != "kimi-k3" && model != "kimi-k3.1-pro") || effort != "max" || r.Header.Get("Authorization") != "Bearer feat164-cutover-kimi" {
			t.Error("unexpected synthetic model profile")
			w.WriteHeader(400)
			return
		}
		input, _ := json.Marshal(body["input"])
		kind := "ordinary"
		if strings.Contains(string(input), "CONTEXT CHECKPOINT COMPACTION") {
			kind = "automatic_compaction"
		}
		h.mu.Lock()
		h.calls = append(h.calls, feat164CutoverCall{model, effort, kind, fmt.Sprintf("%x", sha256.Sum256(raw)), len(input)})
		allowed := h.allowedModel
		n := len(h.calls)
		h.mu.Unlock()
		if model != allowed {
			t.Error("model identity outside current generation reached loopback")
			w.WriteHeader(400)
			return
		}
		if n > 16 {
			t.Error("bounded local request limit exceeded")
			w.WriteHeader(429)
			return
		}
		if h.stream != nil && h.stream(w, r, body, n) {
			return
		}
		item := h.reply(body, n)
		item["id"] = fmt.Sprintf("cutover_item_%d", n)
		id := fmt.Sprintf("cutover_response_%d", n)
		// Explicit synthetic protocol usage, derived from this ordinary fixture's
		// actual request/output size. It is not a vendor token or cost measurement.
		output, _ := json.Marshal(item)
		inTokens, outTokens := len(input)/4+1, len(output)/4+1
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []any{
			map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}},
			map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item},
			map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item},
			map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": inTokens, "output_tokens": outTokens, "total_tokens": inTokens + outTokens}}},
		} {
			b, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", b)
			w.(http.Flusher).Flush()
		}
	})}
	go func() { _ = h.server.Serve(listener) }()
	t.Cleanup(func() {
		ok := h.stop()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if e := h.server.Shutdown(ctx); e != nil {
			t.Error("normal loopback shutdown", e)
			ok = false
		}
		h.mu.Lock()
		log, _ := json.Marshal(h.calls)
		h.mu.Unlock()
		t.Logf("loopback ledger=%s; real_provider_http=0", log)
		if ok && !h.keep {
			_ = os.RemoveAll(h.root)
		} else {
			t.Log("retained temporary root", h.root)
		}
	})
	return h
}
func (h *feat164Cutover) count() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.calls) }
func (h *feat164Cutover) start(candidate bool) {
	t := h.t
	t.Helper()
	if h.manager != nil {
		t.Fatal("normal shutdown required before generation change")
	}
	binary, manifest := integrationArtifactPaths(t)
	if !candidate {
		binary, manifest = os.Getenv("YIJIE_FEAT164_LEGACY_BINARY"), os.Getenv("YIJIE_FEAT164_LEGACY_MANIFEST")
		if binary == "" || manifest == "" {
			t.Fatal("explicit canonical legacy artifact required")
		}
	}
	cfg := integrationConfig(binary, manifest, h.home, "feat164-cutover-minimax")
	cfg.ChatModelsEnabled = true
	cfg.KimiK31Candidate = candidate
	cfg.KimiK31AllowManagedPredecessor = candidate
	cfg.RuntimePermissionsEnabled = true
	cfg.KimiAPIKey = "feat164-cutover-kimi"
	cfg.PermissionVerificationBaseURL = h.url
	var m *Manager
	// Only supported native configuration is used for the historical producer
	// meter and ordinary compatibility catalog. Exact artifacts and policies stay.
	cfg.testBeforeProcessStart = func(context.Context) error {
		content := ""
		if h.catalog != "" {
			content += fmt.Sprintf("model_catalog_json = %q\n", h.catalog)
		}
		if !candidate {
			for _, p := range []string{"kimi", "minimax"} {
				content += fmt.Sprintf("[model_providers.%s]\nbase_url = %q\nrequest_max_retries = 0\nstream_max_retries = 0\n", p, h.url)
			}
		}
		if content == "" {
			return nil
		}
		args, e := nativeConfigArguments([]byte(content))
		if e == nil {
			m.nativeConfigArgs = append(m.nativeConfigArgs, args...)
		}
		return e
	}
	h.mu.Lock()
	h.allowedModel = "kimi-k3"
	if candidate {
		h.allowedModel = "kimi-k3.1-pro"
	}
	h.mu.Unlock()
	m = NewManager(cfg, nil)
	h.manager = m
	h.notifications = newIntegrationNotifications()
	if e := m.SetNotificationHandler(func(method string, params json.RawMessage) {
		h.notifications.handle(method, params)
		if method == "error" {
			var v struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(params, &v) == nil {
				h.mu.Lock()
				h.errors = append(h.errors, v.Error.Message)
				h.mu.Unlock()
			}
		}
	}); e != nil {
		t.Fatal(e)
	}
	if e := m.Start(context.Background()); e != nil {
		t.Fatal("exact Runtime startup", e)
	}
	var effective struct {
		Config struct {
			Providers map[string]struct {
				URL            string `json:"base_url"`
				RequestRetries int    `json:"request_max_retries"`
				StreamRetries  int    `json:"stream_max_retries"`
			} `json:"model_providers"`
		} `json:"config"`
	}
	if e := m.request(context.Background(), "config/read", map[string]any{"includeLayers": false}, &effective); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{"kimi", "minimax"} {
		v := effective.Config.Providers[p]
		if v.URL != h.url || v.RequestRetries != 0 || v.StreamRetries != 0 {
			t.Fatal("effective process route not bounded loopback", p)
		}
	}
}
func (h *feat164Cutover) stop() bool {
	if h.manager == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if e := h.manager.Shutdown(ctx); e != nil {
		h.t.Error("normal Runtime shutdown pending", e)
		return false
	}
	h.manager = nil
	return true
}
func (h *feat164Cutover) nativeThread(model string, config map[string]any) string {
	h.t.Helper()
	var out threadResponse
	if config == nil {
		config = map[string]any{}
	}
	config["model_reasoning_effort"] = "max"
	p := map[string]any{"model": model, "modelProvider": "kimi", "cwd": h.cwd, "approvalPolicy": "never", "sandbox": "read-only", "ephemeral": false, "config": config}
	if e := h.manager.request(context.Background(), "thread/start", p, &out); e != nil {
		h.t.Fatal(e)
	}
	if out.Model != model || out.ReasoningEffort != "max" {
		h.t.Fatal("native effective identity differs")
	}
	return out.Thread.ID
}
func (h *feat164Cutover) nativeTurn(thread, text, expected string) {
	h.t.Helper()
	var out turnStartResponse
	p := map[string]any{"threadId": thread, "input": []any{map[string]any{"type": "text", "text": text, "text_elements": []any{}}}, "effort": "max"}
	if e := h.manager.request(context.Background(), "turn/start", p, &out); e != nil {
		h.t.Fatal(e)
	}
	state, _, e := h.notifications.waitForTurn(out.Turn.ID, 30*time.Second)
	if e != nil || state != expected {
		h.t.Fatalf("native turn state=%s want=%s error=%v", state, expected, e)
	}
}
func (h *feat164Cutover) hostTurn(ctx context.Context, thread, text string) {
	h.t.Helper()
	input, _ := TextUserInput(text)
	turn, e := h.manager.StartTurnV2(ctx, thread, []UserInput{input}, "max")
	if e != nil {
		h.t.Fatal(e)
	}
	state, _, e := h.notifications.waitForTurn(turn.ID, 30*time.Second)
	if e != nil || state != "completed" {
		h.t.Fatal("host turn", state, e)
	}
}
func (h *feat164Cutover) loaded(ids ...string) {
	h.t.Helper()
	var out struct {
		Data []string `json:"data"`
	}
	if e := h.manager.request(context.Background(), "thread/loaded/list", map[string]any{}, &out); e != nil {
		h.t.Fatal(e)
	}
	for _, id := range ids {
		found := false
		for _, v := range out.Data {
			found = found || v == id
		}
		if !found {
			h.t.Fatal("native loaded set missing expected thread")
		}
	}
}
func feat164ToolOutput(body map[string]any, call string) map[string]any {
	input, _ := body["input"].([]any)
	for _, raw := range input {
		v, _ := raw.(map[string]any)
		if v["type"] == "function_call_output" && v["call_id"] == call {
			var out map[string]any
			s, _ := v["output"].(string)
			_ = json.Unmarshal([]byte(s), &out)
			return out
		}
	}
	return nil
}
func feat164UserContains(body map[string]any, marker string) bool {
	input, _ := body["input"].([]any)
	for _, raw := range input {
		v, _ := raw.(map[string]any)
		if v["role"] == "user" {
			s, _ := json.Marshal(v["content"])
			if strings.Contains(string(s), marker) {
				return true
			}
		}
	}
	return false
}
func feat164WaitResult(t *testing.T, body map[string]any, call, child, key string) {
	t.Helper()
	out := feat164ToolOutput(body, call)
	status, _ := out["status"].(map[string]any)
	s, _ := status[child].(map[string]any)
	if out["timed_out"] != false || s[key] == nil {
		b, _ := json.Marshal(out)
		t.Logf("native wait diagnostic=%s", b)
		t.Errorf("native child terminal missing %s (no raw prompt retained)", key)
	}
}

func TestFEAT164LoadedLegacyGraphCutover(t *testing.T) {
	var parents int
	var child string
	var mu sync.Mutex
	h := newFEAT164Cutover(t, func(body map[string]any, _ int) map[string]any {
		if body["model"] == "kimi-k3.1-pro" {
			return feat164AuxText("FEAT164_NEW_COMPLETED")
		}
		if feat164UserContains(body, "FEAT164_OLD_CHILD") {
			return feat164AuxText("FEAT164_OLD_CHILD_COMPLETED")
		}
		mu.Lock()
		defer mu.Unlock()
		parents++
		if parents == 1 {
			return feat164AuxTool("multi_agent_v1", "spawn_agent", "old_spawn", map[string]any{"message": "FEAT164_OLD_CHILD: repeat this ordinary note without tools.", "fork_context": false})
		}
		if parents == 2 {
			child, _ = feat164ToolOutput(body, "old_spawn")["agent_id"].(string)
			if child == "" {
				t.Error("old native child receipt missing")
			}
			return feat164AuxTool("multi_agent_v1", "wait_agent", "old_wait", map[string]any{"targets": []string{child}, "timeout_ms": 10000})
		}
		feat164WaitResult(t, body, "old_wait", child, "completed")
		return feat164AuxText("FEAT164_OLD_PARENT_COMPLETED")
	})
	h.start(false)
	old := WithPermissionMode(WithModelProfile(context.Background(), "kimi-k3-max-v1"), PermissionAsk)
	parent, e := h.manager.StartThread(old, h.cwd)
	if e != nil {
		t.Fatal(e)
	}
	h.hostTurn(old, parent.ID, "Create one native child to echo the ordinary supplied note, wait for completion, then finish. No external resources or file operations.")
	mu.Lock()
	childID := child
	mu.Unlock()
	h.loaded(parent.ID, childID)
	if h.count() != 4 {
		t.Fatal("old graph count", h.count())
	}
	if !h.stop() {
		t.Fatal("cutover cannot follow incomplete normal shutdown")
	}
	cutoff := h.count()
	h.start(true)
	current := feat164AuxCtx()
	for _, id := range []string{parent.ID, childID} {
		raw, e := h.manager.ReadThread(context.Background(), id)
		if e != nil || !strings.Contains(string(raw), "FEAT164_OLD_") {
			t.Fatal("old native history unavailable", e)
		}
		if h.count() != cutoff {
			t.Fatal("history read sent model request")
		}
		info, e := h.manager.ResumeThread(current, id)
		if e != nil || info.ID != id || info.Model != "kimi-k3.1-pro" || info.ReasoningEffort != "max" {
			t.Fatal("explicit new-profile resume", e)
		}
		h.hostTurn(current, id, "Return FEAT164_NEW_COMPLETED from this resumed historical conversation; no tools.")
		cutoff++
	}
	h.loaded(parent.ID, childID)
	input, _ := TextUserInput("ordinary retired profile continuation")
	if _, e = h.manager.StartTurnV2(old, parent.ID, []UserInput{input}, "max"); e == nil {
		t.Fatal("retired Host turn admitted")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.calls) != 6 {
		t.Fatal("graph total")
	}
	for _, c := range h.calls[4:] {
		if c.Model != "kimi-k3.1-pro" {
			t.Fatal("retired model after cutover")
		}
	}
	t.Log("old parent3+child1 were loaded and completed; normal whole-Runtime shutdown; current parent1+child1; old history retained; no old HTTP after cutoff")
}

func TestFEAT164LoadedRetiredAgentGuard(t *testing.T) {
	var parentCalls int
	var child string
	var mu sync.Mutex
	h := newFEAT164Cutover(t, func(body map[string]any, _ int) map[string]any {
		mu.Lock()
		defer mu.Unlock()
		if body["model"] != "kimi-k3.1-pro" {
			t.Error("retired native identity reached transport")
		}
		parentCalls++
		switch parentCalls {
		case 1:
			return feat164AuxTool("multi_agent_v1", "spawn_agent", "retired_spawn", map[string]any{"model": "kimi-k3", "message": "Return one ordinary local note, no tools.", "fork_context": false})
		case 2:
			child, _ = feat164ToolOutput(body, "retired_spawn")["agent_id"].(string)
			if child == "" {
				t.Error("retired child receipt missing")
			}
			return feat164AuxTool("multi_agent_v1", "wait_agent", "retired_wait", map[string]any{"targets": []string{child}, "timeout_ms": 10000})
		case 3:
			feat164WaitResult(t, body, "retired_wait", child, "errored")
			return feat164AuxText("Retired child was not executed.")
		case 4:
			return feat164AuxTool("multi_agent_v1", "send_input", "retired_followup", map[string]any{"target": child, "message": "Return another ordinary local note, no tools.", "interrupt": false})
		case 5:
			return feat164AuxTool("multi_agent_v1", "wait_agent", "retired_wait_again", map[string]any{"targets": []string{child}, "timeout_ms": 10000})
		case 6:
			feat164WaitResult(t, body, "retired_wait_again", child, "errored")
			return feat164AuxText("Loaded retired child remained rejected.")
		case 7:
			return feat164AuxText("Explicitly migrated child completed normally.")
		case 8:
			return feat164AuxTool("multi_agent_v1", "wait_agent", "migrated_child_wait", map[string]any{"targets": []string{child}, "timeout_ms": 10000})
		default:
			feat164WaitResult(t, body, "migrated_child_wait", child, "completed")
			return feat164AuxText("The same child has a successful new-model turn.")
		}
	})
	// Ordinary compatibility metadata lets the native layer reproduce a
	// previously loaded identity. The product Host catalog is not changed.
	oldRaw, _ := chatModelCatalog()
	newRaw, _ := kimiK31Catalog()
	var oldCatalog, newCatalog map[string]any
	_ = json.Unmarshal(oldRaw, &oldCatalog)
	_ = json.Unmarshal(newRaw, &newCatalog)
	for _, v := range oldCatalog["models"].([]any) {
		if v.(map[string]any)["slug"] == "kimi-k3" {
			newCatalog["models"] = append(newCatalog["models"].([]any), v)
		}
	}
	catalogBytes, _ := json.MarshalIndent(newCatalog, "", "  ")
	h.catalog = filepath.Join(h.root, "legacy-loaded-catalog.json")
	if e := os.WriteFile(h.catalog, catalogBytes, 0600); e != nil {
		t.Fatal(e)
	}
	h.start(true)
	old := h.nativeThread("kimi-k3", nil)
	h.loaded(old)
	h.nativeTurn(old, "ordinary old identity note", "failed")
	if h.count() != 0 {
		t.Fatal("loaded retired parent sent HTTP")
	}
	current := feat164AuxCtx()
	parent, e := h.manager.StartThread(current, h.cwd)
	if e != nil {
		t.Fatal(e)
	}
	h.hostTurn(current, parent.ID, "For this native compatibility check, ask one legacy kimi-k3 child for an ordinary note and observe its status; no tools beyond native agent lifecycle.")
	mu.Lock()
	childID := child
	mu.Unlock()
	h.loaded(parent.ID, childID)
	h.hostTurn(current, parent.ID, "Ask that same already-loaded child for one ordinary follow-up, without interruption, and observe its status.")
	if h.count() != 6 {
		t.Fatal("only current parent must reach loopback", h.count())
	}
	h.mu.Lock()
	guardErrors := append([]string(nil), h.errors...)
	h.mu.Unlock()
	if len(guardErrors) != 3 {
		t.Fatal("expected parent and both child retirement guards", len(guardErrors))
	}
	for _, message := range guardErrors {
		if message != "The kimi-k3 model is retired; select kimi-k3.1-pro before executing." {
			t.Fatal("native rejection was not the reviewed retirement guard")
		}
	}
	t.Log("three native retirement guard errors verified without retaining provider details")
	checkHistory := func(wantTurns int) {
		t.Helper()
		raw, err := h.manager.ReadThread(context.Background(), childID)
		if err != nil {
			t.Fatal(err)
		}
		var observed struct {
			Turns []struct {
				Status string `json:"status"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"turns"`
		}
		if err := json.Unmarshal(raw, &observed); err != nil {
			t.Fatal(err)
		}
		if len(observed.Turns) != wantTurns {
			t.Fatalf("child history count=%d want=%d", len(observed.Turns), wantTurns)
		}
		for i, turn := range observed.Turns {
			if i < 2 {
				if turn.Status != "failed" || turn.Error == nil || turn.Error.Message != "Turn failed; error details were not retained." {
					t.Errorf("native durable failure absent or not sanitized at turn %d", i)
				}
			} else if turn.Status != "completed" || turn.Error != nil {
				t.Error("next normal turn inherited failure")
			}
		}
	}
	checkHistory(2)
	// Already-loaded native threads ignore resume model overrides. Exercise the
	// stable turn/start model field explicitly to verify same-session reset;
	// Host cold migration is covered separately by LoadedLegacyGraphCutover.
	var selectedTurn turnStartResponse
	params := map[string]any{"threadId": childID, "model": "kimi-k3.1-pro", "effort": "max", "input": []any{map[string]any{"type": "text", "text": "Return one ordinary local note after this explicit new-model selection.", "text_elements": []any{}}}}
	if e := h.manager.request(context.Background(), "turn/start", params, &selectedTurn); e != nil {
		t.Fatal(e)
	}
	if status, _, e := h.notifications.waitForTurn(selectedTurn.Turn.ID, 30*time.Second); e != nil || status != "completed" {
		t.Fatal("same-session explicitly selected native turn", status, e)
	}
	h.hostTurn(current, parent.ID, "Observe the same child's successful new-model completion.")
	checkHistory(3)
	if h.count() != 9 {
		t.Fatal("unexpected native request count", h.count())
	}
	if !h.stop() {
		t.Fatal("normal shutdown incomplete")
	}
	h.start(true)
	checkHistory(3)
	if h.count() != 9 {
		t.Fatal("cold history read submitted model HTTP")
	}
	t.Log("three native retirement errors; both child waits errored; explicit same-child native model selection and parent wait completed; all failed history persists safely across normal restart; nine current-only loopback requests")
}

func TestFEAT164AutomaticCompaction(t *testing.T) {
	h := newFEAT164Cutover(t, func(body map[string]any, _ int) map[string]any {
		input, _ := json.Marshal(body["input"])
		if strings.Contains(string(input), "CONTEXT CHECKPOINT COMPACTION") {
			return feat164AuxText("FEAT164_COMPACT_SUMMARY: ordinary repeated local notes; no operations.")
		}
		return feat164AuxText("FEAT164_AUTOMATIC_TEXT_DONE")
	})
	h.start(true)
	id := h.nativeThread("kimi-k3.1-pro", map[string]any{"model_auto_compact_token_limit": 4096})
	h.nativeTurn(id, strings.Repeat("Ordinary local inventory note: sample count 164; synthetic text only.\n", 700), "completed")
	if h.count() != 1 {
		t.Fatal("first ordinary turn count", h.count())
	}
	h.nativeTurn(id, "Summarize the ordinary local note in one sentence, no tools.", "completed")
	raw, e := h.manager.ReadThread(context.Background(), id)
	if e != nil || !strings.Contains(string(raw), "contextCompaction") {
		t.Fatal("automatic native compaction fact missing", e)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.calls) != 3 || h.calls[1].Kind != "automatic_compaction" {
		t.Fatal("automatic request sequence", h.calls)
	}
	for _, c := range h.calls {
		if c.Model != "kimi-k3.1-pro" {
			t.Fatal("automatic compaction identity changed")
		}
	}
	t.Log("ordinary large text then automatic pre-turn compaction then new-model answer; three loopback requests; no explicit compact API")
}

// These ordinary metadata variants are not the product's current equal-window,
// no-comp_hash catalog. They qualify the previous-model guard, not successful
// migration of hypothetical future catalog changes.
func TestFEAT164PreviousModelCompactionGuard(t *testing.T) {
	for _, variant := range []string{"comp_hash", "window_downshift"} {
		t.Run(variant, func(t *testing.T) {
			h := newFEAT164Cutover(t, func(body map[string]any, n int) map[string]any {
				if n != 1 || body["model"] != "kimi-k3" {
					t.Error("previous-model branch reached transport after cutoff")
				}
				return feat164AuxText("FEAT164_OLD_CONTEXT_DONE")
			})
			raw, e := chatModelCatalog()
			if e != nil {
				t.Fatal(e)
			}
			var catalog map[string]any
			if e = json.Unmarshal(raw, &catalog); e != nil {
				t.Fatal(e)
			}
			var old map[string]any
			for _, entry := range catalog["models"].([]any) {
				v := entry.(map[string]any)
				if v["slug"] == "kimi-k3" {
					old = v
				}
			}
			if old == nil {
				t.Fatal("legacy catalog entry absent")
			}
			data, _ := json.Marshal(old)
			var current map[string]any
			_ = json.Unmarshal(data, &current)
			current["slug"] = "kimi-k3.1-pro"
			current["display_name"] = "Kimi K3.1 Pro"
			if variant == "comp_hash" {
				old["comp_hash"] = "feat164-old"
				current["comp_hash"] = "feat164-new"
			} else {
				old["context_window"] = 65536
				old["max_context_window"] = 65536
				current["context_window"] = 4096
				current["max_context_window"] = 4096
			}
			catalog["models"] = append(catalog["models"].([]any), current)
			data, _ = json.MarshalIndent(catalog, "", "  ")
			h.catalog = filepath.Join(h.root, "compatibility-catalog.json")
			if e = os.WriteFile(h.catalog, data, 0600); e != nil {
				t.Fatal(e)
			}
			h.start(false)
			id := h.nativeThread("kimi-k3", map[string]any{"model_auto_compact_token_limit": 1000000})
			h.nativeTurn(id, strings.Repeat("Ordinary retained context note 164; no external operation.\n", 700), "completed")
			if h.count() != 1 {
				t.Fatal("legacy seed count")
			}
			if !h.stop() {
				t.Fatal("normal shutdown required")
			}
			h.start(true)
			var resumed threadResponse
			if e = h.manager.request(context.Background(), "thread/resume", map[string]any{"threadId": id, "model": "kimi-k3.1-pro", "modelProvider": "kimi", "cwd": h.cwd, "approvalPolicy": "never", "sandbox": "read-only", "config": map[string]any{"model_reasoning_effort": "max", "model_auto_compact_token_limit": 1000000}}, &resumed); e != nil {
				t.Fatal(e)
			}
			if resumed.Model != "kimi-k3.1-pro" {
				t.Fatal("new identity not configured")
			}
			h.loaded(id)
			h.nativeTurn(id, "Continue the ordinary historical context with the selected model.", "failed")
			if h.count() != 1 {
				t.Fatal("previous retired model escaped request guard")
			}
			history, e := h.manager.ReadThread(context.Background(), id)
			if e != nil || !strings.Contains(string(history), "FEAT164_OLD_CONTEXT_DONE") {
				t.Fatal("historical answer changed", e)
			}
			h.mu.Lock()
			nativeErrors := strings.Join(h.errors, "\n")
			h.mu.Unlock()
			if !strings.Contains(nativeErrors, "kimi-k3 model is retired") {
				t.Fatal("native failure was not the retired request guard")
			}
			t.Logf("%s previous-model automatic compaction rejected by retired guard; old seed1 before cutoff, zero model HTTP after cutoff; history retained", variant)
		})
	}
}

func TestFEAT164MigratedAutomaticCompaction(t *testing.T) {
	h := newFEAT164Cutover(t, func(body map[string]any, _ int) map[string]any {
		if body["model"] == "kimi-k3" {
			return feat164AuxText("FEAT164_MIGRATED_OLD_NOTE_DONE")
		}
		input, _ := json.Marshal(body["input"])
		if strings.Contains(string(input), "CONTEXT CHECKPOINT COMPACTION") {
			return feat164AuxText("FEAT164_MIGRATED_SUMMARY: ordinary old local notes retained; no external operation.")
		}
		return feat164AuxText("FEAT164_MIGRATED_NEW_NOTE_DONE")
	})
	h.start(false)
	id := h.nativeThread("kimi-k3", map[string]any{"model_context_window": 1048576, "model_auto_compact_token_limit": 4096})
	h.nativeTurn(id, strings.Repeat("Ordinary migrated inventory note 164; synthetic text only.\n", 700), "completed")
	if h.count() != 1 {
		t.Fatal("old note must complete before cutover")
	}
	h.loaded(id)
	if !h.stop() {
		t.Fatal("normal shutdown required before cutoff")
	}
	h.start(true)
	var out threadResponse
	if e := h.manager.request(context.Background(), "thread/resume", map[string]any{"threadId": id, "model": "kimi-k3.1-pro", "modelProvider": "kimi", "cwd": h.cwd, "approvalPolicy": "never", "sandbox": "read-only", "config": map[string]any{"model_reasoning_effort": "max", "model_context_window": 1048576, "model_auto_compact_token_limit": 4096}}, &out); e != nil {
		t.Fatal(e)
	}
	if out.Model != "kimi-k3.1-pro" || out.ReasoningEffort != "max" {
		t.Fatal("migrated native identity differs")
	}
	h.nativeTurn(id, "Continue the ordinary retained notes using the selected current model, no tools.", "completed")
	history, e := h.manager.ReadThread(context.Background(), id)
	if e != nil || !strings.Contains(string(history), "contextCompaction") || !strings.Contains(string(history), "FEAT164_MIGRATED_NEW_NOTE_DONE") {
		t.Fatal("migrated automatic compaction/answer absent", e)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.calls) != 3 || h.calls[0].Model != "kimi-k3" || h.calls[1].Kind != "automatic_compaction" {
		t.Fatal("migrated automatic sequence differs")
	}
	for _, c := range h.calls[1:] {
		if c.Model != "kimi-k3.1-pro" {
			t.Fatal("old model after automatic migration cutoff")
		}
	}
	t.Log("canonical old note1; normal shutdown; equal-window/no-comp_hash current catalog; automatic current compaction1+answer1; no old HTTP after cutoff")
}
