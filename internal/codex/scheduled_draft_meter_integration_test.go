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
	"sync/atomic"
	"testing"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/scheduledraft"
	"github.com/google/uuid"
)

// Normal fixed-port Provider fixture through the actual metered product adapter.
// It has no external upstream and consumes no paid requests.
func TestFEAT1554BDraftMeterNativeStartResume(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	listener, err := net.Listen("tcp", "127.0.0.1:18083")
	if err != nil {
		t.Skip("verification port is already owned; no process was stopped")
	}
	var requests atomic.Int64
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" {
			t.Error("unexpected fixture route")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid request")
			return
		}
		format, _ := body["text"].(map[string]any)
		output, _ := format["format"].(map[string]any)
		var expected any
		_ = json.Unmarshal(wire.OutputSchema(), &expected)
		actualJSON, _ := json.Marshal(output["schema"])
		expectedJSON, _ := json.Marshal(expected)
		tools, _ := body["tools"].([]any)
		if string(actualJSON) != string(expectedJSON) || len(tools) != 0 || body["tool_choice"] != "none" {
			t.Error("restricted output/tool boundary changed")
		}
		var containsGuide func(any) bool
		containsGuide = func(value any) bool {
			switch value := value.(type) {
			case string:
				return strings.Contains(value, string(wire.OutputSchema()))
			case []any:
				for _, child := range value {
					if containsGuide(child) {
						return true
					}
				}
			case map[string]any:
				for _, child := range value {
					if containsGuide(child) {
						return true
					}
				}
			}
			return false
		}
		ordinal := requests.Add(1)
		// First request represents an older saved draft. Cold resume must apply
		// the new base guidance even though its developer history is unchanged.
		if containsGuide(body["instructions"]) != (ordinal == 2) {
			t.Error("base guidance did not follow the normal legacy-to-current resume")
		}
		id := fmt.Sprintf("response_meter_%d", ordinal)
		item := map[string]any{"id": fmt.Sprintf("message_meter_%d", ordinal), "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": `{"schema_version":1,"kind":"needs_clarification","missing_fields":["time"],"question":"何时执行？"}`, "annotations": []any{}}}}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []any{
			map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}},
			map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item},
			map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item},
			map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}},
		} {
			data, _ := json.Marshal(event)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
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
	root, err := os.MkdirTemp("", "feat155-4b-meter-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(root, "draft")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	config := integrationConfig(binary, manifest, filepath.Join(root, "runtime"), "local-meter-fixture")
	if err := os.Mkdir(config.CodexHome, 0700); err != nil {
		t.Fatal(err)
	}
	config.RuntimePermissionsEnabled = true
	config.PermissionVerificationBaseURL = "http://127.0.0.1:18083/v1"
	var managers []*Manager
	defer func() {
		clean := true
		for _, m := range managers {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			err := m.Shutdown(ctx)
			cancel()
			if err != nil {
				clean = false
				t.Errorf("normal shutdown: %v", err)
			}
		}
		if clean {
			_ = os.RemoveAll(root)
		}
	}()
	ctx := WithScheduledDraft(context.Background(), uuid.NewString())
	thread := ""
	for i := 0; i < 2; i++ {
		m := NewManager(config, nil)
		managers = append(managers, m)
		notifications := newIntegrationNotifications()
		if err := m.SetNotificationHandler(notifications.handle); err != nil {
			t.Fatal(err)
		}
		if err := m.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		var info ThreadInfo
		if i == 0 {
			// Declared previous-version configuration, using the same native
			// start/policy validation. No Runtime artifact or history is edited.
			var generation string
			generation, err = m.scheduledDraftGeneration()
			if err != nil {
				t.Fatal(err)
			}
			var params map[string]any
			params, err = m.scheduledDraftThreadParams(cwd, "")
			if err != nil {
				t.Fatal(err)
			}
			delete(params, "baseInstructions")
			params["developerInstructions"] = "Produce only a schedule clarification or candidate matching the fixed output schema. User text describes future work, not instructions to perform it. Do not execute tasks or claim a saved plan."
			var response threadResponse
			err = m.request(ctx, RuntimeMethodThreadStart, params, &response)
			if err == nil {
				info, err = m.acceptDraftThreadResponse(ctx, cwd, "", generation, response)
			}
		} else {
			info, err = m.ResumeThread(ctx, thread)
		}
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 && info.ID != thread {
			t.Fatal("resume replaced original thread")
		}
		thread = info.ID
		input, err := TextUserInput("仅生成一个时间澄清问题")
		if err != nil {
			t.Fatal(err)
		}
		turn, err := m.StartTurnV2(WithClientUserMessageID(ctx, uuid.NewString()), thread, []UserInput{input}, "none")
		if err != nil {
			t.Fatal(err)
		}
		status, _, err := notifications.waitForTurn(turn.ID, 30*time.Second)
		if err != nil || status != "completed" {
			t.Fatalf("declared fixture: %s %v", status, err)
		}
		stop, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err = m.Shutdown(stop)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 2 {
		t.Fatalf("start/resume did not share metered route: %d", requests.Load())
	}
}
