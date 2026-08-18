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
)

const (
	maxTurnInputBytes    = 1 << 20
	maxTurnV2InputCount  = 16
	maxImageDataURLBytes = 13981039

	RuntimeMethodThreadResume        = "thread/resume"
	RuntimeMethodThreadStart         = "thread/start"
	RuntimeMethodThreadDelete        = "thread/delete"
	RuntimeNotificationThreadDeleted = "thread/deleted"
	RuntimeMethodTurnInterrupt       = "turn/interrupt"
	RuntimeMethodTurnStart           = "turn/start"
	SessionApprovalPolicy            = "never"
	SessionSandbox                   = "read-only"
)

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

func (m *Manager) SetNotificationHandler(handler NotificationHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return errors.New("notification handler must be set before Runtime startup")
	}
	m.notificationHandler = handler
	return nil
}

func (m *Manager) StartThread(ctx context.Context, cwd string) (ThreadInfo, error) {
	if !m.config.MiniMax.Enabled && !m.config.FakeResponses.Enabled {
		return ThreadInfo{}, errors.New("model provider is not configured")
	}
	if err := validateWorkspace(cwd); err != nil {
		return ThreadInfo{}, err
	}
	params := struct {
		Model                 string `json:"model"`
		ModelProvider         string `json:"modelProvider"`
		Cwd                   string `json:"cwd"`
		ApprovalPolicy        string `json:"approvalPolicy"`
		Sandbox               string `json:"sandbox"`
		DeveloperInstructions string `json:"developerInstructions"`
		Ephemeral             bool   `json:"ephemeral"`
	}{
		Model:                 MiniMaxModel,
		ModelProvider:         MiniMaxProviderID,
		Cwd:                   cwd,
		ApprovalPolicy:        SessionApprovalPolicy,
		Sandbox:               SessionSandbox,
		DeveloperInstructions: "Runtime Baseline 2 is read-only. Do not modify files or request elevated permissions.",
		Ephemeral:             false,
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
	var response threadResponse
	if err := m.request(ctx, RuntimeMethodThreadResume, struct {
		ThreadID string `json:"threadId"`
	}{ThreadID: threadID}, &response); err != nil {
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
		ThreadID string `json:"threadId"`
		Input    []any  `json:"input"`
		Effort   string `json:"effort"`
	}{
		ThreadID: threadID,
		Input: []any{map[string]any{
			"type": "text",
			"text": input,
		}},
		Effort: reasoningEffort,
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
		ThreadID string `json:"threadId"`
		Input    []any  `json:"input"`
		Effort   string `json:"effort"`
	}{
		ThreadID: threadID,
		Input:    wireInputs,
		Effort:   reasoningEffort,
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
