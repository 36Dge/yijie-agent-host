package codex

// Test-only execution driver. Real and scripted providers use the same native
// Manager/thread/turn/tool path. The external meter owns paid admission; no key
// or supplier REST client enters this driver. Never used by the Desktop App.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/runtimenativetools"
)

type q2Event struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type q2Capture struct {
	mu     sync.Mutex
	events []q2Event
	full   bool
}

func (c *q2Capture) handle(method string, params json.RawMessage) {
	if method != "item/started" && method != "item/completed" && method != "turn/completed" && method != "turn/started" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.events) >= 2000 {
		c.full = true
		return
	}
	c.events = append(c.events, q2Event{method, append(json.RawMessage(nil), params...)})
}

func (c *q2Capture) unfinished() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	pending := map[string]string{}
	for _, e := range c.events {
		var p struct {
			ThreadID string              `json:"threadId"`
			Turn     struct{ ID string } `json:"turn"`
		}
		if json.Unmarshal(e.Params, &p) == nil && p.Turn.ID != "" {
			if e.Method == "turn/started" {
				pending[p.Turn.ID] = p.ThreadID
			} else if e.Method == "turn/completed" {
				delete(pending, p.Turn.ID)
			}
		}
	}
	return pending
}

func q2HostedReconciled(events []q2Event, requests []map[string]any) bool {
	provider, runtime := map[string]bool{}, map[string]bool{}
	for _, row := range requests {
		ids, ok := row["hosted_item_ids"].([]any)
		if !ok {
			return false
		}
		for _, value := range ids {
			id, ok := value.(string)
			if !ok || id == "" || provider[id] {
				return false
			}
			provider[id] = true
		}
	}
	for _, e := range events {
		var p struct{ Item struct{ ID, Type string } }
		if e.Method == "item/completed" && json.Unmarshal(e.Params, &p) == nil && p.Item.Type == "webSearch" {
			if p.Item.ID == "" || runtime[p.Item.ID] {
				return false
			}
			runtime[p.Item.ID] = true
		}
	}
	if len(provider) != len(runtime) {
		return false
	}
	for id := range provider {
		if !runtime[id] {
			return false
		}
	}
	return true
}

func q2Write(file string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(b, '\n'), 0600)
}

func TestFEAT175Q2HostedReconciliation(t *testing.T) {
	event := q2Event{"item/completed", json.RawMessage(`{"item":{"id":"ordinary-search","type":"webSearch"}}`)}
	for _, tc := range []struct {
		name   string
		events []q2Event
		ids    []any
		want   bool
	}{
		{"no searches", nil, []any{}, true},
		{"same native inventory", []q2Event{event}, []any{"ordinary-search"}, true},
		{"provider only", nil, []any{"ordinary-search"}, false},
		{"runtime only", []q2Event{event}, []any{}, false},
		{"missing native identity", []q2Event{event}, []any{nil}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if q2HostedReconciled(tc.events, []map[string]any{{"hosted_item_ids": tc.ids}}) != tc.want {
				t.Fatal("provider/native inventory mismatch")
			}
		})
	}
}

func TestFEAT175Q2Execution(t *testing.T) {
	file := os.Getenv("YIJIE_FEAT175_Q2_RUN_CONFIG")
	if file == "" {
		t.Skip("only the dedicated Q2 local/live runner may provide an owned run config")
	}
	var cfgFile struct {
		Root          string `json:"root"`
		Endpoint      string `json:"endpoint"`
		ControlToken  string `json:"control_token"`
		RuntimeBearer string `json:"runtime_bearer"`
		MCPToken      string `json:"mcp_token"`
		Kind          string `json:"kind"`
		PackageSHA256 string `json:"package_sha256"`
	}
	info, err := os.Lstat(file)
	if err != nil || !filepath.IsAbs(file) || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		t.Fatal("owner-only run configuration required")
	}
	data, err := os.ReadFile(file)
	if err != nil || json.Unmarshal(data, &cfgFile) != nil {
		t.Fatal("invalid Q2 run configuration")
	}
	u, err := url.Parse(cfgFile.Endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		!filepath.IsAbs(cfgFile.Root) || filepath.Dir(file) != cfgFile.Root ||
		(cfgFile.Kind != "synthetic" && cfgFile.Kind != "recorded-native") {
		t.Fatal("owned loopback Q2 endpoint and fresh root required")
	}
	data, err = os.ReadFile(filepath.Join("testdata", "feat175-q2", "cases.json"))
	sum := sha256.Sum256(data)
	if err != nil || hex.EncodeToString(sum[:]) != cfgFile.PackageSHA256 {
		t.Fatal("frozen case package differs")
	}
	var plan struct {
		Models []struct{ Model, Profile, Effort string }
		Cases  []struct{ ID, Prompt string }
	}
	if err = json.Unmarshal(data, &plan); err != nil || len(plan.Models) != 2 || len(plan.Cases) != 6 {
		t.Fatal("invalid frozen plan")
	}
	client := &http.Client{Timeout: 35 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	control := func(action string, value any, out any) error {
		b, _ := json.Marshal(value)
		req, e := http.NewRequest(http.MethodPost, cfgFile.Endpoint+"/control/"+action, bytes.NewReader(b))
		if e != nil {
			return e
		}
		req.Header.Set("X-FEAT175-Control", cfgFile.ControlToken)
		req.Header.Set("Content-Type", "application/json")
		res, e := client.Do(req)
		if e != nil {
			return fmt.Errorf("Q2 control unavailable")
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return fmt.Errorf("Q2 admission stopped: HTTP %d", res.StatusCode)
		}
		if out != nil {
			return json.NewDecoder(res.Body).Decode(out)
		}
		return nil
	}
	stop := func() { _ = control("stop", map[string]any{}, nil) }
	defer stop()
	if err := os.Mkdir(filepath.Join(cfgFile.Root, "runtime"), 0700); err != nil {
		t.Fatal("fresh native home required", err)
	}
	// Repository-local evidence must not make the model inherit repository
	// AGENTS.md, Git context or Skills. Use the same neutral CWD pattern as Q1.
	cwd, err := os.MkdirTemp("", "feat175-q2-workspace-")
	if err != nil {
		t.Fatal(err)
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	binary, manifest := integrationArtifactPaths(t)
	cfg := integrationConfig(binary, manifest, filepath.Join(cfgFile.Root, "runtime"), cfgFile.RuntimeBearer)
	cfg.ChatModelsEnabled, cfg.KimiK31Candidate, cfg.RuntimePermissionsEnabled, cfg.NativeToolsEnabled = true, true, true, true
	feat175Config(&cfg)
	cfg.KimiAPIKey, cfg.PermissionVerificationBaseURL = cfgFile.RuntimeBearer, cfgFile.Endpoint+"/v1"
	m := NewManager(cfg, nil)
	if err = m.SetBuiltinWebMCP(cfgFile.Endpoint+"/mcp", cfgFile.MCPToken,
		func(context.Context, json.RawMessage, wire.McpServerElicitationRequestParams) wire.McpServerElicitationRequestResponse {
			stop() // A Q2 read-only call requiring approval is preserved as a failed qualification.
			return wire.McpServerElicitationRequestResponse{Action: wire.Decline}
		}, nil); err != nil {
		t.Fatal(err)
	}
	notifications, capture := newIntegrationNotifications(), &q2Capture{}
	if err = m.SetNotificationHandler(func(method string, params json.RawMessage) {
		capture.handle(method, params)
		notifications.handle(method, params)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		if err := m.Shutdown(ctx); err != nil {
			_ = q2Write(filepath.Join(cfgFile.Root, "native-shutdown.json"), map[string]any{"state": "STOP_PENDING", "force_kill": false})
			t.Error("normal Runtime shutdown pending; evidence and processes retained")
			return
		}
		if err := q2Write(filepath.Join(cfgFile.Root, "native-shutdown.json"), map[string]any{"state": "stopped_normally", "force_kill": false}); err != nil {
			t.Error(err)
		}
		if err := os.RemoveAll(cwd); err != nil {
			t.Error("normal isolated workspace cleanup pending", err)
		}
	})
	if err = m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	observations := []any{}
	persist := func() {
		if err := q2Write(filepath.Join(cfgFile.Root, "evidence.json"), map[string]any{"schema_version": 1, "feature": "FEAT-175",
			"package_sha256": cfgFile.PackageSHA256, "kind": cfgFile.Kind, "observations": observations}); err != nil {
			t.Fatal(err)
		}
	}
	persist()
	for _, model := range plan.Models {
		for _, test := range plan.Cases {
			capture.mu.Lock()
			capture.events, capture.full = nil, false
			capture.mu.Unlock()
			ctx := WithPermissionMode(WithModelProfile(context.Background(), model.Profile), PermissionAsk)
			thread, err := m.StartThread(ctx, cwd)
			if err != nil {
				t.Fatal(err)
			}
			if err = control("arm", map[string]any{"model": model.Model, "case_id": test.ID, "thread_id": thread.ID}, nil); err != nil {
				t.Fatal(err)
			}
			input, _ := TextUserInput(test.Prompt)
			turn, err := m.StartTurnV2(ctx, thread.ID, []UserInput{input}, model.Effort)
			if err != nil {
				t.Fatal("native turn start failed", err)
			}
			status, waitErr := "unknown", error(nil)
			deadline := time.Now().Add(4 * time.Minute)
			for time.Now().Before(deadline) {
				status, _, waitErr = notifications.waitForTurn(turn.ID, 500*time.Millisecond)
				if waitErr == nil {
					break
				}
				var state struct {
					Halted bool `json:"halted"`
				}
				if control("status", map[string]any{}, &state) != nil || state.Halted {
					break
				}
			}
			if waitErr != nil {
				stop()
				interruptCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				_ = m.InterruptTurn(interruptCtx, thread.ID, turn.ID)
				cancel()
				status, _, _ = notifications.waitForTurn(turn.ID, 30*time.Second)
			}
			childrenComplete := true
			for childTurn, childThread := range capture.unfinished() {
				if _, _, err := notifications.waitForTurn(childTurn, 30*time.Second); err != nil {
					childrenComplete = false
					stop()
					interruptCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					_ = m.InterruptTurn(interruptCtx, childThread, childTurn)
					cancel()
				}
			}
			var settlement struct {
				Halted           bool             `json:"halted"`
				HostedVisibility string           `json:"hosted_visibility"`
				Requests         []map[string]any `json:"requests"`
			}
			finishErr := control("finish", map[string]any{}, &settlement)
			capture.mu.Lock()
			events, full := append([]q2Event(nil), capture.events...), capture.full
			capture.mu.Unlock()
			additional, ancestryOK := []any{}, true
			bindings := map[string]bool{thread.ID + "/" + turn.ID: true}
			for _, event := range events {
				var p struct {
					ThreadID string              `json:"threadId"`
					TurnID   string              `json:"turnId"`
					Turn     struct{ ID string } `json:"turn"`
				}
				if json.Unmarshal(event.Params, &p) != nil {
					ancestryOK = false
					continue
				}
				if p.TurnID == "" {
					p.TurnID = p.Turn.ID
				}
				key := p.ThreadID + "/" + p.TurnID
				if bindings[key] {
					continue
				}
				owner, err := m.nativeApprovalOwnerForTurn(context.Background(), p.ThreadID, p.TurnID, false)
				var history native.ThreadReadProjection
				if err != nil || owner != thread.ID || m.request(ctx, "thread/read", map[string]any{"threadId": p.ThreadID, "includeTurns": true}, &history) != nil || history.Thread.ParentThreadID == nil {
					ancestryOK = false
					continue
				}
				bindings[key] = true
				additional = append(additional, map[string]any{"thread_id": p.ThreadID, "turn_id": p.TurnID, "parent_thread_id": *history.Thread.ParentThreadID})
			}
			for _, row := range settlement.Requests {
				if !bindings[fmt.Sprint(row["thread_id"])+"/"+fmt.Sprint(row["turn_id"])] {
					ancestryOK = false
				}
			}
			visibility := settlement.HostedVisibility
			reconciled := q2HostedReconciled(events, settlement.Requests)
			if full || !ancestryOK || finishErr != nil || !childrenComplete || !reconciled {
				visibility = "unknown"
			}
			hash := sha256.Sum256([]byte(test.Prompt))
			observations = append(observations, map[string]any{"model": model.Model, "case_id": test.ID,
				"submitted_prompt_sha256": hex.EncodeToString(hash[:]), "thread_id": thread.ID, "turn_id": turn.ID,
				"hosted_visibility": visibility, "query_relevance": "not_reviewed", "events": events, "additional_turns": additional,
				"capture_complete": !full, "native_ancestry_verified": ancestryOK, "native_children_complete": childrenComplete,
				"hosted_inventory_reconciled": reconciled, "meter_settlement": settlement})
			persist()
			t.Logf("%s %s native_status=%s visibility=%s", model.Model, test.ID, status, visibility)
			if status != "completed" || waitErr != nil || settlement.Halted || finishErr != nil || visibility != "complete" {
				t.Fatal("Q2 stopped; original observations retained, no retry or replacement sample")
			}
		}
	}
}
