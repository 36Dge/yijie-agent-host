package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	web "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
)

// Public resource selectors on newly connected tools often follow one of these
// two naming conventions. Do not select identities for people or credentials.
var resourceSelector = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,48}(_id|Id|ID|_code|Code|_number|Number)$`)
var privateSelector = regexp.MustCompile(`(?i)auth|credential|secret|token|password|session|account|user|person|contact|member|employee|customer|buyer|email|phone|mobile|card|passport|identity|verification|payment|pinCode|pin_code`)

func marketplaceToolName(service, method string) string {
	digest := sha256.Sum256([]byte(service + "\n" + method))
	return "m_" + hex.EncodeToString(digest[:30])
}

func publicTarget(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > 64 || v5SensitiveCommandLine(value) {
		return ""
	}
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && !strings.ContainsRune(" -_.'’", r) {
			return ""
		}
	}
	return value
}

func publicMethod(name string) string {
	if !nativeMcpIdentifier.MatchString(name) || len(name) > 100 || isMarketplaceAlias(name) || v5SensitiveCommandLine(name) {
		return ""
	}
	return strings.Join(strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' || r == '.' }), " ")
}

func isMarketplaceAlias(name string) bool {
	return len(name) == 62 && strings.HasPrefix(name, "m_") && safeMarketToolName(name)
}

// This metadata is display-only. The original alias binds a newly discovered
// method to its service even on a cold read without an installed connector.
func marketMethodLabel(wire nativeWireItem) string {
	var result struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if json.Unmarshal(wire.Result, &result) != nil {
		return ""
	}
	var receipt broker.GatewayCallReceipt
	raw := result.Meta[broker.GatewayResultKey]
	if len(raw) > broker.GatewayResultMaxMetadataBytes || json.Unmarshal(raw, &receipt) != nil ||
		receipt.Validate() != nil || string(receipt.ToolName) != wire.Tool {
		return ""
	}
	service := string(receipt.ServiceId)
	var method string
	if json.Unmarshal(result.Meta[broker.GatewayResultMethodKey], &method) == nil && marketplaceToolName(service, method) == wire.Tool {
		if label := publicMethod(method); label != "" {
			return label
		}
	}
	return "使用 " + service
}

// Only the root and known request/filter wrappers participate. Key priority is
// deterministic and is independent of the provider's JSON member order.
type titleArguments []map[string]json.RawMessage

func (args titleArguments) value(key string) json.RawMessage {
	for _, fields := range args {
		if value, ok := fields[key]; ok {
			return value
		}
	}
	return nil
}

func (args titleArguments) first(keys ...string) string {
	for _, key := range keys {
		raw := args.value(key)
		if len(raw) == 0 {
			continue
		}
		switch raw[0] {
		case '"':
			var text string
			if json.Unmarshal(raw, &text) == nil {
				if target := publicTarget(text); target != "" {
					return target
				}
			}
		case '[':
			var values []string
			if json.Unmarshal(raw, &values) == nil && len(values) > 0 {
				if target := publicTarget(values[0]); target != "" {
					if len(values) > 1 {
						return target + " 等 " + strconv.Itoa(len(values)) + " 项"
					}
					return target
				}
			}
		default:
			var n int64
			if raw[0] >= '0' && raw[0] <= '9' && json.Unmarshal(raw, &n) == nil {
				return strconv.FormatInt(n, 10)
			}
		}
	}
	return ""
}

// A title may identify a business object, but never reproduces a free-form
// payload, prompt, message body, transport cursor or credential.
func (args titleArguments) object(allowName bool) string {
	if target := args.first(
		"asin", "asins", "sku", "skus", "product_id", "productId", "product_ids", "item_id", "itemId", "node_id", "nodeId",
		"ts_code", "stock_code", "symbol", "symbols", "ticker", "issue_number", "issueNumber",
		"repo", "repository", "table_name", "tableName", "product_name", "productName",
		"category_name", "categoryName", "keyword", "keywords", "query", "queries", "search_query",
		"searchQuery", "search_term", "searchTerm", "q", "city", "location", "country", "region",
		"isbn", "book", "dataset", "model", "brand", "shop_name", "seller_name",
	); target != "" {
		return target
	}
	var keys []string
	for _, fields := range args {
		for key := range fields {
			if resourceSelector.MatchString(key) && !privateSelector.MatchString(key) {
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)
	keys = append(keys, "id")
	if allowName {
		keys = append(keys, "name", "display_name", "displayName")
	}
	if target := args.first(keys...); target != "" {
		return target
	}
	urlValue := args.value("url")
	if len(urlValue) == 0 && strings.EqualFold(args.first("type"), "url") {
		urlValue = args.value("content")
	}
	var address string
	if json.Unmarshal(urlValue, &address) == nil {
		if host := displayableSite(address); host != "" {
			return host
		}
	}
	for _, key := range []string{"file_name", "filename", "fileName", "relativePath", "relative_path", "path"} {
		var name string
		if json.Unmarshal(args.value(key), &name) == nil && name != "" && !strings.HasPrefix(name, "/") && !strings.ContainsAny(name, "\\:~") && !strings.Contains(name, "..") {
			if target := publicTarget(path.Base(name)); target != "" {
				return target
			}
		}
	}
	return ""
}

func mcpQueryTarget(raw json.RawMessage, allowName bool) string {
	var root map[string]json.RawMessage
	if len(raw) > nativeTextLimit || json.Unmarshal(raw, &root) != nil || root == nil {
		return ""
	}
	args := titleArguments{root}
	for _, key := range []string{"request", "params", "input", "filter", "filters"} {
		var fields map[string]json.RawMessage
		if json.Unmarshal(root[key], &fields) == nil && fields != nil {
			args = append(args, fields)
		}
	}
	var parts []string
	if target := args.object(allowName); target != "" {
		parts = append(parts, target)
	}
	if date := args.first("trade_date", "tradeDate", "date"); date != "" {
		if _, err := time.Parse("20060102", strings.ReplaceAll(date, "-", "")); err == nil {
			parts = append(parts, date)
		}
	}
	if value := args.first("page", "page_number", "pageNumber", "page_num", "pageNo"); value != "" {
		if page, err := strconv.Atoi(value); err == nil && page > 0 {
			parts = append(parts, "第 "+strconv.Itoa(page)+" 页")
		}
	}
	return strings.Join(parts, " · ")
}

// The same projection runs for every mcpToolCall. It only supplies a title;
// it must never change the footer source, tool result, duration or lifecycle.
func mcpObjectTitle(wire nativeWireItem, out *native.NativeItem, existing *string) string {
	if out.Mcp == nil || out.Mcp.Server == nil || out.Mcp.Tool == nil {
		return ""
	}
	special := sorftimeCallTitle(wire)
	if strings.Contains(special, " · ") {
		return special
	}
	if wire.Server == "yijie_web" && (wire.Tool == "kimi_web_search_pro" || wire.Tool == "kimi_web_fetch") {
		// Use the same native web selectors as the specialized disclosure, including on
		// cold reads. Only the row label is projected here.
		if wire.Tool == "kimi_web_fetch" {
			if host := fetchSite(wire.Arguments); host != "" {
				return boundedMCPTitle("读取指定网页", host)
			}
		} else {
			var args web.ProArguments
			if len(wire.Arguments) <= nativeTextLimit && web.Validate("ProArguments", wire.Arguments) == nil &&
				json.Unmarshal(wire.Arguments, &args) == nil && redactV5CommandText(args.Query) == args.Query {
				if q := strings.TrimSpace(args.Query); q != "" {
					return boundedMCPTitle("网页深度搜索", q)
				}
			}
		}
		return ""
	}
	action := special
	if action == "" && existing != nil {
		action = *existing
	} else if action == "" && out.ToolLabel != nil && *out.ToolLabel != wire.Tool && *out.ToolLabel != "工具调用" && *out.ToolLabel != "连接器工具" && !strings.Contains(*out.ToolLabel, " / ") {
		action = *out.ToolLabel
	}
	if action == "" && wire.Server == "yijie_market" && isMarketplaceAlias(wire.Tool) {
		action = marketMethodLabel(wire)
	}
	target := mcpQueryTarget(wire.Arguments, !privateSelector.MatchString(wire.Server+" "+wire.Tool+" "+action))
	if target == "" {
		if special != "" || (existing == nil && wire.Server == "yijie_market") {
			return action
		}
		return ""
	}
	if action == "" {
		switch wire.Server + "/" + wire.Tool {
		case "yijie_web/kimi_web_search":
			action = "采集网页资料"
		case "yijie_market/kimi_web_search":
			action = "搜索网页（Kimi Basic）"
		case "yijie_market/kimi_web_search_pro":
			action = "搜索网页（Kimi Pro）"
		case "yijie_market/kimi_web_fetch":
			action = "读取网页（Kimi）"
		default:
			action = publicMethod(wire.Tool)
		}
	}
	if action == "" {
		action = "使用连接器"
	}
	return boundedMCPTitle(action, target)
}

func boundedMCPTitle(action, target string) string {
	if runes := []rune(target); len(runes) > 96 {
		target = string(runes[:95]) + "…"
	}
	const separator = " · "
	maxAction := 160 - utf8.RuneCountInString(separator+target)
	runes := []rune(action)
	if len(runes) > maxAction {
		action = string(runes[:maxAction])
	}
	return action + separator + target
}
