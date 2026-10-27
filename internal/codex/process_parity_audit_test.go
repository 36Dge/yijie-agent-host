package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise actual Manager entry points over the existing in-process RPC harness.
// No substituted executable, provider, network service or paid request.
func TestFEAT163StartResumeInstructionParity(t *testing.T) {
	for _, profile := range []string{"kimi-k3-max-v1", "minimax-m3-high-v1"} {
		for _, entry := range []struct {
			name                  string
			image, market, expert bool
		}{
			{"ordinary", false, false, false}, {"image", true, false, false},
			{"connector", false, true, false}, {"image-and-connector", true, true, false},
			{"expert", false, false, true},
		} {
			t.Run(profile+"/"+entry.name, func(t *testing.T) {
				h := newFEAT137WireHarness(t, false, entry.image)
				h.manager.config.RuntimePermissionsEnabled = true
				h.manager.config.ChatModelsEnabled = true
				h.manager.config.KimiAPIKey = "ordinary-fixture-not-a-credential"
				ctx := WithModelProfile(context.Background(), profile)
				selected, _ := ModelProfile(profile)
				if entry.market {
					ctx = WithMarketPrepared(ctx, &MarketPrepared{Config: map[string]any{"web_search": "disabled"}})
				}
				if entry.expert {
					var err error
					ctx, err = WithExpertInstructions(ctx, "Ordinary local reference role.", "Read the relevant local reference.")
					if err != nil {
						t.Fatal(err)
					}
				}
				captured := []map[string]any{}
				for _, method := range []string{RuntimeMethodThreadStart, RuntimeMethodThreadResume} {
					done := make(chan error, 1)
					cwd := t.TempDir()
					go func() {
						var err error
						if method == RuntimeMethodThreadStart {
							_, err = h.manager.StartThread(ctx, cwd)
						} else {
							_, err = h.manager.ResumeThread(ctx, "parity-thread")
						}
						done <- err
					}()
					request := h.nextRequest(t)
					var params map[string]any
					if request.Method != method || json.Unmarshal(request.Params, &params) != nil {
						t.Fatal("unexpected RPC")
					}
					captured = append(captured, params)
					response, _ := json.Marshal(map[string]any{"thread": map[string]any{"id": "parity-thread", "sessionId": "parity-session"}, "model": selected.Model, "modelProvider": selected.Provider, "reasoningEffort": selected.Effort})
					h.responses <- response
					if err := <-done; err != nil {
						t.Fatal(err)
					}
				}
				for _, field := range []string{"baseInstructions", "developerInstructions", "approvalPolicy", "sandbox"} {
					if captured[0][field] != captured[1][field] {
						t.Errorf("%s changes on ordinary resume", field)
					}
				}
				for _, p := range captured {
					if !strings.HasPrefix(p["baseInstructions"].(string), permissionInstructions) {
						t.Error("shared native communication baseline lost")
					}
					if entry.image && strings.Contains(p["developerInstructions"].(string), "It is the only permitted tool.") {
						t.Error("dynamic tool guidance disabled ordinary native tools")
					}
					if entry.image && !strings.Contains(p["developerInstructions"].(string), "Never call it for ordinary image viewing or analysis") {
						t.Error("image intent boundary lost")
					}
					if entry.market && p["config"].(map[string]any)["web_search"] != "disabled" {
						t.Error("tool configuration changed")
					}
					if entry.expert && !strings.Contains(p["baseInstructions"].(string), "Ordinary local reference role.") {
						t.Error("expert context lost")
					}
				}
			})
		}
	}

}

// Upgrade only a known prior managed catalog, then verify a second ordinary open.
func TestFEAT163ReauditCatalogUpgrade(t *testing.T) {
	for _, instructions := range []string{previousFEAT163PermissionInstructions, previousFEAT163ReauditInstructions} {
		home := canonicalOwnedTempDir(t)
		authority := acquireTestCodexHomeAuthority(t, home)
		previous, err := chatModelCatalogInstructions(false, instructions)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(home, chatModelsCatalogName), previous, 0600); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if _, err = prepareChatModelLayer(authority); err != nil {
				t.Fatal(err)
			}
			if err = validateChatModelLayer(authority); err != nil {
				t.Fatal(err)
			}
		}
	}
}
