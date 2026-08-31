package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxTurnInputBytes      = 1 << 20
	maxTurnV2InputCount    = 16
	maxImageDataURLBytes   = 13981039
	maxApprovalReasonBytes = 512

	RuntimeMethodThreadResume                = "thread/resume"
	RuntimeMethodThreadStart                 = "thread/start"
	RuntimeMethodThreadDelete                = "thread/delete"
	RuntimeNotificationThreadDeleted         = "thread/deleted"
	RuntimeMethodTurnInterrupt               = "turn/interrupt"
	RuntimeMethodTurnStart                   = "turn/start"
	RuntimeMethodDynamicToolCall             = "item/tool/call"
	RuntimeMethodCommandApproval             = "item/commandExecution/requestApproval"
	RuntimeNotificationServerRequestResolved = "serverRequest/resolved"
	DynamicToolGenerateImage                 = "generate_image"
	SessionApprovalPolicy                    = "never"
	SessionApprovalPolicyOnRequest           = "on-request"
	SessionSandbox                           = "read-only"
)

const feat137ManagedInstructions = "Runtime Baseline 2 is read-only for workspace and operating-system actions. Run exactly one command through exec_command with cmd set exactly to git rev-parse --is-inside-work-tree, the default login-shell setting, and sandbox_permissions=use_default. The Host-managed exec policy will request approval without elevating permissions; never use require_escalated or add a justification. Do not run or request any other command, network access, file change, additional permission, policy amendment, explicit shell wrapper, pipe, redirect, environment assignment, or unsandboxed execution."

var sessionRuntimeMethods = []string{
	RuntimeMethodThreadResume,
	RuntimeMethodThreadStart,
	RuntimeMethodTurnInterrupt,
	RuntimeMethodTurnStart,
}

func SupportedV2RuntimeMethods() []string {
	return []string{RuntimeMethodThreadDelete}
}

func SupportedSessionRuntimeMethods() []string {
	return append([]string(nil), sessionRuntimeMethods...)
}

type NotificationHandler func(method string, params json.RawMessage)

type DynamicToolCall struct {
	ThreadID  string
	TurnID    string
	CallID    string
	Tool      string
	Arguments json.RawMessage
}

type DynamicToolResult struct {
	Success bool
	Text    string
}

type DynamicToolHandler func(context.Context, DynamicToolCall) DynamicToolResult

type CommandApprovalRequest struct {
	RuntimeGeneration string
	RequestIDKey      string
	ThreadID          string
	TurnID            string
	ItemID            string
	StartedAtMS       int64
	Command           string
	CommandActions    []CommandApprovalAction
	Cwd               string
	EnvironmentID     *string
}

type CommandApprovalAction struct {
	Type    string
	Command string
}

type CommandApprovalResult struct {
	Respond  bool
	Decision string
}

type CommandApprovalResolved struct {
	RuntimeGeneration string
	RequestIDKey      string
	ThreadID          string
}

type CommandApprovalResponseWriteResult struct {
	RuntimeGeneration string
	RequestIDKey      string
	ThreadID          string
	Succeeded         bool
}

type CommandApprovalHandler interface {
	HandleCommandApproval(context.Context, CommandApprovalRequest) CommandApprovalResult
	HandleCommandApprovalResponseWritten(CommandApprovalResponseWriteResult)
	HandleCommandApprovalResolved(CommandApprovalResolved)
	HandleCommandApprovalGenerationClosed(string)
}

type dynamicToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func imageGenerationToolSpec() dynamicToolSpec {
	return dynamicToolSpec{
		Name:        DynamicToolGenerateImage,
		Description: "Generate one new real image from text, or generate one new image based on the current turn's single character reference. This is not a general image-editing or image-analysis tool.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt":       map[string]any{"type": "string", "minLength": 1, "maxLength": 1500},
				"mode":         map[string]any{"type": "string", "enum": []string{"text_to_image", "subject_reference"}},
				"aspect_ratio": map[string]any{"type": "string", "enum": []string{"1:1", "16:9", "4:3", "3:2", "2:3", "3:4", "9:16", "21:9"}},
			},
			"required":             []string{"prompt", "mode"},
			"additionalProperties": false,
		},
	}
}

type ThreadInfo struct {
	ID             string
	RuntimeSession string
	Model          string
	ModelProvider  string
	Turns          []TurnInfo
}

type TurnInfo struct {
	ID     string
	Status string
}

type UserInput struct {
	kind     string
	text     string
	imageURL string
}

func TextUserInput(text string) (UserInput, error) {
	if strings.TrimSpace(text) == "" {
		return UserInput{}, errors.New("Runtime text input is required")
	}
	if len(text) > maxTurnInputBytes {
		return UserInput{}, fmt.Errorf("Runtime text input exceeds %d bytes", maxTurnInputBytes)
	}
	return UserInput{kind: "text", text: text}, nil
}

func ImageUserInput(dataURL string) (UserInput, error) {
	if len(dataURL) == 0 || len(dataURL) > maxImageDataURLBytes ||
		!strings.HasPrefix(dataURL, "data:image/") {
		return UserInput{}, errors.New("Runtime image input is invalid")
	}
	return UserInput{kind: "image", imageURL: dataURL}, nil
}

func (input UserInput) wireValue() (map[string]any, error) {
	switch input.kind {
	case "text":
		if _, err := TextUserInput(input.text); err != nil {
			return nil, err
		}
		return map[string]any{"type": "text", "text": input.text}, nil
	case "image":
		if _, err := ImageUserInput(input.imageURL); err != nil {
			return nil, err
		}
		return map[string]any{"type": "image", "url": input.imageURL}, nil
	default:
		return nil, errors.New("Runtime user input kind is unsupported")
	}
}

type threadWire struct {
	ID        string     `json:"id"`
	SessionID string     `json:"sessionId"`
	Turns     []turnWire `json:"turns"`
}

type turnWire struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type threadResponse struct {
	Thread        threadWire `json:"thread"`
	Model         string     `json:"model"`
	ModelProvider string     `json:"modelProvider"`
}

type turnStartResponse struct {
	Turn turnWire `json:"turn"`
}

type readOnlySandboxPolicy struct {
	Type          string `json:"type"`
	NetworkAccess bool   `json:"networkAccess"`
}

func (m *Manager) sessionApprovalPolicy() string {
	if m.config.CommandApprovalEnabled {
		return SessionApprovalPolicyOnRequest
	}
	return SessionApprovalPolicy
}

func (m *Manager) sessionDeveloperInstructions() string {
	if m.config.CommandApprovalEnabled {
		return feat137ManagedInstructions
	}
	return "Runtime Baseline 2 is read-only for workspace and operating-system actions. Do not modify files or request elevated permissions."
}

func sessionTurnSandboxPolicy() readOnlySandboxPolicy {
	return readOnlySandboxPolicy{Type: "readOnly", NetworkAccess: false}
}

func (m *Manager) SetNotificationHandler(handler NotificationHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return errors.New("notification handler must be set before Runtime startup")
	}
	m.notificationHandler = handler
	return nil
}

func (m *Manager) SetDynamicToolHandler(handler DynamicToolHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return errors.New("dynamic tool handler must be set before Runtime startup")
	}
	if m.config.DynamicToolsEnabled && handler == nil {
		return errors.New("dynamic tool handler is required when Runtime dynamic tools are enabled")
	}
	m.dynamicToolHandler = handler
	return nil
}

func (m *Manager) SetCommandApprovalHandler(handler CommandApprovalHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return errors.New("command approval handler must be set before Runtime startup")
	}
	if m.config.CommandApprovalEnabled && handler == nil {
		return errors.New("command approval handler is required when Runtime command approvals are enabled")
	}
	m.commandApprovalHandler = handler
	return nil
}

func (m *Manager) handleServerRequest(ctx context.Context, request serverRequest) serverRequestResult {
	if request.method == RuntimeMethodCommandApproval && m.config.CommandApprovalEnabled {
		return m.handleCommandApprovalRequest(ctx, request)
	}
	if request.method != RuntimeMethodDynamicToolCall || !m.config.DynamicToolsEnabled {
		return serverRequestResult{respond: true, rpcError: &RPCError{
			Code: methodNotFoundCode, Message: "Method not supported by Yijie Agent Host Runtime Baseline 2",
		}}
	}
	var params struct {
		ThreadID  string          `json:"threadId"`
		TurnID    string          `json:"turnId"`
		CallID    string          `json:"callId"`
		Namespace *string         `json:"namespace"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(request.params)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&params); err != nil || params.ThreadID == "" || params.TurnID == "" ||
		params.CallID == "" || params.Tool != DynamicToolGenerateImage || params.Namespace != nil || len(params.Arguments) == 0 {
		return serverRequestResult{respond: true, rpcError: &RPCError{Code: -32602, Message: "Invalid dynamic tool call"}}
	}
	m.mu.Lock()
	handler := m.dynamicToolHandler
	m.mu.Unlock()
	if handler == nil {
		return serverRequestResult{respond: true, rpcError: &RPCError{Code: -32603, Message: "Dynamic tool unavailable"}}
	}
	result := handler(ctx, DynamicToolCall{
		ThreadID: params.ThreadID, TurnID: params.TurnID, CallID: params.CallID,
		Tool: params.Tool, Arguments: append(json.RawMessage(nil), params.Arguments...),
	})
	return serverRequestResult{respond: true, value: struct {
		ContentItems []map[string]string `json:"contentItems"`
		Success      bool                `json:"success"`
	}{ContentItems: []map[string]string{{"type": "inputText", "text": result.Text}}, Success: result.Success}}
}

func (m *Manager) handleCommandApprovalRequest(ctx context.Context, request serverRequest) serverRequestResult {
	cancel := serverRequestResult{respond: true, value: struct {
		Decision string `json:"decision"`
	}{Decision: "cancel"}}
	if !request.exactEnvelope {
		return cancel
	}
	m.mu.Lock()
	handler := m.commandApprovalHandler
	generation := m.runtimeGeneration
	m.mu.Unlock()
	if handler == nil || generation == "" {
		return cancel
	}
	params, err := decodeCommandApprovalParams(request.params)
	if err != nil {
		// The typed Runtime request identity becomes authoritative after the
		// exact outer envelope is accepted, before params eligibility. Notify the
		// Host authority so a malformed replay cannot receive a second response
		// or leave an existing same-key pending request actionable. The mapper
		// still forces Cancel for a newly rejected request and never trusts an
		// invalid payload to select a wider response.
		result := handler.HandleCommandApproval(ctx, CommandApprovalRequest{
			RuntimeGeneration: generation,
			RequestIDKey:      request.idKey,
		})
		if !result.Respond {
			return serverRequestResult{}
		}
		cancel.onResponseWritten = commandApprovalResponseWriteCallback(
			handler, generation, request.idKey, "",
		)
		return cancel
	}
	result := handler.HandleCommandApproval(ctx, CommandApprovalRequest{
		RuntimeGeneration: generation,
		RequestIDKey:      request.idKey,
		ThreadID:          params.ThreadID,
		TurnID:            params.TurnID,
		ItemID:            params.ItemID,
		StartedAtMS:       params.StartedAtMS,
		Command:           params.Command,
		CommandActions:    params.CommandActions,
		Cwd:               params.Cwd,
		EnvironmentID:     params.EnvironmentID,
	})
	if !result.Respond {
		return serverRequestResult{}
	}
	if result.Decision != "accept" && result.Decision != "cancel" {
		return serverRequestResult{respond: true, rpcError: &RPCError{Code: -32603, Message: "Command approval unavailable"}}
	}
	return serverRequestResult{respond: true, value: struct {
		Decision string `json:"decision"`
	}{Decision: result.Decision}, onResponseWritten: commandApprovalResponseWriteCallback(
		handler, generation, request.idKey, params.ThreadID,
	)}
}

func commandApprovalResponseWriteCallback(
	handler CommandApprovalHandler,
	generation, requestIDKey, threadID string,
) func(error) {
	return func(err error) {
		handler.HandleCommandApprovalResponseWritten(CommandApprovalResponseWriteResult{
			RuntimeGeneration: generation,
			RequestIDKey:      requestIDKey,
			ThreadID:          threadID,
			Succeeded:         err == nil,
		})
	}
}

type commandApprovalParams struct {
	ThreadID       string
	TurnID         string
	ItemID         string
	StartedAtMS    int64
	Command        string
	CommandActions []CommandApprovalAction
	Cwd            string
	EnvironmentID  *string
}

func decodeCommandApprovalParams(raw json.RawMessage) (commandApprovalParams, error) {
	allowed := map[string]struct{}{
		"threadId": {}, "turnId": {}, "itemId": {}, "startedAtMs": {}, "command": {},
		"commandActions": {}, "cwd": {}, "approvalId": {}, "environmentId": {}, "reason": {},
		"availableDecisions": {},
	}
	fields, err := decodeUniqueJSONObject(raw, allowed)
	if err != nil {
		return commandApprovalParams{}, err
	}
	for _, required := range []string{"threadId", "turnId", "itemId", "startedAtMs", "command", "commandActions", "cwd"} {
		if _, ok := fields[required]; !ok {
			return commandApprovalParams{}, errors.New("command approval request omitted a required field")
		}
	}
	var params commandApprovalParams
	if json.Unmarshal(fields["threadId"], &params.ThreadID) != nil || params.ThreadID == "" ||
		json.Unmarshal(fields["turnId"], &params.TurnID) != nil || params.TurnID == "" ||
		json.Unmarshal(fields["itemId"], &params.ItemID) != nil || params.ItemID == "" ||
		json.Unmarshal(fields["startedAtMs"], &params.StartedAtMS) != nil ||
		json.Unmarshal(fields["command"], &params.Command) != nil || params.Command == "" ||
		json.Unmarshal(fields["cwd"], &params.Cwd) != nil || params.Cwd == "" {
		return commandApprovalParams{}, errors.New("command approval request has an invalid required field")
	}
	if approvalID, ok := fields["approvalId"]; ok && string(approvalID) != "null" {
		return commandApprovalParams{}, errors.New("command approvalId must be absent or null")
	}
	if environmentID, ok := fields["environmentId"]; ok && string(environmentID) != "null" {
		var value string
		if json.Unmarshal(environmentID, &value) != nil || value == "" {
			return commandApprovalParams{}, errors.New("command environmentId is invalid")
		}
		params.EnvironmentID = &value
	}
	if reason, ok := fields["reason"]; ok && string(reason) != "null" {
		var value string
		if !utf8.Valid(reason) || json.Unmarshal(reason, &value) != nil || len(value) > maxApprovalReasonBytes ||
			!utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
			return commandApprovalParams{}, errors.New("command approval reason is invalid")
		}
		// Runtime reason is explanatory model text, not approval authority. It is
		// deliberately validated and discarded here so it cannot enter Host
		// session state, events, logs, replay fingerprints, or persistence.
	}
	var actions []json.RawMessage
	if json.Unmarshal(fields["commandActions"], &actions) != nil || len(actions) != 1 {
		return commandApprovalParams{}, errors.New("command approval requires one action")
	}
	actionFields, err := decodeUniqueJSONObject(actions[0], map[string]struct{}{"type": {}, "command": {}})
	if err != nil || len(actionFields) != 2 {
		return commandApprovalParams{}, errors.New("command approval action is invalid")
	}
	var action CommandApprovalAction
	if json.Unmarshal(actionFields["type"], &action.Type) != nil ||
		json.Unmarshal(actionFields["command"], &action.Command) != nil || action.Type == "" || action.Command == "" {
		return commandApprovalParams{}, errors.New("command approval action is invalid")
	}
	params.CommandActions = []CommandApprovalAction{action}
	return params, nil
}

func (m *Manager) StartThread(ctx context.Context, cwd string) (ThreadInfo, error) {
	if !m.config.MiniMax.Enabled && !m.config.FakeResponses.Enabled {
		return ThreadInfo{}, errors.New("model provider is not configured")
	}
	if err := validateWorkspace(cwd); err != nil {
		return ThreadInfo{}, err
	}
	params := struct {
		Model                 string            `json:"model"`
		ModelProvider         string            `json:"modelProvider"`
		Cwd                   string            `json:"cwd"`
		ApprovalPolicy        string            `json:"approvalPolicy"`
		Sandbox               string            `json:"sandbox"`
		DeveloperInstructions string            `json:"developerInstructions"`
		Ephemeral             bool              `json:"ephemeral"`
		DynamicTools          []dynamicToolSpec `json:"dynamicTools,omitempty"`
	}{
		Model:                 MiniMaxModel,
		ModelProvider:         MiniMaxProviderID,
		Cwd:                   cwd,
		ApprovalPolicy:        m.sessionApprovalPolicy(),
		Sandbox:               SessionSandbox,
		DeveloperInstructions: m.sessionDeveloperInstructions(),
		Ephemeral:             false,
	}
	if m.config.DynamicToolsEnabled {
		if m.dynamicToolHandler == nil {
			return ThreadInfo{}, errors.New("dynamic tool handler is not configured")
		}
		params.DeveloperInstructions += " The Host-owned generate_image dynamic tool is explicitly allowed in this read-only session and does not modify the workspace. It is the only permitted tool. Call generate_image exactly once only when the user explicitly asks to generate a new image, or to generate from the current turn's single character reference image. Never call it for ordinary image viewing or analysis, and never invent image output."
		params.DynamicTools = []dynamicToolSpec{imageGenerationToolSpec()}
	}
	var response threadResponse
	if err := m.request(ctx, RuntimeMethodThreadStart, params, &response); err != nil {
		return ThreadInfo{}, err
	}
	return validateThreadResponse(response)
}

func (m *Manager) ResumeThread(ctx context.Context, threadID string) (ThreadInfo, error) {
	if threadID == "" {
		return ThreadInfo{}, errors.New("Codex thread id is required")
	}
	type resumeParams struct {
		ThreadID              string  `json:"threadId"`
		ApprovalPolicy        *string `json:"approvalPolicy,omitempty"`
		Sandbox               *string `json:"sandbox,omitempty"`
		DeveloperInstructions *string `json:"developerInstructions,omitempty"`
	}
	params := resumeParams{ThreadID: threadID}
	if m.config.CommandApprovalEnabled {
		approvalPolicy := m.sessionApprovalPolicy()
		sandbox := SessionSandbox
		developerInstructions := m.sessionDeveloperInstructions()
		params.ApprovalPolicy = &approvalPolicy
		params.Sandbox = &sandbox
		params.DeveloperInstructions = &developerInstructions
	}
	var response threadResponse
	if err := m.request(ctx, RuntimeMethodThreadResume, params, &response); err != nil {
		return ThreadInfo{}, err
	}
	thread, err := validateThreadResponse(response)
	if err != nil {
		return ThreadInfo{}, err
	}
	if thread.ID != threadID {
		return ThreadInfo{}, errors.New("thread/resume returned an unexpected thread id")
	}
	return thread, nil
}

func (m *Manager) StartTurn(
	ctx context.Context,
	threadID string,
	input string,
	reasoningEffort string,
) (TurnInfo, error) {
	if threadID == "" {
		return TurnInfo{}, errors.New("Codex thread id is required")
	}
	if strings.TrimSpace(input) == "" {
		return TurnInfo{}, errors.New("turn input is required")
	}
	if len(input) > maxTurnInputBytes {
		return TurnInfo{}, fmt.Errorf("turn input exceeds %d bytes", maxTurnInputBytes)
	}
	if reasoningEffort == "" {
		reasoningEffort = "none"
	}
	if reasoningEffort != "none" && reasoningEffort != "high" {
		return TurnInfo{}, errors.New("reasoning effort must be none or high")
	}
	params := struct {
		ThreadID       string                 `json:"threadId"`
		Input          []any                  `json:"input"`
		Effort         string                 `json:"effort"`
		ApprovalPolicy *string                `json:"approvalPolicy,omitempty"`
		SandboxPolicy  *readOnlySandboxPolicy `json:"sandboxPolicy,omitempty"`
	}{
		ThreadID: threadID,
		Input: []any{map[string]any{
			"type": "text",
			"text": input,
		}},
		Effort: reasoningEffort,
	}
	if m.config.CommandApprovalEnabled {
		approvalPolicy := m.sessionApprovalPolicy()
		sandboxPolicy := sessionTurnSandboxPolicy()
		params.ApprovalPolicy = &approvalPolicy
		params.SandboxPolicy = &sandboxPolicy
	}
	var response turnStartResponse
	if err := m.request(ctx, RuntimeMethodTurnStart, params, &response); err != nil {
		return TurnInfo{}, err
	}
	if response.Turn.ID == "" {
		return TurnInfo{}, errors.New("turn/start response omitted turn id")
	}
	return TurnInfo{ID: response.Turn.ID, Status: response.Turn.Status}, nil
}

func (m *Manager) StartTurnV2(
	ctx context.Context,
	threadID string,
	inputs []UserInput,
	reasoningEffort string,
) (TurnInfo, error) {
	if threadID == "" {
		return TurnInfo{}, errors.New("Codex thread id is required")
	}
	if len(inputs) == 0 || len(inputs) > maxTurnV2InputCount {
		return TurnInfo{}, fmt.Errorf("Runtime turn input count must be between 1 and %d", maxTurnV2InputCount)
	}
	if reasoningEffort == "" {
		reasoningEffort = "none"
	}
	if reasoningEffort != "none" && reasoningEffort != "high" {
		return TurnInfo{}, errors.New("reasoning effort must be none or high")
	}
	wireInputs := make([]any, 0, len(inputs))
	for _, input := range inputs {
		wire, err := input.wireValue()
		if err != nil {
			return TurnInfo{}, err
		}
		wireInputs = append(wireInputs, wire)
	}
	params := struct {
		ThreadID       string                 `json:"threadId"`
		Input          []any                  `json:"input"`
		Effort         string                 `json:"effort"`
		ApprovalPolicy *string                `json:"approvalPolicy,omitempty"`
		SandboxPolicy  *readOnlySandboxPolicy `json:"sandboxPolicy,omitempty"`
	}{
		ThreadID: threadID, Input: wireInputs, Effort: reasoningEffort,
	}
	if m.config.CommandApprovalEnabled {
		approvalPolicy := m.sessionApprovalPolicy()
		sandboxPolicy := sessionTurnSandboxPolicy()
		params.ApprovalPolicy = &approvalPolicy
		params.SandboxPolicy = &sandboxPolicy
	}
	var response turnStartResponse
	if err := m.request(ctx, RuntimeMethodTurnStart, params, &response); err != nil {
		return TurnInfo{}, err
	}
	if response.Turn.ID == "" {
		return TurnInfo{}, errors.New("turn/start response omitted turn id")
	}
	return TurnInfo{ID: response.Turn.ID, Status: response.Turn.Status}, nil
}

func (m *Manager) InterruptTurn(ctx context.Context, threadID, turnID string) error {
	if threadID == "" || turnID == "" {
		return errors.New("Codex thread id and turn id are required")
	}
	return m.request(ctx, RuntimeMethodTurnInterrupt, struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
	}{ThreadID: threadID, TurnID: turnID}, &struct{}{})
}

func (m *Manager) DeleteThread(ctx context.Context, threadID string) error {
	if threadID == "" {
		return errors.New("Codex thread id is required")
	}
	m.mu.Lock()
	if _, exists := m.deleteWaiters[threadID]; exists {
		m.mu.Unlock()
		return errors.New("thread/delete is already pending")
	}
	waiter := make(chan struct{})
	m.deleteWaiters[threadID] = waiter
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		if current := m.deleteWaiters[threadID]; current == waiter {
			delete(m.deleteWaiters, threadID)
		}
		m.mu.Unlock()
	}()
	if err := m.request(ctx, RuntimeMethodThreadDelete, struct {
		ThreadID string `json:"threadId"`
	}{ThreadID: threadID}, &struct{}{}); err != nil {
		return err
	}
	timer := time.NewTimer(m.config.RequestTimeout)
	defer timer.Stop()
	select {
	case <-waiter:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errors.New("thread/delete notification was not confirmed")
	}
}

func IsThreadNotFound(err error, threadID string) bool {
	var rpcError *RPCError
	return errors.As(err, &rpcError) && rpcError.Code == -32600 && rpcError.Message == "thread not found: "+threadID
}

func (m *Manager) request(ctx context.Context, method string, params, result any) error {
	m.mu.Lock()
	client := m.client
	ready := m.status.Ready && m.status.State == StateReady
	m.mu.Unlock()
	if !ready || client == nil {
		return errors.New("Codex Runtime is not ready")
	}
	requestCtx, cancel := context.WithTimeout(ctx, m.config.RequestTimeout)
	defer cancel()
	if err := client.Request(requestCtx, method, params, result); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	return nil
}

func validateWorkspace(cwd string) error {
	if cwd == "" || !filepath.IsAbs(cwd) {
		return errors.New("workspace cwd must be an absolute path")
	}
	info, err := os.Stat(cwd)
	if err != nil {
		return fmt.Errorf("stat workspace cwd: %w", err)
	}
	if !info.IsDir() {
		return errors.New("workspace cwd is not a directory")
	}
	return nil
}

func validateThreadResponse(response threadResponse) (ThreadInfo, error) {
	if response.Thread.ID == "" || response.Thread.SessionID == "" || response.Model == "" || response.ModelProvider == "" {
		return ThreadInfo{}, errors.New("thread response is incomplete")
	}
	turns := make([]TurnInfo, 0, len(response.Thread.Turns))
	for _, turn := range response.Thread.Turns {
		if turn.ID == "" {
			continue
		}
		turns = append(turns, TurnInfo{ID: turn.ID, Status: turn.Status})
	}
	return ThreadInfo{
		ID:             response.Thread.ID,
		RuntimeSession: response.Thread.SessionID,
		Model:          response.Model,
		ModelProvider:  response.ModelProvider,
		Turns:          turns,
	}, nil
}
