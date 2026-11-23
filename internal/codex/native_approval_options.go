package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	options "github.com/36Dge/yijie-agent-host/internal/contracts/runtimeapprovalsv4"
	"github.com/google/uuid"
)

// A handle selects this callback's frozen native response. Neither the UI nor
// the HTTP client can propose arguments, a permission profile or a rule.
type nativeOption struct {
	label, status, legacy string
	detail                string
	response              any
	commandRule           bool
}

// Describe the choice already frozen by the Runtime adapter. Labels can be
// translated or redacted without changing the handle or its native response.
func (v nativeOption) presentation(id string) options.NativeApprovalOption {
	choice := options.NativeApprovalOption{Id: id, Label: v.label}
	var kind options.NativeApprovalOptionKind
	switch {
	case v.commandRule:
		kind = options.CommandPrefix
	case v.legacy == "approve_once":
		kind = options.AllowOnce
	case v.legacy == "reject":
		kind = options.Decline
	case v.legacy == "cancel":
		kind = options.Cancel
	case v.status == "approved":
		kind = options.AllowScope
	case v.status == "rejected":
		kind = options.DenyRule
	default:
		return choice
	}
	choice.Kind = &kind
	if v.detail != "" {
		detail := v.detail
		choice.Detail = &detail
	}
	return choice
}

func (e *runtimeApprovalCallback) setOptions(values []nativeOption) {
	e.options = values
	e.view.NativeChoices = make([]options.NativeApprovalOption, 0, len(values))
	for i, v := range values {
		e.view.NativeChoices = append(e.view.NativeChoices, v.presentation(fmt.Sprintf("native_%d", i)))
	}
}

func IsNativeApprovalHandle(s string) bool {
	suffix, ok := strings.CutPrefix(s, "native_")
	n, err := strconv.Atoi(suffix)
	return ok && err == nil && n >= 0 && n < 128 && strconv.Itoa(n) == suffix
}

func (e *runtimeApprovalCallback) option(choice string) *nativeOption {
	if IsNativeApprovalHandle(choice) {
		n, _ := strconv.Atoi(choice[len("native_"):])
		if n < len(e.options) {
			return &e.options[n]
		}
		return nil
	}
	var match *nativeOption
	for i := range e.options {
		if e.options[i].legacy == choice {
			if match != nil {
				return nil
			}
			match = &e.options[i]
		}
	}
	return match
}

func (m *Manager) callbackOption(e *runtimeApprovalCallback, decision string) *nativeOption {
	m.permissionCallbacks.mu.Lock()
	defer m.permissionCallbacks.mu.Unlock()
	if choice := e.option(decision); choice != nil {
		copy := *choice
		return &copy
	}
	return nil
}

func simpleNativeDecision(name string) (nativeOption, bool) {
	v := nativeOption{status: "approved", response: map[string]string{"decision": name}}
	switch name {
	case "accept":
		v.label = "允许一次"
		v.legacy = "approve_once"
	case "acceptForSession":
		v.label = "在本会话允许"
		v.legacy = "approve_session"
	case "decline":
		v.label = "拒绝并继续"
		v.status = "rejected"
		v.legacy = "reject"
	case "cancel":
		v.label = "取消并中断本轮"
		v.status = "cancelled"
		v.legacy = "cancel"
	default:
		return nativeOption{}, false
	}
	return v, true
}

func commandOption(raw json.RawMessage) (nativeOption, bool) {
	var name string
	if json.Unmarshal(raw, &name) == nil {
		return simpleNativeDecision(name)
	}
	var value map[string]json.RawMessage
	if json.Unmarshal(raw, &value) != nil || len(value) != 1 {
		return nativeOption{}, false
	}
	if amend := value["acceptWithExecpolicyAmendment"]; amend != nil {
		var rule struct {
			Prefix []string `json:"execpolicy_amendment"`
		}
		if json.Unmarshal(amend, &rule) != nil || len(rule.Prefix) == 0 {
			return nativeOption{}, false
		}
		display, _ := json.Marshal(rule.Prefix)
		return nativeOption{label: "允许并保存命令前缀 " + string(display), detail: string(display), status: "approved", legacy: ApproveSimilar, response: map[string]any{"decision": raw}, commandRule: true}, true
	}
	if amend := value["applyNetworkPolicyAmendment"]; amend != nil {
		var rule struct {
			Network struct{ Host, Action string } `json:"network_policy_amendment"`
		}
		if json.Unmarshal(amend, &rule) != nil || rule.Network.Host == "" {
			return nativeOption{}, false
		}
		status, label := "approved", "允许并保存网络规则"
		switch rule.Network.Action {
		case "allow":
		case "deny":
			status, label = "rejected", "拒绝并保存网络规则"
		default:
			return nativeOption{}, false
		}
		// Display all native rule fields, including future transport selectors.
		return nativeOption{label: label + " " + string(amend), status: status, response: map[string]any{"decision": raw}}, true
	}
	return nativeOption{}, false
}
func rawChoice(v any) json.RawMessage      { b, _ := json.Marshal(v); return b }
func presentNative(v json.RawMessage) bool { return len(v) > 0 && string(v) != "null" }

// Older app-server requests can omit availableDecisions. These are the fixed
// yijie-Codex TUI's default_exec_approval_decisions for that wire version.
func commandOptions(available, network, prefix, networkRules, additional json.RawMessage) ([]nativeOption, bool) {
	var decisions []json.RawMessage
	if presentNative(available) {
		if json.Unmarshal(available, &decisions) != nil || len(decisions) > 128 {
			return nil, false
		}
	} else {
		decisions = append(decisions, rawChoice("accept"))
		if presentNative(network) {
			decisions = append(decisions, rawChoice("acceptForSession"))
			var rules []json.RawMessage
			if presentNative(networkRules) && json.Unmarshal(networkRules, &rules) != nil {
				return nil, false
			}
			for _, r := range rules {
				var a struct{ Action string }
				if json.Unmarshal(r, &a) == nil && a.Action == "allow" {
					decisions = append(decisions, rawChoice(map[string]any{"applyNetworkPolicyAmendment": map[string]any{"network_policy_amendment": r}}))
					break
				}
			}
		} else if !presentNative(additional) && presentNative(prefix) {
			decisions = append(decisions, rawChoice(map[string]any{"acceptWithExecpolicyAmendment": map[string]any{"execpolicy_amendment": prefix}}))
		}
		decisions = append(decisions, rawChoice("cancel"))
	}
	vals := make([]nativeOption, 0, len(decisions))
	for _, r := range decisions {
		if o, ok := commandOption(r); ok {
			vals = append(vals, o)
		}
	}
	return vals, len(vals) > 0
}

func fileOptions() []nativeOption {
	vals := make([]nativeOption, 0, 3)
	for _, n := range []string{"accept", "acceptForSession", "cancel"} {
		v, _ := simpleNativeDecision(n)
		if n == "acceptForSession" {
			v.label = "本会话允许修改这些文件"
		}
		vals = append(vals, v)
	}
	return vals
}
func permissionOptions(profile json.RawMessage) []nativeOption {
	return []nativeOption{
		{label: "仅本轮授予这些权限", status: "approved", legacy: "approve_once", response: map[string]any{"permissions": profile, "scope": "turn"}},
		{label: "仅本轮授予并启用严格自动审核", status: "approved", response: map[string]any{"permissions": profile, "scope": "turn", "strictAutoReview": true}},
		{label: "本会话授予这些权限", status: "approved", response: map[string]any{"permissions": profile, "scope": "session"}},
		{label: "继续执行并拒绝额外权限", status: "rejected", legacy: "reject", response: map[string]any{"permissions": map[string]any{}, "scope": "turn"}},
	}
}

func (m *Manager) approvalCallbackOwner(ctx context.Context, thread, turn string) (string, error) {
	if m.config.NativeToolsEnabled {
		return m.nativeApprovalOwner(ctx, thread, turn)
	}
	m.permissionCallbacks.mu.Lock()
	defer m.permissionCallbacks.mu.Unlock()
	if thread != "" && turn != "" && m.permissionCallbacks.activeTurns[thread] == turn {
		return thread, nil
	}
	return "", errors.New("native approval is not active")
}

func (m *Manager) handleNativeMcpConfirmation(ctx context.Context, r serverRequest) (serverRequestResult, bool) {
	if !m.config.RuntimePermissionsEnabled || !r.exactEnvelope {
		return serverRequestResult{}, false
	}
	var p struct {
		Thread        string `json:"threadId"`
		Turn          string `json:"turnId"`
		Server        string `json:"serverName"`
		Mode, Message string
		Meta          map[string]json.RawMessage `json:"_meta"`
		Schema        struct {
			Type       string
			Properties map[string]json.RawMessage
			Required   []string
		} `json:"requestedSchema"`
	}
	if json.Unmarshal(r.params, &p) != nil || p.Thread == "" || p.Turn == "" || p.Server == "" || p.Mode != "form" || p.Schema.Type != "object" || p.Schema.Properties == nil || len(p.Schema.Properties) != 0 || len(p.Schema.Required) != 0 || p.Meta["yijieKind"] != nil {
		return serverRequestResult{}, false
	}
	// Parameter forms and URL sign-ins require actual user input. An approval
	// button is only a valid client for an empty confirmation form.
	var title, kind string
	_ = json.Unmarshal(p.Meta["tool_title"], &title)
	_ = json.Unmarshal(p.Meta["codex_approval_kind"], &kind)
	summary := p.Server
	if title != "" {
		summary += " · " + title
	}
	body := string(p.Meta["tool_params"])
	owner, err := m.approvalCallbackOwner(ctx, p.Thread, p.Turn)
	if err != nil {
		return marketNativeReply("decline", nil)
	}
	entry := &runtimeApprovalCallback{threadID: p.Thread, ownerThreadID: owner, turnID: p.Turn, requestKey: r.idKey,
		resolve: make(chan string, 1), written: make(chan struct{}), view: RuntimeApproval{ID: uuid.NewString(), Kind: "permissions", Summary: summary, Scope: body, Reason: p.Message, Status: "pending"}}
	reply := func(action, persist string) map[string]any {
		v := map[string]any{"action": action, "content": nil, "_meta": nil}
		if action == "accept" {
			v["content"] = map[string]any{}
		}
		if persist != "" {
			v["_meta"] = map[string]string{"persist": persist}
		}
		return v
	}
	vals := []nativeOption{{label: "允许一次", status: "approved", legacy: "approve_once", response: reply("accept", "")}}
	if kind == "mcp_tool_call" {
		var scopes []string
		var single string
		if v := p.Meta["persist"]; len(v) > 0 {
			if json.Unmarshal(v, &single) == nil {
				scopes = []string{single}
			} else {
				_ = json.Unmarshal(v, &scopes)
			}
		}
		for _, scope := range scopes {
			switch scope {
			case "session":
				vals = append(vals, nativeOption{label: "本会话允许此工具", status: "approved", response: reply("accept", scope)})
			case "always":
				vals = append(vals, nativeOption{label: "始终允许此工具", status: "approved", response: reply("accept", scope)})
			}
		}
	}
	vals = append(vals, nativeOption{label: "拒绝", status: "rejected", legacy: "reject", response: reply("decline", "")}, nativeOption{label: "取消本次", status: "cancelled", legacy: "cancel", response: reply("cancel", "")})
	entry.setOptions(vals)
	if m.addRuntimeApproval(entry) != nil {
		return marketNativeReply("decline", nil)
	}
	select {
	case <-ctx.Done():
		m.finishRuntimeApproval(entry, ctx.Err())
		return serverRequestResult{}, true
	case choice := <-entry.resolve:
		opt := m.callbackOption(entry, choice)
		if opt == nil {
			return marketNativeReply("decline", nil)
		}
		return serverRequestResult{respond: true, value: opt.response, onResponseWritten: func(e error) { m.finishRuntimeApproval(entry, e) }}, true
	}
}
