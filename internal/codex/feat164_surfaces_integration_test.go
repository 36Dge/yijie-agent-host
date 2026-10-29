package codex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The real pinned Runtime reads an ordinary temporary note. Every model endpoint
// is the same loopback fixture. No executable substitutions or external tools.
func TestFEAT164CallSurfacesRuntime(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	root, err := os.MkdirTemp("", "feat164-surfaces-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	home, cwd := filepath.Join(root, "runtime"), filepath.Join(root, "workspace")
	for _, d := range []string{home, cwd} {
		if err = os.Mkdir(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(filepath.Join(cwd, "note.txt"), []byte("ordinary local reference 164\n"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	var surface atomic.Value
	surface.Store("ordinary")
	var toolSent atomic.Bool
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method != "POST" || r.URL.Path != "/v1/responses" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("unexpected loopback request")
			w.WriteHeader(400)
			return
		}
		n := requests.Add(1)
		if n > 6 {
			t.Error("extra model request")
			w.WriteHeader(429)
			return
		}
		reasoning, _ := body["reasoning"].(map[string]any)
		if body["model"] != "kimi-k3.1-pro" || reasoning["effort"] != "max" {
			t.Error("model/effort drift")
		}
		if _, ok := body["max_output_tokens"]; ok {
			t.Error("unexpected output token cap")
		}
		for _, v := range body["include"].([]any) {
			if v != "web_search_call.action.sources" && v != "web_search_call.results" {
				t.Error("unsupported Kimi include")
			}
		}
		name := surface.Load().(string)
		encoded, _ := json.Marshal(body["input"])
		output := "FEAT164 ordinary local response"
		switch name {
		case "ordinary":
			// FEAT-162: qualify the normal wire instructions with the real Runtime.
			// The observer must not remove native search to force a connector route.
			instructions, _ := body["instructions"].(string)
			for _, required := range []string{
				"Respect the user's active connector selection.",
				"use that connector's tools and follow their tool descriptions",
				"When no selected retrieval connector applies, use native web search",
			} {
				if !strings.Contains(instructions, required) || !strings.Contains(string(encoded), required) {
					t.Error("selected connector preference or native fallback lost from normal Runtime instructions")
				}
			}
			nativeSearch := false
			for _, raw := range body["tools"].([]any) {
				tool, _ := raw.(map[string]any)
				nativeSearch = nativeSearch || strings.HasPrefix(fmt.Sprint(tool["type"]), "web_search")
			}
			if !nativeSearch {
				t.Error("ordinary native search tool was removed")
			}
			var auxiliary []string
			for _, raw := range body["tools"].([]any) {
				tool, _ := raw.(map[string]any)
				name, _ := tool["name"].(string)
				if strings.Contains(name, "agent") || strings.Contains(name, "memory") {
					auxiliary = append(auxiliary, name)
				}
			}
			t.Logf("ordinary auxiliary tool projection: %v (not invoked)", auxiliary)
			if !strings.Contains(string(encoded), "input_image") || !strings.Contains(string(encoded), "ordinary attachment text") {
				t.Error("image/file input lost")
			}
		case "title":
			output = `{"title":"本机标题验证"}`
		case "draft":
			output = `{"schema_version":1,"kind":"needs_clarification","missing_fields":["content"],"question":"需要执行什么任务？"}`
			if body["tool_choice"] != "auto" || len(body["tools"].([]any)) != 0 {
				t.Error("draft empty tool surface/upstream choice lost")
			}
			text, _ := body["text"].(map[string]any)
			if text["format"] == nil {
				t.Error("draft schema lost")
			}
		case "expert":
			all, _ := json.Marshal(body)
			if !strings.Contains(string(all), "ordinary expert reference 164") {
				t.Error("expert instructions lost")
			}
			for _, raw := range body["tools"].([]any) {
				v, _ := raw.(map[string]any)
				if strings.HasPrefix(fmt.Sprint(v["type"]), "web_search") {
					t.Error("expert search isolation lost")
				}
			}
		case "continuation":
			if toolSent.Load() && (!strings.Contains(string(encoded), "function_call_output") || !strings.Contains(string(encoded), "ordinary local reference 164")) {
				t.Error("normal tool result not continued")
			}
		}
		id := fmt.Sprintf("feat164_surface_%d", n)
		item := map[string]any{"id": "message_" + id, "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": output, "annotations": []any{}}}}
		if name == "continuation" && !toolSent.Swap(true) {
			tool := ""
			for _, raw := range body["tools"].([]any) {
				v, _ := raw.(map[string]any)
				if v["name"] == "exec_command" {
					tool = "exec_command"
				}
			}
			if tool == "" {
				t.Error("normal command tool unavailable")
			}
			args, _ := json.Marshal(map[string]any{"cmd": "cat note.txt", "workdir": cwd, "max_output_tokens": 100})
			item = map[string]any{"id": "call_164", "type": "function_call", "call_id": "call_164", "name": tool, "arguments": string(args)}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []any{map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}}, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item}, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item}, map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}}} {
			raw, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", raw)
			w.(http.Flusher).Flush()
		}
	})}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	config := integrationConfig(binary, manifest, home, "ordinary-minimax-fixture")
	config.ChatModelsEnabled, config.KimiK31Candidate, config.RuntimePermissionsEnabled = true, true, true
	config.KimiAPIKey = "ordinary-kimi-fixture"
	config.PermissionVerificationBaseURL = "http://" + listener.Addr().String() + "/v1"
	manager := NewManager(config, nil)
	notifications := newIntegrationNotifications()
	if err = manager.SetNotificationHandler(notifications.handle); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if err := manager.Shutdown(ctx); err != nil {
			t.Error("normal cleanup pending; retained files", err)
		} else {
			_ = os.RemoveAll(root)
		}
	}()
	if err = manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	var effective struct {
		Config map[string]any `json:"config"`
	}
	if err = manager.request(context.Background(), "config/read", map[string]any{"includeLayers": false}, &effective); err != nil {
		t.Fatal(err)
	}
	features, _ := effective.Config["features"].(map[string]any)
	t.Logf("native config: multi_agent=%v memories=%v review_model=%v", features["multi_agent"], features["memories"], effective.Config["review_model"])
	catalog, err := kimiK31Catalog()
	if err != nil {
		t.Fatal(err)
	}
	var models struct {
		Models []map[string]any `json:"models"`
	}
	if err = json.Unmarshal(catalog, &models); err != nil {
		t.Fatal(err)
	}
	for _, model := range models.Models {
		if model["slug"] == "kimi-k3" || model["auto_review_model_override"] != nil || model["comp_hash"] != nil {
			t.Fatal("unexpected auxiliary model override")
		}
	}
	ctx := WithPermissionMode(WithModelProfile(context.Background(), KimiK31ProfileID), PermissionAsk)
	for _, name := range []string{"ordinary", "title", "draft", "expert", "continuation"} {
		surface.Store(name)
		if name == "title" {
			title, e := manager.GenerateTitle(ctx, "本机标题验证")
			if e != nil || title != "本机标题验证" {
				t.Fatal("title", title, e)
			}
			continue
		}
		selected := ctx
		if name == "draft" {
			selected = WithScheduledDraft(ctx, uuid.NewString())
		}
		if name == "expert" {
			selected, err = WithExpertInstructions(ctx, "Use ordinary expert reference 164.", "Local reference index.")
			if err != nil {
				t.Fatal(err)
			}
			servers, e := disabledMarketServers(nil, "http://"+listener.Addr().String())
			if e != nil {
				t.Fatal(e)
			}
			selected = WithMarketPrepared(selected, &MarketPrepared{Config: map[string]any{"mcp_servers": servers}})
		}
		thread, e := manager.StartThread(selected, cwd)
		if e != nil {
			t.Fatal(name, e)
		}
		if thread.Model != "kimi-k3.1-pro" || thread.ModelProvider != "kimi" || thread.ReasoningEffort != "max" {
			t.Fatal("effective identity", name)
		}
		text, _ := TextUserInput("Read the normal note if requested. ordinary attachment text")
		input := []UserInput{text}
		if name == "ordinary" {
			var pngBytes bytes.Buffer
			if e := png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 8, 8))); e != nil {
				t.Fatal(e)
			}
			pic, e := ImageUserInput("data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes.Bytes()))
			if e != nil {
				t.Fatal(e)
			}
			input = append(input, pic)
		}
		turn, e := manager.StartTurnV2(selected, thread.ID, input, "max")
		if e != nil {
			t.Fatal(name, e)
		}
		state, _, e := notifications.waitForTurn(turn.ID, 30*time.Second)
		if e != nil || state != "completed" {
			t.Fatal(name, state, e)
		}
		if name == "draft" {
			if _, e = manager.ResumeThread(selected, thread.ID); e != nil {
				t.Fatal("draft resume", e)
			}
		}
	}
	if requests.Load() != 6 {
		t.Fatal("loopback HTTP count", requests.Load())
	}
	t.Log("6 loopback Responses: image/text, title, input-only draft + resume, expert, normal local read + continuation; zero external HTTP")
}

func TestFEAT164DraftLoopbackAdmission(t *testing.T) {
	for _, value := range []string{"http://127.0.0.1:18083/v1", "http://127.0.0.1:19000/v1"} {
		if !draftVerificationLoopback(value) {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"http://localhost:18083/v1", "https://127.0.0.1:18083/v1", "http://192.0.2.1:18083/v1", "http://127.0.0.1/v1", "http://127.0.0.1:18083/v1?mode=1"} {
		if draftVerificationLoopback(value) {
			t.Fatal("unexpected endpoint admitted")
		}
	}
}

func TestFEAT164MarketCandidateAssembly(t *testing.T) {
	cfg := integrationConfig("/ordinary/codex", "/ordinary/runtime-manifest.json", integrationPrivateTempDir(t), "ordinary-fixture")
	cfg.ChatModelsEnabled, cfg.KimiK31Candidate, cfg.RuntimePermissionsEnabled = true, true, true
	cfg.Market = MarketConfig{Manifest: "/ordinary/worker-manifest.json", LibraryHome: t.TempDir(), HostInstanceID: uuid.NewString()}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	cfg.DynamicToolsEnabled = true
	if err := cfg.validate(); err == nil {
		t.Fatal("independent image service unexpectedly enabled")
	}
}
