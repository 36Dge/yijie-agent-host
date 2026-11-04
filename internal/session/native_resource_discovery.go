package session

import (
	"encoding/json"
	"fmt"
	"strings"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
)

// The native discovery result is a closed envelope. Reuse native MCP text,
// redaction, limits and lifecycle; never forward resource URIs or opaque cursors.
func projectNativeResourceDiscovery(wire nativeWireItem, out *native.NativeItem, diagnostic func(native.NativeMcpDisplayDiagnostics)) bool {
	if wire.Tool != "list_mcp_resources" && wire.Tool != "list_mcp_resource_templates" {
		return false
	}
	var args map[string]json.RawMessage
	if json.Unmarshal(wire.Arguments, &args) != nil || args == nil {
		out.ArgumentsSummary = nativeString("参数信息不可用")
		diagnostic(native.ArgumentsUnavailable)
	} else {
		label := "查询所有已连接服务的可用资源"
		for key := range args {
			if key != "server" && key != "cursor" {
				diagnostic(native.ArgumentsUnavailable)
			}
		}
		var server string
		if raw, ok := args["server"]; ok && string(raw) != "null" {
			if json.Unmarshal(raw, &server) != nil || len(server) > 64 || !nativeMcpIdentifier.MatchString(server) || v5SensitiveCommandLine(server) {
				diagnostic(native.ArgumentsUnavailable)
				label = "查询已连接服务的可用资源"
			} else {
				label = "查询服务 " + server + " 的可用资源"
			}
		}
		out.ArgumentsSummary = nativeString(label)
	}
	if len(wire.Result) == 0 {
		return true
	}
	if string(wire.Result) == "null" {
		out.Mcp.ResultKind = native.Null
		return true
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if json.Unmarshal(wire.Result, &result) != nil || len(result.Content) != 1 || result.Content[0].Type != "text" {
		out.Mcp.ResultKind = native.Omitted
		diagnostic(native.ResultUnavailable)
		return true
	}
	if result.IsError {
		clean := redactV5CommandText(result.Content[0].Text)
		bounded, truncated := truncateV5UTF8(clean, nativeTextLimit)
		if clean != result.Content[0].Text {
			diagnostic(native.ContentRedacted)
		}
		if truncated {
			diagnostic(native.ContentTruncated)
		}
		out.Mcp.ResultKind = native.Text
		out.Mcp.Texts = []native.NativeMcpText{{Index: 0, Text: bounded}}
		out.ResultSummary = nativeString("资源查询返回错误")
		return true
	}
	var payload map[string]json.RawMessage
	field, noun := "resources", "资源"
	if wire.Tool == "list_mcp_resource_templates" {
		field, noun = "resourceTemplates", "资源模板"
	}
	var resources []struct {
		Name  string `json:"name"`
		Title string `json:"title"`
	}
	if json.Unmarshal([]byte(result.Content[0].Text), &payload) != nil || json.Unmarshal(payload[field], &resources) != nil || resources == nil {
		out.Mcp.ResultKind = native.Omitted
		diagnostic(native.ResultUnavailable)
		return true
	}
	label := fmt.Sprintf("找到 %d 项可用%s", len(resources), noun)
	if len(resources) == 0 {
		label = "未发现可用" + noun
	}
	if cursor := payload["nextCursor"]; len(cursor) > 0 && string(cursor) != "null" && string(cursor) != `""` {
		label += "（还有后续分页）"
	}
	lines := []string{label}
	for i, resource := range resources {
		if i >= 32 {
			diagnostic(native.ContentLimit)
			break
		}
		name := resource.Title
		if strings.TrimSpace(name) == "" {
			name = resource.Name
		}
		clean := redactV5CommandText(name)
		if strings.ContainsRune(clean, 0) {
			diagnostic(native.ResultUnavailable)
			continue
		}
		if clean != name {
			diagnostic(native.ContentRedacted)
		}
		bounded, truncated := truncateV5UTF8(clean, 512)
		if truncated {
			diagnostic(native.ContentTruncated)
		}
		if strings.TrimSpace(bounded) != "" {
			lines = append(lines, bounded)
		}
	}
	out.Mcp.ResultKind = native.Text
	out.Mcp.Texts = []native.NativeMcpText{{Index: 0, Text: strings.Join(lines, "\n")}}
	out.ResultSummary = nativeString(label)
	return true
}
