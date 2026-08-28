package session

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	maxV4IdentityRunes         = 256
	maxV4IdentityBytes         = 1024
	maxV4AgentTextRunes        = 1 << 20
	maxV4AgentTextBytes        = 1 << 20
	maxV4PlanSteps             = 128
	maxV4PlanTextRunes         = 16 << 10
	maxV4PlanTextBytes         = 64 << 10
	maxV4RememberedAgentPhases = 512
	maxV4ReasoningItemsPerTurn = 8
	maxV4WarningCodeRunes      = 256
	maxV4WarningMessageRunes   = 16 << 10
	maxV4WarningMessageBytes   = 64 << 10

	v4ProjectionLimitCode    = "limit_exceeded"
	v4ProjectionLimitMessage = "agent event exceeded local projection limit"
)

type v4TurnKey struct {
	sessionID string
	turnID    string
}

type v4TurnState struct {
	projectionFailed  bool
	terminalPublished bool
	agentPhases       map[string]*string
	agentPhaseOrder   []string
	reasoningItems    map[string]struct{}
	reasoningLimited  map[string]struct{}
}

func nullableString(value *string) **string {
	return &value
}

func copiedStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (s *Service) publishV4(record Record, event Event) error {
	if s.eventsV4 == nil {
		return nil
	}
	decorateEvent(record, &event)
	if event.Payload.V4Text != nil {
		event.Payload.Text = copiedStringPointer(event.Payload.V4Text)
		event.Payload.V4Text = nil
	}
	if event.Payload.V4Message != nil {
		event.Payload.Message = copiedStringPointer(event.Payload.V4Message)
		event.Payload.V4Message = nil
	}

	if event.TurnID != "" && s.v4TurnProjectionFailed(record.AgentSessionID, event.TurnID) {
		if event.Terminal {
			return s.publishV4SanitizedTerminal(record, event.TurnID)
		}
		return nil
	}
	if err := validateV4Event(event); err != nil {
		return s.rejectV4Event(record, event)
	}
	if event.EventType == EventItemAgentMessageDelta &&
		!s.hasV4AgentLifecycle(record.AgentSessionID, event.TurnID, event.ItemID) {
		return s.rejectV4Event(record, event)
	}
	if (event.EventType == EventItemReasoningTextDelta || event.EventType == EventItemReasoningFinalized) &&
		!s.admitV4ReasoningItem(record.AgentSessionID, event.TurnID, event.ItemID) {
		return s.publishV4ReasoningLimit(record, event.TurnID, event.ItemID)
	}
	if event.Payload.ItemType == "agentMessage" &&
		(event.EventType == EventItemStarted || event.EventType == EventItemCompleted) {
		s.rememberV4AgentPhase(record.AgentSessionID, event.TurnID, event.ItemID, *event.Payload.Phase)
	}
	if _, err := s.eventsV4.Publish(event); err != nil {
		if errors.Is(err, ErrEventLimitExceeded) {
			return s.rejectV4Event(record, event)
		}
		return err
	}
	if event.Terminal {
		s.clearV4Turn(record.AgentSessionID, event.TurnID)
	}
	return nil
}

func (s *Service) rejectV4Event(record Record, event Event) error {
	if event.TurnID == "" {
		if event.EventType != EventWarning {
			return ErrEventLimitExceeded
		}
		willRetry := false
		warning := Event{
			EventType: EventWarning,
			Payload: EventPayload{
				Code:      v4ProjectionLimitCode,
				Message:   stringPointer(v4ProjectionLimitMessage),
				WillRetry: &willRetry,
			},
		}
		decorateV4ProblemEvent(record, &warning)
		_, err := s.eventsV4.Publish(warning)
		return err
	}

	first := s.markV4TurnProjectionFailed(record.AgentSessionID, event.TurnID)
	if event.Terminal {
		return s.publishV4SanitizedTerminal(record, event.TurnID)
	}
	if !first {
		return nil
	}
	willRetry := false
	problem := Event{
		TurnID:    event.TurnID,
		EventType: EventError,
		Payload: EventPayload{
			Code:      v4ProjectionLimitCode,
			Message:   stringPointer(v4ProjectionLimitMessage),
			WillRetry: &willRetry,
		},
	}
	decorateV4ProblemEvent(record, &problem)
	_, err := s.eventsV4.Publish(problem)
	return err
}

func (s *Service) publishV4SanitizedTerminal(record Record, turnID string) error {
	if !s.beginV4SanitizedTerminal(record.AgentSessionID, turnID) {
		return nil
	}
	terminal := Event{
		TurnID:    turnID,
		EventType: EventTurnCompleted,
		Terminal:  true,
		Payload: EventPayload{
			Status:  "failed",
			Code:    v4ProjectionLimitCode,
			Message: stringPointer(v4ProjectionLimitMessage),
		},
	}
	decorateV4ProblemEvent(record, &terminal)
	_, err := s.eventsV4.Publish(terminal)
	if err != nil {
		s.rollbackV4SanitizedTerminal(record.AgentSessionID, turnID)
	}
	return err
}

func decorateV4ProblemEvent(record Record, event *Event) {
	decorateEvent(record, event)
	if utf8.RuneCountInString(event.TraceID) > maxV4IdentityRunes {
		event.TraceID = ""
	}
	if utf8.RuneCountInString(event.RequestID) > maxV4IdentityRunes {
		event.RequestID = ""
	}
	if utf8.RuneCountInString(event.TenantID) > maxV4IdentityRunes {
		event.TenantID = ""
	}
	if utf8.RuneCountInString(event.UserID) > maxV4IdentityRunes {
		event.UserID = ""
	}
}

func (s *Service) v4TurnProjectionFailed(sessionID, turnID string) bool {
	s.v4Mu.Lock()
	defer s.v4Mu.Unlock()
	state := s.v4Turns[v4TurnKey{sessionID: sessionID, turnID: turnID}]
	return state != nil && state.projectionFailed
}

func (s *Service) markV4TurnProjectionFailed(sessionID, turnID string) bool {
	s.v4Mu.Lock()
	defer s.v4Mu.Unlock()
	key := v4TurnKey{sessionID: sessionID, turnID: turnID}
	state := s.v4Turns[key]
	if state == nil {
		state = newV4TurnState()
		s.v4Turns[key] = state
	}
	first := !state.projectionFailed
	state.projectionFailed = true
	return first
}

func newV4TurnState() *v4TurnState {
	return &v4TurnState{
		agentPhases:      make(map[string]*string),
		reasoningItems:   make(map[string]struct{}),
		reasoningLimited: make(map[string]struct{}),
	}
}

func (s *Service) beginV4SanitizedTerminal(sessionID, turnID string) bool {
	s.v4Mu.Lock()
	defer s.v4Mu.Unlock()
	key := v4TurnKey{sessionID: sessionID, turnID: turnID}
	state := s.v4Turns[key]
	if state == nil {
		state = newV4TurnState()
		s.v4Turns[key] = state
	}
	state.projectionFailed = true
	if state.terminalPublished {
		return false
	}
	state.terminalPublished = true
	return true
}

func (s *Service) rollbackV4SanitizedTerminal(sessionID, turnID string) {
	s.v4Mu.Lock()
	if state := s.v4Turns[v4TurnKey{sessionID: sessionID, turnID: turnID}]; state != nil {
		state.terminalPublished = false
	}
	s.v4Mu.Unlock()
}

func (s *Service) v4SanitizedTerminalPublished(sessionID, turnID string) bool {
	s.v4Mu.Lock()
	defer s.v4Mu.Unlock()
	state := s.v4Turns[v4TurnKey{sessionID: sessionID, turnID: turnID}]
	return state != nil && state.terminalPublished
}

func (s *Service) rememberV4AgentPhase(sessionID, turnID, itemID string, phase *string) {
	s.v4Mu.Lock()
	defer s.v4Mu.Unlock()
	key := v4TurnKey{sessionID: sessionID, turnID: turnID}
	state := s.v4Turns[key]
	if state == nil {
		state = newV4TurnState()
		s.v4Turns[key] = state
	}
	if _, exists := state.agentPhases[itemID]; !exists {
		if len(state.agentPhases) >= maxV4RememberedAgentPhases {
			oldest := state.agentPhaseOrder[0]
			state.agentPhaseOrder = state.agentPhaseOrder[1:]
			delete(state.agentPhases, oldest)
		}
		state.agentPhaseOrder = append(state.agentPhaseOrder, itemID)
	}
	state.agentPhases[itemID] = copiedStringPointer(phase)
}

func (s *Service) hasV4AgentLifecycle(sessionID, turnID, itemID string) bool {
	s.v4Mu.Lock()
	defer s.v4Mu.Unlock()
	state := s.v4Turns[v4TurnKey{sessionID: sessionID, turnID: turnID}]
	if state == nil {
		return false
	}
	_, ok := state.agentPhases[itemID]
	return ok
}

func (s *Service) admitV4ReasoningItem(sessionID, turnID, itemID string) bool {
	s.v4Mu.Lock()
	defer s.v4Mu.Unlock()
	key := v4TurnKey{sessionID: sessionID, turnID: turnID}
	state := s.v4Turns[key]
	if state == nil {
		state = newV4TurnState()
		s.v4Turns[key] = state
	}
	if _, ok := state.reasoningItems[itemID]; ok {
		return true
	}
	if len(state.reasoningItems) >= maxV4ReasoningItemsPerTurn {
		return false
	}
	state.reasoningItems[itemID] = struct{}{}
	return true
}

func (s *Service) hasV4ReasoningItem(sessionID, turnID, itemID string) bool {
	s.v4Mu.Lock()
	defer s.v4Mu.Unlock()
	state := s.v4Turns[v4TurnKey{sessionID: sessionID, turnID: turnID}]
	if state == nil {
		return false
	}
	_, exists := state.reasoningItems[itemID]
	return exists
}

func (s *Service) publishV4ReasoningLimit(record Record, turnID, itemID string) error {
	if !s.beginV4ReasoningLimit(record.AgentSessionID, turnID, itemID) {
		return nil
	}
	contents := []ReasoningContent{}
	event := Event{
		TurnID:    turnID,
		ItemID:    itemID,
		EventType: EventItemReasoningFinalized,
		Payload: EventPayload{
			Status:     "unavailable",
			Contents:   &contents,
			ReasonCode: "limit_exceeded",
		},
	}
	decorateV4ProblemEvent(record, &event)
	_, err := s.eventsV4.Publish(event)
	if err != nil {
		s.rollbackV4ReasoningLimit(record.AgentSessionID, turnID, itemID)
	}
	return err
}

func (s *Service) beginV4ReasoningLimit(sessionID, turnID, itemID string) bool {
	s.v4Mu.Lock()
	defer s.v4Mu.Unlock()
	key := v4TurnKey{sessionID: sessionID, turnID: turnID}
	state := s.v4Turns[key]
	if state == nil {
		state = newV4TurnState()
		s.v4Turns[key] = state
	}
	if _, exists := state.reasoningLimited[itemID]; exists {
		return false
	}
	state.reasoningLimited[itemID] = struct{}{}
	return true
}

func (s *Service) rollbackV4ReasoningLimit(sessionID, turnID, itemID string) {
	s.v4Mu.Lock()
	if state := s.v4Turns[v4TurnKey{sessionID: sessionID, turnID: turnID}]; state != nil {
		delete(state.reasoningLimited, itemID)
	}
	s.v4Mu.Unlock()
}

func (s *Service) clearV4Turn(sessionID, turnID string) {
	s.v4Mu.Lock()
	delete(s.v4Turns, v4TurnKey{sessionID: sessionID, turnID: turnID})
	s.v4Mu.Unlock()
}

func (s *Service) clearV4Session(sessionID string) {
	s.v4Mu.Lock()
	for key := range s.v4Turns {
		if key.sessionID == sessionID {
			delete(s.v4Turns, key)
		}
	}
	s.v4Mu.Unlock()
}

func validateV4Event(event Event) error {
	for _, value := range []string{event.TraceID, event.RequestID, event.TenantID, event.UserID} {
		if value != "" && utf8.RuneCountInString(value) > maxV4IdentityRunes {
			return ErrEventLimitExceeded
		}
	}
	if event.ItemID != "" && (utf8.RuneCountInString(event.ItemID) > maxV4IdentityRunes || len(event.ItemID) > maxV4IdentityBytes) {
		return ErrEventLimitExceeded
	}
	switch event.EventType {
	case EventThreadStarted:
		if event.TurnID != "" || event.ItemID != "" || event.Terminal ||
			!payloadShape(event.Payload, []string{"model", "model_provider"}, "model", "model_provider") ||
			!validV4ManagedIdentity(event.Payload.Model) || !validV4ManagedIdentity(event.Payload.ModelProvider) {
			return ErrEventLimitExceeded
		}
	case EventTurnStarted:
		if !validV4TurnWithoutItem(event) || event.Terminal || event.Payload.Status != "in_progress" ||
			!payloadShape(event.Payload, []string{"status"}, "status") {
			return ErrEventLimitExceeded
		}
	case EventItemStarted, EventItemCompleted:
		if !validV4ItemEvent(event) || event.Terminal || event.Payload.ItemType == "" {
			return ErrEventLimitExceeded
		}
		if event.Payload.ItemType == "agentMessage" {
			if event.Payload.Text == nil || event.Payload.Phase == nil ||
				!payloadShape(event.Payload, []string{"item_type", "text", "phase"}, "item_type", "text", "phase") {
				return ErrEventLimitExceeded
			}
			if phase := *event.Payload.Phase; phase != nil && *phase != "commentary" && *phase != "final_answer" {
				return ErrEventLimitExceeded
			}
		} else {
			required := []string{"item_type"}
			if !payloadShape(event.Payload, required, "item_type", "text") || event.Payload.Phase != nil {
				return ErrEventLimitExceeded
			}
		}
		if event.Payload.Text != nil && !validV4AgentText(*event.Payload.Text) {
			return ErrEventLimitExceeded
		}
	case EventTurnPlanUpdated:
		if !validV4TurnWithoutItem(event) || event.Terminal || event.Payload.Plan == nil || *event.Payload.Plan == nil ||
			!payloadShape(event.Payload, []string{"plan"}, "explanation", "plan") || len(*event.Payload.Plan) > maxV4PlanSteps {
			return ErrEventLimitExceeded
		}
		if event.Payload.Explanation != nil {
			if explanation := *event.Payload.Explanation; explanation != nil && !validV4PlanText(*explanation) {
				return ErrEventLimitExceeded
			}
		}
		for _, step := range *event.Payload.Plan {
			if !validV4PlanText(step.Step) {
				return ErrEventLimitExceeded
			}
			switch step.Status {
			case "pending", "in_progress", "completed":
			default:
				return ErrEventLimitExceeded
			}
		}
	case EventItemAgentMessageDelta:
		if !validV4ItemEvent(event) || event.Terminal || event.Payload.Delta == nil ||
			!payloadShape(event.Payload, []string{"delta"}, "delta") {
			return ErrEventLimitExceeded
		}
	case EventItemReasoningTextDelta:
		if !validV4ItemEvent(event) || event.Terminal || event.Payload.ContentIndex == nil || event.Payload.Delta == nil ||
			!payloadShape(event.Payload, []string{"content_index", "delta"}, "content_index", "delta") ||
			*event.Payload.ContentIndex < 0 || *event.Payload.ContentIndex > 7 || *event.Payload.Delta == "" ||
			utf8.RuneCountInString(*event.Payload.Delta) > 16<<10 || len(*event.Payload.Delta) > 16<<10 {
			return ErrEventLimitExceeded
		}
	case EventItemReasoningFinalized:
		if !validV4ItemEvent(event) || event.Terminal || !validV4ReasoningFinalized(event.Payload) {
			return ErrEventLimitExceeded
		}
	case EventTurnCompleted:
		if !validV4TurnWithoutItem(event) || !event.Terminal ||
			!payloadShape(event.Payload, []string{"status"}, "status", "code", "message") {
			return ErrEventLimitExceeded
		}
		switch event.Payload.Status {
		case "completed", "interrupted", "failed":
		default:
			return ErrEventLimitExceeded
		}
	case EventError:
		if !validV4TurnWithoutItem(event) || event.Terminal || event.Payload.Message == nil || event.Payload.WillRetry == nil ||
			!payloadShape(event.Payload, []string{"message", "will_retry"}, "code", "message", "will_retry") {
			return ErrEventLimitExceeded
		}
	case EventWarning:
		if event.TurnID != "" || event.ItemID != "" || event.Terminal || event.Payload.Message == nil || event.Payload.WillRetry == nil || *event.Payload.WillRetry ||
			!payloadShape(event.Payload, []string{"message", "will_retry"}, "code", "message", "will_retry") ||
			utf8.RuneCountInString(event.Payload.Code) > maxV4WarningCodeRunes ||
			utf8.RuneCountInString(*event.Payload.Message) > maxV4WarningMessageRunes ||
			len(*event.Payload.Message) > maxV4WarningMessageBytes {
			return ErrEventLimitExceeded
		}
	case EventItemArtifactStarted:
		if !validV4ArtifactEnvelope(event) || !validV4ArtifactBase(event.Payload) || event.Payload.Status != "in_progress" ||
			event.Payload.Ordinal == nil || *event.Payload.Ordinal < 0 ||
			!payloadShape(event.Payload, []string{"artifact_id", "kind", "provenance", "status", "ordinal"},
				"artifact_id", "kind", "provenance", "status", "ordinal", "display_name") ||
			!validV4DisplayName(event.Payload.DisplayName, false) {
			return ErrEventLimitExceeded
		}
	case EventItemArtifactProgress:
		if !validV4ArtifactEnvelope(event) || !validV4ArtifactBase(event.Payload) || event.Payload.Status != "in_progress" ||
			event.Payload.Ordinal == nil || *event.Payload.Ordinal < 0 || (event.Payload.Stage == "" && event.Payload.Progress == nil) ||
			!payloadShape(event.Payload, []string{"artifact_id", "kind", "provenance", "status", "ordinal"},
				"artifact_id", "kind", "provenance", "status", "ordinal", "stage", "progress_percent") {
			return ErrEventLimitExceeded
		}
		if event.Payload.Stage != "" && event.Payload.Stage != "generating" && event.Payload.Stage != "processing" && event.Payload.Stage != "finalizing" {
			return ErrEventLimitExceeded
		}
		if event.Payload.Progress != nil && (*event.Payload.Progress < 0 || *event.Payload.Progress > 100) {
			return ErrEventLimitExceeded
		}
	case EventItemArtifactCompleted:
		if !validV4ArtifactEnvelope(event) || !validV4ArtifactCompleted(event) {
			return ErrEventLimitExceeded
		}
	case EventItemArtifactFailed:
		if !validV4ArtifactEnvelope(event) || !validV4ArtifactBase(event.Payload) || event.Payload.Status != "failed" ||
			event.Payload.Ordinal == nil || *event.Payload.Ordinal < 0 || event.Payload.Retryable == nil ||
			!payloadShape(event.Payload, []string{"artifact_id", "kind", "provenance", "status", "ordinal", "error_code", "retryable"},
				"artifact_id", "kind", "provenance", "status", "ordinal", "error_code", "retryable", "message") ||
			!validV4ArtifactErrorCode(event.Payload.ErrorCode) ||
			(event.Payload.Message != nil && (*event.Payload.Message == "" || utf8.RuneCountInString(*event.Payload.Message) > 512)) {
			return ErrEventLimitExceeded
		}
	default:
		return ErrEventLimitExceeded
	}
	return nil
}

func validateV4RetainedEvent(event Event) error {
	if event.SchemaVersion != EventSchemaVersionV4 || event.Sequence == 0 || event.OccurredAt.IsZero() ||
		!validUUID(event.EventID) || !validUUID(event.StreamID) || !validUUID(event.TaskID) ||
		!validUUID(event.AgentSessionID) || !validUUID(event.CodexThreadID) {
		return ErrEventLimitExceeded
	}
	return validateV4Event(event)
}

func payloadShape(payload EventPayload, required []string, allowed ...string) bool {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(encoded, &fields) != nil {
		return false
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key := range fields {
		if _, ok := allowedSet[key]; !ok {
			return false
		}
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return false
		}
	}
	return true
}

func validV4TurnWithoutItem(event Event) bool {
	return validUUID(event.TurnID) && event.ItemID == ""
}

func validV4ItemEvent(event Event) bool {
	return validUUID(event.TurnID) && event.ItemID != ""
}

func validV4ReasoningFinalized(payload EventPayload) bool {
	if payload.Contents == nil || *payload.Contents == nil ||
		!payloadShape(payload, []string{"status", "contents"}, "status", "contents", "reason_code") {
		return false
	}
	contents := *payload.Contents
	if len(contents) > 8 {
		return false
	}
	switch payload.Status {
	case "complete":
		if len(contents) == 0 || payload.ReasonCode != "" {
			return false
		}
	case "incomplete":
		if len(contents) == 0 || !validV4ReasoningReason(payload.ReasonCode) {
			return false
		}
	case "unavailable":
		return len(contents) == 0 && validV4ReasoningReason(payload.ReasonCode)
	default:
		return false
	}
	total := 0
	for index, part := range contents {
		if part.ContentIndex != index || part.Text == "" || utf8.RuneCountInString(part.Text) > 64<<10 || len(part.Text) > 64<<10 {
			return false
		}
		total += len(part.Text)
	}
	return total <= 128<<10
}

func validV4ReasoningReason(value string) bool {
	switch value {
	case "reasoning_not_emitted", "turn_interrupted", "stream_gap", "runtime_error", "limit_exceeded", "protocol_error", "host_shutdown":
		return true
	default:
		return false
	}
}

func validV4ArtifactEnvelope(event Event) bool {
	return validUUID(event.TurnID) && !event.Terminal
}

func validV4ArtifactBase(payload EventPayload) bool {
	if !validUUID(payload.ArtifactID) {
		return false
	}
	switch payload.Kind {
	case "image", "video", "file", "report":
	default:
		return false
	}
	switch payload.Provenance {
	case "synthetic", "provider", "tool":
		return true
	default:
		return false
	}
}

func validV4DisplayName(value string, required bool) bool {
	if value == "" {
		return !required
	}
	return utf8.RuneCountInString(value) <= 255 && !strings.ContainsAny(value, "/\\")
}

func validV4ArtifactCompleted(event Event) bool {
	payload := event.Payload
	if !validV4ArtifactBase(payload) || payload.Status != "ready" || payload.Ordinal == nil || *payload.Ordinal < 0 ||
		payload.SizeBytes == nil || *payload.SizeBytes < 1 || *payload.SizeBytes > 64<<20 || !validV4SHA256(payload.SHA256) ||
		!payloadShape(payload,
			[]string{"artifact_id", "kind", "provenance", "status", "ordinal", "media_type", "size_bytes", "sha256", "content_href"},
			"artifact_id", "kind", "provenance", "status", "ordinal", "display_name", "media_type", "size_bytes", "sha256", "content_href", "poster_href") ||
		!validV4DisplayName(payload.DisplayName, false) {
		return false
	}
	base := "/v3/agent-sessions/" + event.AgentSessionID + "/artifacts/" + payload.ArtifactID
	if payload.ContentHref != base+"/content" || (payload.PosterHref != "" && payload.PosterHref != base+"/poster") {
		return false
	}
	switch payload.Kind {
	case "image":
		return *payload.SizeBytes <= 20<<20 && payload.PosterHref == "" &&
			(payload.MediaType == "image/png" || payload.MediaType == "image/jpeg" || payload.MediaType == "image/webp")
	case "video":
		return payload.MediaType == "video/mp4"
	case "file":
		return payload.PosterHref == "" && validV4FileMediaType(payload.MediaType)
	case "report":
		return payload.PosterHref == "" && payload.MediaType == "application/vnd.yijie.report+json;version=1"
	default:
		return false
	}
}

func validV4SHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, value := range []byte(value) {
		if (value < '0' || value > '9') && (value < 'a' || value > 'f') {
			return false
		}
	}
	return true
}

func validV4FileMediaType(value string) bool {
	switch value {
	case "text/plain", "text/csv", "application/json", "application/pdf", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return true
	default:
		return false
	}
}

func validV4ArtifactErrorCode(value string) bool {
	switch value {
	case "generation_failed", "unsupported_provider", "resource_unavailable", "limit_exceeded", "integrity_failed", "protocol_error", "turn_interrupted", "host_shutdown":
		return true
	default:
		return false
	}
}

func validV4ManagedIdentity(value string) bool {
	return value != "" && utf8.RuneCountInString(value) <= maxV4IdentityRunes && len(value) <= maxV4IdentityBytes
}

func validV4AgentText(value string) bool {
	return utf8.RuneCountInString(value) <= maxV4AgentTextRunes && len(value) <= maxV4AgentTextBytes
}

func validV4PlanText(value string) bool {
	return utf8.RuneCountInString(value) <= maxV4PlanTextRunes && len(value) <= maxV4PlanTextBytes
}

func (s *Service) rejectMalformedV4Notification(method string, params json.RawMessage) {
	if s.eventsV4 == nil {
		return
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(params, &envelope) != nil {
		return
	}
	var threadID string
	if encoded := envelope["threadId"]; len(encoded) == 0 || json.Unmarshal(encoded, &threadID) != nil || !validUUID(threadID) {
		return
	}
	record, err := s.store.GetByThread(threadID)
	if err != nil {
		return
	}
	if method == RuntimeNotificationWarning {
		_ = s.rejectV4Event(record, Event{EventType: EventWarning})
		return
	}

	var turnID string
	if encoded := envelope["turnId"]; len(encoded) != 0 {
		_ = json.Unmarshal(encoded, &turnID)
	}
	if turnID == "" {
		var turn struct {
			ID string `json:"id"`
		}
		if encoded := envelope["turn"]; len(encoded) != 0 && json.Unmarshal(encoded, &turn) == nil {
			turnID = turn.ID
		}
	}
	if !validUUID(turnID) {
		return
	}
	event := Event{TurnID: turnID, EventType: EventError}
	if method == RuntimeNotificationTurnCompleted {
		_ = s.rejectMalformedV4Terminal(record, turnID)
		return
	}
	_ = s.rejectV4Event(record, event)
}

func (s *Service) rejectMalformedV4Terminal(record Record, turnID string) error {
	if s.v4SanitizedTerminalPublished(record.AgentSessionID, turnID) {
		return nil
	}
	completed, err := s.store.CompleteTurn(record.AgentSessionID, turnID, "failed")
	if err != nil {
		return err
	}
	s.clearImageTurn(record.CodexThreadID)
	s.abortSyntheticTerminalBarrier(record.AgentSessionID)
	if err := s.publishMalformedV4ReasoningFinalized(completed, turnID); err != nil {
		return err
	}
	s.markV4TurnProjectionFailed(record.AgentSessionID, turnID)
	return s.publishV4SanitizedTerminal(completed, turnID)
}

func (s *Service) publishMalformedV4ReasoningFinalized(record Record, turnID string) error {
	itemIDs := s.takeUnfinishedReasoningItems(record.AgentSessionID, turnID)
	for _, itemID := range itemIDs {
		if !s.hasV4ReasoningItem(record.AgentSessionID, turnID, itemID) {
			continue
		}
		contents := []ReasoningContent{}
		event := Event{
			TurnID:    turnID,
			ItemID:    itemID,
			EventType: EventItemReasoningFinalized,
			Payload: EventPayload{
				Status:     "unavailable",
				Contents:   &contents,
				ReasonCode: "protocol_error",
			},
		}
		decorateV4ProblemEvent(record, &event)
		if _, err := s.eventsV4.Publish(event); err != nil {
			return err
		}
	}
	return nil
}
