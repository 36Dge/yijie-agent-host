package codex

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Ordinary in-process RPC fixtures: no executable, process or Provider call.
func TestChatModelSwitchAfterTerminalErrorRetainsNativeLifecycle(t *testing.T) {
	for _, state := range []string{"idle", "notLoaded", "systemError", "active", "futureStatus"} {
		t.Run(state, func(t *testing.T) {
			requests, input := io.Pipe()
			output, responses := io.Pipe()
			client := NewClient(input, output, 1<<20, 8, nil, nil)
			client.Start()
			t.Cleanup(func() { _ = client.CloseInput(); _ = responses.Close(); _ = requests.Close(); _ = output.Close() })
			config := DefaultConfig()
			config.ChatModelsEnabled = true
			config.KimiAPIKey = "ordinary-fixture-not-a-credential"
			config.RuntimePermissionsEnabled = true
			manager := NewManager(config, nil)
			manager.client = client
			manager.status.Ready, manager.status.State = true, StateReady
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			methods := make(chan string, 8)
			go func() {
				decoder := json.NewDecoder(requests)
				for {
					var request struct {
						ID     json.RawMessage `json:"id"`
						Method string          `json:"method"`
						Params map[string]any  `json:"params"`
					}
					if decoder.Decode(&request) != nil {
						return
					}
					methods <- request.Method
					result := map[string]any{}
					switch request.Method {
					case "thread/read":
						if request.Params["includeTurns"] != false || request.Params["threadId"] != "ordinary-thread" {
							return
						}
						result["thread"] = map[string]any{"id": "ordinary-thread", "status": map[string]any{"type": state}}
					case RuntimeMethodThreadResume:
						if request.Params["threadId"] != "ordinary-thread" || request.Params["model"] != "kimi-k3" || request.Params["modelProvider"] != "kimi" || request.Params["approvalPolicy"] != "never" || request.Params["sandbox"] != "read-only" {
							return
						}
						result = map[string]any{"thread": map[string]any{"id": "ordinary-thread", "sessionId": "ordinary-session"}, "model": "kimi-k3", "modelProvider": "kimi", "reasoningEffort": "max"}
					case RuntimeMethodThreadUnsubscribe:
						if request.Params["threadId"] != "ordinary-thread" {
							return
						}
					default:
						return
					}
					if json.NewEncoder(responses).Encode(map[string]any{"id": request.ID, "result": result}) != nil {
						return
					}
				}
			}()
			thread, err := manager.SwitchThread(WithModelProfile(ctx, "kimi-k3-max-v1"), "ordinary-thread")
			var observed []string
			for len(methods) > 0 {
				observed = append(observed, <-methods)
			}
			if state == "active" || state == "futureStatus" {
				if err == nil || !reflect.DeepEqual(observed, []string{"thread/read"}) {
					t.Fatalf("unavailable thread mutated: %v %v", observed, err)
				}
				return
			}
			if err != nil || thread.ID != "ordinary-thread" || thread.ReasoningEffort != "max" || !reflect.DeepEqual(observed, []string{"thread/read", RuntimeMethodThreadUnsubscribe, RuntimeMethodThreadResume}) {
				t.Fatalf("terminal thread did not switch without a new turn: %v %v", observed, err)
			}
		})
	}
}

func TestChatModelCatalogKeepsLegacyEffortsAndExactKimiMax(t *testing.T) {
	old, _ := miniMaxModelCatalog()
	next, err := chatModelCatalog()
	if err != nil {
		t.Fatal(err)
	}
	var a, b map[string]any
	_ = json.Unmarshal(old, &a)
	_ = json.Unmarshal(next, &b)
	legacy := a["models"].([]any)[0].(map[string]any)
	for _, raw := range b["models"].([]any) {
		p := raw.(map[string]any)
		switch p["slug"] {
		case MiniMaxModel:
			if !reflect.DeepEqual(p["supported_reasoning_levels"], legacy["supported_reasoning_levels"]) || p["base_instructions"] != legacy["base_instructions"] {
				t.Fatal("legacy model semantics changed")
			}
		case "kimi-k3":
			if p["default_reasoning_level"] != "max" {
				t.Fatal("Kimi effort differs")
			}
		}
	}
}
func TestChatModelVerificationCoversBothProvidersAndCredentialsAreScoped(t *testing.T) {
	c := DefaultConfig()
	c.ChatModelsEnabled = true
	c.RuntimePermissionsEnabled = true
	c.PermissionVerificationBaseURL = "http://127.0.0.1:18083/v1"
	overrides := NewManager(c, nil).verificationProviderConfig()
	for _, p := range []string{"minimax", "kimi"} {
		if overrides["model_providers."+p+".base_url"] != c.PermissionVerificationBaseURL {
			t.Fatal("uncapped verification route")
		}
	}
	env := runtimeEnvironment([]string{"KIMI_API_KEY=declared-fixture", "MOONSHOT_API_KEY=declared-fixture", "YIJIE_KIMI_API_KEY_FILE=/declared-fixture", "PATH=/usr/bin"}, "/declared-home", "", false)
	for _, v := range env {
		if strings.Contains(v, "KIMI") || strings.Contains(v, "MOONSHOT") {
			t.Fatal("ambient provider credential inherited")
		}
	}
}
