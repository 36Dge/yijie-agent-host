package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Normal Responses streams through the exact managed Runtime. These are protocol
// checks, not supplier generation evals. No remote provider, altered executable,
// forced stop, permissions fault or simulated tool result is used.
func TestPublicProgressNativeMessagesAndContinuation(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	message := func(id, phase, text string) map[string]any {
		item := map[string]any{"type": "message", "id": id, "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}}
		if phase != "" {
			item["phase"] = phase
		}
		return item
	}
	opening := message("progress_opening", "commentary", "我会先区分范围，再整理这份说明。")
	interim := message("progress_interim", "commentary", "已明确两种评价范围，后续将分别说明。")
	answer := message("progress_answer", "final_answer", "结果：城市介绍需要分别考虑短期游览与长期生活。")
	declaredOpening := message("progress_opening", "", "[[yijie.public-message.v1:commentary]]\n我会先区分范围，再整理这份说明。")
	declaredInterim := message("progress_interim", "", "[[yijie.public-message.v1:commentary]]\n已明确两种评价范围，后续将分别说明。")
	declaredAnswer := message("progress_answer", "", "[[yijie.public-message.v1:final_answer]]\n结果：城市介绍需要分别考虑短期游览与长期生活。")
	declaredViews := map[string]map[string]any{"progress_opening": opening, "progress_interim": interim, "progress_answer": answer}

	command := map[string]any{"type": "function_call", "id": "progress_command", "call_id": "progress_call", "name": "exec_command", "arguments": `{"cmd":"/bin/pwd","max_output_tokens":1000}`}
	type response struct {
		items        []map[string]any
		continueTurn bool
	}
	for _, test := range []struct {
		name      string
		responses []response
		commands  int
		recovery  bool
		declared  bool
		cancel    bool
	}{
		{"same_response", []response{{items: []map[string]any{opening, interim, answer}}}, 0, false, false, false},
		{"native_no_tool_continuation", []response{{[]map[string]any{opening}, true}, {[]map[string]any{interim}, true}, {items: []map[string]any{answer}}}, 0, false, false, false},
		{"tool_then_native_continuation", []response{{items: []map[string]any{opening, command}}, {[]map[string]any{interim}, true}, {items: []map[string]any{answer}}}, 1, false, false, false},
		{"unknown_phase", []response{{items: []map[string]any{message("progress_opening", "", "我会区分范围。"), message("progress_interim", "", "已明确两种范围。"), message("progress_answer", "", "结果：分别评价两种范围。")}}}, 0, false, false, false},
		{"existing_premature_completion_guard", []response{{items: []map[string]any{opening}}, {items: []map[string]any{answer}}}, 0, true, false, false},
		{"declared_no_tool_continuation", []response{{items: []map[string]any{declaredOpening}}, {items: []map[string]any{declaredInterim}}, {items: []map[string]any{declaredAnswer}}}, 0, false, true, false},
		{"declared_same_response", []response{{items: []map[string]any{declaredOpening, declaredInterim, declaredAnswer}}}, 0, false, true, false},
		{"declared_tool_then_continuation", []response{{items: []map[string]any{declaredOpening, command}}, {items: []map[string]any{declaredInterim}}, {items: []map[string]any{declaredAnswer}}}, 1, false, true, false},
		{"declared_direct_final", []response{{items: []map[string]any{declaredAnswer}}}, 0, false, true, false},
		{"declared_cancel_before_completion", []response{{items: []map[string]any{declaredOpening}}}, 0, false, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, err := os.MkdirTemp("", "yijie-public-progress-")
			if err != nil {
				t.Fatal(err)
			}
			root, err = filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			home, cwd := filepath.Join(root, "runtime"), filepath.Join(root, "workspace")
			for _, path := range []string{home, cwd} {
				if err = os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			var mu sync.Mutex
			var bodies []map[string]any
			var methods []string
			var completed []map[string]any
			var started []map[string]any
			var streamedText string
			var warningText string
			commands := 0
			finishResponse := make(chan struct{})
			// This fixture owns a finite response source. Native turn cancellation
			// is not a promise that a supplier acknowledges TCP cancellation.
			// Finish the ordinary response before graceful fixture teardown.
			defer close(finishResponse)
			openingDeltaSeen := make(chan struct{})
			var openingOnce sync.Once
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if r.Method != "POST" || r.URL.Path != "/v1/responses" || json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Error("unexpected normal loopback request")
					w.WriteHeader(400)
					return
				}
				// Finish consuming the ordinary request body before waiting on a
				// response stream; net/http can then observe peer cancellation.
				if err := r.Body.Close(); err != nil {
					t.Error("normal loopback request-body close", err)
					return
				}
				mu.Lock()
				bodies = append(bodies, body)
				number := len(bodies)
				mu.Unlock()
				if number > len(test.responses) {
					t.Error("unexpected continuation")
					w.WriteHeader(429)
					return
				}
				part := test.responses[number-1]
				id := fmt.Sprintf("progress_response_%d", number)
				w.Header().Set("Content-Type", "text/event-stream")
				emit := func(value any) {
					raw, _ := json.Marshal(value)
					fmt.Fprintf(w, "data: %s\n\n", raw)
					w.(http.Flusher).Flush()
				}
				emit(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
				for index, item := range part.items {
					start := make(map[string]any, len(item))
					for k, v := range item {
						start[k] = v
					}
					if item["type"] == "message" {
						start["content"] = []any{}
						start["status"] = "in_progress"
					}
					emit(map[string]any{"type": "response.output_item.added", "output_index": index, "item": start})
					if item["type"] == "message" {
						text := item["content"].([]any)[0].(map[string]any)["text"].(string)
						chunks := []string{text}
						if test.declared {
							// Ordinary arbitrary stream boundaries split the ASCII
							// declaration. Public text must flow before response done.
							chunks = []string{text[:8], text[8:25], text[25:]}
						}
						for _, chunk := range chunks {
							emit(map[string]any{"type": "response.output_text.delta", "output_index": index, "content_index": 0, "item_id": item["id"], "delta": chunk})
						}
						if item["id"] == "progress_opening" {
							// Observe the real app-server delta before this normal stream
							// publishes its next Item or response completion.
							select {
							case <-openingDeltaSeen:
								if test.cancel {
									// Native interruption is observed by the test first.
									// The remaining normal response then finishes; it
									// must not restart this cancelled turn.
									<-finishResponse
								}
							case <-time.After(10 * time.Second):
								t.Error("opening delta buffered until completion")
								return
							case <-r.Context().Done():
								return
							}
						}
					}
					emit(map[string]any{"type": "response.output_item.done", "output_index": index, "item": item})
				}
				result := map[string]any{"id": id, "status": "completed", "output": part.items, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}
				if part.continueTurn {
					result["end_turn"] = false
				}
				emit(map[string]any{"type": "response.completed", "response": result})
			})}
			go func() { _ = server.Serve(listener) }()
			cfg := integrationConfig(binary, manifest, home, "ordinary-loopback-minimax")
			cfg.ChatModelsEnabled, cfg.KimiK31Candidate, cfg.RuntimePermissionsEnabled = true, true, true
			cfg.KimiAPIKey = "ordinary-loopback-kimi"
			cfg.PermissionVerificationBaseURL = "http://" + listener.Addr().String() + "/v1"
			manager := NewManager(cfg, nil)
			notifications := newIntegrationNotifications()
			if err = manager.SetNotificationHandler(func(method string, raw json.RawMessage) {
				var event map[string]any
				_ = json.Unmarshal(raw, &event)
				mu.Lock()
				methods = append(methods, method)
				if method == "warning" {
					warningText += string(raw)
				}
				if method == "item/started" {
					item, _ := event["item"].(map[string]any)
					if item["type"] == "agentMessage" {
						started = append(started, item)
					}
				}
				if method == "item/agentMessage/delta" {
					text, _ := event["delta"].(string)
					streamedText += text
					if event["itemId"] == "progress_opening" && strings.Contains(streamedText, "区分范围") {
						openingOnce.Do(func() { close(openingDeltaSeen) })
					}
				}
				if method == "item/completed" {
					item, _ := event["item"].(map[string]any)
					if item["type"] == "agentMessage" {
						completed = append(completed, item)
					}
					if item["type"] == "commandExecution" {
						commands++
						if item["exitCode"] != float64(0) {
							t.Error("normal read-only command did not succeed")
						}
					}
				}
				mu.Unlock()
				notifications.handle(method, raw)
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				stopped := true
				if err := manager.Shutdown(ctx); err != nil {
					t.Error("normal Runtime shutdown pending", err)
					stopped = false
				}
				if err := server.Shutdown(ctx); err != nil {
					t.Error("normal loopback shutdown pending", err)
					stopped = false
				}
				if test.cancel {
					mu.Lock()
					if len(bodies) != 1 {
						t.Error("late provider completion restarted an interrupted turn")
					}
					mu.Unlock()
				}
				if stopped {
					_ = os.RemoveAll(root)
				}
			})
			if err = manager.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			ctx := feat164AuxCtx()
			thread, err := manager.StartThread(ctx, cwd)
			if err != nil {
				t.Fatal(err)
			}
			input, _ := TextUserInput("Explain how to distinguish the two ordinary scopes; read the workspace directory only if needed.")
			turn, err := manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, "max")
			if err != nil {
				t.Fatal(err)
			}
			if test.cancel {
				select {
				case <-openingDeltaSeen:
				case <-time.After(10 * time.Second):
					t.Fatal("no public text available before normal cancellation")
				}
				if err := manager.InterruptTurn(ctx, thread.ID, turn.ID); err != nil {
					t.Fatal(err)
				}
			}
			status, _, err := notifications.waitForTurn(turn.ID, time.Minute)
			if test.cancel {
				if err != nil || status != "interrupted" {
					t.Fatal("native cancellation did not preserve its terminal", status, err)
				}
				mu.Lock()
				defer mu.Unlock()
				if len(bodies) != 1 || len(started) != 1 || started[0]["phase"] != "commentary" ||
					streamedText != "我会先区分范围，再整理这份说明。" {
					t.Fatal("cancellation changed visible message or started another request")
				}
				t.Log("normal native interruption retained the visible public message; requests=1; paid=0")
				return
			}
			if err != nil || status != "completed" {
				t.Fatal("normal native turn did not complete", status, err)
			}
			history, err := manager.ReadThread(ctx, thread.ID)
			if err != nil {
				t.Fatal(err)
			}
			type historicalMessage struct {
				ID    string  `json:"id"`
				Type  string  `json:"type"`
				Text  string  `json:"text"`
				Phase *string `json:"phase"`
			}
			var historical struct {
				Turns []struct {
					Items []historicalMessage `json:"items"`
				} `json:"turns"`
			}
			if err = json.Unmarshal(history, &historical); err != nil {
				t.Fatal(err)
			}
			var savedMessages []historicalMessage
			for _, savedTurn := range historical.Turns {
				for _, item := range savedTurn.Items {
					if item.Type == "agentMessage" {
						savedMessages = append(savedMessages, item)
					}
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if len(bodies) != len(test.responses) || commands != test.commands {
				t.Fatalf("requests=%d commands=%d", len(bodies), commands)
			}
			want := []map[string]any{}
			for _, part := range test.responses {
				for _, item := range part.items {
					if item["type"] == "message" {
						want = append(want, item)
					}
				}
			}
			if len(completed) != len(want) || len(savedMessages) != len(want) {
				t.Fatalf("public message count live=%d history=%d want=%d", len(completed), len(savedMessages), len(want))
			}
			if test.declared && (strings.Contains(streamedText, "yijie.public-message") || len(started) != len(want)) {
				t.Error("declaration leaked or native started identity was duplicated")
			}
			for i, original := range want {
				item := original
				if test.declared {
					item = declaredViews[original["id"].(string)]
					if started[i]["id"] != item["id"] || started[i]["phase"] != item["phase"] {
						t.Errorf("declared phase was unavailable at native Item start %d", i)
					}
				}
				if completed[i]["id"] != item["id"] || completed[i]["phase"] != item["phase"] {
					t.Fatalf("native message identity/phase changed at index %d", i)
				}
				// The native history reader assigns its own canonical item IDs;
				// provider/stream IDs are not its durable identity contract.
				phase := ""
				if savedMessages[i].Phase != nil {
					phase = *savedMessages[i].Phase
				}
				wantPhase, _ := item["phase"].(string)
				wantText := item["content"].([]any)[0].(map[string]any)["text"].(string)
				if savedMessages[i].ID == "" || savedMessages[i].Text != wantText || phase != wantPhase {
					t.Errorf("native history text/order/phase mismatch at %d", i)
				}
			}
			for index, body := range bodies {
				raw, _ := json.Marshal(body)
				if strings.Count(string(raw), "[yijie-public-progress-v3]") != 1 {
					t.Error("shared policy missing or duplicated in actual request")
				}
				tools, _ := body["tools"].([]any)
				if test.recovery && index > 0 {
					if len(tools) != 0 {
						t.Error("existing answer-only finalizer exposed tools")
					}
				} else if len(tools) == 0 {
					t.Error("normal continuation unexpectedly entered answer-only finalizer")
				}
				if index > 0 && !test.recovery {
					items, _ := body["input"].([]any)
					found := false
					for _, value := range items {
						prior, _ := value.(map[string]any)
						content, _ := json.Marshal(prior["content"])
						if prior["role"] == "assistant" && strings.Contains(string(content), "我会先区分范围") {
							found = true
							if test.declared {
								if prior["phase"] != nil || !strings.Contains(string(content), "[[yijie.public-message.v1:commentary]]") {
									t.Error("raw provider phase or declaration was rewritten in model history")
								}
							} else if prior["phase"] != "commentary" {
								t.Error("native request history lost commentary phase")
							}
						}
					}
					if !found {
						t.Error("native continuation lost previous public message")
					}
				}
			}
			if !test.recovery && strings.Contains(warningText, "YIJIE_ANSWER_") {
				t.Error("normal continuation triggered answer recovery")
			}
			firstDelta, terminal := -1, -1
			for i, method := range methods {
				if method == "item/agentMessage/delta" && firstDelta < 0 {
					firstDelta = i
				}
				if method == "turn/completed" {
					terminal = i
				}
			}
			if firstDelta < 0 || terminal <= firstDelta {
				t.Error("delta and terminal order invalid")
			}
			t.Logf("native messages=%d requests=%d real read-only commands=%d phase/history preserved; opening delta observed before response completion; paid=0", len(completed), len(bodies), commands)
		})
	}
}

func TestPublicProgressPolicyUsesCurrentNativeContext(t *testing.T) {
	var mu sync.Mutex
	var captured []map[string]any
	h := feat164AuxRuntimeProfiles(t, func(body map[string]any, _ int64) map[string]any {
		mu.Lock()
		captured = append(captured, body)
		mu.Unlock()
		return feat164AuxText("Ordinary complete result: two scopes can be compared separately.")
	}, true)
	kimi := feat164AuxCtx()
	mini := WithPermissionMode(WithModelProfile(context.Background(), "minimax-m3-high-v1"), PermissionAsk)
	expert, err := WithExpertInstructions(kimi, "ordinary-progress-expert", "ordinary-reference-index")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := h.manager.StartThread(kimi, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	for i, step := range []struct {
		ctx    context.Context
		effort string
	}{{kimi, "max"}, {kimi, "max"}, {mini, "high"}, {expert, "max"}, {kimi, "max"}} {
		if i > 0 {
			thread, err = h.manager.SwitchThread(step.ctx, thread.ID)
			if err != nil {
				t.Fatal(err)
			}
		}
		input, _ := TextUserInput("Compare the ordinary scopes without external research.")
		turn, err := h.manager.StartTurnV2(step.ctx, thread.ID, []UserInput{input}, step.effort)
		if err != nil {
			t.Fatal(err)
		}
		status, _, err := h.notifications.waitForTurn(turn.ID, 30*time.Second)
		if err != nil || status != "completed" {
			t.Fatal(status, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for i, body := range captured {
		raw, _ := json.Marshal(body)
		if strings.Count(string(raw), "[yijie-public-progress-v3]") != 1 || strings.Count(string(raw), "[yijie-answer-completion-v1]") != 1 || strings.Count(string(raw), "[yijie-public-message-v1]") != 1 {
			t.Fatalf("shared current developer policy not unique at step %d", i)
		}
		base, _ := body["instructions"].(string)
		if strings.Contains(base, "[yijie-public-progress-v3]") || !strings.HasPrefix(base, executionBaseInstructions) || strings.Contains(base, "Skills are opt-in") {
			t.Fatalf("current base ownership or native Skill discovery changed at step %d", i)
		}
		if len(body["tools"].([]any)) == 0 {
			t.Fatal("withdrawn light tool mode was reintroduced")
		}
	}
	t.Log("current shared policy once through native start/resume/model/expert switches; native Skill discovery and full tools retained;0 paid")
}
