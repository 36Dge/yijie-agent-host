package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxV5CommandSummaryBytes        = 4 << 10
	maxV5CommandCwdBytes            = 1 << 10
	maxV5CommandDeltaBytes          = 16 << 10
	maxV5CommandLiveBytes           = 256 << 10
	maxV5CommandSnapshotBytes       = 256 << 10
	maxV5CommandSnapshotPartBytes   = 128 << 10
	maxV5ToolIdentityBytes          = 256
	maxV5ToolArgumentsBytes         = 8 << 10
	maxV5ToolProgressBytes          = 4 << 10
	maxV5ToolProgressEvents         = 32
	maxV5ToolProgressTotalBytes     = 64 << 10
	maxV5ToolResultBytes            = 64 << 10
	maxV5ErrorSummaryBytes          = 4 << 10
	maxV5MetadataInputBytes         = 1 << 20
	maxV5MetadataDepth              = 64
	maxV5DurationMilliseconds       = int64(9007199254740991)
	v5TruncationReasonUTF8ByteLimit = "utf8_byte_limit"
	v5CommandLiveTruncationMarker   = "\n[输出已截断]\n"
)

var v5WindowsDevicePattern = regexp.MustCompile(`(?i)^(?:con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\..*)?$`)

type BoundedSummary struct {
	Text             string `json:"text"`
	Truncated        bool   `json:"truncated"`
	TruncationReason string `json:"truncation_reason,omitempty"`
}

type CommandCwd struct {
	Kind     string   `json:"kind"`
	Segments []string `json:"segments,omitempty"`
}

type CommandOutput struct {
	Retention        string  `json:"retention"`
	Text             *string `json:"text,omitempty"`
	Head             *string `json:"head,omitempty"`
	Tail             *string `json:"tail,omitempty"`
	Reason           string  `json:"reason,omitempty"`
	Truncated        bool    `json:"truncated"`
	TruncationReason string  `json:"truncation_reason,omitempty"`
}

type ProjectionError struct {
	Code    string `json:"code"`
	Summary string `json:"summary"`
}

type ToolIdentity struct {
	Resolution string `json:"resolution"`
	ServerName string `json:"server_name"`
	ToolName   string `json:"tool_name"`
}

type v5ItemKey struct {
	sessionID string
	turnID    string
	itemID    string
}

type v5ItemState struct {
	kind          string
	sealed        bool
	command       BoundedSummary
	cwd           CommandCwd
	identity      ToolIdentity
	arguments     BoundedSummary
	liveBytes     int
	liveCapped    bool
	liveContinued bool
	progressCount int
	progressBytes int
}

func (s *Service) publishV4Projection(event Event) (Event, error) {
	published, err := s.eventsV4.Publish(event)
	if err != nil {
		return Event{}, err
	}
	if (s.eventsV5 == nil && s.eventsV6 == nil) || !isV5InheritedEvent(event) {
		return published, nil
	}
	if v5Err := s.publishV5Projection(event); v5Err != nil {
		s.logger.Warn("discarding invalid inherited v5 projection", "event_type", event.EventType)
		return published, nil
	}
	if event.Terminal {
		s.clearV5Turn(event.AgentSessionID, event.TurnID)
	}
	return published, nil
}

func (s *Service) publishV5Projection(event Event) error {
	if s.eventsV5 != nil {
		if _, err := s.eventsV5.Publish(event); err != nil {
			return err
		}
	}
	if s.eventsV6 != nil {
		if _, err := s.eventsV6.Publish(event); err != nil {
			return err
		}
	}
	return nil
}

func isV5InheritedEvent(event Event) bool {
	if event.EventType != EventItemStarted && event.EventType != EventItemCompleted {
		return true
	}
	if event.Payload.ItemType == "commandExecution" || event.Payload.ItemType == "mcpToolCall" {
		return false
	}
	if event.Payload.ItemType == "agentMessage" {
		return true
	}
	return isV5GenericItemType(event.Payload.ItemType)
}

func isV5GenericItemType(value string) bool {
	switch value {
	case "userMessage", "hookPrompt", "reasoning", "collabAgentToolCall", "subAgentActivity",
		"webSearch", "imageView", "sleep", "imageGeneration", "enteredReviewMode",
		"exitedReviewMode", "contextCompaction":
		return true
	default:
		return false
	}
}

func (s *Service) publishV5Lifecycle(
	record Record,
	method string,
	raw json.RawMessage,
	envelope itemNotification,
) error {
	if s.eventsV5 == nil && s.eventsV6 == nil {
		return nil
	}
	// A terminal Turn is the final authority. Late lifecycle notifications must
	// never recreate running Items after turn.completed cleared the live state.
	if record.ActiveTurnID == "" || record.ActiveTurnID != envelope.TurnID {
		return nil
	}
	key := v5ItemKey{sessionID: record.AgentSessionID, turnID: envelope.TurnID, itemID: envelope.Item.ID}
	s.v5ItemsMu.Lock()
	defer s.v5ItemsMu.Unlock()

	state := s.v5Items[key]
	if method == RuntimeNotificationItemStarted {
		if state != nil {
			return nil
		}
	}

	var notification v5LifecycleNotification
	if err := json.Unmarshal(raw, &notification); err != nil ||
		notification.ThreadID != envelope.ThreadID || notification.TurnID != envelope.TurnID ||
		notification.Item.ID != envelope.Item.ID || notification.Item.Type != envelope.Item.Type {
		return s.publishV5ProtocolFailureLocked(record, method, key, envelope.Item.Type, state)
	}

	if method == RuntimeNotificationItemStarted {
		created, event, err := newV5StartedProjection(record, notification)
		if err != nil {
			return s.publishV5ProtocolFailureLocked(record, method, key, envelope.Item.Type, nil)
		}
		s.v5Items[key] = created
		if err := s.publishV5Projection(event); err != nil {
			delete(s.v5Items, key)
			return err
		}
		return nil
	}

	if method != RuntimeNotificationItemCompleted {
		return errors.New("unsupported v5 lifecycle method")
	}
	if state == nil {
		recovered, started, err := newV5RecoveredStartedProjection(record, notification)
		if err != nil {
			return s.publishV5ProtocolFailureLocked(record, method, key, envelope.Item.Type, nil)
		}
		s.v5Items[key] = recovered
		if err := s.publishV5Projection(started); err != nil {
			delete(s.v5Items, key)
			return err
		}
		state = recovered
	}
	if state.sealed {
		return nil
	}
	if state.kind != notification.Item.Type {
		event := newV5ProtocolCompleted(record, notification.TurnID, notification.Item.ID, state)
		state.sealed = true
		if err := s.publishV5Projection(event); err != nil {
			state.sealed = false
			return err
		}
		return nil
	}
	event, err := newV5CompletedProjection(record, notification, state)
	if err != nil {
		event = newV5ProtocolCompleted(record, notification.TurnID, notification.Item.ID, state)
	}
	state.sealed = true
	if err := s.publishV5Projection(event); err != nil {
		state.sealed = false
		return err
	}
	return nil
}

func (s *Service) publishV5ProtocolFailureLocked(
	record Record,
	method string,
	key v5ItemKey,
	kind string,
	state *v5ItemState,
) error {
	if state == nil {
		state = fallbackV5ItemState(kind)
		if state == nil {
			return errors.New("unsupported malformed v5 lifecycle item type")
		}
		started := fallbackV5StartedEvent(record, key.turnID, key.itemID, state)
		s.v5Items[key] = state
		if err := s.publishV5Projection(started); err != nil {
			delete(s.v5Items, key)
			return err
		}
	} else if method == RuntimeNotificationItemStarted {
		return nil
	}
	if state.sealed {
		return nil
	}
	completed := newV5ProtocolCompleted(record, key.turnID, key.itemID, state)
	state.sealed = true
	if err := s.publishV5Projection(completed); err != nil {
		state.sealed = false
		return err
	}
	return nil
}

func fallbackV5ItemState(kind string) *v5ItemState {
	state := &v5ItemState{kind: kind}
	switch kind {
	case "commandExecution":
		state.command = boundedV5Summary("执行命令", maxV5CommandSummaryBytes)
		state.cwd = CommandCwd{Kind: "redacted"}
	case "mcpToolCall":
		state.identity = unknownV5ToolIdentity()
		state.arguments = boundedV5Summary("参数不可用", maxV5ToolArgumentsBytes)
	default:
		return nil
	}
	return state
}

func fallbackV5StartedEvent(record Record, turnID, itemID string, state *v5ItemState) Event {
	payload := EventPayload{ItemType: state.kind}
	if state.kind == "commandExecution" {
		payload.Status = "running"
		payload.CommandSummary = &state.command
		payload.CommandCwd = &state.cwd
	} else {
		payload.Status = "in_progress"
		payload.ToolIdentity = &state.identity
		payload.ArgumentsSummary = &state.arguments
	}
	return decoratedV5ItemEvent(record, turnID, itemID, EventItemStarted, payload)
}

func newV5RecoveredStartedProjection(record Record, notification v5LifecycleNotification) (*v5ItemState, Event, error) {
	state := &v5ItemState{kind: notification.Item.Type}
	payload := EventPayload{ItemType: notification.Item.Type}
	switch notification.Item.Type {
	case "commandExecution":
		state.command = v5CommandSummary(notification)
		state.cwd = v5CommandCwd(record.Cwd, notification.Item.Cwd)
		payload.Status = "running"
		payload.CommandSummary = &state.command
		payload.CommandCwd = &state.cwd
	case "mcpToolCall":
		arguments, err := v5JSONMetadataSummary(notification.Item.Arguments, "参数")
		if err != nil {
			return nil, Event{}, errors.New("tool completed lifecycle has invalid arguments")
		}
		state.identity = unknownV5ToolIdentity()
		state.arguments = boundedV5Summary(arguments, maxV5ToolArgumentsBytes)
		payload.Status = "in_progress"
		payload.ToolIdentity = &state.identity
		payload.ArgumentsSummary = &state.arguments
	default:
		return nil, Event{}, errors.New("unsupported recovered v5 lifecycle item type")
	}
	return state, decoratedV5ItemEvent(record, notification.TurnID, notification.Item.ID, EventItemStarted, payload), nil
}

func newV5ProtocolCompleted(record Record, turnID, itemID string, state *v5ItemState) Event {
	payload := EventPayload{ItemType: state.kind, Status: "failed"}
	if state.kind == "commandExecution" {
		output := CommandOutput{Retention: "unavailable", Reason: "not_available"}
		payload.CommandSummary = &state.command
		payload.CommandCwd = &state.cwd
		payload.Output = &output
		payload.ItemError = &ProjectionError{Code: "protocol_error", Summary: "命令生命周期发生冲突"}
	} else {
		payload.ToolIdentity = &state.identity
		payload.ArgumentsSummary = &state.arguments
		payload.ItemError = &ProjectionError{Code: "protocol_error", Summary: "工具生命周期发生冲突"}
	}
	return decoratedV5ItemEvent(record, turnID, itemID, EventItemCompleted, payload)
}

func newV5StartedProjection(record Record, notification v5LifecycleNotification) (*v5ItemState, Event, error) {
	payload := EventPayload{ItemType: notification.Item.Type}
	state := &v5ItemState{kind: notification.Item.Type}
	switch notification.Item.Type {
	case "commandExecution":
		if notification.Item.Status == nil || *notification.Item.Status != "inProgress" {
			return nil, Event{}, errors.New("command started lifecycle has invalid status")
		}
		state.command = v5CommandSummary(notification)
		state.cwd = v5CommandCwd(record.Cwd, notification.Item.Cwd)
		payload.Status = "running"
		payload.CommandSummary = &state.command
		payload.CommandCwd = &state.cwd
	case "mcpToolCall":
		if notification.Item.Status == nil || *notification.Item.Status != "inProgress" {
			return nil, Event{}, errors.New("tool started lifecycle has invalid status")
		}
		arguments, err := v5JSONMetadataSummary(notification.Item.Arguments, "参数")
		if err != nil {
			return nil, Event{}, errors.New("tool started lifecycle has invalid arguments")
		}
		state.identity = unknownV5ToolIdentity()
		state.arguments = boundedV5Summary(arguments, maxV5ToolArgumentsBytes)
		payload.Status = "in_progress"
		payload.ToolIdentity = &state.identity
		payload.ArgumentsSummary = &state.arguments
	default:
		return nil, Event{}, errors.New("unsupported v5 lifecycle item type")
	}
	return state, decoratedV5ItemEvent(record, notification.TurnID, notification.Item.ID, EventItemStarted, payload), nil
}

func newV5CompletedProjection(record Record, notification v5LifecycleNotification, state *v5ItemState) (Event, error) {
	if state.kind == "commandExecution" {
		return newV5CommandCompleted(record, notification, state), nil
	}
	if state.kind == "mcpToolCall" {
		return newV5ToolCompleted(record, notification, state), nil
	}
	return Event{}, errors.New("unsupported v5 completed item type")
}

func newV5CommandCompleted(record Record, notification v5LifecycleNotification, _ *v5ItemState) Event {
	status, projectionError := v5CommandTerminalStatus(notification.Item.Status)
	output := v5CommandOutput(notification.Item.AggregatedOutput)
	// Runtime completed Items carry a complete snapshot. Recompute every safe
	// projection from that authority rather than retaining potentially stale
	// started fields.
	command := v5CommandSummary(notification)
	cwd := v5CommandCwd(record.Cwd, notification.Item.Cwd)
	duration := validV5Duration(notification.Item.DurationMS)
	if notification.Item.DurationMS != nil && duration == nil {
		status = "failed"
		projectionError = &ProjectionError{Code: "protocol_error", Summary: "命令完成信息无效"}
	}
	payload := EventPayload{
		ItemType:       "commandExecution",
		Status:         status,
		CommandSummary: &command,
		CommandCwd:     &cwd,
		DurationMS:     duration,
		ExitCode:       notification.Item.ExitCode,
		Output:         &output,
		ItemError:      projectionError,
	}
	return decoratedV5ItemEvent(record, notification.TurnID, notification.Item.ID, EventItemCompleted, payload)
}

func v5CommandTerminalStatus(status *string) (string, *ProjectionError) {
	if status == nil {
		return "failed", &ProjectionError{Code: "protocol_error", Summary: "命令完成状态缺失"}
	}
	switch *status {
	case "completed":
		return "completed", nil
	case "failed":
		return "failed", &ProjectionError{Code: "command_failed", Summary: "命令执行失败"}
	case "declined":
		return "declined", &ProjectionError{Code: "command_declined", Summary: "命令未获执行"}
	default:
		return "failed", &ProjectionError{Code: "protocol_error", Summary: "命令完成状态无效"}
	}
}

func newV5ToolCompleted(record Record, notification v5LifecycleNotification, state *v5ItemState) Event {
	status := "failed"
	var projectionError *ProjectionError
	var resultSummary *BoundedSummary
	argumentsSummary := state.arguments
	arguments, argumentsErr := v5JSONMetadataSummary(notification.Item.Arguments, "参数")
	if argumentsErr == nil {
		argumentsSummary = boundedV5Summary(arguments, maxV5ToolArgumentsBytes)
	} else {
		projectionError = &ProjectionError{Code: "protocol_error", Summary: "工具完成参数无效"}
	}
	if notification.Item.Status == nil {
		projectionError = &ProjectionError{Code: "protocol_error", Summary: "工具完成状态缺失"}
	} else if argumentsErr == nil {
		switch *notification.Item.Status {
		case "completed":
			if summary, err := v5JSONMetadataSummary(notification.Item.Result, "结果"); err == nil {
				bounded := boundedV5Summary(summary, maxV5ToolResultBytes)
				resultSummary = &bounded
				status = "completed"
			} else {
				projectionError = &ProjectionError{Code: "protocol_error", Summary: "工具完成结果无效"}
			}
		case "failed":
			projectionError = &ProjectionError{Code: "tool_failed", Summary: "工具执行失败"}
			if len(bytes.TrimSpace(notification.Item.Result)) > 0 && string(bytes.TrimSpace(notification.Item.Result)) != "null" {
				if summary, err := v5JSONMetadataSummary(notification.Item.Result, "结果"); err == nil {
					bounded := boundedV5Summary(summary, maxV5ToolResultBytes)
					resultSummary = &bounded
				}
			}
		default:
			projectionError = &ProjectionError{Code: "protocol_error", Summary: "工具完成状态无效"}
		}
	}
	duration := validV5Duration(notification.Item.DurationMS)
	if notification.Item.DurationMS != nil && duration == nil {
		status = "failed"
		resultSummary = nil
		projectionError = &ProjectionError{Code: "protocol_error", Summary: "工具完成信息无效"}
	}
	payload := EventPayload{
		ItemType:         "mcpToolCall",
		Status:           status,
		ToolIdentity:     &state.identity,
		ArgumentsSummary: &argumentsSummary,
		DurationMS:       duration,
		ResultSummary:    resultSummary,
		ItemError:        projectionError,
	}
	return decoratedV5ItemEvent(record, notification.TurnID, notification.Item.ID, EventItemCompleted, payload)
}

func (s *Service) publishV5CommandDelta(record Record, notification commandOutputDeltaNotification) error {
	if (s.eventsV5 == nil && s.eventsV6 == nil) || notification.Delta == nil {
		return nil
	}
	if record.ActiveTurnID == "" || record.ActiveTurnID != notification.TurnID {
		return nil
	}
	key := v5ItemKey{sessionID: record.AgentSessionID, turnID: notification.TurnID, itemID: notification.ItemID}
	s.v5ItemsMu.Lock()
	defer s.v5ItemsMu.Unlock()
	state := s.v5Items[key]
	if state == nil || state.kind != "commandExecution" || state.sealed || state.liveCapped {
		return nil
	}
	delta, continued := redactV5CommandDelta(*notification.Delta, state.liveContinued)
	state.liveContinued = continued
	if delta == "" {
		return nil
	}
	contentCap := maxV5CommandLiveBytes - len(v5CommandLiveTruncationMarker)
	remaining := contentCap - state.liveBytes
	if remaining <= 0 {
		payload := EventPayload{
			Delta:            stringPointer(v5CommandLiveTruncationMarker),
			Truncated:        boolPointer(true),
			TruncationReason: v5TruncationReasonUTF8ByteLimit,
		}
		event := decoratedV5ItemEvent(record, notification.TurnID, notification.ItemID, EventItemCommandOutputDelta, payload)
		if err := s.publishV5Projection(event); err != nil {
			return err
		}
		state.liveBytes += len(v5CommandLiveTruncationMarker)
		state.liveCapped = true
		return nil
	}

	aggregateTruncated := len(delta) > remaining
	limit := maxV5CommandDeltaBytes
	needsMarker := aggregateTruncated || len(delta) > maxV5CommandDeltaBytes
	if needsMarker {
		limit -= len(v5CommandLiveTruncationMarker)
	}
	if remaining < limit {
		limit = remaining
	}
	retained, eventTruncated := truncateV5UTF8(delta, limit)
	if retained == "" && !needsMarker {
		return nil
	}
	if needsMarker {
		retained += v5CommandLiveTruncationMarker
	}
	if aggregateTruncated {
		state.liveCapped = true
	}
	truncated := eventTruncated || aggregateTruncated
	payload := EventPayload{Delta: stringPointer(retained), Truncated: boolPointer(truncated)}
	if truncated {
		payload.TruncationReason = v5TruncationReasonUTF8ByteLimit
	}
	event := decoratedV5ItemEvent(record, notification.TurnID, notification.ItemID, EventItemCommandOutputDelta, payload)
	if err := s.publishV5Projection(event); err != nil {
		return err
	}
	state.liveBytes += len(retained)
	return nil
}

func (s *Service) publishV5ToolProgress(record Record, notification toolProgressNotification) error {
	if s.eventsV5 == nil && s.eventsV6 == nil {
		return nil
	}
	if record.ActiveTurnID == "" || record.ActiveTurnID != notification.TurnID {
		return nil
	}
	key := v5ItemKey{sessionID: record.AgentSessionID, turnID: notification.TurnID, itemID: notification.ItemID}
	s.v5ItemsMu.Lock()
	defer s.v5ItemsMu.Unlock()
	state := s.v5Items[key]
	if state == nil || state.kind != "mcpToolCall" || state.sealed || state.progressCount >= maxV5ToolProgressEvents {
		return nil
	}
	summary := boundedV5Summary("工具正在执行", maxV5ToolProgressBytes)
	if state.progressBytes+len(summary.Text) > maxV5ToolProgressTotalBytes {
		return nil
	}
	index := state.progressCount
	payload := EventPayload{
		ItemType:      "mcpToolCall",
		Status:        "in_progress",
		ToolIdentity:  &state.identity,
		ProgressIndex: &index,
		Summary:       &summary,
	}
	event := decoratedV5ItemEvent(record, notification.TurnID, notification.ItemID, EventItemToolProgress, payload)
	if err := s.publishV5Projection(event); err != nil {
		return err
	}
	state.progressCount++
	state.progressBytes += len(summary.Text)
	return nil
}

func decoratedV5ItemEvent(record Record, turnID, itemID, eventType string, payload EventPayload) Event {
	event := Event{TurnID: turnID, ItemID: itemID, EventType: eventType, Payload: payload}
	decorateEvent(record, &event)
	return event
}

func v5CommandSummary(notification v5LifecycleNotification) BoundedSummary {
	label := "执行命令"
	if len(notification.Item.CommandActions) == 1 {
		switch notification.Item.CommandActions[0].Type {
		case "read":
			label = "读取工作区内容"
		case "listFiles":
			label = "列出工作区文件"
		case "search":
			label = "搜索工作区内容"
		}
	}
	return boundedV5Summary(label, maxV5CommandSummaryBytes)
}

func v5CommandCwd(workspace string, runtimeCwd *string) CommandCwd {
	redacted := CommandCwd{Kind: "redacted"}
	if runtimeCwd == nil || *runtimeCwd == "" || workspace == "" {
		return redacted
	}
	root := filepath.Clean(workspace)
	candidate := filepath.Clean(*runtimeCwd)
	if filepath.Dir(root) == root {
		return redacted
	}
	if candidate == root {
		return CommandCwd{Kind: "workspace_root"}
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return redacted
	}
	segments := strings.Split(relative, string(filepath.Separator))
	if !validV5CwdSegments(segments) {
		return redacted
	}
	return CommandCwd{Kind: "workspace_relative", Segments: segments}
}

func validV5CwdSegments(segments []string) bool {
	if len(segments) < 1 || len(segments) > 128 {
		return false
	}
	total := len(segments) - 1
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." || len(segment) > 255 ||
			strings.ContainsAny(segment, "/\\:") || strings.HasSuffix(segment, " ") || strings.HasSuffix(segment, ".") ||
			v5WindowsDevicePattern.MatchString(segment) {
			return false
		}
		for _, current := range segment {
			if current <= '\u001f' || (current >= '\u007f' && current <= '\u009f') || isV5BidiControl(current) {
				return false
			}
		}
		if normalizeV5CommandText(segment) != segment || v5SensitiveCommandLine(segment) {
			return false
		}
		total += len(segment)
	}
	return total <= maxV5CommandCwdBytes
}

func v5CommandOutput(value *string) CommandOutput {
	if value == nil {
		return CommandOutput{Retention: "unavailable", Reason: "not_available"}
	}
	sanitized := redactV5CommandText(*value)
	if len(sanitized) <= maxV5CommandSnapshotBytes {
		return CommandOutput{Retention: "complete", Text: stringPointer(sanitized)}
	}
	head, _ := truncateV5UTF8(sanitized, maxV5CommandSnapshotPartBytes)
	tail := tailV5UTF8(sanitized, maxV5CommandSnapshotPartBytes)
	return CommandOutput{
		Retention:        "head_tail",
		Head:             stringPointer(head),
		Tail:             stringPointer(tail),
		Truncated:        true,
		TruncationReason: v5TruncationReasonUTF8ByteLimit,
	}
}

// fitV5RetainedEvent applies the compact-JSON transport cap after EventHub has
// assigned the final IDs, sequence and timestamp. Go's JSON encoder expands
// characters such as '<', '>' and '&', so the UTF-8 snapshot cap alone cannot
// prove that the SSE data value remains within 1 MiB. When needed, convert the
// authoritative completed output to a smaller head/tail representation.
func fitV5RetainedEvent(event *Event) error {
	encoded, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if len(encoded) <= maxRetainedEventDataBytes {
		return nil
	}
	if event.EventType != EventItemCompleted || event.Payload.ItemType != "commandExecution" ||
		event.Payload.Output == nil {
		return ErrEventLimitExceeded
	}

	output := event.Payload.Output
	var headSource, tailSource string
	switch output.Retention {
	case "complete":
		if output.Text == nil {
			return ErrEventLimitExceeded
		}
		headSource, tailSource = *output.Text, *output.Text
	case "head_tail":
		if output.Head == nil || output.Tail == nil {
			return ErrEventLimitExceeded
		}
		headSource, tailSource = *output.Head, *output.Tail
	default:
		return ErrEventLimitExceeded
	}

	high := maxV5CommandSnapshotPartBytes
	if len(headSource) < high {
		high = len(headSource)
	}
	if len(tailSource) < high {
		high = len(tailSource)
	}
	low := 0
	best := -1
	var fitted Event
	for low <= high {
		part := low + (high-low)/2
		head, _ := truncateV5UTF8(headSource, part)
		tail := tailV5UTF8(tailSource, part)
		candidate := *event
		candidateOutput := CommandOutput{
			Retention:        "head_tail",
			Head:             stringPointer(head),
			Tail:             stringPointer(tail),
			Truncated:        true,
			TruncationReason: v5TruncationReasonUTF8ByteLimit,
		}
		candidate.Payload.Output = &candidateOutput
		candidateJSON, marshalErr := json.Marshal(candidate)
		if marshalErr != nil {
			return marshalErr
		}
		if len(candidateJSON) <= maxRetainedEventDataBytes {
			best = part
			fitted = candidate
			low = part + 1
		} else {
			high = part - 1
		}
	}
	if best < 0 {
		return ErrEventLimitExceeded
	}
	*event = fitted
	return nil
}

func boundedV5Summary(value string, limit int) BoundedSummary {
	retained, truncated := truncateV5UTF8(value, limit)
	summary := BoundedSummary{Text: retained, Truncated: truncated}
	if truncated {
		summary.TruncationReason = v5TruncationReasonUTF8ByteLimit
	}
	return summary
}

func v5JSONMetadataSummary(raw json.RawMessage, label string) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", errors.New("missing JSON value")
	}
	if len(trimmed) > maxV5MetadataInputBytes {
		return "", ErrEventLimitExceeded
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	first, err := decoder.Token()
	if err != nil {
		return "", err
	}
	var summary string
	switch typed := first.(type) {
	case json.Delim:
		count, countErr := countV5TopLevelJSON(decoder, typed, 1)
		if countErr != nil {
			return "", countErr
		}
		if typed == '{' {
			summary = fmt.Sprintf("%s包含 %d 个结构化字段", label, count)
		} else if typed == '[' {
			summary = fmt.Sprintf("%s包含 %d 个项目", label, count)
		} else {
			return "", errors.New("invalid top-level JSON delimiter")
		}
	case string:
		summary = label + "为文本"
	case json.Number, bool:
		summary = label + "为标量"
	case nil:
		summary = label + "不可用"
	default:
		return "", errors.New("unsupported JSON token")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return "", errors.New("multiple JSON values")
	}
	return summary, nil
}

func countV5TopLevelJSON(decoder *json.Decoder, opening json.Delim, depth int) (int, error) {
	if depth > maxV5MetadataDepth || (opening != '{' && opening != '[') {
		return 0, errors.New("JSON metadata nesting exceeds the safe projection limit")
	}
	count := 0
	for decoder.More() {
		if opening == '{' {
			key, err := decoder.Token()
			if err != nil {
				return 0, err
			}
			if _, ok := key.(string); !ok {
				return 0, errors.New("JSON object key is not a string")
			}
		}
		value, err := decoder.Token()
		if err != nil {
			return 0, err
		}
		if err := skipV5JSONValue(decoder, value, depth+1); err != nil {
			return 0, err
		}
		count++
	}
	closing, err := decoder.Token()
	if err != nil {
		return 0, err
	}
	want := json.Delim('}')
	if opening == '[' {
		want = ']'
	}
	if closing != want {
		return 0, errors.New("JSON metadata has mismatched delimiters")
	}
	return count, nil
}

func skipV5JSONValue(decoder *json.Decoder, token json.Token, depth int) error {
	opening, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	_, err := countV5TopLevelJSON(decoder, opening, depth)
	return err
}

func unknownV5ToolIdentity() ToolIdentity {
	return ToolIdentity{Resolution: "unknown", ServerName: "unknown", ToolName: "unknown"}
}

func validV5Duration(value *int64) *int64 {
	if value == nil || *value < 0 || *value > maxV5DurationMilliseconds {
		return nil
	}
	copy := *value
	return &copy
}

func boolPointer(value bool) *bool { return &value }

func (s *Service) clearV5Turn(sessionID, turnID string) {
	s.v5ItemsMu.Lock()
	for key := range s.v5Items {
		if key.sessionID == sessionID && key.turnID == turnID {
			delete(s.v5Items, key)
		}
	}
	s.v5ItemsMu.Unlock()
}

func (s *Service) clearV5Session(sessionID string) {
	s.v5ItemsMu.Lock()
	for key := range s.v5Items {
		if key.sessionID == sessionID {
			delete(s.v5Items, key)
		}
	}
	s.v5ItemsMu.Unlock()
}

func validateV5RetainedEvent(event Event) error {
	if event.SchemaVersion != EventSchemaVersionV5 || event.Sequence == 0 || event.OccurredAt.IsZero() ||
		!validUUID(event.EventID) || !validUUID(event.StreamID) || !validUUID(event.TaskID) ||
		!validUUID(event.AgentSessionID) || !validUUID(event.CodexThreadID) {
		return ErrEventLimitExceeded
	}
	return validateV5Event(event)
}

func validateV5Event(event Event) error {
	for _, value := range []string{event.TraceID, event.RequestID, event.TenantID, event.UserID} {
		if value != "" && (utf8.RuneCountInString(value) > 256 || len(value) > 1024) {
			return ErrEventLimitExceeded
		}
	}
	if event.ItemID != "" && (utf8.RuneCountInString(event.ItemID) > 256 || len(event.ItemID) > 1024) {
		return ErrEventLimitExceeded
	}
	switch event.EventType {
	case EventItemStarted:
		if !validV4ItemEvent(event) || event.Terminal {
			return ErrEventLimitExceeded
		}
		switch event.Payload.ItemType {
		case "commandExecution":
			if event.Payload.Status != "running" || event.Payload.CommandSummary == nil || event.Payload.CommandCwd == nil ||
				!payloadShape(event.Payload, []string{"item_type", "status", "command_summary", "cwd"}, "item_type", "status", "command_summary", "cwd") ||
				!validV5BoundedSummary(*event.Payload.CommandSummary, maxV5CommandSummaryBytes) || !validV5CommandCwd(*event.Payload.CommandCwd) {
				return ErrEventLimitExceeded
			}
		case "mcpToolCall":
			if event.Payload.Status != "in_progress" || event.Payload.ToolIdentity == nil || event.Payload.ArgumentsSummary == nil ||
				!payloadShape(event.Payload, []string{"item_type", "status", "identity", "arguments_summary"}, "item_type", "status", "identity", "arguments_summary") ||
				!validV5ToolIdentity(*event.Payload.ToolIdentity) || !validV5BoundedSummary(*event.Payload.ArgumentsSummary, maxV5ToolArgumentsBytes) {
				return ErrEventLimitExceeded
			}
		case "agentMessage":
			return validateV4Event(event)
		default:
			if !isV5GenericItemType(event.Payload.ItemType) ||
				!payloadShape(event.Payload, []string{"item_type"}, "item_type") {
				return ErrEventLimitExceeded
			}
		}
	case EventItemCompleted:
		if !validV4ItemEvent(event) || event.Terminal {
			return ErrEventLimitExceeded
		}
		switch event.Payload.ItemType {
		case "commandExecution":
			if !validV5CommandCompleted(event.Payload) {
				return ErrEventLimitExceeded
			}
		case "mcpToolCall":
			if !validV5ToolCompleted(event.Payload) {
				return ErrEventLimitExceeded
			}
		case "agentMessage":
			return validateV4Event(event)
		default:
			if !isV5GenericItemType(event.Payload.ItemType) ||
				!payloadShape(event.Payload, []string{"item_type"}, "item_type") {
				return ErrEventLimitExceeded
			}
		}
	case EventItemCommandOutputDelta:
		if !validV4ItemEvent(event) || event.Terminal || event.Payload.Delta == nil || *event.Payload.Delta == "" ||
			event.Payload.Truncated == nil || len(*event.Payload.Delta) > maxV5CommandDeltaBytes ||
			!payloadShape(event.Payload, []string{"delta", "truncated"}, "delta", "truncated", "truncation_reason") ||
			!validV5Truncation(*event.Payload.Truncated, event.Payload.TruncationReason) {
			return ErrEventLimitExceeded
		}
	case EventItemToolProgress:
		if !validV4ItemEvent(event) || event.Terminal || event.Payload.ItemType != "mcpToolCall" ||
			event.Payload.Status != "in_progress" || event.Payload.ToolIdentity == nil || event.Payload.ProgressIndex == nil ||
			event.Payload.Summary == nil || *event.Payload.ProgressIndex < 0 || *event.Payload.ProgressIndex >= maxV5ToolProgressEvents ||
			!payloadShape(event.Payload, []string{"item_type", "status", "identity", "progress_index", "summary"}, "item_type", "status", "identity", "progress_index", "summary") ||
			!validV5ToolIdentity(*event.Payload.ToolIdentity) || !validV5BoundedSummary(*event.Payload.Summary, maxV5ToolProgressBytes) {
			return ErrEventLimitExceeded
		}
	default:
		return validateV4Event(event)
	}
	return nil
}

func validV5CommandCompleted(payload EventPayload) bool {
	if payload.CommandSummary == nil || payload.CommandCwd == nil || payload.Output == nil ||
		!payloadShape(payload, []string{"item_type", "status", "command_summary", "cwd", "output"},
			"item_type", "status", "command_summary", "cwd", "duration_ms", "exit_code", "output", "error") ||
		!validV5BoundedSummary(*payload.CommandSummary, maxV5CommandSummaryBytes) || !validV5CommandCwd(*payload.CommandCwd) ||
		!validV5CommandOutput(*payload.Output) || !validV5DurationField(payload.DurationMS) {
		return false
	}
	switch payload.Status {
	case "completed":
		return payload.ItemError == nil
	case "failed":
		return payload.ItemError != nil && validV5ProjectionError(*payload.ItemError, false)
	case "declined":
		return payload.ItemError != nil && payload.ItemError.Code == "command_declined" && len(payload.ItemError.Summary) <= maxV5ErrorSummaryBytes
	default:
		return false
	}
}

func validV5ToolCompleted(payload EventPayload) bool {
	if payload.ToolIdentity == nil || payload.ArgumentsSummary == nil ||
		!payloadShape(payload, []string{"item_type", "status", "identity", "arguments_summary"},
			"item_type", "status", "identity", "arguments_summary", "duration_ms", "result_summary", "error") ||
		!validV5ToolIdentity(*payload.ToolIdentity) || !validV5BoundedSummary(*payload.ArgumentsSummary, maxV5ToolArgumentsBytes) ||
		!validV5DurationField(payload.DurationMS) {
		return false
	}
	if payload.ResultSummary != nil && !validV5BoundedSummary(*payload.ResultSummary, maxV5ToolResultBytes) {
		return false
	}
	switch payload.Status {
	case "completed":
		return payload.ResultSummary != nil && payload.ItemError == nil
	case "failed":
		return payload.ItemError != nil && validV5ToolError(*payload.ItemError)
	case "declined":
		return payload.ResultSummary == nil && payload.ItemError != nil && payload.ItemError.Code == "tool_declined" && len(payload.ItemError.Summary) <= maxV5ErrorSummaryBytes
	default:
		return false
	}
}

func validV5BoundedSummary(summary BoundedSummary, limit int) bool {
	return len(summary.Text) <= limit && validV5Truncation(summary.Truncated, summary.TruncationReason)
}

func validV5Truncation(truncated bool, reason string) bool {
	if !truncated {
		return reason == ""
	}
	return reason == v5TruncationReasonUTF8ByteLimit || reason == "upstream_truncated"
}

func validV5CommandCwd(cwd CommandCwd) bool {
	switch cwd.Kind {
	case "workspace_root", "redacted":
		return len(cwd.Segments) == 0
	case "workspace_relative":
		return validV5CwdSegments(cwd.Segments)
	default:
		return false
	}
}

func validV5CommandOutput(output CommandOutput) bool {
	switch output.Retention {
	case "complete":
		return output.Text != nil && output.Head == nil && output.Tail == nil && output.Reason == "" &&
			!output.Truncated && output.TruncationReason == "" && len(*output.Text) <= maxV5CommandSnapshotBytes
	case "head_tail":
		return output.Text == nil && output.Head != nil && output.Tail != nil && output.Reason == "" && output.Truncated &&
			validV5Truncation(true, output.TruncationReason) && len(*output.Head) <= maxV5CommandSnapshotPartBytes &&
			len(*output.Tail) <= maxV5CommandSnapshotPartBytes && len(*output.Head)+len(*output.Tail) <= maxV5CommandSnapshotBytes
	case "unavailable":
		return output.Text == nil && output.Head == nil && output.Tail == nil && output.Reason == "not_available" &&
			!output.Truncated && output.TruncationReason == ""
	default:
		return false
	}
}

func validV5ProjectionError(projectionError ProjectionError, commandDeclinedAllowed bool) bool {
	if len(projectionError.Summary) > maxV5ErrorSummaryBytes {
		return false
	}
	switch projectionError.Code {
	case "command_failed", "projection_limit_exceeded", "projection_redaction_failed", "protocol_error":
		return true
	case "command_declined":
		return commandDeclinedAllowed
	default:
		return false
	}
}

func validV5ToolError(projectionError ProjectionError) bool {
	if len(projectionError.Summary) > maxV5ErrorSummaryBytes {
		return false
	}
	switch projectionError.Code {
	case "tool_failed", "unknown_tool", "projection_limit_exceeded", "projection_redaction_failed", "protocol_error":
		return true
	default:
		return false
	}
}

func validV5ToolIdentity(identity ToolIdentity) bool {
	if identity.Resolution == "unknown" {
		return identity.ServerName == "unknown" && identity.ToolName == "unknown"
	}
	return identity.Resolution == "known" && identity.ServerName != "" && identity.ToolName != "" &&
		len(identity.ServerName) <= maxV5ToolIdentityBytes && len(identity.ToolName) <= maxV5ToolIdentityBytes
}

func validV5DurationField(value *int64) bool {
	return value == nil || (*value >= 0 && *value <= maxV5DurationMilliseconds)
}
