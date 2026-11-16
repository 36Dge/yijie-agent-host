package codex

// Q2-v2 is local-only QA. It reuses Manager, the canonical native events,
// ancestry resolution, turn/interrupt and the frozen v1 capture helpers.
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

func q2V2First(c *q2Capture) map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, event := range c.events {
		if event.Method != "item/started" {
			continue
		}
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Item     struct{ ID, Type string }
		}
		if json.Unmarshal(event.Params, &p) == nil && (p.Item.Type == "webSearch" || p.Item.Type == "mcpToolCall") {
			return map[string]string{"thread_id": p.ThreadID, "turn_id": p.TurnID, "item_id": p.Item.ID}
		}
	}
	return nil
}

func q2V2Bindings(m *Manager, root, turn string, events []q2Event, rows []map[string]any) ([]any, bool) {
	additional := []any{}
	bindings := map[string]bool{root + "/" + turn: true}
	for _, event := range events {
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Turn     struct{ ID string }
		}
		if json.Unmarshal(event.Params, &p) != nil {
			return additional, false
		}
		if p.TurnID == "" {
			p.TurnID = p.Turn.ID
		}
		key := p.ThreadID + "/" + p.TurnID
		if bindings[key] {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		owner, err := m.nativeApprovalOwnerForTurn(ctx, p.ThreadID, p.TurnID, false)
		var history native.ThreadReadProjection
		if err != nil || owner != root || m.request(ctx, "thread/read", map[string]any{"threadId": p.ThreadID, "includeTurns": true}, &history) != nil || history.Thread.ParentThreadID == nil {
			cancel()
			return additional, false
		}
		cancel()
		bindings[key] = true
		additional = append(additional, map[string]any{"thread_id": p.ThreadID, "turn_id": p.TurnID, "parent_thread_id": *history.Thread.ParentThreadID})
	}
	for _, row := range rows {
		if !bindings[fmt.Sprint(row["thread_id"])+"/"+fmt.Sprint(row["turn_id"])] {
			return additional, false
		}
	}
	return additional, true
}

func TestFEAT175Q2V2Native(t *testing.T) {
	file := os.Getenv("YIJIE_FEAT175_Q2_V2_RUN_CONFIG")
	if file == "" {
		t.Skip("local-only Q2-v2 run configuration required")
	}
	var run struct {
		Root          string `json:"root"`
		Endpoint      string `json:"endpoint"`
		Control       string `json:"control_token"`
		RuntimeBearer string `json:"runtime_bearer"`
		MCPToken      string `json:"mcp_token"`
		Kind          string `json:"kind"`
		Mode          string `json:"mode"`
		PackageHash   string `json:"package_sha256"`
		SamplingHash  string `json:"sampling_sha256"`
	}
	info, err := os.Lstat(file)
	if err != nil || !filepath.IsAbs(file) || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		t.Fatal("owned configuration required")
	}
	data, err := os.ReadFile(file)
	if err != nil || json.Unmarshal(data, &run) != nil {
		t.Fatal("invalid local run configuration")
	}
	u, err := url.Parse(run.Endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" ||
		run.Kind != "synthetic" || (run.Mode != "first_action" && run.Mode != "full_turn") || !filepath.IsAbs(run.Root) || filepath.Dir(file) != run.Root {
		t.Fatal("only local synthetic Q2-v2 is executable")
	}
	data, err = os.ReadFile("testdata/feat175-q2/cases.json")
	sum := sha256.Sum256(data)
	if err != nil || hex.EncodeToString(sum[:]) != run.PackageHash {
		t.Fatal("frozen natural prompts changed")
	}
	var plan struct {
		Models []struct{ Model, Profile, Effort string }
		Cases  []struct{ ID, Prompt string }
	}
	if json.Unmarshal(data, &plan) != nil || len(plan.Models) != 2 || len(plan.Cases) != 6 {
		t.Fatal("invalid frozen plan")
	}
	policy, err := os.ReadFile("testdata/feat175-q2-v2/sampling.json")
	sum = sha256.Sum256(policy)
	if err != nil || hex.EncodeToString(sum[:]) != run.SamplingHash {
		t.Fatal("sampling policy differs")
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	control := func(action string, value any, out any) error {
		body, _ := json.Marshal(value)
		req, e := http.NewRequest(http.MethodPost, run.Endpoint+"/control/"+action, bytes.NewReader(body))
		if e != nil {
			return e
		}
		req.Header.Set("X-FEAT175-Control", run.Control)
		req.Header.Set("Content-Type", "application/json")
		res, e := client.Do(req)
		if e != nil {
			return fmt.Errorf("local control unavailable")
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return fmt.Errorf("local control stopped, status %d", res.StatusCode)
		}
		if out != nil {
			return json.NewDecoder(res.Body).Decode(out)
		}
		return nil
	}
	stop := func() { _ = control("stop", map[string]any{}, nil) }
	defer stop()
	if err = os.Mkdir(filepath.Join(run.Root, "runtime"), 0700); err != nil {
		t.Fatal("fresh Runtime home required", err)
	}
	cwd, err := os.MkdirTemp("", "feat175-q2-v2-")
	if err != nil {
		t.Fatal(err)
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	binary, manifest := integrationArtifactPaths(t)
	cfg := integrationConfig(binary, manifest, filepath.Join(run.Root, "runtime"), run.RuntimeBearer)
	cfg.ChatModelsEnabled, cfg.KimiK31Candidate, cfg.RuntimePermissionsEnabled, cfg.NativeToolsEnabled = true, true, true, true
	feat175Config(&cfg)
	cfg.KimiAPIKey, cfg.PermissionVerificationBaseURL = run.RuntimeBearer, run.Endpoint+"/v1"
	m := NewManager(cfg, nil)
	if err = m.SetBuiltinWebMCP(run.Endpoint+"/mcp", run.MCPToken, func(context.Context, json.RawMessage, wire.McpServerElicitationRequestParams) wire.McpServerElicitationRequestResponse {
		stop()
		return wire.McpServerElicitationRequestResponse{Action: wire.Decline}
	}, nil); err != nil {
		t.Fatal(err)
	}
	notifications := newIntegrationNotifications()
	var scopeMu sync.Mutex
	var capture *q2Capture
	if err = m.SetNotificationHandler(func(method string, params json.RawMessage) {
		scopeMu.Lock()
		current := capture
		scopeMu.Unlock()
		if current != nil {
			current.handle(method, params)
		}
		notifications.handle(method, params)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		if err := m.Shutdown(ctx); err != nil {
			_ = q2Write(filepath.Join(run.Root, "native-shutdown.json"), map[string]any{"state": "STOP_PENDING", "force_kill": false})
			t.Error("normal shutdown pending; evidence retained")
			return
		}
		if err := q2Write(filepath.Join(run.Root, "native-shutdown.json"), map[string]any{"state": "stopped_normally", "force_kill": false}); err != nil {
			t.Error(err)
		}
		if err := os.RemoveAll(cwd); err != nil {
			t.Error("isolated workspace cleanup pending", err)
		}
	})
	if err = m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	observations := []any{}
	persist := func() {
		if err := q2Write(filepath.Join(run.Root, "evidence.json"), map[string]any{"schema_version": 2, "feature": "FEAT-175", "package_sha256": run.PackageHash, "sampling_sha256": run.SamplingHash, "kind": "synthetic", "mode": run.Mode, "observations": observations}); err != nil {
			t.Fatal(err)
		}
	}
	persist()
	for _, model := range plan.Models {
		for _, test := range plan.Cases {
			current := &q2Capture{}
			scopeMu.Lock()
			capture = current
			scopeMu.Unlock()
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
				t.Fatal(err)
			}
			sampling := map[string]any{"mode": run.Mode, "stop_result": "not_requested"}
			sampled := false
			status := "unknown"
			var waitErr error
			sampleFirst := func() error {
				first := q2V2First(current)
				if first == nil || sampled || run.Mode != "first_action" {
					return nil
				}
				owner, err := m.nativeApprovalOwnerForTurn(ctx, first["thread_id"], first["turn_id"], false)
				if err != nil || owner != thread.ID {
					return fmt.Errorf("native sample ancestry unavailable")
				}
				first["root_thread_id"] = thread.ID
				var boundary map[string]any
				if err = control("sample", first, &boundary); err != nil {
					return err
				}
				sampled = true
				sampling["trigger"], sampling["boundary"] = first, boundary
				interrupts := []any{}
				pending := current.unfinished()
				for childTurn, childThread := range pending {
					interruptCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					owner, e := m.nativeApprovalOwnerForTurn(interruptCtx, childThread, childTurn, false)
					if e != nil || owner != thread.ID {
						cancel()
						return fmt.Errorf("native stop ownership unavailable")
					}
					e = m.InterruptTurn(interruptCtx, childThread, childTurn)
					cancel()
					result := "returned"
					if e != nil {
						result = "control_error"
					}
					interrupts = append(interrupts, map[string]any{"thread_id": childThread, "turn_id": childTurn, "result": result})
				}
				sampling["native_interrupts"] = interrupts
				return control("interrupt-returned", first, nil)
			}
			deadline := time.Now().Add(60 * time.Second)
			for time.Now().Before(deadline) {
				if err = sampleFirst(); err != nil {
					waitErr = err
					break
				}
				status, _, waitErr = notifications.waitForTurn(turn.ID, 25*time.Millisecond)
				if waitErr == nil {
					if err = sampleFirst(); err != nil {
						waitErr = err
					}
					break
				}
				var meterState struct {
					Halted bool `json:"halted"`
				}
				if control("status", map[string]any{}, &meterState) != nil || meterState.Halted {
					break
				}
			}
			if waitErr != nil {
				stop()
				interruptCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				_ = m.InterruptTurn(interruptCtx, thread.ID, turn.ID)
				cancel()
				status, _, _ = notifications.waitForTurn(turn.ID, 20*time.Second)
			}
			childrenComplete := true
			for childTurn, childThread := range current.unfinished() {
				if _, _, err := notifications.waitForTurn(childTurn, 10*time.Second); err != nil {
					childrenComplete = false
					stop()
					interruptCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					_ = m.InterruptTurn(interruptCtx, childThread, childTurn)
					cancel()
				}
			}
			if sampled {
				sampling["stop_result"] = "unqualified_stop"
				if status == "interrupted" {
					sampling["stop_result"] = "interrupted"
				} else if status == "completed" {
					sampling["stop_result"] = "natural_completion_race"
				}
			}
			var settlement struct {
				Halted           bool             `json:"halted"`
				HostedVisibility string           `json:"hosted_visibility"`
				Requests         []map[string]any `json:"requests"`
				Task             map[string]any   `json:"task"`
				Diagnostics      []any            `json:"diagnostics"`
				Journal          []any            `json:"journal"`
			}
			finishErr := control("finish", map[string]any{}, &settlement)
			current.mu.Lock()
			events, full := append([]q2Event(nil), current.events...), current.full
			current.mu.Unlock()
			additional, ancestryOK := q2V2Bindings(m, thread.ID, turn.ID, events, settlement.Requests)
			reconciled := q2HostedReconciled(events, settlement.Requests)
			visibility := settlement.HostedVisibility
			if full || !ancestryOK || !childrenComplete || !reconciled || finishErr != nil {
				visibility = "unknown"
			}
			hash := sha256.Sum256([]byte(test.Prompt))
			observations = append(observations, map[string]any{"model": model.Model, "case_id": test.ID, "submitted_prompt_sha256": hex.EncodeToString(hash[:]), "thread_id": thread.ID, "turn_id": turn.ID,
				"hosted_visibility": visibility, "query_relevance": "not_reviewed", "events": events, "additional_turns": additional, "sampling": sampling,
				"capture_complete": !full, "native_ancestry_verified": ancestryOK, "native_children_complete": childrenComplete, "hosted_inventory_reconciled": reconciled, "meter_settlement": settlement})
			persist()
			t.Logf("%s %s mode=%s native_status=%s sample=%v visibility=%s", model.Model, test.ID, run.Mode, status, sampling["stop_result"], visibility)
			validEnd := status == "completed" || (run.Mode == "first_action" && sampled && status == "interrupted")
			if !validEnd || waitErr != nil || settlement.Halted || finishErr != nil || visibility != "complete" {
				t.Fatal("local Q2-v2 stopped; original events and diagnostics retained")
			}
		}
	}
}
