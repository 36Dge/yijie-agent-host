package session

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	web "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
)

var webResultTimingPrefix = regexp.MustCompile(`^Wall time: [0-9.]+ seconds\nOutput:\n`)

// Keep the article destination when it passes the ordinary search-source
// checks. A result can still identify its site if only the origin is displayable.
func deepSearchSourceURL(raw string) string {
	if safeNativeSourceURL(raw) {
		return raw
	}
	page, err := url.Parse(raw)
	if err != nil || page.User != nil {
		return ""
	}
	origin := (&url.URL{Scheme: page.Scheme, Host: page.Host}).String()
	if !safeNativeSourceURL(origin) {
		return ""
	}
	return origin
}

func nativeWebResultPayload(wire nativeWireItem, out *native.NativeItem, diagnostic func(native.NativeMcpDisplayDiagnostics)) ([]byte, bool) {
	if len(wire.Result) == 0 {
		return nil, false
	}
	if bytes.Equal(bytes.TrimSpace(wire.Result), []byte("null")) {
		out.Mcp.ResultKind = native.Null
		return nil, false
	}

	// Only the original text block is a source. Never forward result metadata,
	// raw structuredContent, retrieval chunks, or an unrecognized error body.
	out.Mcp.ResultKind = native.Omitted
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if len(wire.Result) > maxRetainedEventDataBytes || json.Unmarshal(wire.Result, &result) != nil {
		diagnostic(native.ResultUnavailable)
		return nil, false
	}
	if result.IsError && (out.Status == nil || *out.Status == native.NativeItemStatusCompleted) {
		out.Status = nativeString(native.NativeItemStatusFailed)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "text" {
		diagnostic(native.ResultUnavailable)
		return nil, false
	}
	payload := []byte(webResultTimingPrefix.ReplaceAllString(result.Content[0].Text, ""))
	if len(payload) > web.MaxResultBytes {
		diagnostic(native.ContentLimit)
		return nil, false
	}
	return payload, result.IsError
}

func projectDeepWebSearch(wire nativeWireItem, out *native.NativeItem, diagnostic func(native.NativeMcpDisplayDiagnostics)) {
	if len(wire.Arguments) > 0 {
		var args web.ProArguments
		if len(wire.Arguments) <= nativeTextLimit && web.Validate("ProArguments", wire.Arguments) == nil &&
			json.Unmarshal(wire.Arguments, &args) == nil && redactV5CommandText(args.Query) == args.Query {
			out.ArgumentsSummary = nativeString(strings.TrimSpace(args.Query))
		} else {
			diagnostic(native.ArgumentsUnavailable)
		}
	}
	payload, isError := nativeWebResultPayload(wire, out, diagnostic)
	if payload == nil {
		return
	}
	var display []byte
	var success web.ProSuccess
	switch {
	case !isError && web.Validate("ProSuccess", payload) == nil && json.Unmarshal(payload, &success) == nil:
		rows := web.SearchSuccess{Results: []web.SearchResult{}, Truncated: success.Truncated || success.DroppedResults > 0}
		cleanText := func(value string, limit int) string {
			clean := redactV5CommandText(value)
			if clean != value {
				diagnostic(native.ContentRedacted)
				rows.Truncated = true
			}
			bounded, complete := safeNativeText(clean, limit)
			if !complete {
				diagnostic(native.ContentTruncated)
				rows.Truncated = true
			}
			return bounded
		}
		for _, row := range success.Results {
			sourceURL := deepSearchSourceURL(row.Url)
			if sourceURL == "" {
				diagnostic(native.ContentRedacted)
				rows.Truncated = true
				continue
			}
			rows.Results = append(rows.Results, web.SearchResult{
				Title: cleanText(row.Title, 512), Url: sourceURL, Snippet: cleanText(row.Snippet, 1024),
			})
		}
		display, _ = json.Marshal(rows)
	case web.Validate("AppErrorOutput", payload) == nil || web.Validate("ErrorOutput", payload) == nil:
		// Both contracts pair the public error code with a fixed display message.
		var publicError web.AppErrorOutput
		if json.Unmarshal(payload, &publicError) != nil {
			diagnostic(native.ResultUnavailable)
			return
		}
		display, _ = json.Marshal(publicError)
		if out.Status != nil && *out.Status == native.NativeItemStatusCompleted {
			out.Status = nativeString(native.NativeItemStatusFailed)
		}
	default:
		diagnostic(native.ResultUnavailable)
		return
	}
	if len(display) > nativeTextLimit {
		diagnostic(native.ContentLimit)
		return
	}
	out.Mcp.ResultKind = native.Text
	out.Mcp.Texts = []native.NativeMcpText{{Index: 0, Text: string(display)}}
	out.ResultSummary = nativeString("结果已接收")
}

// Older event projections predate the display exception for this tool.
func omitLegacyWebDetails(item *native.NativeItem) {
	mcp := item.Mcp
	if mcp == nil || mcp.Server == nil || *mcp.Server != web.ServerName || mcp.Tool == nil || (*mcp.Tool != string(web.KimiWebSearchPro) && *mcp.Tool != string(web.KimiWebFetch)) {
		return
	}
	stripSearchFact(item)
	mcp = item.Mcp
	if item.ArgumentsSummary != nil {
		item.ArgumentsSummary = nativeString("参数不可展示")
		item.Availability = native.NativeItemAvailabilityPartial
	}
	if mcp.ResultKind == native.Text {
		mcp.ResultKind = native.Omitted
		mcp.Texts = []native.NativeMcpText{}
		item.ResultSummary = nativeString("结果已接收")
		item.Availability = native.NativeItemAvailabilityPartial
	}
}
