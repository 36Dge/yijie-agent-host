package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
	"github.com/google/uuid"
)

// Actual fixed Runtime + actual canonical shared worker. The ordinary local
// eligibility callback declines; there is no supplier/model paid request.
func TestFEAT175SharedWorkerBuiltinDiscovery(t *testing.T) {
	manifest := os.Getenv("FEAT175_PRODUCT_WORKER_MANIFEST")
	if manifest == "" {
		t.Skip("canonical new product worker required")
	}
	for _, profile := range []string{KimiK31ProfileID, "minimax-m3-high-v1"} {
		t.Run(profile, func(t *testing.T) {
			var probes atomic.Int32
			admission := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var q wire.WebAdmissionRequest
				if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("b", 64) || json.NewDecoder(r.Body).Decode(&q) != nil || q.ThreadId == "" || q.TurnId == "" {
					t.Error("worker admission identity unavailable")
				}
				probes.Add(1)
				w.WriteHeader(403)
			}))
			defer admission.Close()
			h := newNativeToolsHarnessWithSetup(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
				if n == 1 {
					var developer strings.Builder
					items, _ := body["input"].([]any)
					for _, raw := range items {
						item, _ := raw.(map[string]any)
						if item["role"] == "developer" {
							content, _ := json.Marshal(item["content"])
							developer.Write(content)
						}
					}
					for _, rule := range []string{
						"choose exposed retrieval according to current tool descriptions",
						"built-in web tools do not require connector selection",
						"Returned search metadata can support a list of sources to read later",
						"Apply requested length and format to the entire final answer",
					} {
						if !strings.Contains(developer.String(), rule) {
							t.Errorf("first model request lost retrieval policy: %s", rule)
						}
					}
					if strings.Contains(developer.String(), "Otherwise use exposed native web retrieval") {
						t.Error("first model request retained obsolete connector-only fallback")
					}
				}
				if profile == KimiK31ProfileID && n == 1 {
					return feat164AuxTool("", "tool_search", "builtin-discovery", map[string]any{"query": "web research source collection", "limit": 3})
				}
				step := n
				if profile == KimiK31ProfileID {
					step--
				}
				if step == 1 {
					definitions := body["tools"]
					if profile == KimiK31ProfileID {
						var discoveryJSON string
						if err := json.Unmarshal([]byte(nativeToolsOutput(body, "builtin-discovery")), &discoveryJSON); err != nil {
							t.Fatal("native builtin discovery content is invalid", err)
						}
						var discovery map[string]any
						if err := json.Unmarshal([]byte(discoveryJSON), &discovery); err != nil {
							t.Fatal("native builtin discovery output is invalid", err)
						}
						definitions = discovery["tools"]
					}
					encoded, err := json.Marshal(definitions)
					if err != nil {
						t.Fatal(err)
					}
					var namespaces []struct {
						Type, Name, Description string
						Tools                   []struct{ Name, Description string }
					}
					if err = json.Unmarshal(encoded, &namespaces); err != nil {
						t.Fatal(err)
					}
					guided := false
					visible := make(map[string]string)
					for _, namespace := range namespaces {
						if namespace.Type == "namespace" && namespace.Name == "mcp__yijie_web" {
							guided = namespace.Description == wire.ServerInstructions
							for _, tool := range namespace.Tools {
								visible[tool.Name] = tool.Description
							}
						} else if namespace.Type == "function" && strings.HasPrefix(namespace.Name, "mcp__yijie_web__") {
							visible[strings.TrimPrefix(namespace.Name, "mcp__yijie_web__")] = namespace.Description
						}
					}
					// Kimi consumes the native namespace; MiniMax's native model
					// profile flattens it into functions, retaining tool descriptions.
					if profile == KimiK31ProfileID && !guided {
						t.Error("native model-facing namespace lost the source retrieval guidance")
					}
					var sourceTools []struct{ Name, Description string }
					if err = json.Unmarshal(wire.Tools, &sourceTools); err != nil {
						t.Fatal(err)
					}
					for _, tool := range sourceTools {
						if !strings.HasPrefix(visible[tool.Name], tool.Description) {
							t.Errorf("native tool description missing source guidance: %s", tool.Name)
						}
						for _, rule := range []string{"needs no connector selection", "specific missing part of the requested deliverable", "user's full output limit"} {
							if !strings.Contains(visible[tool.Name], rule) {
								t.Errorf("native function lost shared completion rule: %s: %s", tool.Name, rule)
							}
						}
					}
					for _, rule := range []string{"call kimi_web_search (Basic)", "does not replace this Basic step", "enough relevant titles, URLs and snippets satisfy a metadata-only list", "Do not run Pro or Fetch merely to verify those links"} {
						if !strings.Contains(visible["kimi_web_search"], rule) {
							t.Errorf("native Basic function lost collection rule: %s", rule)
						}
					}
					if !strings.Contains(visible["kimi_web_fetch"], "Changing only #fragment is not pagination") {
						t.Error("native Fetch function lost same-page reuse rule")
					}
					// Check the model-facing Pro function itself: MiniMax has no
					// namespace description to supply these required shared rules.
					for _, rule := range []string{
						"call kimi_web_search_pro before answering",
						"does not replace this Pro step or its filters",
						"Reuse sufficient Pro evidence",
					} {
						if !strings.Contains(visible["kimi_web_search_pro"], rule) {
							t.Errorf("native Pro function lost shared research rule: %s", rule)
						}
					}
					if profile == KimiK31ProfileID {
						if !strings.Contains(nativeToolsOutput(body, "builtin-discovery"), "mcp__yijie_web__kimi_web_search") {
							t.Error("shared builtin tools missing without selection")
						}
						return feat164AuxTool("", "exec", "builtin-call", map[string]any{"input": `text(await tools.mcp__yijie_web__kimi_web_search({query:"Python asyncio 入门教程",limit:3,include_content:false}));`})
					}
					return feat164AuxTool("", "mcp__yijie_web__kimi_web_search", "builtin-call", map[string]any{"query": "Python asyncio 入门教程", "limit": 3, "include_content": false})
				}
				if !strings.Contains(nativeToolsOutput(body, "builtin-call"), "not_entitled") {
					t.Error("worker did not retain the local admission refusal")
				}
				return feat164AuxText("Local shared worker protocol complete.")
			}, func(m *Manager) {
				if err := m.ConfigureBuiltinWeb("https://api.example", admission.URL, strings.Repeat("b", 64), nil); err != nil {
					t.Fatal(err)
				}
			}, func(c *Config) {
				feat175Config(c)
				root, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				root = filepath.Join(root, "host-home")
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
				c.Market = MarketConfig{Manifest: manifest, LibraryHome: filepath.Join(root, "market-worker", "library-home"), HostInstanceID: uuid.NewString()}
			})
			install := wire.WebCapabilityInstall{Method: "web_capability_install", ProcessEpoch: uuid.New(), BindingEpoch: uuid.New(), Delegation: wire.Delegation{BindingId: uuid.New(), GrantId: uuid.New(), Bearer: strings.Repeat("a", 64), Audience: "builtin-web-retrieval/1", AuthorizationRevision: 1, CredentialRevision: 1, ExpiresAtUnixMs: int(time.Now().Add(299 * time.Second).UnixMilli())}}
			raw, _ := json.Marshal(install)
			ack, err := h.manager.BuiltinWebControl(context.Background(), "WebCapabilityInstall", raw)
			if err != nil || ack.State != "applied" {
				t.Fatal("native-owned builtin install failed", err)
			}
			ctx := WithPermissionMode(WithModelProfile(context.Background(), profile), PermissionAsk)
			thread, err := h.manager.StartThread(ctx, h.cwd)
			if err != nil {
				t.Fatal(err)
			}
			// The model response above is scripted: this proves actual first-request
			// exposure and policy projection, not real-model natural routing.
			h.turn(t, profile, thread.ID, "请采集三条 asyncio 入门资料，只列标题、网址和一句摘要，供我之后挑选阅读。")
			if probes.Load() != 1 {
				t.Fatal("unexpected local admission count", probes.Load())
			}
			want := int64(2)
			if profile == KimiK31ProfileID {
				want = 3
			}
			if h.calls.Load() != want {
				t.Fatal("unexpected local model request count", h.calls.Load())
			}
			t.Logf("loopback_model_http=%d local_admissions=%d supplier_rest=0", h.calls.Load(), probes.Load())
		})
	}
}
