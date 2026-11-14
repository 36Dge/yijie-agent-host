package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	host "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	provider "github.com/36Dge/yijie-agent-host/internal/contracts/marketprovider"
	selection "github.com/36Dge/yijie-agent-host/internal/contracts/marketselection"
	"github.com/google/uuid"
)

// The real product worker and the same native features used by daily Desktop,
// with an ordinary local response. No connector is executed or supplier called.
func TestFEAT174DailyMarketNewResumeAndColdChat(t *testing.T) {
	manifest := os.Getenv("YIJIE_MARKET_BROKER_TEST_MANIFEST")
	if manifest == "" {
		t.Skip("requires the canonical product worker")
	}
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, _ map[string]any, _ int64) map[string]any {
		return feat164AuxText("Ordinary local Skill marketplace response.")
	}, toolSearchConfig, func(c *Config) {
		c.NativeSkillMarketplaceEnabled = true
		for _, name := range []string{"yijie-builtin", "yijie-user"} {
			root := filepath.Join(c.CodexHome, "ordinary-markets", name)
			if err := os.MkdirAll(filepath.Join(root, ".agents/plugins"), 0700); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(map[string]any{"name": name, "plugins": []any{}})
			if err := os.WriteFile(filepath.Join(root, ".agents/plugins/marketplace.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			c.SkillMarketplaceRoots = append(c.SkillMarketplaceRoots, root)
		}
		c.Market = MarketConfig{Manifest: manifest, LibraryHome: filepath.Join(c.CodexHome, "market-worker/library-home"), HostInstanceID: uuid.NewString()}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for _, root := range h.manager.config.SkillMarketplaceRoots {
		if err := h.manager.RegisterSkillMarketplace(ctx, root); err != nil {
			t.Fatal(err)
		}
	}
	h.manager.SetSkillSubmissionReady(true)
	id := func() broker.CanonicalId { return broker.CanonicalId(uuid.NewString()) }
	scope := broker.ScopeBinding{OwnerUserId: id(), TenantId: id(), NativeProcessEpoch: id(), AuthorizationRevision: 1, AuthorizationExpiresAtUnixMs: broker.UnixMilliseconds(time.Now().Add(3 * time.Minute).UnixMilli())}
	ref := broker.SelectionRef{InstallationId: id(), Revision: 1, Generation: 1}
	binding := provider.ProviderBinding{Reference: ref, ServiceId: "kimi-web", CredentialRef: id()}
	owner, err := h.manager.MarketOwner()
	if err != nil {
		t.Fatal(err)
	}
	payload := provider.ProviderPayload{OperationId: id(), HostInstanceId: broker.CanonicalId(h.manager.config.Market.HostInstanceID), Scope: scope, Binding: binding}
	for {
		probe, err := owner.Probe(ctx, provider.ProbeRequest{SchemaVersion: 1, RequestId: id(), Method: "provider_probe", Payload: payload})
		if err != nil {
			t.Fatal(err)
		}
		if probe.Data.OperationState != "starting" {
			if !probe.Data.ExecutionAvailable {
				t.Fatal("normal built-in metadata unavailable")
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
	sessionID := string(id())
	for index, selected := range []bool{false, true, false, false} {
		if index == 3 {
			cfg := h.manager.config
			h.restart(t, cfg)
			h.manager.SetSkillSubmissionReady(true)
		}
		refs := selection.Selection{}
		services := []host.ServiceBinding{}
		if selected {
			refs = append(refs, ref)
			services = append(services, binding)
		}
		op := id()
		sum, err := selection.SelectionDigest(string(op), refs)
		if err != nil {
			t.Fatal(err)
		}
		var block host.ContentBlock
		if err = json.Unmarshal([]byte(`{"type":"text","text":"FEAT174 ordinary daily chat"}`), &block); err != nil {
			t.Fatal(err)
		}
		submitted := host.Submission{TaskId: id(), LocalSessionId: id(), SubmissionOperationId: id(), Cwd: h.cwd, ContentBlocks: []host.ContentBlock{block}, Intent: host.ModelIntent{ProfileId: host.ProfileId(KimiK31ProfileID)}, PermissionMode: host.PermissionModeAsk, Snapshot: selection.SelectionSnapshot{SchemaVersion: 1, TurnOperationId: op, Selection: refs, SelectionDigest: selection.SelectionDigestValue(sum)}, Services: services}
		var previous *string
		if threadID != "" {
			previous = &threadID
		}
		prepared, err := h.manager.PrepareMarket(ctx, scope, submitted, sessionID, previous)
		if err != nil {
			t.Fatal("daily prepare", index, err)
		}
		selectedCtx := WithMarketPrepared(WithPermissionMode(WithModelProfile(ctx, KimiK31ProfileID), PermissionAsk), prepared)
		var thread ThreadInfo
		if previous == nil {
			thread, err = h.manager.StartThread(selectedCtx, h.cwd)
		} else {
			thread, err = h.manager.SwitchThread(selectedCtx, threadID)
		}
		if err != nil {
			t.Fatal("daily start/resume", index, err)
		}
		threadID = thread.ID
		input, _ := TextUserInput("FEAT174 ordinary daily chat")
		turn, err := h.manager.StartTurnV2(selectedCtx, threadID, []UserInput{input}, "max")
		if err != nil {
			t.Fatal(err)
		}
		status, _, err := h.notifications.waitForTurn(turn.ID, 20*time.Second)
		if err != nil || status != "completed" {
			t.Fatal(index, status, err)
		}
		if err = h.manager.DiscardMarketPrepared(ctx, prepared); err != nil {
			t.Fatal(err)
		}
		if _, err = h.manager.ClearMarketConfiguration(ctx, threadID, h.cwd); err != nil {
			t.Fatal("daily clear configuration", err)
		}
	}
	if h.calls.Load() != 4 {
		t.Fatal("unexpected local request count", h.calls.Load())
	}
	t.Log("four normal local completions: empty selection new chat; selected built-in connector without execution; cleared selection; normal cold resume; supplier calls = 0")
}
