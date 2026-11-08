package codex

// This is a callback adapter. Codex owns sandbox enforcement and risk review.
// Raw Runtime payloads never leave this process or become persistent records.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const permissionInstructions = "You are the local Yijie assistant. Follow the user's task and native permissions/approvals; approval covers only the exact requested action. " +
	"[yijie-answer-completion-v1] Deliver the requested result, supported partial results with limitations, or a necessary user question. An intention is not completion. Use MCP resource discovery only for needed connected-service resources, never to search the web or keep a turn alive. " +
	"Deliver chat results as concise prose or a table when useful. Create files, sites, apps or images only when requested or necessary. Available tools or full access do not expand the requested output. " +
	publicProgressInstructions + "\n\n" +
	"Respect active connector selection: use an applicable selected retrieval connector and its documented tools; do not substitute native search or use both for the same information need. Otherwise use exposed native web retrieval. " +
	"Cite key external facts with specific pages actually returned/read, using Markdown links near the supported claims; prefer original official sources. Explanations and identified inference need no artificial citations or citation after every sentence. Quantitative comparisons need source, year, measurement basis and the same indicator/denominator; gross output and value added cannot establish a direct ratio. Disclose mixed years/populations. For studies/rankings preserve the actual sample period, named entities and comparison group; do not present historical findings as current. If evidence lacks these details, omit or qualify the claim. Search hits, a query or a homepage alone do not prove a specific claim. Never invent URLs, citation IDs, quotes or results. Without support or a usable source URL, say unverified, omit precise figures or give a qualitative comparison; never guess. Keep the final answer focused. " +
	webRetrievalInstructions +
	"Every tool call must advance the requested result. Do not inspect the workspace or run true/echo/printf or other commands merely to announce progress or keep a turn going. Provider-hosted results are already supplied evidence, not a reason for dummy tool calls. Once evidence suffices, answer; if no useful retrieval remains, state the limitation and finish with supported facts. Quote paths containing spaces in necessary shell commands."

type PermissionMode string

const (
	PermissionAsk  PermissionMode = "ask"
	PermissionAuto PermissionMode = "auto"
	PermissionFull PermissionMode = "full"
)

func (mode PermissionMode) Valid() bool {
	return mode == PermissionAsk || mode == PermissionAuto || mode == PermissionFull
}

type permissionContextKey struct{}

func WithPermissionMode(ctx context.Context, mode PermissionMode) context.Context {
	return context.WithValue(ctx, permissionContextKey{}, mode)
}

func permissionPolicy(mode PermissionMode) (string, string, map[string]any, error) {
	switch mode {
	case PermissionAsk, PermissionAuto:
		reviewer := "user"
		if mode == PermissionAuto {
			reviewer = "auto_review"
		}
		return "on-request", reviewer, map[string]any{"type": "workspaceWrite", "networkAccess": false}, nil
	case PermissionFull:
		return "never", "user", map[string]any{"type": "dangerFullAccess"}, nil
	default:
		return "", "", nil, errors.New("invalid permission mode")
	}
}

func (m *Manager) ValidatePermissionMode(mode PermissionMode) error {
	if !m.config.RuntimePermissionsEnabled || !mode.Valid() {
		return errors.New("runtime permission mode is unavailable")
	}
	return nil
}

type RuntimeApproval struct {
	ID, Kind, Summary, Scope, Reason, Status string
	Mcp                                      *McpApprovalScope
	CommandPrefix                            []string
}

const ApproveSimilar = "approve_similar"

// Use only the Runtime's offer for this callback. Newer senders supply the
// actual choices; on the stable wire the proposed amendment can stand alone.
func offeredCommandPrefix(proposed, available json.RawMessage) []string {
	var proposedPrefix []string
	if len(proposed) != 0 && json.Unmarshal(proposed, &proposedPrefix) != nil {
		return nil
	}
	valid := func(prefix []string) bool {
		if len(prefix) == 0 || len(prefix) > 64 {
			return false
		}
		for _, arg := range prefix {
			if len(arg) == 0 || len(arg) > 256 {
				return false
			}
		}
		return true
	}
	if len(available) == 0 || string(available) == "null" {
		if valid(proposedPrefix) {
			return proposedPrefix
		}
		return nil
	}
	var choices []json.RawMessage
	if json.Unmarshal(available, &choices) != nil {
		return nil
	}
	var selected []string
	for _, choice := range choices {
		var offer struct {
			Accept *struct {
				Prefix []string `json:"execpolicy_amendment"`
			} `json:"acceptWithExecpolicyAmendment"`
		}
		if json.Unmarshal(choice, &offer) != nil || offer.Accept == nil {
			continue
		}
		prefix := offer.Accept.Prefix
		if !valid(prefix) || (proposedPrefix != nil && !slices.Equal(proposedPrefix, prefix)) || (selected != nil && !slices.Equal(selected, prefix)) {
			return nil // No unambiguous, reviewable native option.
		}
		selected = prefix
	}
	return selected
}

type runtimeApprovalCallback struct {
	ownerThreadID                          string
	market                                 *marketApprovalCallback
	view                                   RuntimeApproval
	threadID, turnID, requestKey, decision string
	permissions                            json.RawMessage
	guardian                               json.RawMessage
	resolve                                chan string
	written                                chan struct{}
	writtenOnce                            sync.Once
	writtenErr                             error
}

type runtimeApprovalCallbacks struct {
	mu          sync.Mutex
	entries     []*runtimeApprovalCallback
	fileScopes  map[string]string
	activeTurns map[string]string
	nativeRoots map[string]bool
}

func (m *Manager) ListRuntimeApprovals(threadID string) []RuntimeApproval {
	m.permissionCallbacks.mu.Lock()
	defer m.permissionCallbacks.mu.Unlock()
	views := make([]RuntimeApproval, 0)
	for _, entry := range m.permissionCallbacks.entries {
		if entry.threadID == threadID || entry.ownerThreadID == threadID {
			views = append(views, entry.view)
		}
	}
	return views
}

func (m *Manager) addRuntimeApproval(entry *runtimeApprovalCallback) error {
	c := &m.permissionCallbacks
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= 128 {
		for i, old := range c.entries {
			if old.view.Status != "pending" {
				c.entries = append(c.entries[:i], c.entries[i+1:]...)
				break
			}
		}
	}
	if len(c.entries) >= 128 {
		return errors.New("too many pending runtime callbacks")
	}
	c.entries = append(c.entries, entry)
	return nil
}

func (m *Manager) DecideRuntimeApproval(ctx context.Context, threadID, id, decision string) (RuntimeApproval, error) {
	if decision != "approve_once" && decision != "reject" && decision != "cancel" && decision != ApproveSimilar {
		return RuntimeApproval{}, errors.New("invalid approval decision")
	}
	var authority *managedCodexHomeAuthority
	if decision == ApproveSimilar {
		m.mu.Lock()
		if !m.shutdownRequested {
			authority = m.codexHomeAuthority
		}
		m.mu.Unlock()
	}
	c := &m.permissionCallbacks
	c.mu.Lock()
	var entry *runtimeApprovalCallback
	for _, candidate := range c.entries {
		if (candidate.threadID == threadID || candidate.ownerThreadID == threadID) && candidate.view.ID == id {
			entry = candidate
			break
		}
	}
	if entry == nil || entry.market != nil || entry.view.Kind == "mcp" || entry.view.Mcp != nil {
		c.mu.Unlock()
		return RuntimeApproval{}, errors.New("runtime approval not found")
	}
	if decision == "cancel" && entry.view.Kind != "mcp" {
		c.mu.Unlock()
		return RuntimeApproval{}, errors.New("invalid approval decision")
	}
	if entry.decision != "" {
		if entry.decision != decision {
			c.mu.Unlock()
			return RuntimeApproval{}, errors.New("approval decision conflict")
		}
	} else {
		if entry.view.Status != "pending" || (entry.guardian == nil && c.activeTurns[entry.threadID] != entry.turnID) {
			c.mu.Unlock()
			return RuntimeApproval{}, errors.New("runtime approval is unavailable")
		}
		if decision == ApproveSimilar {
			if entry.view.Kind != "command" || len(entry.view.CommandPrefix) == 0 || authority == nil {
				c.mu.Unlock()
				return RuntimeApproval{}, errors.New("command prefix was not offered")
			}
			if err := authority.prepareNativeCommandRules(); err != nil {
				c.mu.Unlock()
				return RuntimeApproval{}, err
			}
		}
		entry.decision = decision
		if entry.guardian != nil {
			c.mu.Unlock()
			var err error
			if decision == "approve_once" {
				err = m.request(ctx, "thread/approveGuardianDeniedAction", map[string]any{"threadId": entry.threadID, "event": entry.guardian}, &struct{}{})
			}
			m.finishRuntimeApproval(entry, err)
			c.mu.Lock()
		} else {
			entry.resolve <- decision
		}
	}
	c.mu.Unlock()
	select {
	case <-entry.written:
		c.mu.Lock()
		defer c.mu.Unlock()
		return entry.view, entry.writtenErr
	case <-ctx.Done():
		return RuntimeApproval{}, ctx.Err()
	}
}

func (m *Manager) finishRuntimeApproval(entry *runtimeApprovalCallback, err error) {
	c := &m.permissionCallbacks
	c.mu.Lock()
	defer c.mu.Unlock()
	entry.writtenOnce.Do(func() {
		entry.writtenErr = err
		if err != nil {
			entry.view.Status = "unavailable"
		} else if entry.decision == "approve_once" || entry.decision == ApproveSimilar {
			entry.view.Status = "approved"
		} else if entry.decision == "cancel" {
			entry.view.Status = "cancelled"
		} else {
			entry.view.Status = "rejected"
		}
		if entry.market != nil {
			entry.market.view.State = marketApprovalState(entry.view.Status)
		}
		entry.permissions = nil
		entry.view.CommandPrefix = nil
		entry.guardian = nil
		close(entry.written)
	})
}

func (m *Manager) handleRuntimePermissionRequest(ctx context.Context, request serverRequest) (serverRequestResult, bool) {
	kinds := map[string]string{RuntimeMethodCommandApproval: "command", "item/fileChange/requestApproval": "file_change", "item/permissions/requestApproval": "permissions"}
	kind, supported := kinds[request.method]
	if !supported {
		return serverRequestResult{}, false
	}
	var p struct {
		ThreadID              string          `json:"threadId"`
		TurnID                string          `json:"turnId"`
		ItemID                string          `json:"itemId"`
		Command               string          `json:"command"`
		Cwd                   string          `json:"cwd"`
		Reason                string          `json:"reason"`
		GrantRoot             string          `json:"grantRoot"`
		Permissions           json.RawMessage `json:"permissions"`
		Network               json.RawMessage `json:"networkApprovalContext"`
		ProposedPrefix        json.RawMessage `json:"proposedExecpolicyAmendment"`
		AvailableDecisions    json.RawMessage `json:"availableDecisions"`
		AdditionalPermissions json.RawMessage `json:"additionalPermissions"`
	}
	invalid := func() (serverRequestResult, bool) {
		return serverRequestResult{respond: true, rpcError: &RPCError{Code: -32602, Message: "Invalid approval request"}}, true
	}
	if json.Unmarshal(request.params, &p) != nil || p.ThreadID == "" || p.TurnID == "" || p.ItemID == "" {
		return invalid()
	}
	ownerThreadID := p.ThreadID
	if m.config.NativeToolsEnabled {
		owner, err := m.nativeApprovalOwner(ctx, p.ThreadID, p.TurnID)
		if err != nil {
			return invalid()
		}
		ownerThreadID = owner
	} else {
		m.permissionCallbacks.mu.Lock()
		active := m.permissionCallbacks.activeTurns[p.ThreadID] == p.TurnID
		m.permissionCallbacks.mu.Unlock()
		if !active {
			return invalid()
		}
	}
	entry := &runtimeApprovalCallback{view: RuntimeApproval{ID: uuid.NewString(), Kind: kind, Summary: p.Command, Scope: p.Cwd, Reason: p.Reason, Status: "pending"}, threadID: p.ThreadID, turnID: p.TurnID, requestKey: request.idKey, permissions: p.Permissions, resolve: make(chan string, 1), written: make(chan struct{})}
	entry.ownerThreadID = ownerThreadID
	var prefix []string
	if kind == "command" && (len(p.Network) == 0 || string(p.Network) == "null") && (len(p.AdditionalPermissions) == 0 || string(p.AdditionalPermissions) == "null") {
		prefix = offeredCommandPrefix(p.ProposedPrefix, p.AvailableDecisions)
		entry.view.CommandPrefix = prefix
	}
	if kind == "file_change" {
		entry.view.Summary = "修改文件"
		m.permissionCallbacks.mu.Lock()
		entry.view.Scope = m.permissionCallbacks.fileScopes[p.ThreadID+"/"+p.ItemID]
		m.permissionCallbacks.mu.Unlock()
		if entry.view.Scope == "" {
			entry.view.Scope = p.GrantRoot
		}
	}
	if kind == "permissions" {
		if len(p.Permissions) == 0 || string(p.Permissions) == "null" {
			return invalid()
		}
		entry.view.Summary = "访问文件或互联网"
		entry.view.Scope = string(p.Permissions)
	}
	if len(p.Network) > 0 && string(p.Network) != "null" {
		entry.view.Scope += "\n" + string(p.Network)
	}
	if ownerThreadID != p.ThreadID {
		entry.view.Summary = "子任务：" + entry.view.Summary
	}
	if err := m.addRuntimeApproval(entry); err != nil {
		return serverRequestResult{respond: true, rpcError: &RPCError{Code: -32603, Message: "Approval capacity unavailable"}}, true
	}
	select {
	case <-ctx.Done():
		m.finishRuntimeApproval(entry, ctx.Err())
		return serverRequestResult{}, true
	case decision := <-entry.resolve:
		var value any
		if kind == "permissions" {
			permissions := json.RawMessage(`{}`)
			if decision == "approve_once" {
				permissions = p.Permissions
			}
			value = map[string]any{"permissions": permissions, "scope": "turn"}
		} else if decision == ApproveSimilar {
			value = map[string]any{"decision": map[string]any{"acceptWithExecpolicyAmendment": map[string][]string{"execpolicy_amendment": prefix}}}
		} else {
			native := "decline"
			if decision == "approve_once" {
				native = "accept"
			}
			value = map[string]string{"decision": native}
		}
		return serverRequestResult{respond: true, value: value, onResponseWritten: func(err error) { m.finishRuntimeApproval(entry, err) }}, true
	}
}

func snakeKey(key string) string {
	var b strings.Builder
	for _, r := range key {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// The app-server emits camelCase action fields; its approve operation consumes
// the upstream core GuardianAssessmentEvent (snake_case). This is a wire adapter.
func coreGuardianValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			if key == "type" || key == "source" {
				if s, ok := item.(string); ok {
					item = snakeKey(s)
				}
			}
			result[snakeKey(key)] = coreGuardianValue(item)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = coreGuardianValue(item)
		}
		return result
	default:
		return value
	}
}

func (m *Manager) handlePermissionNotification(method string, raw json.RawMessage) {
	if !m.config.RuntimePermissionsEnabled {
		return
	}
	var p map[string]any
	if json.Unmarshal(raw, &p) != nil {
		return
	}
	threadID, _ := p["threadId"].(string)
	if method == "turn/started" {
		turn, _ := p["turn"].(map[string]any)
		id, _ := turn["id"].(string)
		if threadID == "" || id == "" {
			return
		}
		m.permissionCallbacks.mu.Lock()
		if m.permissionCallbacks.activeTurns == nil {
			m.permissionCallbacks.activeTurns = make(map[string]string)
		}
		m.permissionCallbacks.activeTurns[threadID] = id
		m.permissionCallbacks.mu.Unlock()
		return
	}
	if method == "item/started" {
		item, _ := p["item"].(map[string]any)
		if item["type"] != "fileChange" {
			return
		}
		id, _ := item["id"].(string)
		changes, _ := item["changes"].([]any)
		var files []string
		for _, change := range changes {
			if c, ok := change.(map[string]any); ok {
				if path, ok := c["path"].(string); ok {
					files = append(files, path)
				}
			}
		}
		m.permissionCallbacks.mu.Lock()
		defer m.permissionCallbacks.mu.Unlock()
		if m.permissionCallbacks.fileScopes == nil {
			m.permissionCallbacks.fileScopes = make(map[string]string)
		}
		if len(m.permissionCallbacks.fileScopes) < 128 {
			m.permissionCallbacks.fileScopes[threadID+"/"+id] = strings.Join(files, "\n")
		}
		return
	}
	if method == "serverRequest/resolved" || method == "turn/completed" {
		key, _ := requestIDKey(json.RawMessage(mustJSON(p["requestId"])))
		turn, _ := p["turn"].(map[string]any)
		turnID, _ := turn["id"].(string)
		m.permissionCallbacks.mu.Lock()
		var ended []*runtimeApprovalCallback
		for _, entry := range m.permissionCallbacks.entries {
			if entry.threadID == threadID && entry.view.Status == "pending" && entry.decision == "" && entry.guardian == nil && ((method == "serverRequest/resolved" && entry.requestKey == key) || (method == "turn/completed" && entry.turnID == turnID)) {
				ended = append(ended, entry)
			}
		}
		if method == "turn/completed" {
			if m.permissionCallbacks.activeTurns[threadID] == turnID {
				delete(m.permissionCallbacks.activeTurns, threadID)
			}
			for k := range m.permissionCallbacks.fileScopes {
				if strings.HasPrefix(k, threadID+"/") {
					delete(m.permissionCallbacks.fileScopes, k)
				}
			}
		}
		m.permissionCallbacks.mu.Unlock()
		for _, entry := range ended {
			m.finishRuntimeApproval(entry, errors.New("runtime request ended"))
			select {
			case entry.resolve <- "reject":
			default:
			}
		}
		return
	}
	if method != "item/autoApprovalReview/completed" {
		return
	}
	review, _ := p["review"].(map[string]any)
	if review["status"] != "denied" {
		return
	}
	action, _ := p["action"].(map[string]any)
	if threadID == "" || action == nil {
		return
	}
	turnID, _ := p["turnId"].(string)
	event := map[string]any{"id": p["reviewId"], "turn_id": turnID, "target_item_id": p["targetItemId"], "started_at_ms": p["startedAtMs"], "completed_at_ms": p["completedAtMs"], "status": "denied", "risk_level": review["riskLevel"], "user_authorization": review["userAuthorization"], "rationale": review["rationale"], "decision_source": p["decisionSource"], "action": coreGuardianValue(action)}
	reason, _ := review["rationale"].(string)
	entry := &runtimeApprovalCallback{view: RuntimeApproval{ID: uuid.NewString(), Kind: "auto_review", Summary: fmt.Sprint(action["type"]), Scope: mustJSON(action), Reason: reason, Status: "pending"}, threadID: threadID, turnID: turnID, guardian: json.RawMessage(mustJSON(event)), written: make(chan struct{})}
	if m.config.NativeToolsEnabled {
		m.permissionCallbacks.mu.Lock()
		root := m.permissionCallbacks.nativeRoots[threadID]
		m.permissionCallbacks.mu.Unlock()
		if !root {
			go m.addNativeGuardianApproval(entry)
			return
		}
		entry.ownerThreadID = threadID
	}
	_ = m.addRuntimeApproval(entry)
}

func mustJSON(value any) string { encoded, _ := json.Marshal(value); return string(encoded) }

// A request to this adapter is never a new model call. The upstream reviewer
// itself may call the configured provider as part of the user's active turn.

// Only the explicitly opted-in local verification flow uses the fixed-route
// request meter. A missing meter fails closed; there is no direct fallback.
func (m *Manager) verificationProviderConfig() map[string]any {
	if !m.config.RuntimePermissionsEnabled || m.config.PermissionVerificationBaseURL == "" {
		return nil
	}
	config := map[string]any{
		"model_providers.minimax.base_url":            m.config.PermissionVerificationBaseURL,
		"model_providers.minimax.request_max_retries": 0,
		"model_providers.minimax.stream_max_retries":  0,
	}
	if m.config.ChatModelsEnabled {
		config["model_providers.kimi.base_url"] = m.config.PermissionVerificationBaseURL
		config["model_providers.kimi.request_max_retries"] = 0
		config["model_providers.kimi.stream_max_retries"] = 0
	}
	return config
}

func (m *Manager) permissionVerificationConfig() map[string]any {
	config := m.verificationProviderConfig()
	if config == nil {
		return nil
	}
	if m.config.PermissionVerificationPolicy != "" {
		// A supported native policy override, only for explicitly scoped local
		// verification. No review outcome or callback is generated by the Host.
		config["auto_review.policy"] = m.config.PermissionVerificationPolicy
	}
	return config
}
