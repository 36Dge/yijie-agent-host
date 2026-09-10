package session

// This is a bounded display projection of complete native Items. It owns no
// call lifecycle, retry, content accumulator, or persistent MCP result store.
import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	legacy "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversation"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv2"
)

var nativeMcpIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var nativeMcpASIN = regexp.MustCompile(`^[A-Z0-9]{10}$`)

func projectNativeMcp(wire nativeWireItem, out *native.NativeItem) {
	display := &native.NativeMcpDisplay{ResultKind: native.Absent, Texts: []native.NativeMcpText{}, Diagnostics: []native.NativeMcpDisplayDiagnostics{}}
	out.Mcp = display
	diagnostic := func(code native.NativeMcpDisplayDiagnostics) {
		out.Availability = native.NativeItemAvailabilityPartial
		for _, existing := range display.Diagnostics {
			if existing == code {
				return
			}
		}
		display.Diagnostics = append(display.Diagnostics, code)
	}
	if len(wire.Server) <= 64 && nativeMcpIdentifier.MatchString(wire.Server) && !v5SensitiveCommandLine(wire.Server) {
		display.Server = &wire.Server
	} else {
		diagnostic(native.IdentityUnavailable)
	}
	if len(wire.Tool) <= 128 && nativeMcpIdentifier.MatchString(wire.Tool) && !v5SensitiveCommandLine(wire.Tool) {
		display.Tool = &wire.Tool
	} else {
		diagnostic(native.IdentityUnavailable)
	}
	out.ToolLabel = nativeString("工具调用")
	if display.Server != nil && display.Tool != nil {
		out.ToolLabel = nativeString(wire.Server + " / " + wire.Tool)
	}
	if wire.DurationMS != nil && *wire.DurationMS >= 0 {
		out.DurationMs = wire.DurationMS
	}
	var args map[string]json.RawMessage
	var asin, site string
	if wire.Server == "sorftime" && wire.Tool == "product_detail" &&
		json.Unmarshal(wire.Arguments, &args) == nil && len(args) == 2 &&
		json.Unmarshal(args["asin"], &asin) == nil && nativeMcpASIN.MatchString(asin) &&
		json.Unmarshal(args["amz_site"], &site) == nil && site == "US" {
		out.ArgumentsSummary = nativeString("ASIN: " + asin + " · 站点: US")
	} else if len(wire.Arguments) > 0 {
		out.ArgumentsSummary = nativeString("参数不可展示")
		diagnostic(native.ArgumentsUnavailable)
	}
	if len(wire.Result) == 0 {
		return
	}
	if string(wire.Result) == "null" {
		display.ResultKind = native.Null
		return
	}
	out.ResultSummary = nativeString("结果已接收") // Metadata retains its v1 meaning.
	if wire.Server != "sorftime" || wire.Tool != "product_detail" {
		display.ResultKind = native.Omitted
		diagnostic(native.ResultUnavailable)
		return
	}
	var result struct {
		Content    []json.RawMessage `json:"content"`
		Structured json.RawMessage   `json:"structuredContent"`
	}
	if json.Unmarshal(wire.Result, &result) != nil || result.Content == nil {
		display.ResultKind = native.Omitted
		diagnostic(native.ResultUnavailable)
		return
	}
	display.ResultKind = native.Empty
	remaining := nativeTextLimit
	unsupported, omitted := false, false
	for index, raw := range result.Content {
		if index >= 32 {
			omitted = true
			diagnostic(native.ContentLimit)
			break
		}
		var block struct {
			Type string  `json:"type"`
			Text *string `json:"text"`
		}
		if json.Unmarshal(raw, &block) != nil || block.Type != "text" || block.Text == nil {
			diagnostic(native.UnsupportedContent)
			unsupported = true
			continue
		}
		// Whole-block redaction MUST precede truncation. Preserve the original
		// content index even when preceding non-text blocks are omitted.
		clean := redactV5CommandText(*block.Text)
		if clean != *block.Text {
			diagnostic(native.ContentRedacted)
		}
		if strings.ContainsRune(clean, 0) || (*block.Text != "" && clean == "") {
			omitted = true
			diagnostic(native.ResultUnavailable)
			continue
		}
		bounded, truncated := truncateV5UTF8(clean, remaining)
		if truncated {
			diagnostic(native.ContentTruncated)
		}
		if remaining == 0 && clean != "" {
			omitted = true
			diagnostic(native.ContentLimit)
			continue
		}
		display.Texts = append(display.Texts, native.NativeMcpText{Index: index, Text: bounded})
		remaining -= len(bounded)
	}
	if len(result.Structured) > 0 && string(result.Structured) != "null" {
		unsupported = true
		diagnostic(native.UnsupportedContent)
	}
	if len(display.Texts) > 0 {
		display.ResultKind = native.Text
	} else if omitted {
		display.ResultKind = native.Omitted
	} else if unsupported {
		display.ResultKind = native.Unsupported
	}
	// JSON escaping can exceed the existing 1 MiB event limit even when the
	// text satisfies its UTF-8 limit. Bound this complete object, preserving
	// native identity/status and indexes, before it reaches the shared hub.
	for {
		encoded, err := json.Marshal(out)
		if err == nil && len(encoded) <= maxRetainedEventDataBytes-(32<<10) {
			break
		}
		diagnostic(native.ContentLimit)
		index := -1
		for i := len(display.Texts) - 1; i >= 0; i-- {
			if display.Texts[i].Text != "" {
				index = i
				break
			}
		}
		if index < 0 {
			break
		} // The bounded metadata-only shape fits the reserve.
		value, _ := truncateV5UTF8(display.Texts[index].Text, len(display.Texts[index].Text)/2)
		if value == "" {
			display.Texts = append(display.Texts[:index], display.Texts[index+1:]...)
			if len(display.Texts) == 0 {
				display.ResultKind = native.Omitted
			}
		} else {
			display.Texts[index].Text = value
		}
	}
}

// Keep the retired wire family's closed shape. This adapter strips only the
// v2 display extension; it never mutates the shared native event buffer.
func LegacyNativeEvent(event Event) Event {
	event.SchemaVersion = 7
	if event.Payload.Native == nil {
		return event
	}
	encoded, _ := json.Marshal(event.Payload.Native)
	var copy native.NativeNotification
	_ = json.Unmarshal(encoded, &copy)
	strip := func(item *native.NativeItem) {
		if item.Mcp != nil {
			item.Mcp = nil
			item.ToolLabel = nativeString("工具调用")
			if item.ArgumentsSummary != nil {
				item.ArgumentsSummary = nativeString("参数已隐藏")
			}
			item.DurationMs = nil
			item.Availability = native.NativeItemAvailabilityPartial
		}
	}
	if copy.Item != nil {
		strip(copy.Item)
	}
	if copy.Turn != nil {
		for i := range copy.Turn.Items {
			strip(&copy.Turn.Items[i])
		}
	}
	event.Payload.Native = &copy
	return event
}

func (s *Service) ReadNativeThread(ctx context.Context, sessionID string) (legacy.NativeThreadSnapshot, error) {
	value, err := s.ReadNativeThreadV2(ctx, sessionID)
	if err != nil {
		return legacy.NativeThreadSnapshot{}, err
	}
	for i := range value.Turns {
		notification := native.NativeNotification{Turn: &value.Turns[i]}
		event := LegacyNativeEvent(Event{Payload: EventPayload{Native: &notification}})
		value.Turns[i] = *event.Payload.Native.Turn
	}
	value.SchemaVersion = 1
	data, err := json.Marshal(value)
	var result legacy.NativeThreadSnapshot
	if err == nil {
		err = json.Unmarshal(data, &result)
	}
	return result, err
}
