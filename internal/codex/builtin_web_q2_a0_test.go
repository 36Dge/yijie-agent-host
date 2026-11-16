package codex

// Test-only native lifecycle bridge. No planner, fabricated native events,
// supplier client or product state is added.
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
	"sort"
	"sync"
	"testing"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
)

type q2A0Life struct {
	Thread, Turn, State, Status string
	Sequence                    int
}
type q2A0Owned struct {
	Root, Thread, Turn, Status string
	Ended                      bool
}
type q2A0Registry struct {
	mu       sync.Mutex
	events   []q2Event
	owners   map[string]q2A0Owned
	ended    map[string]string
	pending  int
	sequence int
	failed   bool
	bytes    int
	wake     chan q2A0Life
}

func (r *q2A0Registry) observe(method string, params json.RawMessage) {
	if method != "item/started" && method != "item/completed" && method != "turn/started" && method != "turn/completed" {
		return
	}
	var p struct {
		ThreadID string `json:"threadId"`
		Turn     struct{ ID, Status string }
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sequence++
	if len(r.events) >= 12000 || r.bytes+len(params) > 4*1024*1024 {
		r.failed = true
	} else {
		r.events = append(r.events, q2Event{method, append(json.RawMessage(nil), params...)})
		r.bytes += len(params)
	}
	if method != "turn/started" && method != "turn/completed" {
		return
	}
	if json.Unmarshal(params, &p) != nil || p.ThreadID == "" || p.Turn.ID == "" {
		r.failed = true
		return
	}
	state := "started"
	if method == "turn/completed" {
		state = "ended"
		r.ended[p.ThreadID+"/"+p.Turn.ID] = p.Turn.Status
	}
	r.pending++
	select {
	case r.wake <- q2A0Life{p.ThreadID, p.Turn.ID, state, p.Turn.Status, r.sequence}:
	default:
		r.pending--
		r.failed = true
	}
}
func (r *q2A0Registry) snapshot(root string) ([]q2Event, []q2A0Owned, int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	owned := []q2A0Owned{}
	events := []q2Event{}
	for _, b := range r.owners {
		if b.Root == root {
			owned = append(owned, b)
		}
	}
	for _, e := range r.events {
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Turn     struct{ ID string }
		}
		if json.Unmarshal(e.Params, &p) != nil {
			continue
		}
		if p.TurnID == "" {
			p.TurnID = p.Turn.ID
		}
		if b, ok := r.owners[p.ThreadID+"/"+p.TurnID]; ok && b.Root == root {
			events = append(events, e)
		}
	}
	return events, owned, r.pending, r.failed
}

func TestFEAT175Q2A0Native(t *testing.T) {
	file := os.Getenv("YIJIE_FEAT175_Q2_A0_CONFIG")
	if file == "" {
		t.Skip("explicit A0 controller required")
	}
	var run struct {
		Root        string `json:"root"`
		Endpoint    string `json:"endpoint"`
		Control     string `json:"control_token"`
		Bearer      string `json:"runtime_bearer"`
		MCP         string `json:"mcp_token"`
		Kind        string `json:"kind"`
		Generation  string `json:"generation"`
		PolicyHash  string `json:"policy_sha256"`
		PackageHash string `json:"package_sha256"`
		Tasks       int    `json:"tasks"`
		Scenario    string `json:"scenario"`
	}
	info, err := os.Lstat(file)
	if err != nil || !filepath.IsAbs(file) || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		t.Fatal("owned controller configuration required")
	}
	data, err := os.ReadFile(file)
	if err != nil || json.Unmarshal(data, &run) != nil {
		t.Fatal("invalid controller configuration")
	}
	u, err := url.Parse(run.Endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || filepath.Dir(file) != run.Root ||
		(run.Kind != "synthetic" && run.Kind != "recorded-native") || (run.Tasks != 4 && run.Tasks != 12) {
		t.Fatal("fixed controller scope required")
	}
	data, err = os.ReadFile("testdata/feat175-q2/cases.json")
	sum := sha256.Sum256(data)
	if err != nil || hex.EncodeToString(sum[:]) != run.PackageHash {
		t.Fatal("prompt freeze changed")
	}
	var plan struct {
		Models []struct{ Model, Profile, Effort string }
		Cases  []struct{ ID, Prompt string }
	}
	if json.Unmarshal(data, &plan) != nil {
		t.Fatal("invalid plan")
	}
	data, err = os.ReadFile("testdata/feat175-q2-a0/policy.json")
	sum = sha256.Sum256(data)
	if err != nil || hex.EncodeToString(sum[:]) != run.PolicyHash {
		t.Fatal("A0 policy changed")
	}
	var policy struct {
		Schedule []struct {
			Model  string `json:"model"`
			CaseID string `json:"case_id"`
		}
	}
	if json.Unmarshal(data, &policy) != nil || len(policy.Schedule) != 12 {
		t.Fatal("invalid schedule")
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
			return fmt.Errorf("controller unavailable")
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return fmt.Errorf("controller refusal %d", res.StatusCode)
		}
		if out != nil {
			return json.NewDecoder(res.Body).Decode(out)
		}
		return nil
	}
	stop := func() { _ = control("stop", map[string]any{}, nil) }
	defer stop()
	if err = os.Mkdir(filepath.Join(run.Root, "runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.MkdirTemp("", "feat175-a0-")
	if err != nil {
		t.Fatal(err)
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	binary, manifest := integrationArtifactPaths(t)
	cfg := integrationConfig(binary, manifest, filepath.Join(run.Root, "runtime"), run.Bearer)
	cfg.ChatModelsEnabled, cfg.KimiK31Candidate, cfg.RuntimePermissionsEnabled, cfg.NativeToolsEnabled = true, true, true, true
	feat175Config(&cfg)
	cfg.KimiAPIKey, cfg.PermissionVerificationBaseURL = run.Bearer, run.Endpoint+"/v1"
	m := NewManager(cfg, nil)
	if err = m.SetBuiltinWebMCP(run.Endpoint+"/mcp", run.MCP, func(context.Context, json.RawMessage, wire.McpServerElicitationRequestParams) wire.McpServerElicitationRequestResponse {
		stop()
		return wire.McpServerElicitationRequestResponse{Action: wire.Decline}
	}, nil); err != nil {
		t.Fatal(err)
	}
	notifications := newIntegrationNotifications()
	r := &q2A0Registry{owners: map[string]q2A0Owned{}, ended: map[string]string{}, wake: make(chan q2A0Life, 256)}
	if err = m.SetNotificationHandler(func(method string, p json.RawMessage) { r.observe(method, p); notifications.handle(method, p) }); err != nil {
		t.Fatal(err)
	}
	workerDone, workerExit := make(chan struct{}), make(chan struct{})
	// Native calls are NEVER made on the stdio notification reader.
	go func() {
		defer close(workerExit)
		for {
			select {
			case <-workerDone:
				return
			case life := <-r.wake:
				key := life.Thread + "/" + life.Turn
				r.mu.Lock()
				old, known := r.owners[key]
				terminal, ended := r.ended[key]
				r.mu.Unlock()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				owner := ""
				var e error
				if known {
					owner = old.Root
				} else {
					owner, e = m.nativeApprovalOwnerForTurn(ctx, life.Thread, life.Turn, life.State == "started" && !ended)
				}
				cancel()
				if e == nil && owner != "" {
					if run.Kind == "synthetic" && run.Scenario == "late-binding" && life.State == "started" && life.Thread != owner {
						time.Sleep(300 * time.Millisecond)
					}
					r.mu.Lock()
					terminal, ended = r.ended[key]
					binding := q2A0Owned{Root: owner, Thread: life.Thread, Turn: life.Turn, Ended: ended || life.State == "ended", Status: terminal}
					r.owners[key] = binding
					r.mu.Unlock()
					state := life.State
					if ended {
						state = "ended"
					}
					var answer struct {
						Accepted bool `json:"accepted"`
						Closed   bool `json:"closed"`
					}
					e = control("bind", map[string]any{"generation": run.Generation, "root_thread_id": owner, "thread_id": life.Thread, "turn_id": life.Turn, "state": state, "native_status": terminal, "native_sequence": life.Sequence}, &answer)
					if e == nil && life.State == "started" && !ended && !answer.Accepted {
						interruptCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
						_ = m.InterruptTurn(interruptCtx, life.Thread, life.Turn)
						c()
					}
				}
				r.mu.Lock()
				r.pending--
				if e != nil || owner == "" {
					r.failed = true
				}
				r.mu.Unlock()
				if e != nil || owner == "" {
					stop()
				}
			}
		}
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		err := m.Shutdown(ctx)
		close(workerDone)
		select {
		case <-workerExit:
		case <-ctx.Done():
			err = ctx.Err()
		}
		state := "stopped_normally"
		if err != nil {
			state = "STOP_PENDING"
			t.Error("normal shutdown pending; preserve evidence")
		}
		_ = q2Write(filepath.Join(run.Root, "native-shutdown.json"), map[string]any{"state": state, "force_kill": false})
		if err == nil {
			_ = os.RemoveAll(cwd)
		}
	})
	if err = m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	observations := []any{}
	envelope := func(observations []any) map[string]any {
		return map[string]any{"schema_version": 3, "feature": "FEAT-175", "kind": run.Kind, "mode": "first_action", "package_sha256": run.PackageHash, "policy_sha256": run.PolicyHash, "observations": observations}
	}
	for index, entry := range policy.Schedule[:run.Tasks] {
		var profile, effort, prompt string
		for _, model := range plan.Models {
			if model.Model == entry.Model {
				profile, effort = model.Profile, model.Effort
			}
		}
		for _, c := range plan.Cases {
			if c.ID == entry.CaseID {
				prompt = c.Prompt
			}
		}
		ctx := WithPermissionMode(WithModelProfile(context.Background(), profile), PermissionAsk)
		thread, e := m.StartThread(ctx, cwd)
		if e != nil {
			t.Fatal(e)
		}
		if e = control("arm", map[string]any{"model": entry.Model, "case_id": entry.CaseID, "thread_id": thread.ID}, nil); e != nil {
			t.Fatal(e)
		}
		input, _ := TextUserInput(prompt)
		turn, e := m.StartTurnV2(ctx, thread.ID, []UserInput{input}, effort)
		if e != nil {
			t.Fatal(e)
		}
		sampling := map[string]any{"stop_result": "not_requested"}
		sampled := false
		rootClosed := false
		status := "unknown"
		runFailed := false
		interruptOwned := func() {
			_, owned, _, _ := r.snapshot(thread.ID)
			sort.Slice(owned, func(i, j int) bool { return owned[i].Thread != thread.ID && owned[j].Thread == thread.ID })
			results := []any{}
			for _, b := range owned {
				if b.Ended {
					continue
				}
				c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				err := m.InterruptTurn(c, b.Thread, b.Turn)
				cancel()
				result := "returned"
				if err != nil {
					result = "control_error"
				}
				results = append(results, map[string]any{"thread_id": b.Thread, "turn_id": b.Turn, "result": result})
			}
			sampling["native_interrupts"] = results
		}
		deadline := time.Now().Add(180 * time.Second)
		for time.Now().Before(deadline) {
			events, owned, pending, failed := r.snapshot(thread.ID)
			capture := &q2Capture{events: events}
			first := q2V2First(capture)
			if first != nil && !sampled {
				first["root_thread_id"] = thread.ID
				var boundary map[string]any
				if control("sample", first, &boundary) != nil {
					runFailed = true
					stop()
					break
				}
				sampled, rootClosed = true, true
				sampling["trigger"], sampling["boundary"] = first, boundary
				interruptOwned()
				if control("interrupt-returned", first, nil) != nil {
					runFailed = true
					stop()
					break
				}
			}
			active := 0
			rootEnded := false
			for _, b := range owned {
				if !b.Ended {
					active++
				}
				if b.Thread == thread.ID && b.Turn == turn.ID && b.Ended {
					rootEnded = true
					status = b.Status
				}
			}
			var state struct {
				Halted bool `json:"halted"`
			}
			if control("status", map[string]any{}, &state) != nil || state.Halted || failed {
				runFailed = true
				interruptOwned()
				break
			}
			if rootEnded && active == 0 && pending == 0 {
				if !rootClosed {
					if control("close-root", map[string]any{"root_thread_id": thread.ID, "reason": "natural_terminal"}, nil) != nil {
						runFailed = true
					}
					rootClosed = true
				}
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !rootClosed {
			_ = control("close-root", map[string]any{"root_thread_id": thread.ID, "reason": "observation_deadline"}, nil)
			rootClosed = true
			runFailed = true
			interruptOwned()
		}
		// Fence the same native connection, drain its lifecycle queue, and keep
		// closed-root tombstones for any later events throughout the whole run.
		barrierOK := false
		barrier := map[string]any{}
		until := time.Now().Add(40 * time.Second)
		for time.Now().Before(until) {
			_, owned, pending, failed := r.snapshot(thread.ID)
			active := 0
			for _, b := range owned {
				if !b.Ended {
					active++
				}
				if b.Thread == thread.ID && b.Turn == turn.ID && b.Ended {
					status = b.Status
				}
			}
			if active == 0 && pending == 0 && !failed {
				fenceCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
				_, err := m.ReadThread(fenceCtx, thread.ID)
				c()
				_, after, pendingAfter, failedAfter := r.snapshot(thread.ID)
				activeAfter := 0
				for _, b := range after {
					if !b.Ended {
						activeAfter++
					}
				}
				if err == nil && activeAfter == 0 && pendingAfter == 0 && !failedAfter {
					barrierOK = true
					barrier = map[string]any{"active": 0, "pending": 0, "native_fence": true}
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !barrierOK {
			runFailed = true
			stop()
			barrier = map[string]any{"active": -1, "pending": -1, "native_fence": false}
		}
		// Control RPCs stay short. Waiting for the already-sent provider stream
		// is a separate bounded phase and never starts another model request.
		settled := false
		settlementDeadline := time.Now().Add(65 * time.Second)
		for time.Now().Before(settlementDeadline) {
			var state struct {
				Settled bool `json:"settled"`
			}
			if control("settlement-status", map[string]any{}, &state) != nil {
				break
			}
			if state.Settled {
				settled = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !settled {
			runFailed = true
			stop()
		}
		var settlement map[string]json.RawMessage
		finishErr := control("finish", map[string]any{}, &settlement)
		var rows []map[string]any
		_ = json.Unmarshal(settlement["requests"], &rows)
		events, _, _, failed := r.snapshot(thread.ID)
		additional, ancestry := q2V2Bindings(m, thread.ID, turn.ID, events, rows)
		// Bindings may have ended without any model dispatch; retain those turns too.
		if sampled {
			sampling["stop_result"] = "unqualified_stop"
			if status == "interrupted" {
				sampling["stop_result"] = "interrupted"
			} else if status == "completed" {
				sampling["stop_result"] = "natural_completion_race"
			}
		}
		hash := sha256.Sum256([]byte(prompt))
		o := map[string]any{"model": entry.Model, "case_id": entry.CaseID, "thread_id": thread.ID, "turn_id": turn.ID, "submitted_prompt_sha256": hex.EncodeToString(hash[:]),
			"events": events, "additional_turns": additional, "sampling": sampling, "capture_complete": !failed, "native_ancestry_verified": ancestry,
			"native_children_complete": barrierOK, "native_barrier": barrier, "hosted_visibility": "unknown", "query_relevance": "not_reviewed", "meter_settlement": settlement}
		observations = append(observations, o)
		if err = q2Write(filepath.Join(run.Root, fmt.Sprintf("case-%02d.json", index)), envelope([]any{o})); err != nil {
			t.Fatal(err)
		}
		if err = q2Write(filepath.Join(run.Root, "evidence.json"), envelope(observations)); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s %s terminal=%s sampled=%v barrier=%v", entry.Model, entry.CaseID, status, sampled, barrierOK)
		if runFailed || finishErr != nil || !barrierOK || !ancestry {
			t.Fatal("A0 stopped; native evidence retained")
		}
		for {
			var review struct {
				State string `json:"state"`
			}
			if e = control("review", map[string]any{"index": index}, &review); e != nil {
				t.Fatal(e)
			}
			if review.State == "accepted" {
				break
			}
			if review.State != "review_required" {
				t.Fatal("review did not authorize next task")
			}
			time.Sleep(time.Second)
		}
	}
}
