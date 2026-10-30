package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	host "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	provider "github.com/36Dge/yijie-agent-host/internal/contracts/marketprovider"
	selection "github.com/36Dge/yijie-agent-host/internal/contracts/marketselection"
	"github.com/google/uuid"
)

func TestFEAT162SelectedRetrievalConfig(t *testing.T) {
	for _, model := range []string{KimiK31ProfileID, "minimax-m3-high-v1", "kimi-k3-max-v1"} {
		for _, service := range []string{"", "kimi-web", "ordinary-service"} {
			v := host.Submission{Intent: host.ModelIntent{ProfileId: host.ProfileId(model)}}
			if service != "" {
				v.Services = []host.ServiceBinding{{ServiceId: provider.ServiceId(service)}}
			}
			config := map[string]any{"ordinary": true}
			applySelectedRetrievalConfig(config, v)
			_, disabled := config["web_search"]
			if disabled != (model == KimiK31ProfileID && service == "kimi-web") || config["ordinary"] != true {
				t.Fatalf("route changed: model=%s service=%s", model, service)
			}
		}
	}
}

// Actual pinned Runtime + canonical worker; synthetic local credential, no
// supplier endpoint or tool execution. The loopback model always ends normally.
func TestFEAT162SelectedRetrievalRuntime(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	workerManifest := os.Getenv("YIJIE_MARKET_BROKER_TEST_MANIFEST")
	if workerManifest == "" {
		t.Skip("canonical worker manifest required")
	}
	root := integrationPrivateTempDir(t)
	cwd := filepath.Join(root, "workspace")
	for _, name := range []string{cwd, filepath.Join(root, "runtime"), filepath.Join(root, "host-home")} {
		if err := os.Mkdir(name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	var requests atomic.Int64
	var expectSelected atomic.Bool
	var expectNative atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method != "POST" || r.URL.Path != "/v1/responses" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("unexpected local request")
			w.WriteHeader(400)
			return
		}
		n := requests.Add(1)
		if n > 6 {
			t.Error("unexpected retry")
			w.WriteHeader(429)
			return
		}
		native := false
		tools := ""
		for _, raw := range body["tools"].([]any) {
			v := raw.(map[string]any)
			native = native || strings.HasPrefix(fmt.Sprint(v["type"]), "web_search")
			encoded, _ := json.Marshal(v)
			tools += string(encoded)
		}
		if native != expectNative.Load() || body["tool_choice"] != "auto" {
			t.Error("native routing / auto choice mismatch")
		}
		for _, name := range []string{"kimi_web_search", "kimi_web_search_pro", "kimi_web_fetch"} {
			if strings.Contains(tools, name+"\"") != expectSelected.Load() {
				t.Errorf("tool discovery mismatch: %s", name)
			}
		}
		id := fmt.Sprintf("local_route_%d", n)
		item := map[string]any{"id": "message_" + id, "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "ordinary local response", "annotations": []any{}}}}
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
	}))
	defer server.Close()
	c := integrationConfig(binary, manifest, filepath.Join(root, "runtime"), "ordinary-local-minimax")
	c.ChatModelsEnabled, c.KimiK31Candidate, c.RuntimePermissionsEnabled = true, true, true
	c.KimiAPIKey, c.PermissionVerificationBaseURL = "ordinary-local-kimi", server.URL+"/v1"
	c.Market = MarketConfig{Manifest: workerManifest, LibraryHome: filepath.Join(root, "host-home/market-worker/library-home"), HostInstanceID: uuid.NewString()}
	m := NewManager(c, nil)
	notifications := newIntegrationNotifications()
	if err := m.SetNotificationHandler(notifications.handle); err != nil {
		t.Fatal(err)
	}
	defer shutdownIntegrationManager(t, m, 25*time.Second)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	id := func() broker.CanonicalId { return broker.CanonicalId(uuid.NewString()) }
	scope := broker.ScopeBinding{OwnerUserId: id(), TenantId: id(), NativeProcessEpoch: id(), AuthorizationRevision: 1, AuthorizationExpiresAtUnixMs: broker.UnixMilliseconds(time.Now().Add(3 * time.Minute).UnixMilli())}
	ref := broker.SelectionRef{InstallationId: id(), Revision: 1, Generation: 1}
	binding := provider.ProviderBinding{Reference: ref, ServiceId: "kimi-web", CredentialRef: id()}
	owner, err := m.MarketOwner()
	if err != nil {
		t.Fatal(err)
	}
	payload := provider.ProviderPayload{OperationId: id(), HostInstanceId: broker.CanonicalId(c.Market.HostInstanceID), Scope: scope, Binding: binding}
	for {
		probe, e := owner.Probe(ctx, provider.ProbeRequest{SchemaVersion: 1, RequestId: id(), Method: "provider_probe", Payload: payload})
		if e != nil {
			t.Fatal(e)
		}
		if probe.Data.OperationState != "starting" {
			if !probe.Data.ExecutionAvailable || probe.Data.Qualification != "qualified" {
				t.Fatal("ordinary local preparation unavailable")
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	threadID := ""
	for _, step := range []struct {
		name, model      string
		selected, native bool
	}{
		{"selected-new", KimiK31ProfileID, true, false}, {"selected-resume", KimiK31ProfileID, true, false},
		{"cleared", KimiK31ProfileID, false, true}, {"selected-again", KimiK31ProfileID, true, false},
		{"model-switch", "minimax-m3-high-v1", false, true}, {"back-to-kimi", KimiK31ProfileID, false, true},
	} {
		refs := selection.Selection{}
		services := []host.ServiceBinding{}
		if step.selected {
			refs = append(refs, ref)
			services = append(services, binding)
		}
		op := id()
		digest, e := selection.SelectionDigest(string(op), refs)
		if e != nil {
			t.Fatal(e)
		}
		var block host.ContentBlock
		if e = json.Unmarshal([]byte(`{"type":"text","text":"ordinary local message"}`), &block); e != nil {
			t.Fatal(e)
		}
		v := host.Submission{TaskId: id(), LocalSessionId: id(), SubmissionOperationId: id(), Cwd: cwd, ContentBlocks: []host.ContentBlock{block}, Intent: host.ModelIntent{ProfileId: host.ProfileId(step.model)}, PermissionMode: host.PermissionModeAsk, Snapshot: selection.SelectionSnapshot{SchemaVersion: 1, TurnOperationId: op, Selection: refs, SelectionDigest: selection.SelectionDigestValue(digest)}, Services: services}
		var old *string
		if threadID != "" {
			old = &threadID
		}
		prepared, e := m.PrepareMarket(ctx, scope, v, string(id()), old)
		if e != nil {
			t.Fatal(step.name, e)
		}
		selected := WithMarketPrepared(WithPermissionMode(WithModelProfile(ctx, step.model), PermissionAsk), prepared)
		var thread ThreadInfo
		if old == nil {
			thread, e = m.StartThread(selected, cwd)
		} else {
			thread, e = m.SwitchThread(selected, threadID)
		}
		if e != nil {
			t.Fatal(step.name, e)
		}
		threadID = thread.ID
		expectSelected.Store(step.selected)
		expectNative.Store(step.native)
		input, _ := TextUserInput("ordinary local routing check")
		profile, _ := ModelProfile(step.model)
		turn, e := m.StartTurnV2(selected, thread.ID, []UserInput{input}, profile.Effort)
		if e != nil {
			t.Fatal(step.name, e)
		}
		state, _, e := notifications.waitForTurn(turn.ID, 20*time.Second)
		if e != nil || state != "completed" {
			t.Fatal(step.name, state, e)
		}
		if e = m.DiscardMarketPrepared(ctx, prepared); e != nil {
			t.Fatal(e)
		}
		if step.selected {
			// A normal stale revision is rejected at preparation, before a turn.
			v.Services[0].Reference.Revision++
			v.Snapshot.Selection[0].Revision++
			v.Snapshot.TurnOperationId = id()
			d, _ := selection.SelectionDigest(string(v.Snapshot.TurnOperationId), v.Snapshot.Selection)
			v.Snapshot.SelectionDigest = selection.SelectionDigestValue(d)
			if p, e := m.PrepareMarket(ctx, scope, v, string(id()), &threadID); e == nil || p != nil {
				t.Fatal("stale selection admitted")
			}
		}
	}
	if requests.Load() != 6 {
		t.Fatal("loopback request count", requests.Load())
	}
	t.Log("6 loopback model requests; real worker three-tool discovery; start/resume/clear/model switch; stale refs rejected; 0 supplier calls")
}
