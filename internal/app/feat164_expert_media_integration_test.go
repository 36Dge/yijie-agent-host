package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	expert "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	market "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	selection "github.com/36Dge/yijie-agent-host/internal/contracts/marketselection"
	"github.com/36Dge/yijie-agent-host/internal/security"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/36Dge/yijie-agent-host/internal/skills"
	"github.com/google/uuid"
)

// Real resource service, domain grant/submit, Worker, Runtime, MCP adapter and
// authenticated Host media route. The only model endpoint is a loopback fixture.
func TestFEAT164ExpertMediaCanonicalRuntime(t *testing.T) {
	for _, media := range []bool{false, true} {
		name := "experts"
		if media {
			name = "media"
		}
		t.Run(name, func(t *testing.T) { runFEAT164ExpertMedia(t, media) })
	}
}
func runFEAT164ExpertMedia(t *testing.T, media bool) {
	expectedRequests := int64(1)
	if media {
		expectedRequests = 3
	}
	require := func(key string) string {
		v := os.Getenv(key)
		if v == "" {
			t.Fatal("explicit local artifact required", key)
		}
		return v
	}
	binary, manifest := require("YIJIE_CODEX_INTEGRATION_BINARY"), require("YIJIE_CODEX_INTEGRATION_MANIFEST")
	worker, bundle, adapterBinary := require("YIJIE_MARKET_BROKER_TEST_MANIFEST"), require("YIJIE_FEAT164_EXPERT_BUNDLE"), require("YIJIE_FEAT164_ARTIFACT_ADAPTER")
	root, err := os.MkdirTemp("", "feat164-expert-media-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"runtime", "host", "workspace"} {
		if err = os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	cwd := filepath.Join(root, "workspace")
	files := map[string][]byte{"report.html": []byte("<!doctype html><html><meta charset=utf-8><h1>普通本机报告</h1></html>"), "drawing.svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="80" height="60"><rect width="80" height="60" fill="teal"/></svg>`)}
	for name, data := range files {
		if err = os.WriteFile(filepath.Join(cwd, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var requests atomic.Int64
	providerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	provider := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method != "POST" || r.URL.Path != "/v1/responses" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("unexpected model route")
			w.WriteHeader(400)
			return
		}
		n := requests.Add(1)
		reasoning, _ := body["reasoning"].(map[string]any)
		if n > expectedRequests || body["model"] != "kimi-k3.1-pro" || reasoning["effort"] != "max" {
			t.Error("unexpected model/count")
			w.WriteHeader(400)
			return
		}
		tools, _ := body["tools"].([]any)
		toolName, toolNamespace := "", ""
		for _, raw := range tools {
			v, _ := raw.(map[string]any)
			if strings.Contains(fmt.Sprint(v["type"]), "web_search") {
				t.Error("expert search unexpectedly enabled")
			}
			name, _ := v["name"].(string)
			if strings.Contains(name, "publish_artifact") {
				toolName = name
			}
			if nested, ok := v["tools"].([]any); ok {
				for _, rawTool := range nested {
					tool, _ := rawTool.(map[string]any)
					if tool["name"] == "publish_artifact" {
						toolName = "publish_artifact"
						toolNamespace = name
					}
				}
			}
		}
		if (toolName != "") != media {
			t.Error("media tool/profile mismatch")
		}
		item := map[string]any{"id": fmt.Sprintf("message_%d", n), "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "FEAT164 本机专家与产物登记完成。", "annotations": []any{}}}}
		if media && (n == 1 || n == 2) {
			name := "report.html"
			if n == 2 {
				name = "drawing.svg"
			}
			args, _ := json.Marshal(map[string]any{"relative_path": name, "display_name": name})
			item = map[string]any{"id": fmt.Sprintf("item_%d", n), "type": "function_call", "call_id": fmt.Sprintf("call_%d", n), "name": toolName, "arguments": string(args)}
			if toolNamespace != "" {
				item["namespace"] = toolNamespace
			}
		}
		if media && n >= 2 {
			input, _ := json.Marshal(body["input"])
			if !bytes.Contains(input, []byte("staged")) {
				t.Error("real media receipt missing from continuation")
			}
		}
		id := fmt.Sprintf("response_%d", n)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []any{map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}}, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item}, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item}, map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}}} {
			b, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", b)
			w.(http.Flusher).Flush()
		}
	})}
	go func() { _ = provider.Serve(providerListener) }()
	config := codex.DefaultConfig()
	config.BinaryPath = binary
	config.ManifestPath = manifest
	config.CodexHome = filepath.Join(root, "runtime")
	config.StartupTimeout = 30 * time.Second
	config.RequestTimeout = 30 * time.Second
	config.ShutdownTimeout = 25 * time.Second
	config.ChatModelsEnabled, config.KimiK31Candidate, config.RuntimePermissionsEnabled = true, true, true
	config.MiniMax = codex.MiniMaxConfig{Enabled: true, APIKey: "ordinary-loopback-minimax"}
	config.KimiAPIKey = "ordinary-loopback-kimi"
	config.PermissionVerificationBaseURL = "http://" + providerListener.Addr().String() + "/v1"
	instance := uuid.NewString()
	config.Market = codex.MarketConfig{Manifest: worker, LibraryHome: filepath.Join(root, "host", "market-worker", "library-home"), HostInstanceID: instance}
	runtime := codex.NewManager(config, nil)
	var adapter *artifact.Adapter
	var server *http.Server
	var resources *skills.Service
	var store *session.Store
	var spool *artifact.Store
	defer func() {
		clean := true
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		if e := runtime.Shutdown(ctx); e != nil {
			t.Error("Runtime normal cleanup pending", e)
			clean = false
		}
		if adapter != nil {
			if e := adapter.Stop(ctx); e != nil {
				t.Error("adapter normal cleanup pending", e)
				clean = false
			}
		}
		for _, httpServer := range []*http.Server{server, provider} {
			if httpServer != nil {
				if e := httpServer.Shutdown(ctx); e != nil {
					t.Error(e)
					clean = false
				}
			}
		}
		if clean {
			if resources != nil {
				_ = resources.Close()
			}
			if spool != nil {
				_ = spool.Close()
			}
			if store != nil {
				_ = store.Close()
			}
			_ = os.RemoveAll(root)
		}
	}()
	resources, err = skills.NewService(skills.Config{BundleRoot: bundle, ManagedRoot: filepath.Join(root, "installed"), Runtime: runtime, ExpertCatalogDigest: skills.ExpertCatalogDigestForProfile(true)})
	if err != nil {
		t.Fatal(err)
	}
	store, err = session.OpenStore(filepath.Join(root, "host"), session.WithExpertStorage(), session.WithChatModelV2Storage())
	if err != nil {
		t.Fatal(err)
	}
	spool, err = artifact.OpenStore(filepath.Join(root, "host", "artifact-spool"), artifact.Options{})
	if err != nil {
		t.Fatal(err)
	}
	service := session.NewService(runtime, store, session.NewEventHub(128, 64), nil, session.WithMarketAuthority(instance), session.WithExpertResources(resources), session.WithV3Artifacts(session.NewEventHubVersion(3, 64, 32), spool, false), session.WithArtifactMedia())
	done := make(chan string, 8)
	if err = runtime.SetNotificationHandler(func(method string, raw json.RawMessage) {
		service.HandleNotification(method, raw)
		if method == "turn/completed" {
			var v struct {
				Turn struct {
					Status string `json:"status"`
				} `json:"turn"`
			}
			if json.Unmarshal(raw, &v) == nil {
				done <- v.Turn.Status
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	token, err := security.LoadOrCreateAPIToken(filepath.Join(root, "host"))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	appConfig := Config{Environment: "local", Runtime: config, HostHome: filepath.Join(root, "host"), InstanceNonce: instance, ExpertsEnabled: true, ArtifactV3Enabled: true, ArtifactMediaEnabled: media, MultimodalV2Enabled: true}
	appConfig.ParentPID = os.Getppid()
	appConfig.Skills = SkillFeatureConfig{BundleRoot: bundle, InstallRoot: filepath.Join(root, "installed"), Read: true, Manage: true}
	t.Setenv("YIJIE_ENV", "local")
	t.Setenv("YIJIE_LOCAL_PROFILE", "demo_fast")
	t.Setenv("YIJIE_EXPERTS_ENABLED", "true")
	t.Setenv("YIJIE_AGENT_HOST_INSTANCE_NONCE", instance)
	if err = loadExpertProfile(&appConfig); err != nil {
		t.Fatal("canonical expert profile", err)
	}
	server = &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: NewHandler(appConfig, runtime, service, token, WithSkillService(resources))}
	go func() { _ = server.Serve(listener) }()
	if media {
		adapter, err = artifact.StartAdapter(adapterBinary, "http://"+listener.Addr().String(), token)
		if err != nil {
			t.Fatal(err)
		}
		if err = runtime.SetArtifactMCP(adapter.URL, adapter.Token); err != nil {
			t.Fatal(err)
		}
	}

	if err = runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(bundle, "bundle-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Skills []skills.ManifestSkill `json:"skills"`
	}
	if err = json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	var ref *expert.ExpertRef
	for _, item := range catalog.Skills {
		if item.Expert == nil || item.Expert.ExpertId != "jd-ops-expert" {
			continue
		}
		if _, err = resources.Install(context.Background(), skills.InstallInput{OperationID: uuid.NewString(), SkillID: item.ID, ExpectedVersion: item.Version, ExpectedArchiveSHA256: item.Archive.SHA256, CatalogRevision: skills.ExpertCatalogDigestForProfile(true)}); err != nil {
			t.Fatal(err)
		}
		digest, e := expert.KnowledgeManifestDigest(item.Expert.KnowledgeResources)
		if e != nil {
			t.Fatal(e)
		}
		ref = &expert.ExpertRef{ExpertId: expert.ExpertId(item.Expert.ExpertId), Version: expert.Version(item.Version), ArchiveSha256: expert.Digest(item.Archive.SHA256), RoleSha256: item.Expert.RoleResource.Sha256, KnowledgeManifestSha256: expert.Digest(digest)}
		break
	}
	if ref == nil {
		t.Fatal("reviewed expert missing")
	}
	id := func() market.CanonicalId { return market.CanonicalId(uuid.NewString()) }
	scope := market.ScopeBinding{OwnerUserId: id(), TenantId: id(), NativeProcessEpoch: id(), AuthorizationRevision: 1, AuthorizationExpiresAtUnixMs: broker.UnixMilliseconds(time.Now().Add(2 * time.Minute).UnixMilli())}
	if _, err = service.MarketHello(market.HelloPayload{HostInstanceId: market.CanonicalId(instance), OwnerUserId: scope.OwnerUserId, TenantId: scope.TenantId, NativeProcessEpoch: scope.NativeProcessEpoch}); err != nil {
		t.Fatal(err)
	}
	name := "expert"
	if media {
		name = "media"
	}
	{
		op := id()
		refs := selection.Selection{}
		digest, e := selection.SelectionDigest(string(op), refs)
		if e != nil {
			t.Fatal(e)
		}
		var block market.ContentBlock
		if e = json.Unmarshal([]byte(`{"type":"text","text":"仅整理本机参考；需要时登记当前目录中已存在的普通报告。"}`), &block); e != nil {
			t.Fatal(e)
		}
		submission := market.Submission{TaskId: id(), LocalSessionId: id(), SubmissionOperationId: id(), Cwd: cwd, ContentBlocks: []market.ContentBlock{block}, Intent: market.ModelIntent{ProfileId: codex.KimiK31ProfileID}, PermissionMode: market.PermissionModeAsk, Snapshot: selection.SelectionSnapshot{SchemaVersion: 1, TurnOperationId: op, Selection: refs, SelectionDigest: selection.SelectionDigestValue(digest)}, Services: []market.ServiceBinding{}}
		snapshot, e := expert.FreezeSnapshot(op, 1, ref)
		if e != nil {
			t.Fatal(e)
		}
		grant, e := service.RegisterExpertGrant(expert.PrivateGrantPayload{OperationId: id(), HostInstanceId: market.CanonicalId(instance), Scope: scope, Submission: expert.PrivateSubmission{Submission: submission, ExpertSnapshot: snapshot}})
		if e != nil {
			t.Fatal(e)
		}
		trigger := market.SubmitPayload{GrantRef: grant.Grant.GrantRef, TurnOperationId: op}
		receipt, e := service.SubmitExpert(context.Background(), trigger)
		if e != nil {
			t.Fatal(name, e)
		}
		if receipt.Receipt.ProfileId != codex.KimiK31ProfileID || receipt.Receipt.NativeTurnId == nil {
			t.Fatal("identity/turn missing")
		}
		select {
		case status := <-done:
			if status != "completed" {
				t.Fatal(name, status)
			}
		case <-time.After(35 * time.Second):
			t.Fatal(name, "normal turn timeout")
		}
		again, e := service.SubmitExpert(context.Background(), trigger)
		if e != nil || again.Receipt != receipt.Receipt {
			a, _ := json.Marshal(again)
			b, _ := json.Marshal(receipt)
			if e != nil || !bytes.Equal(a, b) {
				t.Fatal("receipt replay", e)
			}
		}
		if name == "media" {
			_, events, _, cancel, e := service.SubscribeMediaEvents(string(*receipt.Receipt.AgentSessionId), "", 0)
			if e != nil {
				t.Fatal(e)
			}
			cancel()
			completed := 0
			for _, event := range events {
				if event.EventType != session.EventItemArtifactCompleted || event.Payload.Status != "ready" {
					continue
				}
				completed++
				resource, e := service.ReadArtifact(string(*receipt.Receipt.AgentSessionId), event.Payload.ArtifactID, session.ArtifactContent)
				if e != nil || !bytes.Equal(resource.Bytes, files[event.Payload.DisplayName]) {
					t.Fatal("registered resource differs", e)
				}
			}
			if completed != 2 {
				t.Fatal("media completion count", completed)
			}
		}
	}
	if requests.Load() != expectedRequests {
		t.Fatal("model loopback count", requests.Load())
	}
	t.Logf("%d loopback Responses; media=%v; real expert install/grant/submit; canonical Worker; source-owned adapter when media enabled; receipt replay sends0; normal EOF cleanup", requests.Load(), media)
}
