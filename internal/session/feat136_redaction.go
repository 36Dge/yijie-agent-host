package session

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	v5URLPattern               = regexp.MustCompile(`(?i)\b(?:https?|wss?)://`)
	v5SensitiveNamePattern     = regexp.MustCompile(`(?i)\b(?:access[_-]?key|api[_-]?key|auth(?:orization)?|credential|password|passwd|private[_-]?key|secret|token)\b`)
	v5GenericAssignmentPattern = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_.-]{1,64}\s*=`)
	v5AbsolutePathPattern      = regexp.MustCompile(`(?:^|[[:space:][:punct:]])(?:[A-Za-z]:[\\/]|~[\\/]|/|\\\\)`)
	v5JWTLikePattern           = regexp.MustCompile(`\b[A-Za-z0-9_-]{8,}(?:\.[A-Za-z0-9_-]{8,}){2,}\b`)
	v5OpaqueTokenPattern       = regexp.MustCompile(`\b[A-Za-z0-9_+/=-]{32,}\b`)
	v5AWSAccessKeyPattern      = regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)
	v5PEMBlockPattern          = regexp.MustCompile(`-----BEGIN [^-\r\n]+-----|-----END [^-\r\n]+-----`)
)

const v5RedactedMarker = "[内容已脱敏]"

// redactV5CommandText deliberately favors confidentiality over fidelity. Command
// summaries never use this function (they are allowlisted category labels); this
// sanitizer is only for bounded output projection.
func redactV5CommandText(value string) string {
	value = normalizeV5CommandText(value)
	var sanitized strings.Builder
	sanitized.Grow(len(value))
	for _, line := range strings.SplitAfter(value, "\n") {
		content := strings.TrimSuffix(line, "\n")
		newline := strings.HasSuffix(line, "\n")
		if v5SensitiveCommandLine(content) {
			sanitized.WriteString(v5RedactedMarker)
		} else {
			sanitized.WriteString(content)
		}
		if newline {
			sanitized.WriteByte('\n')
		}
	}
	result := sanitized.String()
	if strings.TrimSpace(value) != "" && strings.TrimSpace(result) == "" {
		return v5RedactedMarker
	}
	return result
}

// redactV5CommandDelta treats a non-newline-terminated fragment as unsafe at
// the transport boundary. This prevents two individually benign-looking
// Runtime deltas (for example "Bear" + "er token") from reassembling a secret
// in the Desktop. We retain only complete lines that can be inspected in one
// projection call; partial/continued lines receive the fixed marker. The
// boolean records only line-boundary state and never stores raw output.
func redactV5CommandDelta(value string, continued bool) (string, bool) {
	normalized := normalizeV5CommandText(value)
	if normalized == "" {
		return "", continued
	}
	var projected strings.Builder
	projected.Grow(len(normalized))
	remaining := normalized
	for {
		newline := strings.IndexByte(remaining, '\n')
		if newline < 0 {
			if remaining != "" {
				projected.WriteString(v5RedactedMarker)
				continued = true
			}
			break
		}
		line := remaining[:newline]
		if continued || v5SensitiveCommandLine(line) {
			projected.WriteString(v5RedactedMarker)
		} else {
			projected.WriteString(line)
		}
		projected.WriteByte('\n')
		continued = false
		remaining = remaining[newline+1:]
		if remaining == "" {
			break
		}
	}
	return projected.String(), continued
}

func normalizeV5CommandText(value string) string {
	var normalized strings.Builder
	normalized.Grow(len(value))
	for _, current := range value {
		if current == '\n' || current == '\t' {
			normalized.WriteRune(current)
			continue
		}
		if unicode.IsSpace(current) {
			normalized.WriteByte(' ')
			continue
		}
		if unicode.IsControl(current) || unicode.Is(unicode.Cf, current) || isV5BidiControl(current) {
			continue
		}
		normalized.WriteRune(current)
	}
	return normalized.String()
}

func v5SensitiveCommandLine(value string) bool {
	return bearerPattern.MatchString(value) || v5URLPattern.MatchString(value) ||
		v5SensitiveNamePattern.MatchString(value) || v5GenericAssignmentPattern.MatchString(value) ||
		v5AbsolutePathPattern.MatchString(value) || v5JWTLikePattern.MatchString(value) ||
		v5OpaqueTokenPattern.MatchString(value) || v5AWSAccessKeyPattern.MatchString(value) ||
		v5PEMBlockPattern.MatchString(value)
}

func isV5BidiControl(value rune) bool {
	return value == '\u061c' || value == '\u200e' || value == '\u200f' ||
		(value >= '\u202a' && value <= '\u202e') ||
		(value >= '\u2066' && value <= '\u2069')
}

func truncateV5UTF8(value string, limit int) (string, bool) {
	if limit < 0 {
		limit = 0
	}
	if len(value) <= limit {
		return value, false
	}
	end := limit
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end], true
}

func tailV5UTF8(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	start := len(value) - limit
	for start < len(value) && !utf8.ValidString(value[start:]) {
		start++
	}
	return value[start:]
}
