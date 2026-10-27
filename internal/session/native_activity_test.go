package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv5"
	"github.com/getkin/kin-openapi/openapi3"
)

func TestNativeActivityPreservesCommandSubactionsWithoutExtraExecutions(t *testing.T) {
	raw := json.RawMessage(`{"id":"one-command","type":"commandExecution","status":"completed","command":"cat report.md; rg income notes.md","commandActions":[{"type":"read","name":"report.md","path":"/workspace/report.md","command":"cat report.md"},{"type":"search","path":"notes.md","query":"income","command":"rg income notes.md"}],"exitCode":0}`)
	item, err := projectNativeItem(raw, "/workspace", false)
	if err != nil || item.Id != "one-command" || item.CommandActions == nil || len(*item.CommandActions) != 2 {
		t.Fatal("native actions missing")
	}
	actions := *item.CommandActions
	if actions[0].Index != 0 || actions[1].Index != 1 || *actions[0].PathLabel != "report.md" || *actions[1].Query != "income" || *item.CommandPreview != "cat report.md; rg income notes.md" {
		t.Fatal("action details lost")
	}
	old := NativeEventV11(Event{SchemaVersion: 12, Payload: EventPayload{Native: &native.NativeNotification{Item: &item}}})
	if old.Payload.Native.Item.CommandActions != nil || old.Payload.Native.Item.CommandPreview != nil || item.CommandActions == nil || old.Payload.Native.Item.Id != item.Id {
		t.Fatal("closed legacy projection or original identity changed")
	}
}

func TestNativeActivityRelativeActionsUseTheNativeCommandDirectory(t *testing.T) {
	item, err := projectNativeItem(json.RawMessage(`{"id":"subdirectory-command","type":"commandExecution","cwd":"/workspace/reports","status":"completed","commandActions":[{"type":"search","path":"notes.md","query":"income"}]}`), "/workspace", false)
	if err != nil || item.CommandActions == nil || (*item.CommandActions)[0].PathLabel == nil || *(*item.CommandActions)[0].PathLabel != "reports/notes.md" {
		t.Fatal("relative native path was assigned to the wrong directory")
	}
}

func TestNativeActivityFileCountsUseCompleteNativeKindsAndMoveSemantics(t *testing.T) {
	changes := []any{
		map[string]any{"path": "/workspace/new.md", "kind": map[string]any{"type": "add"}, "diff": "a\nb\n"},
		map[string]any{"path": "/workspace/old.md", "kind": map[string]any{"type": "delete"}, "diff": "a\n"},
		map[string]any{"path": "/workspace/edit.md", "kind": map[string]any{"type": "update", "move_path": "/workspace/moved.md"}, "diff": "--- a/edit.md\n+++ b/edit.md\n@@ -1,2 +1,3 @@\n same\n-old\n+new\n+extra\n\n\nMoved to: /workspace/moved.md"},
	}
	raw, _ := json.Marshal(map[string]any{"id": "change", "type": "fileChange", "status": "completed", "changes": changes})
	item, err := projectNativeItem(raw, "/workspace", false)
	if err != nil || item.FileChange == nil || len(item.FileChange.Changes) != 3 {
		t.Fatal("file changes missing")
	}
	for i, want := range [][2]int{{2, 0}, {0, 1}, {2, 1}} {
		got := item.FileChange.Changes[i]
		if got.AddedLines == nil || got.RemovedLines == nil || *got.AddedLines != want[0] || *got.RemovedLines != want[1] {
			t.Fatalf("native count mismatch for index %d", i)
		}
	}
	if *item.FileChange.Changes[2].MovePathLabel != "moved.md" {
		t.Fatal("native move missing")
	}
	old := NativeEventV11(Event{Payload: EventPayload{Native: &native.NativeNotification{Item: &item}}})
	if old.Payload.Native.Item.Type != native.Unknown || old.Payload.Native.Item.FileChange != nil || item.FileChange == nil {
		t.Fatal("legacy downgrade changed original")
	}
}

func TestNativeActivityIncompleteOrUnparseableDiffHasNoInventedZero(t *testing.T) {
	for _, row := range []struct{ status, diff string }{{"inProgress", "a\n"}, {"failed", "a\n"}, {"completed", "incomplete diff"}, {"completed", strings.Repeat("a", 65537)}} {
		raw, _ := json.Marshal(map[string]any{"id": "change", "type": "fileChange", "status": row.status, "changes": []any{map[string]any{"path": "note.md", "kind": map[string]any{"type": "update"}, "diff": row.diff}}})
		item, err := projectNativeItem(raw, "/workspace", false)
		if err != nil || item.FileChange == nil {
			t.Fatal("file identity lost")
		}
		change := item.FileChange.Changes[0]
		if change.AddedLines != nil || change.RemovedLines != nil {
			t.Fatal("unknown stats became zero")
		}
		if change.DiffText != nil && len(*change.DiffText) > 65536 {
			t.Fatal("display exceeded bound")
		}
	}
}

func TestNativeActivityHunkOnlyUpdateFromNativeApplyPatch(t *testing.T) {
	diff := "@@ -10 +10,3 @@\n | total | 103.50 | 14.25 | 117.75 |\n+\n+Verified total: 117.75.\n"
	raw, _ := json.Marshal(map[string]any{"id": "patch", "type": "fileChange", "status": "completed", "changes": []any{map[string]any{"path": "/workspace/result.md", "kind": map[string]any{"type": "update"}, "diff": diff}}})
	item, err := projectNativeItem(raw, "/workspace", false)
	if err != nil || item.FileChange == nil {
		t.Fatal("native patch missing")
	}
	change := item.FileChange.Changes[0]
	if change.AddedLines == nil || change.RemovedLines == nil || *change.AddedLines != 2 || *change.RemovedLines != 0 {
		t.Fatal("complete native hunk statistics missing")
	}
	for _, partial := range []string{"--- result.md\n" + diff, strings.TrimSuffix(diff, "+Verified total: 117.75.\n")} {
		if _, _, ok := nativeDiffCounts("update", partial); ok {
			t.Fatal("incomplete native diff accepted")
		}
	}
}

func TestNativeActivityContextAndPathsUseExistingConfidentialityBoundary(t *testing.T) {
	raw := json.RawMessage(`{"id":"app","type":"mcpToolCall","status":"completed","server":"example","tool":"read_report","appContext":{"connectorId":"example","appName":"Example App","actionName":"Read report","resourceUri":"ordinary-resource","linkId":"connection-identity"}}`)
	item, err := projectNativeItem(raw, "/workspace", false)
	if err != nil || item.AppContext == nil || *item.AppContext.AppName != "Example App" {
		t.Fatal("native app identity lost")
	}
	encoded, _ := json.Marshal(item)
	if strings.Contains(string(encoded), "ordinary-resource") || strings.Contains(string(encoded), "connection-identity") {
		t.Fatal("unneeded resource identity leaked")
	}
	outside := "/another-project/report.md"
	if nativeActivityPath(outside, "/workspace", &item) != nil {
		t.Fatal("out-of-workspace path exposed")
	}
}

func TestNativeActivityNotificationAndHistoryConformToVersionedSource(t *testing.T) {
	item := map[string]any{"id": "native-change", "type": "fileChange", "status": "completed", "changes": []any{map[string]any{"path": "/workspace/note.md", "kind": map[string]any{"type": "add"}, "diff": "one\ntwo\n"}}}
	r := &nativeReadRuntime{raw: rawJSON(t, map[string]any{"id": testThreadID, "turns": []any{map[string]any{"id": testTurnID, "status": "completed", "items": []any{item}}}})}
	s := NewService(r, openBoundFEAT134Store(t), NewEventHub(32, 8), nil)
	s.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{"threadId": testThreadID, "turnId": testTurnID, "item": item}))
	_, events, _, cancel, err := s.SubscribeNativeEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if len(events) != 1 {
		t.Fatal("projection created or lost an execution")
	}
	for _, version := range []int{12, 11, 10, 8, 7} {
		event := events[0]
		file := "native-conversation-v5.yaml"
		switch version {
		case 11:
			event = NativeEventV11(event)
			file = "native-conversation-v4.yaml"
		case 10:
			event = NativeEventV10(event)
			file = "native-conversation-v3.yaml"
		case 8:
			event = NativeEventV8(event)
			file = "native-conversation-v2.yaml"
		case 7:
			event = LegacyNativeEvent(event)
			file = "native-conversation.yaml"
		}
		doc, e := openapi3.NewLoader().LoadFromFile("../../api/openapi/" + file)
		if e != nil {
			t.Fatal(e)
		}
		bytes, _ := json.Marshal(event)
		var value any
		_ = json.Unmarshal(bytes, &value)
		if e = doc.Components.Schemas["NativeEvent"].Value.VisitJSON(value); e != nil {
			t.Fatal(e)
		}
	}
	view, err := s.ReadNativeThreadV5(context.Background(), testSessionID)
	if err != nil || view.Turns[0].Items[0].FileChange == nil {
		t.Fatal("history lost native changes")
	}
	old, err := s.ReadNativeThreadV4(context.Background(), testSessionID)
	if err != nil || string(old.Turns[0].Items[0].Type) != "unknown" {
		t.Fatal("old history shape changed")
	}
}
