package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv2"
)

const nativeTextLimit = 256 << 10

// These are transient decoders for the pinned native protocol, not a second
// conversation model. Public values below use generated contract types.
type nativeWireItem struct {
	ID               string          `json:"id"`
	Type             string          `json:"type"`
	ClientID         *string         `json:"clientId"`
	Text             *string         `json:"text"`
	Phase            *string         `json:"phase"`
	Summary          []string        `json:"summary"`
	Content          json.RawMessage `json:"content"`
	Status           *string         `json:"status"`
	Command          string          `json:"command"`
	Cwd              string          `json:"cwd"`
	AggregatedOutput *string         `json:"aggregatedOutput"`
	ExitCode         *int32          `json:"exitCode"`
	DurationMS       *int64          `json:"durationMs"`
	Server           string          `json:"server"`
	Tool             string          `json:"tool"`
	Arguments        json.RawMessage `json:"arguments"`
	Result           json.RawMessage `json:"result"`
}

type nativeWireTurn struct {
	ID     string            `json:"id"`
	Status string            `json:"status"`
	Items  []json.RawMessage `json:"items"`
	Error  json.RawMessage   `json:"error"`
}

func nativeString[T ~string](value T) *T { return &value }

func safeNativeText(value string, limit int) (string, bool) {
	if strings.ContainsRune(value, 0) {
		return "", false
	}
	text, truncated := truncateV5UTF8(value, limit)
	return text, !truncated
}

func projectNativeItem(raw json.RawMessage, cwd string, allowRawReasoning bool) (native.NativeItem, error) {
	var wire nativeWireItem
	if json.Unmarshal(raw, &wire) != nil || wire.ID == "" || len(wire.ID) > 512 {
		return native.NativeItem{}, errors.New("invalid native item")
	}
	out := native.NativeItem{Id: wire.ID, Type: native.NativeItemType(wire.Type), Availability: native.NativeItemAvailabilityAvailable}
	if !out.Type.Valid() {
		out.Type = native.Unknown
		out.Availability = native.NativeItemAvailabilityUnavailable
		return out, nil
	}
	text := func(value string, limit int) *string {
		v, ok := safeNativeText(value, limit)
		if !ok {
			out.Availability = native.NativeItemAvailabilityPartial
		}
		return &v
	}
	if wire.Status != nil {
		status := native.NativeItemStatus(*wire.Status)
		if status.Valid() {
			out.Status = &status
		} else {
			out.Availability = native.NativeItemAvailabilityPartial
		}
	}
	switch out.Type {
	case native.AgentMessage:
		if wire.Text != nil {
			out.Text = text(*wire.Text, nativeTextLimit)
		} else {
			out.Availability = native.NativeItemAvailabilityPartial
		}
		if wire.Phase != nil {
			phase := native.NativeItemPhase(*wire.Phase)
			if phase.Valid() {
				out.Phase = &phase
			} else {
				out.Availability = native.NativeItemAvailabilityPartial
			}
		}
	case native.UserMessage:
		if wire.ClientID != nil && len(*wire.ClientID) <= 512 {
			out.ClientId = wire.ClientID
		}
		var inputs []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(wire.Content, &inputs) != nil {
			out.Availability = native.NativeItemAvailabilityPartial
			break
		}
		var texts []string
		for _, input := range inputs {
			if input.Type == "text" {
				texts = append(texts, input.Text)
			} else {
				out.Availability = native.NativeItemAvailabilityPartial
			}
		}
		out.Text = text(strings.Join(texts, "\n"), nativeTextLimit)
	case native.Reasoning:
		var content []string
		if json.Unmarshal(wire.Content, &content) != nil {
			out.Availability = native.NativeItemAvailabilityPartial
		}
		bounded := func(values []string) []string {
			result := []string{}
			remaining := nativeTextLimit
			for i, v := range values {
				if i >= 128 || remaining <= 0 {
					out.Availability = native.NativeItemAvailabilityPartial
					break
				}
				safe := text(v, remaining)
				remaining -= len(*safe)
				result = append(result, *safe)
			}
			return result
		}
		if !allowRawReasoning && len(content) > 0 {
			content = nil
			out.Availability = native.NativeItemAvailabilityPartial
		}
		content = bounded(content)
		summary := bounded(wire.Summary)
		out.Content = &content
		out.Summary = &summary
	case native.CommandExecution:
		// Reuse the existing security projection only; do not reuse lifecycle state.
		var life v5LifecycleNotification
		if json.Unmarshal(raw, &life.Item) != nil {
			return native.NativeItem{}, errors.New("invalid command item")
		}
		command := v5CommandSummary(life)
		out.CommandLabel = &command.Text
		location := v5CommandCwd(cwd, &wire.Cwd)
		label := "[路径已隐藏]"
		if location.Kind == "workspace_root" {
			label = "当前项目"
		} else if len(location.Segments) > 0 {
			label = strings.Join(location.Segments, "/")
		}
		out.CwdLabel = &label
		if wire.AggregatedOutput != nil {
			out.OutputText = text(redactV5CommandText(*wire.AggregatedOutput), nativeTextLimit)
		}
		out.ExitCode = wire.ExitCode
		if wire.DurationMS != nil {
			if *wire.DurationMS >= 0 {
				out.DurationMs = wire.DurationMS
			} else {
				out.Availability = native.NativeItemAvailabilityPartial
			}
		}
	case native.McpToolCall:
		projectNativeMcp(wire, &out)
	}
	return out, nil
}

func projectNativeError(raw json.RawMessage) *native.NativeError {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	out := &native.NativeError{Availability: native.NativeErrorAvailabilityPartial}
	var wire struct {
		Message string          `json:"message"`
		Info    json.RawMessage `json:"codexErrorInfo"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		out.Availability = native.NativeErrorAvailabilityUnavailable
		return out
	}
	if wire.Message != "" {
		message, _ := safeNativeText(redactV5CommandText(wire.Message), 4096)
		out.MessageText = &message
	}
	code := native.NativeErrorCodexErrorInfo(normalizeCodexErrorCode(wire.Info))
	if code.Valid() {
		out.CodexErrorInfo = &code
	}
	return out
}

func projectNativeTurn(wire nativeWireTurn, cwd string, allowRawReasoning bool) native.NativeTurn {
	out := native.NativeTurn{Id: wire.ID, Items: []native.NativeItem{}, ItemsComplete: false, Error: projectNativeError(wire.Error)}
	status := native.NativeTurnStatus(wire.Status)
	if status.Valid() {
		out.Status = &status
	}
	if len(wire.Error) > 0 && string(wire.Error) != "null" {
		out.ErrorCode = nativeString("runtime_error")
		if out.Error != nil && out.Error.CodexErrorInfo != nil {
			value := string(*out.Error.CodexErrorInfo)
			out.ErrorCode = &value
		}
	}
	for i, raw := range wire.Items {
		if i >= 512 {
			break
		}
		item, err := projectNativeItem(raw, cwd, allowRawReasoning)
		if err == nil {
			out.Items = append(out.Items, item)
		}
	}
	return out
}

func (s *Service) ReadNativeThreadV2(ctx context.Context, sessionID string) (native.NativeThreadSnapshot, error) {
	if err := requireUUID("agent_session_id", sessionID); err != nil {
		return native.NativeThreadSnapshot{}, err
	}
	record, err := s.store.Get(sessionID)
	if err != nil {
		return native.NativeThreadSnapshot{}, err
	}
	runtime, ok := s.runtime.(interface {
		ReadThread(context.Context, string) (json.RawMessage, error)
	})
	if !ok {
		return native.NativeThreadSnapshot{}, ErrSessionNotUsable
	}
	raw, err := runtime.ReadThread(ctx, record.CodexThreadID)
	if err != nil {
		return native.NativeThreadSnapshot{}, ErrRuntimeRequest
	}
	var wire struct {
		ID    string           `json:"id"`
		Turns []nativeWireTurn `json:"turns"`
	}
	if json.Unmarshal(raw, &wire) != nil || wire.ID != record.CodexThreadID {
		return native.NativeThreadSnapshot{}, ErrRuntimeRequest
	}
	out := native.NativeThreadSnapshot{SchemaVersion: 2, Source: native.RuntimeRead, ThreadId: wire.ID, Turns: []native.NativeTurn{}, Availability: native.NativeThreadSnapshotAvailabilityPartial}
	for i, turn := range wire.Turns {
		if i >= 1024 {
			break
		}
		out.Turns = append(out.Turns, projectNativeTurn(turn, record.Cwd, s.rawReasoning))
	}
	encoded, err := json.Marshal(out)
	if err != nil || len(encoded) > 8<<20 {
		return native.NativeThreadSnapshot{}, ErrEventLimitExceeded
	}
	return out, nil
}

func (s *Service) SubscribeNativeEvents(sessionID, streamID string, after uint64) (string, []Event, <-chan Event, func(), error) {
	if err := requireUUID("agent_session_id", sessionID); err != nil {
		return "", nil, nil, nil, err
	}
	if _, err := s.store.Get(sessionID); err != nil {
		return "", nil, nil, nil, err
	}
	return s.nativeEvents.Subscribe(sessionID, streamID, after)
}

func (s *Service) publishNativeNotification(method string, raw json.RawMessage) {
	var p struct {
		ThreadID     string                  `json:"threadId"`
		TurnID       string                  `json:"turnId"`
		ItemID       string                  `json:"itemId"`
		Item         json.RawMessage         `json:"item"`
		Turn         nativeWireTurn          `json:"turn"`
		Delta        string                  `json:"delta"`
		ContentIndex *int                    `json:"contentIndex"`
		SummaryIndex *int                    `json:"summaryIndex"`
		Plan         []native.NativePlanStep `json:"plan"`
		Explanation  *string                 `json:"explanation"`
	}
	if json.Unmarshal(raw, &p) != nil || p.ThreadID == "" || len(p.TurnID) > 512 || len(p.ItemID) > 512 {
		return
	}
	record, err := s.store.GetByThread(p.ThreadID)
	if err != nil {
		return
	}
	n := native.NativeNotification{Source: native.RuntimeNotification, Method: method, ThreadId: p.ThreadID, Availability: native.NativeNotificationAvailabilityAvailable}
	if p.TurnID != "" {
		n.TurnId = &p.TurnID
	}
	if p.ItemID != "" {
		n.ItemId = &p.ItemID
	}
	issue := func() {
		n.Source = native.ProjectionNotice
		n.Availability = native.NativeNotificationAvailabilityPartial
		n.Method = "projection/error"
		n.Code = nativeString("projection_unavailable")
		n.Item = nil
		n.Turn = nil
		n.Delta = nil
		n.Plan = nil
		n.Explanation = nil
	}
	switch method {
	case RuntimeNotificationItemStarted, RuntimeNotificationItemCompleted:
		item, e := projectNativeItem(p.Item, record.Cwd, s.rawReasoning)
		if e != nil {
			issue()
		} else {
			n.Item = &item
			n.ItemId = &item.Id
		}
	case RuntimeNotificationTurnStarted, RuntimeNotificationTurnCompleted:
		if p.Turn.ID == "" || len(p.Turn.ID) > 512 {
			issue()
		} else {
			n.TurnId = &p.Turn.ID
			turn := projectNativeTurn(p.Turn, record.Cwd, s.rawReasoning)
			n.Turn = &turn
		}
	case RuntimeNotificationItemAgentMessageDelta, RuntimeNotificationReasoningTextDelta, "item/reasoning/summaryTextDelta", "item/reasoning/summaryPartAdded":
		index := p.ContentIndex
		if method == "item/reasoning/summaryTextDelta" || method == "item/reasoning/summaryPartAdded" {
			index = p.SummaryIndex
		}
		if p.ItemID == "" || p.TurnID == "" || len(p.Delta) > nativeTextLimit || strings.ContainsRune(p.Delta, 0) || (method != RuntimeNotificationItemAgentMessageDelta && (index == nil || *index < 0 || *index > 127)) {
			issue()
		} else {
			n.Delta = &p.Delta
			n.Index = index
		}
	case RuntimeNotificationCommandOutputDelta:
		// Arbitrarily split command chunks cannot be redacted safely without an
		// additional Host content accumulator. Keep the native final output instead.
		issue()
		n.Code = nativeString("command_output_pending_final")
	case RuntimeNotificationTurnPlanUpdated:
		if p.Plan == nil || len(p.Plan) > 128 {
			issue()
		} else {
			n.Plan = &p.Plan
			n.Explanation = p.Explanation
			if p.Explanation != nil && (len(*p.Explanation) > 65536 || strings.ContainsRune(*p.Explanation, 0)) {
				n.Explanation = nil
				n.Availability = native.NativeNotificationAvailabilityPartial
			}
			for _, step := range p.Plan {
				if !step.Status.Valid() || len(step.Step) > 65536 {
					issue()
					break
				}
			}
		}
	case RuntimeNotificationError, RuntimeNotificationWarning:
		n.Code = nativeString("runtime_notice")
	default:
		return
	}
	event := Event{AgentSessionID: record.AgentSessionID, TaskID: record.TaskID, CodexThreadID: record.CodexThreadID, TurnID: p.TurnID, ItemID: p.ItemID, EventType: "native.notification", Payload: EventPayload{Native: &n}}
	if n.TurnId != nil {
		event.TurnID = *n.TurnId
	}
	if n.ItemId != nil {
		event.ItemID = *n.ItemId
	}
	event.Terminal = n.Source == native.RuntimeNotification && method == RuntimeNotificationTurnCompleted && n.Turn != nil && n.Turn.Status != nil
	if _, e := s.nativeEvents.Publish(event); e != nil {
		issue()
		event.Terminal = false
		event.Payload.Native = &n
		_, _ = s.nativeEvents.Publish(event)
	}
}

func (s *Service) publishNativeProjectionNotice(raw json.RawMessage) {
	var ids struct {
		ThreadID string `json:"threadId"`
		Turn     struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(raw, &ids) != nil {
		return
	}
	record, err := s.store.GetByThread(ids.ThreadID)
	if err != nil {
		return
	}
	n := native.NativeNotification{Source: native.ProjectionNotice, Method: "projection/error", ThreadId: ids.ThreadID, Availability: native.NativeNotificationAvailabilityPartial, Code: nativeString("projection_unavailable")}
	if validUUID(ids.Turn.ID) {
		n.TurnId = &ids.Turn.ID
	}
	_, _ = s.nativeEvents.Publish(Event{AgentSessionID: record.AgentSessionID, TaskID: record.TaskID, CodexThreadID: record.CodexThreadID, EventType: "native.notification", Payload: EventPayload{Native: &n}})
}
