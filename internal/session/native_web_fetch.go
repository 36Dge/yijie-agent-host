package session

import (
	"encoding/json"
	"net/netip"
	"net/url"
	"strings"
	"unicode"

	web "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
)

// The site is an inert label. Never include a URL path, query, port or fragment.
func fetchSite(raw json.RawMessage) string {
	var args web.FetchArguments
	if len(raw) > nativeTextLimit || web.Validate("FetchArguments", raw) != nil || json.Unmarshal(raw, &args) != nil {
		return ""
	}
	return displayableSite(args.Url)
}

func displayableSite(address string) string {
	page, err := url.Parse(address)
	if err != nil || page.User != nil || !safeNativeSourceURL((&url.URL{Scheme: page.Scheme, Host: page.Host}).String()) {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(page.Hostname(), "."))
	if !strings.Contains(host, ".") || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return ""
	}
	if ip, err := netip.ParseAddr(host); err == nil && (!ip.IsGlobalUnicast() || ip.IsPrivate()) {
		return ""
	}
	return host
}

func projectWebFetch(wire nativeWireItem, out *native.NativeItem, diagnostic func(native.NativeMcpDisplayDiagnostics)) {
	if len(wire.Arguments) > 0 {
		if site := fetchSite(wire.Arguments); site != "" {
			out.ArgumentsSummary = &site
		} else {
			diagnostic(native.ArgumentsUnavailable)
		}
	}
	payload, isError := nativeWebResultPayload(wire, out, diagnostic)
	if payload == nil {
		return
	}
	var page web.FetchSuccess
	var failure web.AppErrorOutput
	var outcome string
	switch {
	case !isError && web.Validate("FetchSuccess", payload) == nil && json.Unmarshal(payload, &page) == nil:
		outcome = "已读取网页正文。"
		if page.Truncated {
			outcome = "已读取部分正文，网页内容较长。"
		}
		title := strings.TrimSpace(page.Title)
		if strings.IndexFunc(title, unicode.IsControl) >= 0 || redactV5CommandText(title) != title {
			diagnostic(native.ContentRedacted)
		} else if title != "" {
			bounded, complete := safeNativeText(title, 512)
			out.ResultSummary = &bounded
			if !complete {
				diagnostic(native.ContentTruncated)
			}
		}
	case web.Validate("AppErrorOutput", payload) == nil && json.Unmarshal(payload, &failure) == nil:
		outcome = failure.Error.Message
		if out.Status == nil || *out.Status == native.NativeItemStatusCompleted {
			out.Status = nativeString(native.NativeItemStatusFailed)
		}
	default:
		diagnostic(native.ResultUnavailable)
		return
	}
	out.Mcp.ResultKind = native.Text
	out.Mcp.Texts = []native.NativeMcpText{{Index: 0, Text: outcome}}
}
