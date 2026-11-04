package session

import (
	"encoding/json"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
	"testing"
)

func TestNativeTraceActionsPreserveOneExecutionAndOriginalIndices(t *testing.T) {
	raw := json.RawMessage(`{"id":"command-one","type":"commandExecution","command":"read files","status":"completed","exitCode":0,"commandActions":[{"type":"read","name":"README.md","path":"/workspace/README.md"},{"type":"search","query":"ordinary query","path":"/workspace/src"},{"type":"unknown","command":"normal command"}]}`)
	got, err := projectNativeItem(raw, "/workspace", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Id != "command-one" || got.CommandActions == nil || len(*got.CommandActions) != 3 || got.Status == nil || *got.Status != native.NativeItemStatusCompleted {
		t.Fatal("native execution changed")
	}
	for i, a := range *got.CommandActions {
		if a.Index != i {
			t.Fatal("action index changed")
		}
	}
	if a := (*got.CommandActions)[0]; a.Label == nil || *a.Label != "README.md" {
		t.Fatal("safe read label missing")
	}
	if a := (*got.CommandActions)[1]; a.Query == nil || *a.Query != "ordinary query" || a.Label == nil || *a.Label != "src" {
		t.Fatal("search metadata missing")
	}
}
func TestNativeTraceNoShellInferenceAndBoundedActions(t *testing.T) {
	got, err := projectNativeItem(json.RawMessage(`{"id":"c","type":"commandExecution","command":"cat README.md","status":"completed"}`), "/workspace", false)
	if err != nil || got.CommandActions != nil {
		t.Fatal("invented actions")
	}
	actions := make([]nativeWireAction, 65)
	for i := range actions {
		actions[i].Type = "unknown"
	}
	out := native.NativeItem{Id: "c", Availability: native.NativeItemAvailabilityAvailable}
	projectNativeActions(nativeWireItem{CommandActions: actions}, "/workspace", &out)
	if len(*out.CommandActions) != 64 || out.Availability != native.NativeItemAvailabilityPartial {
		t.Fatal("bound not applied")
	}
}
func TestNativeTraceToolLabelsUseOnlyCanonicalContext(t *testing.T) {
	got, err := projectNativeItem(json.RawMessage(`{"id":"t","type":"mcpToolCall","server":"ordinary","tool":"read","status":"completed","arguments":{"title":"ignored arbitrary title"},"appContext":{"appName":"资料库","actionName":"读取资料"}}`), "/workspace", false)
	if err != nil || got.ToolDisplay == nil || got.ToolLabel == nil || *got.ToolLabel != "读取资料" {
		t.Fatal("canonical display missing")
	}
}
func TestNativeTraceRetryIntentSurvivesNativeStream(t *testing.T) {
	for _, value := range []any{nil, true, false} {
		s := NewService(&fakeRuntime{}, openBoundFEAT134Store(t), NewEventHub(32, 8), nil)
		s.publishNativeNotification(RuntimeNotificationError, rawJSON(t, map[string]any{"threadId": testThreadID, "turnId": testTurnID, "error": map[string]any{"message": "temporary service error"}, "willRetry": value}))
		_, events, _, cancel, err := s.SubscribeNativeEvents(testSessionID, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		if len(events) != 1 || events[0].SchemaVersion != 13 || events[0].Terminal {
			t.Fatal("error changed lifecycle")
		}
		flag := events[0].Payload.Native.WillRetry
		if value == nil {
			if flag != nil {
				t.Fatal("unknown retry became false")
			}
		} else if flag == nil || *flag != value.(bool) {
			t.Fatal("retry intent lost")
		}
	}
}

func TestNativeTraceRepairRelativePathsUseTheCommandCwd(t *testing.T) {
	for _, sample := range []struct{ kind, cwd, path, want string }{
		{"listFiles", "/workspace", "src", "src"},
		{"search", "/workspace/src", "docs", "src/docs"},
		{"read", "/workspace/src", "/workspace/README.md", "README.md"},
	} {
		raw, _ := json.Marshal(map[string]any{"id": "command", "type": "commandExecution", "command": "ordinary read", "cwd": sample.cwd, "status": "completed", "commandActions": []map[string]any{{"type": sample.kind, "path": sample.path}}})
		got, err := projectNativeItem(raw, "/workspace", false)
		if err != nil || got.CommandActions == nil || (*got.CommandActions)[0].Label == nil || *(*got.CommandActions)[0].Label != sample.want {
			t.Fatalf("%s label: %#v, %v", sample.kind, got.CommandActions, err)
		}
	}
}

func TestNativeTraceRepairCommandPreviewsKeepNativeActionsAndParentIdentity(t *testing.T) {
	raw := json.RawMessage(`{"id":"one-call","type":"commandExecution","command":"shell wrapper","cwd":"/workspace","status":"completed","exitCode":0,"commandActions":[{"type":"unknown","command":"python3 --version"},{"type":"unknown","command":"tail -n 5 README.md"}]}`)
	got, err := projectNativeItem(raw, "/workspace", false)
	if err != nil || got.CommandLabel == nil || *got.CommandLabel != "python3 --version；tail -n 5 README.md" {
		t.Fatalf("preview: %#v %v", got.CommandLabel, err)
	}
	if got.Id != "one-call" || len(*got.CommandActions) != 2 || (*got.CommandActions)[1].Index != 1 || *(*got.CommandActions)[1].Label != "tail -n 5 README.md" {
		t.Fatal("parent/action identity lost")
	}
	// Ordinary absolute locations remain private; the approved executable name
	// is a display fallback, not inferred execution semantics.
	out := native.NativeItem{Id: "display", Availability: native.NativeItemAvailabilityAvailable}
	preview := traceCommandPreview("tail -n 5 /workspace/notes.txt", &out)
	if preview == nil || *preview != "tail · 参数已隐藏" {
		t.Fatal("absolute location escaped preview projection")
	}
	preview = traceCommandPreview("python3 - <<'PY'\nprint('ordinary example')\nPY", &out)
	if preview == nil || *preview != "python3 - <<'PY' …" {
		t.Fatal("multiline program was not reduced to a preview")
	}
	projectNativeCommandPreview(nativeWireItem{Command: "pwd", CommandActions: make([]nativeWireAction, 4)}, &out)
	if out.CommandLabel == nil || *out.CommandLabel != "pwd" {
		t.Fatal("missing action previews must fall back to the parent label, not an ellipsis")
	}
}

func TestNativeTraceRepairToolPresentationUsesAllowedTitlesAndRegisteredMetadata(t *testing.T) {
	for _, sample := range []struct{ raw, title, source, provenance string }{
		{`{"id":"cua","type":"mcpToolCall","server":"cua_repl","tool":"js","status":"completed","arguments":{"title":"查看公开资料","code":"ordinary example"},"result":{"_meta":{"codex/toolSurface":{"kind":"browserUse","backend":"iab"}}}}`, "查看公开资料", "浏览器", "allowedTitle"},
		{`{"id":"app","type":"mcpToolCall","server":"cua_repl","tool":"js","status":"completed","arguments":{"title":"核对保存的记录"},"result":{"_meta":{"codex/toolSurface":{"kind":"computerUse","app":{"kind":"displayName","displayName":"易界 AI"}}}}}`, "核对保存的记录", "易界 AI", "allowedTitle"},
		{`{"id":"artifact","type":"mcpToolCall","server":"yijie_artifacts","tool":"publish_artifact","status":"completed","arguments":{}}`, "登记聊天产物", "易界 AI", "registeredTool"},
	} {
		got, err := projectNativeItem(json.RawMessage(sample.raw), "/workspace", false)
		if err != nil || got.ToolDisplay == nil || got.ToolDisplay.Title == nil || *got.ToolDisplay.Title != sample.title || got.ToolDisplay.SourceLabel == nil || *got.ToolDisplay.SourceLabel != sample.source || string(got.ToolDisplay.Provenance) != sample.provenance {
			t.Fatalf("display mismatch: %#v %v", got.ToolDisplay, err)
		}
	}
	got, err := projectNativeItem(json.RawMessage(`{"id":"ordinary","type":"mcpToolCall","server":"ordinary","tool":"read","status":"completed","arguments":{"title":"Ordinary supplied value"}}`), "/workspace", false)
	if err != nil || got.ToolDisplay != nil {
		t.Fatal("arbitrary tool title accepted")
	}
}
