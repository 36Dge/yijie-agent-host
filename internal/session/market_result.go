package session

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
)

// One display projection for native notifications and market history. It owns
// no call lifecycle and never interprets business fields as execution status.
type marketResultProjection struct {
	text, structured             *string
	source                       *wire.ResultTextSource
	truncated, redacted, isError bool
	unsupported                  bool
	meta                         json.RawMessage
	content                      []native.NativeMcpText
}

func projectMarketResult(raw json.RawMessage) marketResultProjection {
	out := marketResultProjection{}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return out
	}
	var result struct {
		Content    []json.RawMessage `json:"content"`
		Structured json.RawMessage   `json:"structuredContent"`
		IsError    bool              `json:"isError"`
		Meta       json.RawMessage   `json:"_meta"`
	}
	if len(raw) > 1<<20 || json.Unmarshal(raw, &result) != nil {
		out.unsupported, out.truncated = true, true
		return out
	}
	out.meta, out.isError = result.Meta, result.IsError
	remaining := wire.MaxResultTextBytes
	bound := func(text string) string {
		bounded, truncated := truncateV5UTF8(text, remaining)
		out.truncated = out.truncated || truncated
		remaining -= len(bounded)
		return bounded
	}
	var texts []string
	var originalText *string
	for index, rawBlock := range result.Content {
		if index >= 32 {
			out.truncated = true
			break
		}
		var block struct {
			Type string  `json:"type"`
			Text *string `json:"text"`
		}
		if json.Unmarshal(rawBlock, &block) != nil || block.Type != "text" || block.Text == nil {
			out.truncated = true
			continue
		}
		if index == 0 && len(result.Content) == 1 {
			originalText = block.Text
		}
		clean := redactV5CommandText(*block.Text)
		out.redacted = out.redacted || clean != *block.Text
		if strings.ContainsRune(clean, 0) {
			out.truncated = true
			continue
		}
		if len(texts) > 0 {
			if remaining == 0 {
				out.truncated = true
				break
			}
			remaining--
		}
		bounded := bound(clean)
		texts = append(texts, bounded)
		out.content = append(out.content, native.NativeMcpText{Index: index, Text: bounded})
	}
	if len(texts) > 0 {
		text := strings.Join(texts, "\n")
		out.text, out.source = &text, nativeString(wire.ResultTextSourceContent)
	}
	if len(result.Structured) == 0 || bytes.Equal(bytes.TrimSpace(result.Structured), []byte("null")) {
		return out
	}
	if originalText != nil && !out.truncated && !out.redacted && sameMarketJSON(*originalText, result.Structured) {
		return out
	}
	structured, redacted, ok := safeMarketJSON(result.Structured)
	if !ok {
		out.truncated = true
		return out
	}
	out.redacted = out.redacted || redacted
	if remaining == 0 && structured != "" {
		out.truncated = true
		return out
	}
	structured = bound(structured)
	if out.text == nil {
		out.text, out.source = &structured, nativeString(wire.ResultTextSourceStructured)
	} else {
		out.structured = &structured
	}
	return out
}

func safeMarketJSON(raw []byte) (string, bool, bool) {
	if len(raw) > 1<<20 {
		return "", false, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber() // Display identifiers without rounding large JSON numbers.
	var value any
	if decoder.Decode(&value) != nil {
		return "", false, false
	}
	redacted, nodes := false, 0
	var clean func(any, int) (any, bool)
	clean = func(value any, depth int) (any, bool) {
		nodes++
		if depth > 32 || nodes > 32768 {
			return nil, false
		}
		switch current := value.(type) {
		case map[string]any:
			keys := make([]string, 0, len(current))
			for key := range current {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			cleaned := make(map[string]any, len(current))
			for index, key := range keys {
				child := current[key]
				if normalizeV5CommandText(key) != key || strings.ContainsAny(key, "\n\r\t") {
					cleaned["redacted_field_"+strconv.Itoa(index)] = "[redacted]"
					redacted = true
					continue
				}
				normalized := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(key))
				switch normalized {
				case "token", "accesstoken", "refreshtoken", "password", "passwd", "secret", "apikey", "accesskey", "authorization", "auth", "cookie", "clientsecret", "credential", "privatekey":
					cleaned[key], redacted = "[redacted]", true
				default:
					if v5SensitiveCommandLine(key) {
						cleaned["redacted_field_"+strconv.Itoa(index)] = "[redacted]"
						redacted = true
						continue
					}
					v, ok := clean(child, depth+1)
					if !ok {
						return nil, false
					}
					cleaned[key] = v
				}
			}
			return cleaned, true
		case []any:
			for i, child := range current {
				v, ok := clean(child, depth+1)
				if !ok {
					return nil, false
				}
				current[i] = v
			}
		case string:
			text := redactV5CommandText(current)
			if strings.ContainsRune(text, 0) {
				text = "[omitted]"
			}
			redacted = redacted || text != current
			return text, true
		}
		return value, true
	}
	value, ok := clean(value, 0)
	if !ok {
		return "", redacted, false
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	return string(encoded), redacted, err == nil
}

func sameMarketJSON(text string, structured json.RawMessage) bool {
	if !json.Valid([]byte(text)) || !json.Valid(structured) {
		return false
	}
	decode := func(raw []byte) any {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		_ = decoder.Decode(&value) // json.Valid above requires exactly one valid value.
		return value
	}
	// Maps ignore member order; json.Number preserves identifier precision and
	// numeric spelling, while slices retain their original element order.
	return reflect.DeepEqual(decode([]byte(text)), decode(structured))
}

func safeMarketToolName(name string) bool {
	if len(name) > 128 || !nativeMcpIdentifier.MatchString(name) {
		return false
	}
	// generic::Definition publishes this SHA-256-derived identifier, not the
	// supplier's raw name. Exempt only that closed owned-tool namespace.
	if len(name) == 62 && strings.HasPrefix(name, "m_") {
		for _, c := range name[2:] {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
		return true
	}
	return !v5SensitiveCommandLine(name)
}

func (s *Service) projectMarketTool(record Record, turnID string, item nativeWireItem) *wire.ToolObservation {
	if item.Type != "mcpToolCall" || item.Server != "yijie_market" || wire.NativeId(item.ID).Validate() != nil ||
		!safeMarketToolName(item.Tool) {
		return nil
	}
	state := "in_progress"
	if item.Status != nil {
		switch *item.Status {
		case "completed":
			state = "completed"
		case "failed", "declined":
			state = "failed"
		}
	}
	result := projectMarketResult(item.Result)
	view := wire.ToolObservation{NativeItemId: wire.NativeId(item.ID), NativeThreadId: wire.NativeId(record.CodexThreadID), NativeTurnId: wire.NativeId(turnID), ServerName: item.Server, ToolName: item.Tool, State: state, ResultText: result.text, ResultTextSource: result.source, StructuredResultText: result.structured, Truncated: result.truncated}
	if result.isError {
		view.State = "failed"
	}
	if result.unsupported {
		view.ErrorCode = nativeString("unsupported_result")
	}
	view.Correlation = s.marketResultCorrelation(record, item, turnID, result.meta)
	if view.Correlation != nil {
		view.CallRef = &view.Correlation.CallRef
	}
	if view.Validate() != nil {
		return nil
	}
	return &view
}
