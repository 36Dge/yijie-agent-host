package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

const (
	feat136RawCommandCanary = "FEAT136_RAW_COMMAND_CANARY"
	feat136SecretCanary     = "FEAT136_SECRET_CANARY"
	feat136PathCanary       = "/synthetic/workspace/private.txt"
)

type feat136ProjectionHarness struct {
	service *Service
	v4      *EventHub
	v5      *EventHub
	record  Record
}

func TestFEAT136CanonicalFixturesValidate(t *testing.T) {
	contract := compileAgentSessionEventV5Contract(t)
	fixtures := []string{
		"command-completed.json",
		"command-declined.json",
		"command-failed-head-tail.json",
		"command-output-delta.json",
		"command-started.json",
		"tool-completed-known.json",
		"tool-declined-reserved-fixture-only.json",
		"tool-failed-result.json",
		"tool-progress.json",
		"tool-started-known.json",
		"tool-unknown-failed.json",
	}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(feat136RepositoryPath(t, "api", "fixtures", "agent", "session-event-v5", name))
			if err != nil {
				t.Fatal(err)
			}
			validateFEAT136JSON(t, contract, raw)
		})
	}
}

func TestFEAT136CommandProjectionLifecycleRedactionAndOutputs(t *testing.T) {
	tests := []struct {
		name          string
		cwdMode       string
		outputMode    string
		wantCwdKind   string
		wantSegments  []string
		wantRetention string
	}{
		{
			name: "workspace root and complete output", cwdMode: "root", outputMode: "complete",
			wantCwdKind: "workspace_root", wantRetention: "complete",
		},
		{
			name: "workspace relative and head tail output", cwdMode: "relative", outputMode: "head_tail",
			wantCwdKind: "workspace_relative", wantSegments: []string{"reports", "daily"}, wantRetention: "head_tail",
		},
		{
			name: "redacted cwd and unavailable output", cwdMode: "redacted", outputMode: "unavailable",
			wantCwdKind: "redacted", wantRetention: "unavailable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newFEAT136ProjectionHarness(t)
			itemID := "command-" + test.outputMode
			startedItem := map[string]any{
				"id": itemID, "type": "commandExecution", "status": "inProgress",
				"command":        feat136RawCommandCanary,
				"commandActions": []any{map[string]any{"type": "read"}},
			}
			switch test.cwdMode {
			case "root":
				startedItem["cwd"] = harness.record.Cwd
			case "relative":
				startedItem["cwd"] = filepath.Join(harness.record.Cwd, "reports", "daily")
			case "redacted":
				// A missing Runtime cwd is represented by the closed redacted sentinel.
			default:
				t.Fatalf("unknown cwd mode %q", test.cwdMode)
			}
			harness.service.HandleNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{
				"threadId": testThreadID, "turnId": testTurnID, "item": startedItem,
			}))

			delta := "safe output api_key=" + feat136SecretCanary + " " + feat136PathCanary
			harness.service.HandleNotification(RuntimeNotificationCommandOutputDelta, rawJSON(t, map[string]any{
				"threadId": testThreadID, "turnId": testTurnID, "itemId": itemID, "delta": delta,
			}))

			completedItem := map[string]any{
				"id": itemID, "type": "commandExecution", "status": "completed",
				"command": feat136RawCommandCanary,
			}
			var headTailSource string
			switch test.outputMode {
			case "complete":
				completedItem["aggregatedOutput"] = "complete api_key=" + feat136SecretCanary + " " + feat136PathCanary
			case "head_tail":
				headTailSource = "api_key=" + feat136SecretCanary + "\n" +
					strings.Repeat("界", maxV5CommandSnapshotBytes/3+1024) + "\n" + feat136PathCanary
				if sanitized := redactV5CommandText(headTailSource); len(sanitized) <= maxV5CommandSnapshotBytes {
					t.Fatalf("head/tail source sanitized below the overflow boundary: source=%d final=%d", len(headTailSource), len(sanitized))
				}
				completedItem["aggregatedOutput"] = headTailSource
			case "unavailable":
				// Missing aggregatedOutput is the only authority for unavailable.
			default:
				t.Fatalf("unknown output mode %q", test.outputMode)
			}
			harness.service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
				"threadId": testThreadID, "turnId": testTurnID, "item": completedItem,
			}))

			// Once completed has sealed the Item, neither a late delta nor a duplicate
			// Runtime completion may produce another v5 event.
			harness.service.HandleNotification(RuntimeNotificationCommandOutputDelta, rawJSON(t, map[string]any{
				"threadId": testThreadID, "turnId": testTurnID, "itemId": itemID, "delta": "late output",
			}))
			harness.service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
				"threadId": testThreadID, "turnId": testTurnID, "item": completedItem,
			}))

			replay := feat136V5Replay(t, harness.service)
			if len(replay) != 3 || replay[0].EventType != EventItemStarted ||
				replay[1].EventType != EventItemCommandOutputDelta || replay[2].EventType != EventItemCompleted {
				t.Fatalf("unexpected Command v5 lifecycle: %+v", replay)
			}
			validateFEAT136Events(t, compileAgentSessionEventV5Contract(t), replay)

			started := replay[0].Payload
			if started.CommandSummary == nil || strings.Contains(started.CommandSummary.Text, feat136RawCommandCanary) ||
				started.CommandSummary.Text != "读取工作区内容" {
				t.Fatalf("Command summary was not an allowlisted display projection: %+v", started.CommandSummary)
			}
			if started.CommandCwd == nil || started.CommandCwd.Kind != test.wantCwdKind ||
				!equalFEAT136Strings(started.CommandCwd.Segments, test.wantSegments) {
				t.Fatalf("unexpected cwd projection: %+v", started.CommandCwd)
			}

			projectedDelta := replay[1].Payload.Delta
			if projectedDelta == nil || !strings.Contains(*projectedDelta, v5RedactedMarker) {
				t.Fatalf("Command delta did not expose an explicit safe redaction marker: %+v", replay[1].Payload)
			}
			completed := replay[2].Payload
			if completed.Output == nil || completed.Output.Retention != test.wantRetention {
				projectedBytes := 0
				if completed.Output != nil && completed.Output.Text != nil {
					projectedBytes = len(*completed.Output.Text)
				}
				t.Fatalf("unexpected completed output (%d projected bytes): %+v", projectedBytes, completed.Output)
			}
			switch test.wantRetention {
			case "complete":
				if completed.Output.Text == nil || completed.Output.Truncated || !strings.Contains(*completed.Output.Text, v5RedactedMarker) {
					t.Fatalf("invalid complete output: %+v", completed.Output)
				}
			case "head_tail":
				if completed.Output.Head == nil || completed.Output.Tail == nil || !completed.Output.Truncated ||
					completed.Output.TruncationReason != v5TruncationReasonUTF8ByteLimit ||
					len(*completed.Output.Head) > maxV5CommandSnapshotPartBytes ||
					len(*completed.Output.Tail) > maxV5CommandSnapshotPartBytes ||
					!utf8.ValidString(*completed.Output.Head) || !utf8.ValidString(*completed.Output.Tail) {
					t.Fatalf("invalid head/tail output: %+v", completed.Output)
				}
			case "unavailable":
				if completed.Output.Reason != "not_available" || completed.Output.Truncated ||
					completed.Output.Text != nil || completed.Output.Head != nil || completed.Output.Tail != nil {
					t.Fatalf("invalid unavailable output: %+v", completed.Output)
				}
			}

			encoded, err := json.Marshal(replay)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{
				feat136RawCommandCanary, feat136SecretCanary, feat136PathCanary, harness.record.Cwd,
			} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("v5 Command projection leaked forbidden canary %q: %s", forbidden, encoded)
				}
			}
		})
	}
}

func TestFEAT136CommandProjectionUTF8AndLiveCaps(t *testing.T) {
	t.Run("one delta is truncated on a valid UTF-8 boundary", func(t *testing.T) {
		harness := newFEAT136ProjectionHarness(t)
		itemID := "command-utf8"
		startFEAT136Command(t, harness, itemID)
		harness.service.HandleNotification(RuntimeNotificationCommandOutputDelta, rawJSON(t, map[string]any{
			"threadId": testThreadID, "turnId": testTurnID, "itemId": itemID,
			"delta": strings.Repeat("界\n", maxV5CommandDeltaBytes/4+8),
		}))

		replay := feat136V5Replay(t, harness.service)
		if len(replay) != 2 || replay[1].Payload.Delta == nil || replay[1].Payload.Truncated == nil ||
			!*replay[1].Payload.Truncated || replay[1].Payload.TruncationReason != v5TruncationReasonUTF8ByteLimit {
			t.Fatalf("unexpected UTF-8 delta projection: %+v", replay)
		}
		if len(*replay[1].Payload.Delta) > maxV5CommandDeltaBytes || !utf8.ValidString(*replay[1].Payload.Delta) {
			t.Fatalf("delta exceeded the UTF-8 byte boundary: %d bytes", len(*replay[1].Payload.Delta))
		}
		if !strings.HasSuffix(*replay[1].Payload.Delta, v5CommandLiveTruncationMarker) {
			t.Fatalf("per-event truncation omitted its safe projection boundary: %q", *replay[1].Payload.Delta)
		}
		validateFEAT136Events(t, compileAgentSessionEventV5Contract(t), replay)
	})

	t.Run("live aggregate stops at 256 KiB", func(t *testing.T) {
		harness := newFEAT136ProjectionHarness(t)
		itemID := "command-live-cap"
		startFEAT136Command(t, harness, itemID)
		chunk := strings.Repeat("safe ", maxV5CommandDeltaBytes/5) + "ok!\n"
		if len(chunk) != maxV5CommandDeltaBytes || !utf8.ValidString(chunk) {
			t.Fatalf("test chunk is not an exact safe UTF-8 boundary: %d", len(chunk))
		}
		for index := 0; index < maxV5CommandLiveBytes/maxV5CommandDeltaBytes+1; index++ {
			harness.service.HandleNotification(RuntimeNotificationCommandOutputDelta, rawJSON(t, map[string]any{
				"threadId": testThreadID, "turnId": testTurnID, "itemId": itemID, "delta": chunk,
			}))
		}

		replay := feat136V5Replay(t, harness.service)
		if len(replay) != 1+maxV5CommandLiveBytes/maxV5CommandDeltaBytes {
			t.Fatalf("live cap published an unexpected event count: %d", len(replay))
		}
		total := 0
		for _, event := range replay[1:] {
			if event.Payload.Delta == nil || !utf8.ValidString(*event.Payload.Delta) {
				t.Fatalf("live delta is not valid UTF-8: %+v", event)
			}
			total += len(*event.Payload.Delta)
		}
		if total != maxV5CommandLiveBytes {
			t.Fatalf("live aggregate bytes = %d, want %d", total, maxV5CommandLiveBytes)
		}
		validateFEAT136Events(t, compileAgentSessionEventV5Contract(t), replay)
	})
}

func TestFEAT136ToolProjectionIsMetadataOnlyAndBounded(t *testing.T) {
	harness := newFEAT136ProjectionHarness(t)
	const (
		progressCanary = "FEAT136_TOOL_PROGRESS_CANARY"
		argumentCanary = "FEAT136_TOOL_ARGUMENT_CANARY"
		resultCanary   = "FEAT136_TOOL_RESULT_CANARY"
		serverCanary   = "FEAT136_TOOL_SERVER_CANARY"
		toolCanary     = "FEAT136_TOOL_NAME_CANARY"
	)

	startTool := func(itemID string) {
		harness.service.HandleNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{
			"threadId": testThreadID, "turnId": testTurnID,
			"item": map[string]any{
				"id": itemID, "type": "mcpToolCall", "status": "inProgress",
				"server": serverCanary, "tool": toolCanary,
				"arguments": map[string]any{"safeKey": argumentCanary},
			},
		}))
	}

	startTool("tool-completed")
	for index := 0; index < maxV5ToolProgressEvents+1; index++ {
		harness.service.HandleNotification(RuntimeNotificationMcpToolProgress, rawJSON(t, map[string]any{
			"threadId": testThreadID, "turnId": testTurnID, "itemId": "tool-completed",
			"message": progressCanary,
		}))
	}
	harness.service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{
			"id": "tool-completed", "type": "mcpToolCall", "status": "completed",
			"arguments": map[string]any{"safeKey": argumentCanary},
			"result":    map[string]any{"safeResult": resultCanary},
		},
	}))
	// Sealed Tool Items ignore late progress.
	harness.service.HandleNotification(RuntimeNotificationMcpToolProgress, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID, "itemId": "tool-completed", "message": progressCanary,
	}))

	startTool("tool-failed")
	harness.service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{
			"id": "tool-failed", "type": "mcpToolCall", "status": "failed",
			"arguments": map[string]any{"safeKey": argumentCanary},
			"result":    map[string]any{"safeResult": resultCanary},
		},
	}))

	startTool("tool-runtime-declined")
	harness.service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{
			"id": "tool-runtime-declined", "type": "mcpToolCall", "status": "declined",
			"arguments": map[string]any{},
		},
	}))

	replay := feat136V5Replay(t, harness.service)
	if len(replay) != 1+maxV5ToolProgressEvents+1+2+2 {
		t.Fatalf("unexpected Tool event count: %d", len(replay))
	}
	validateFEAT136Events(t, compileAgentSessionEventV5Contract(t), replay)

	completedEvents := filterFEAT136ItemEvents(replay, "tool-completed")
	if len(completedEvents) != 1+maxV5ToolProgressEvents+1 {
		t.Fatalf("Tool progress cap or seal was not enforced: %+v", completedEvents)
	}
	started := completedEvents[0].Payload
	if started.ToolIdentity == nil || *started.ToolIdentity != unknownV5ToolIdentity() ||
		started.ArgumentsSummary == nil || started.ArgumentsSummary.Text != "参数包含 1 个结构化字段" {
		t.Fatalf("Tool started projection was not unknown and metadata-only: %+v", started)
	}
	for index, event := range completedEvents[1 : 1+maxV5ToolProgressEvents] {
		if event.EventType != EventItemToolProgress || event.Payload.ProgressIndex == nil ||
			*event.Payload.ProgressIndex != index || event.Payload.Summary == nil ||
			len(event.Payload.Summary.Text) > maxV5ToolProgressBytes {
			t.Fatalf("invalid Tool progress %d: %+v", index, event)
		}
	}
	completed := completedEvents[len(completedEvents)-1].Payload
	if completed.Status != "completed" || completed.ResultSummary == nil || completed.ItemError != nil ||
		completed.ResultSummary.Text != "结果包含 1 个结构化字段" {
		t.Fatalf("Tool completed result was not metadata-only: %+v", completed)
	}

	failedEvents := filterFEAT136ItemEvents(replay, "tool-failed")
	if len(failedEvents) != 2 || failedEvents[1].Payload.Status != "failed" ||
		failedEvents[1].Payload.ResultSummary == nil || failedEvents[1].Payload.ItemError == nil ||
		failedEvents[1].Payload.ItemError.Code != "tool_failed" {
		t.Fatalf("failed Tool result did not retain safe metadata plus stable error: %+v", failedEvents)
	}
	declinedEvents := filterFEAT136ItemEvents(replay, "tool-runtime-declined")
	if len(declinedEvents) != 2 || declinedEvents[1].Payload.Status != "failed" ||
		declinedEvents[1].Payload.ItemError == nil || declinedEvents[1].Payload.ItemError.Code != "protocol_error" ||
		declinedEvents[1].Payload.ResultSummary != nil {
		t.Fatalf("Runtime manufactured the reserved Tool declined status: %+v", declinedEvents)
	}
	for _, event := range replay {
		if event.Payload.Status == "declined" ||
			(event.Payload.ItemError != nil && event.Payload.ItemError.Code == "tool_declined") {
			t.Fatalf("Runtime produced reserved Tool decline authority: %+v", event)
		}
	}

	encoded, err := json.Marshal(replay)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{progressCanary, argumentCanary, resultCanary, serverCanary, toolCanary} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("Tool projection leaked raw value %q: %s", forbidden, encoded)
		}
	}
}

func TestFEAT136GenericAllowlistIsV5ClosedAndV4Isolated(t *testing.T) {
	allowed := []string{
		"userMessage", "hookPrompt", "reasoning", "collabAgentToolCall", "subAgentActivity",
		"webSearch", "imageView", "sleep", "imageGeneration", "enteredReviewMode",
		"exitedReviewMode", "contextCompaction",
	}
	for _, itemType := range allowed {
		if !isV5GenericItemType(itemType) {
			t.Fatalf("v5 generic allowlist omitted %q", itemType)
		}
	}
	excluded := []string{"fileChange", "dynamicToolCall", "plan", "unknown"}
	for _, itemType := range excluded {
		if isV5GenericItemType(itemType) {
			t.Fatalf("v5 generic allowlist admitted excluded type %q", itemType)
		}
	}

	harness := newFEAT136ProjectionHarness(t)
	itemTypes := append([]string{"userMessage"}, excluded...)
	for index, itemType := range itemTypes {
		harness.service.HandleNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{
			"threadId": testThreadID, "turnId": testTurnID,
			"item": map[string]any{"id": "generic-" + string(rune('a'+index)), "type": itemType},
		}))
	}

	v5Replay := feat136V5Replay(t, harness.service)
	if len(v5Replay) != 1 || v5Replay[0].Payload.ItemType != "userMessage" {
		t.Fatalf("v5 generic projection was not closed: %+v", v5Replay)
	}
	validateFEAT136Events(t, compileAgentSessionEventV5Contract(t), v5Replay)

	v4Replay := feat136V4Replay(t, harness.service)
	if len(v4Replay) != len(itemTypes) {
		t.Fatalf("v5 allowlist changed v4 lifecycle behavior: %+v", v4Replay)
	}
	v4Contract := compileAgentSessionEventV4Contract(t)
	for index, event := range v4Replay {
		if event.Payload.ItemType != itemTypes[index] {
			t.Fatalf("v4 item type changed at %d: %+v", index, event)
		}
		validateFEAT134Event(t, v4Contract, event)
	}
}

func TestFEAT136EventHubReplayUsesEventIdentity(t *testing.T) {
	hub := NewEventHubVersion(EventSchemaVersionV5, 8, 2)
	record := Record{TaskID: testTaskID, AgentSessionID: testSessionID, CodexThreadID: testThreadID}
	delta := "same safe text"
	truncated := false
	event := decoratedV5ItemEvent(record, testTurnID, "command-replay", EventItemCommandOutputDelta, EventPayload{
		Delta: &delta, Truncated: &truncated,
	})
	first, err := hub.Publish(event)
	if err != nil {
		t.Fatal(err)
	}
	second, err := hub.Publish(event)
	if err != nil {
		t.Fatal(err)
	}
	if first.EventID == second.EventID || first.Sequence != 1 || second.Sequence != 2 || first.StreamID != second.StreamID {
		t.Fatalf("distinct equal-text events lost event identity: first=%+v second=%+v", first, second)
	}

	streamID, replay, _, cancel, err := hub.Subscribe(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != 2 || replay[0].EventID != first.EventID || replay[1].EventID != second.EventID {
		t.Fatalf("full replay changed event identity: %+v", replay)
	}
	_, resumed, _, cancelResume, err := hub.Subscribe(testSessionID, streamID, first.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	cancelResume()
	if len(resumed) != 1 || resumed[0].EventID != second.EventID ||
		resumed[0].Payload.Delta == nil || *resumed[0].Payload.Delta != delta {
		t.Fatalf("cursor replay did not retain the second equal-text event: %+v", resumed)
	}
	validateFEAT136Events(t, compileAgentSessionEventV5Contract(t), replay)
}

func TestFEAT136NativeValidatorRejectsExtraFieldsAndIllegalStatus(t *testing.T) {
	record := Record{TaskID: testTaskID, AgentSessionID: testSessionID, CodexThreadID: testThreadID}
	summary := boundedV5Summary("执行只读命令", maxV5CommandSummaryBytes)
	cwd := CommandCwd{Kind: "workspace_root"}
	base := decoratedV5ItemEvent(record, testTurnID, "command-validator", EventItemStarted, EventPayload{
		ItemType: "commandExecution", Status: "running", CommandSummary: &summary, CommandCwd: &cwd,
	})
	if err := validateV5Event(base); err != nil {
		t.Fatalf("valid native v5 event was rejected: %v", err)
	}

	extra := base
	extra.Payload.Text = stringPointer("extra closed-field canary")
	if err := validateV5Event(extra); !errors.Is(err, ErrEventLimitExceeded) {
		t.Fatalf("native v5 validator accepted an extra payload field: %v", err)
	}

	illegalStatus := base
	illegalStatus.Payload.Status = "completed"
	if err := validateV5Event(illegalStatus); !errors.Is(err, ErrEventLimitExceeded) {
		t.Fatalf("native v5 validator accepted a terminal started status: %v", err)
	}
}

func TestFEAT136CompletedReconcilesMissingAndConflictingStart(t *testing.T) {
	t.Run("missing start is reconstructed from safe completed facts", func(t *testing.T) {
		harness := newFEAT136ProjectionHarness(t)
		harness.service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
			"threadId": testThreadID, "turnId": testTurnID,
			"item": map[string]any{
				"id": "command-recovered", "type": "commandExecution", "status": "completed",
				"cwd": harness.record.Cwd, "aggregatedOutput": "safe completed output",
			},
		}))
		replay := feat136V5Replay(t, harness.service)
		if len(replay) != 2 || replay[0].EventType != EventItemStarted || replay[1].EventType != EventItemCompleted ||
			replay[1].Payload.Status != "completed" {
			t.Fatalf("completed reconciliation did not close the recovered Item: %+v", replay)
		}
		validateFEAT136Events(t, compileAgentSessionEventV5Contract(t), replay)
	})

	t.Run("conflicting completed kind seals the original Item", func(t *testing.T) {
		harness := newFEAT136ProjectionHarness(t)
		startFEAT136Command(t, harness, "conflicting-item")
		harness.service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
			"threadId": testThreadID, "turnId": testTurnID,
			"item": map[string]any{
				"id": "conflicting-item", "type": "mcpToolCall", "status": "completed",
				"arguments": map[string]any{}, "result": map[string]any{},
			},
		}))
		replay := feat136V5Replay(t, harness.service)
		if len(replay) != 2 || replay[1].Payload.ItemType != "commandExecution" ||
			replay[1].Payload.Status != "failed" || replay[1].Payload.ItemError == nil ||
			replay[1].Payload.ItemError.Code != "protocol_error" {
			t.Fatalf("conflicting lifecycle left the original Item open: %+v", replay)
		}
		validateFEAT136Events(t, compileAgentSessionEventV5Contract(t), replay)
	})
}

func TestFEAT136RedactionDropsWholeSensitiveLines(t *testing.T) {
	input := strings.Join([]string{
		"safe line",
		`\\\\server name\\private-share`,
		"/Users/example/My Project/private.txt",
		"path:[/Users/example/private.txt]",
		"Bear\u202eer short-canary",
		"Bearer\u00a0short-canary",
		"Bear\u200ber short-canary",
		"AWS_SECRET_ACCESS_KEY=short-canary",
		"Authorization: short-canary",
	}, "\n")
	projected := redactV5CommandText(input)
	if !strings.Contains(projected, "safe line") || strings.Count(projected, v5RedactedMarker) != 8 {
		t.Fatalf("sensitive lines were not conservatively replaced: %q", projected)
	}
	for _, forbidden := range []string{"server name", "Project", "short-canary", "private-share"} {
		if strings.Contains(projected, forbidden) {
			t.Fatalf("sensitive line fragment leaked after redaction: %q", projected)
		}
	}
}

func TestFEAT136SplitDeltaAndCwdSecretsFailClosed(t *testing.T) {
	harness := newFEAT136ProjectionHarness(t)
	startFEAT136Command(t, harness, "split-secret")
	for _, delta := range []string{"Bear", "er short-token\n"} {
		harness.service.HandleNotification(RuntimeNotificationCommandOutputDelta, rawJSON(t, map[string]any{
			"threadId": testThreadID, "turnId": testTurnID, "itemId": "split-secret", "delta": delta,
		}))
	}
	replay := feat136V5Replay(t, harness.service)
	if len(replay) != 3 || replay[1].Payload.Delta == nil || replay[2].Payload.Delta == nil ||
		!strings.Contains(*replay[1].Payload.Delta, v5RedactedMarker) ||
		!strings.Contains(*replay[2].Payload.Delta, v5RedactedMarker) {
		t.Fatalf("split delta did not retain two safe event identities: %+v", replay)
	}
	encoded, err := json.Marshal(replay)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"Bearer", "short-token"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("split delta reassembled forbidden content %q: %s", forbidden, encoded)
		}
	}

	secretRelative := filepath.Join(harness.record.Cwd, "api_key=shortsecret")
	if cwd := v5CommandCwd(harness.record.Cwd, &secretRelative); cwd.Kind != "redacted" {
		t.Fatalf("sensitive relative cwd was exposed: %+v", cwd)
	}
	rootCandidate := string(filepath.Separator) + "tmp"
	root := string(filepath.Separator)
	if cwd := v5CommandCwd(root, &rootCandidate); cwd.Kind != "redacted" {
		t.Fatalf("filesystem-root workspace admitted an absolute cwd: %+v", cwd)
	}
}

func TestFEAT136CompletedSnapshotRecomputesSafeAuthority(t *testing.T) {
	harness := newFEAT136ProjectionHarness(t)
	harness.service.HandleNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{
			"id": "command-authority", "type": "commandExecution", "status": "inProgress",
			"cwd": harness.record.Cwd, "commandActions": []any{map[string]any{"type": "read"}},
		},
	}))
	completedCwd := filepath.Join(harness.record.Cwd, "reports")
	harness.service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{
			"id": "command-authority", "type": "commandExecution", "status": "completed",
			"cwd": completedCwd, "commandActions": []any{map[string]any{"type": "search"}},
			"aggregatedOutput": "safe completed output",
		},
	}))

	harness.service.HandleNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{
			"id": "tool-authority", "type": "mcpToolCall", "status": "inProgress",
			"arguments": map[string]any{"first": true},
		},
	}))
	harness.service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{
			"id": "tool-authority", "type": "mcpToolCall", "status": "completed",
			"arguments": map[string]any{"first": true, "second": true}, "result": map[string]any{},
		},
	}))

	replay := feat136V5Replay(t, harness.service)
	command := filterFEAT136ItemEvents(replay, "command-authority")
	if len(command) != 2 || command[0].Payload.CommandSummary == nil ||
		command[0].Payload.CommandSummary.Text != "读取工作区内容" ||
		command[1].Payload.CommandSummary == nil || command[1].Payload.CommandSummary.Text != "搜索工作区内容" ||
		command[1].Payload.CommandCwd == nil || command[1].Payload.CommandCwd.Kind != "workspace_relative" ||
		!equalFEAT136Strings(command[1].Payload.CommandCwd.Segments, []string{"reports"}) {
		t.Fatalf("Command completed snapshot did not replace started authority: %+v", command)
	}
	tool := filterFEAT136ItemEvents(replay, "tool-authority")
	if len(tool) != 2 || tool[0].Payload.ArgumentsSummary == nil ||
		tool[0].Payload.ArgumentsSummary.Text != "参数包含 1 个结构化字段" ||
		tool[1].Payload.ArgumentsSummary == nil || tool[1].Payload.ArgumentsSummary.Text != "参数包含 2 个结构化字段" {
		t.Fatalf("Tool completed snapshot did not replace started metadata: %+v", tool)
	}
	validateFEAT136Events(t, compileAgentSessionEventV5Contract(t), replay)
}

func TestFEAT136CompactJSONCapAdaptsCompletedOutput(t *testing.T) {
	harness := newFEAT136ProjectionHarness(t)
	startFEAT136Command(t, harness, "escaped-output")
	output := strings.Repeat("<>&", maxV5CommandSnapshotBytes/3)
	if len(output) > maxV5CommandSnapshotBytes {
		t.Fatal("ordinary output fixture exceeded the UTF-8 snapshot cap")
	}
	harness.service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{
			"id": "escaped-output", "type": "commandExecution", "status": "completed",
			"cwd": harness.record.Cwd, "commandActions": []any{}, "aggregatedOutput": output,
		},
	}))
	replay := feat136V5Replay(t, harness.service)
	items := filterFEAT136ItemEvents(replay, "escaped-output")
	if len(items) != 2 || items[1].Payload.Output == nil || items[1].Payload.Output.Retention != "head_tail" {
		t.Fatalf("JSON expansion did not produce a closed completed snapshot: %+v", items)
	}
	encoded, err := json.Marshal(items[1])
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > maxRetainedEventDataBytes {
		t.Fatalf("adapted completed event exceeds compact SSE cap: %d", len(encoded))
	}
	validateFEAT136Events(t, compileAgentSessionEventV5Contract(t), items)
}

func TestFEAT136V5GateOffDoesNotTouchLegacyOrPending(t *testing.T) {
	store := openBoundFEAT134Store(t)
	if _, err := store.BindTurn(testSessionID, testTurnID); err != nil {
		t.Fatal(err)
	}
	v4 := NewEventHubVersion(EventSchemaVersionV4, 16, 4)
	service := NewService(&fakeRuntime{}, store, NewEventHub(16, 4), nil, WithV4Events(v4))
	unknownThread := "019c0123-4567-7abc-8123-456789abcdef"
	service.HandleNotification(RuntimeNotificationCommandOutputDelta, rawJSON(t, map[string]any{
		"threadId": unknownThread, "turnId": testTurnID, "itemId": "gate-off", "delta": "safe",
	}))
	service.HandleNotification(RuntimeNotificationMcpToolProgress, rawJSON(t, map[string]any{
		"threadId": unknownThread, "turnId": testTurnID, "itemId": "gate-off", "message": "safe",
	}))
	service.pendingMu.Lock()
	pendingCount := service.pendingCount
	service.pendingMu.Unlock()
	if pendingCount != 0 {
		t.Fatalf("disabled v5 notifications consumed shared pending capacity: %d", pendingCount)
	}

	service.HandleNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{
			"id": "legacy-command", "type": "commandExecution",
			"cwd": []any{"unexpected"}, "status": map[string]any{"unexpected": true},
			"commandActions": "unexpected",
		},
	}))
	_, replay, _, cancel, err := service.SubscribeEventsV4(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != 1 || replay[0].Payload.ItemType != "commandExecution" {
		t.Fatalf("v5 typed fields changed the legacy lifecycle: %+v", replay)
	}
	validateFEAT134Event(t, compileAgentSessionEventV4Contract(t), replay[0])
}

func TestFEAT136LateLifecycleAfterTurnTerminalIsIgnored(t *testing.T) {
	harness := newFEAT136ProjectionHarness(t)
	startFEAT136Command(t, harness, "before-terminal")
	harness.service.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID,
		"turn":     map[string]any{"id": testTurnID, "status": "completed"},
	}))
	for _, itemID := range []string{"before-terminal", "after-terminal"} {
		harness.service.HandleNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{
			"threadId": testThreadID, "turnId": testTurnID,
			"item": map[string]any{
				"id": itemID, "type": "commandExecution", "status": "inProgress", "cwd": harness.record.Cwd,
			},
		}))
	}
	replay := feat136V5Replay(t, harness.service)
	if len(replay) != 2 || replay[0].ItemID != "before-terminal" ||
		replay[1].EventType != EventTurnCompleted || !replay[1].Terminal {
		t.Fatalf("late lifecycle recreated an Item after terminal authority: %+v", replay)
	}
	validateFEAT136Events(t, compileAgentSessionEventV5Contract(t), replay)
}

func TestFEAT136MetadataProjectionIsStreamingAndBounded(t *testing.T) {
	if summary, err := v5JSONMetadataSummary(json.RawMessage(`{"a":{"nested":[1,2]},"b":true}`), "参数"); err != nil || summary != "参数包含 2 个结构化字段" {
		t.Fatalf("unexpected streaming metadata summary: %q %v", summary, err)
	}
	oversize := json.RawMessage(`"` + strings.Repeat("safe", maxV5MetadataInputBytes/4+1) + `"`)
	if _, err := v5JSONMetadataSummary(oversize, "参数"); !errors.Is(err, ErrEventLimitExceeded) {
		t.Fatalf("oversize metadata did not fail at the raw byte bound: %v", err)
	}
	deep := json.RawMessage(strings.Repeat("[", maxV5MetadataDepth+1) + "null" + strings.Repeat("]", maxV5MetadataDepth+1))
	if _, err := v5JSONMetadataSummary(deep, "参数"); err == nil {
		t.Fatal("deep metadata exceeded the safe nesting bound without an error")
	}
}

func TestFEAT136PendingStartCannotBeOvertakenByCompleted(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	v5 := NewEventHubVersion(EventSchemaVersionV5, 16, 4)
	completed := make(chan struct{})
	var service *Service
	runtime := &fakeRuntime{startThread: func(cwd string) (codex.ThreadInfo, error) {
		service.HandleNotification(RuntimeNotificationTurnStarted, rawJSON(t, map[string]any{
			"threadId": testThreadID,
			"turn":     map[string]any{"id": testTurnID, "status": "inProgress"},
		}))
		service.HandleNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{
			"threadId": testThreadID, "turnId": testTurnID,
			"item": map[string]any{
				"id": "pending-command", "type": "commandExecution", "status": "inProgress", "cwd": cwd,
			},
		}))
		go func() {
			defer close(completed)
			service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
				"threadId": testThreadID, "turnId": testTurnID,
				"item": map[string]any{
					"id": "pending-command", "type": "commandExecution", "status": "completed",
					"cwd": cwd, "aggregatedOutput": "safe output",
				},
			}))
		}()
		return codex.ThreadInfo{
			ID: testThreadID, RuntimeSession: "runtime-session",
			Model: codex.MiniMaxModel, ModelProvider: codex.MiniMaxProviderID,
		}, nil
	}}
	service = NewService(runtime, store, NewEventHub(16, 4), nil,
		WithV4Events(NewEventHubVersion(EventSchemaVersionV4, 16, 4)), WithV5Events(v5))
	record, err := service.StartSession(context.Background(), StartSessionInput{TaskID: testTaskID, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	<-completed
	_, replay, _, cancel, err := service.SubscribeEventsV5(record.AgentSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	itemEvents := filterFEAT136ItemEvents(replay, "pending-command")
	if len(itemEvents) != 2 || itemEvents[0].EventType != EventItemStarted ||
		itemEvents[1].EventType != EventItemCompleted || itemEvents[1].Payload.Status != "completed" {
		t.Fatalf("pending start was overtaken by completed: %+v", replay)
	}
	validateFEAT136Events(t, compileAgentSessionEventV5Contract(t), replay)
}

func newFEAT136ProjectionHarness(t *testing.T) feat136ProjectionHarness {
	t.Helper()
	store := openBoundFEAT134Store(t)
	record, err := store.BindTurn(testSessionID, testTurnID)
	if err != nil {
		t.Fatal(err)
	}
	v4 := NewEventHubVersion(EventSchemaVersionV4, 256, 8)
	v5 := NewEventHubVersion(EventSchemaVersionV5, 256, 8)
	service := NewService(
		&fakeRuntime{}, store, NewEventHub(256, 8), nil,
		WithV4Events(v4), WithV5Events(v5),
	)
	return feat136ProjectionHarness{service: service, v4: v4, v5: v5, record: record}
}

func startFEAT136Command(t *testing.T, harness feat136ProjectionHarness, itemID string) {
	t.Helper()
	harness.service.HandleNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{
			"id": itemID, "type": "commandExecution", "status": "inProgress", "cwd": harness.record.Cwd,
		},
	}))
}

func feat136V5Replay(t *testing.T, service *Service) []Event {
	t.Helper()
	_, replay, _, cancel, err := service.SubscribeEventsV5(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	return replay
}

func feat136V4Replay(t *testing.T, service *Service) []Event {
	t.Helper()
	_, replay, _, cancel, err := service.SubscribeEventsV4(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	return replay
}

func filterFEAT136ItemEvents(events []Event, itemID string) []Event {
	filtered := make([]Event, 0)
	for _, event := range events {
		if event.ItemID == itemID {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func equalFEAT136Strings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func compileAgentSessionEventV5Contract(t *testing.T) *jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	compiler.UseRegexpEngine(compileFEAT136ECMAScriptRegexp)
	contract, err := compiler.Compile(feat136RepositoryPath(
		t, "api", "jsonschema", "agent-session-event-v5.schema.json",
	))
	if err != nil {
		t.Fatalf("compile AgentSessionEventV5 JSON Schema: %v", err)
	}
	return contract
}

type feat136ECMAScriptRegexp regexp2.Regexp

func (expression *feat136ECMAScriptRegexp) MatchString(value string) bool {
	matched, err := (*regexp2.Regexp)(expression).MatchString(value)
	return err == nil && matched
}

func (expression *feat136ECMAScriptRegexp) String() string {
	return (*regexp2.Regexp)(expression).String()
}

func compileFEAT136ECMAScriptRegexp(value string) (jsonschema.Regexp, error) {
	expression, err := regexp2.Compile(value, regexp2.ECMAScript)
	if err != nil {
		return nil, err
	}
	return (*feat136ECMAScriptRegexp)(expression), nil
}

func feat136RepositoryPath(t *testing.T, elements ...string) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate FEAT-136 test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	return filepath.Join(append([]string{root}, elements...)...)
}

func validateFEAT136Events(t *testing.T, contract *jsonschema.Schema, events []Event) {
	t.Helper()
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if len(encoded) > maxRetainedEventDataBytes {
			t.Fatalf("retained v5 event exceeds the compact SSE data cap: %d", len(encoded))
		}
		validateFEAT136JSON(t, contract, encoded)
	}
}

func validateFEAT136JSON(t *testing.T, contract *jsonschema.Schema, encoded []byte) {
	t.Helper()
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("decode v5 event for contract validation: %v", err)
	}
	if err := contract.Validate(instance); err != nil {
		t.Fatalf("v5 event violates AgentSessionEventV5 JSON Schema: %v\n%s", err, encoded)
	}
}
