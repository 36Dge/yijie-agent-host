package session

import "strings"

// Only fixed diagnostic categories cross the existing native notification
// boundary. Raw command text, paths and upstream warning text remain private.
func nativeWarningCode(message string) string {
	if len(message) > 4096 {
		return "runtime_notice"
	}
	switch {
	case strings.HasPrefix(message, "YIJIE_ANSWER_CONTINUATION："):
		return "answer_completion_continuing"
	case strings.HasPrefix(message, "YIJIE_ANSWER_FINALIZING："):
		return "answer_completion_tools_blocked"
	case strings.HasPrefix(message, "YIJIE_ANSWER_INCOMPLETE："):
		return "answer_completion_failed"
	case strings.HasPrefix(message, "YIJIE_WEB_RETRIEVAL_ONLY："):
		return "web_retrieval_local_fetch_blocked"
	case strings.HasPrefix(message, "YIJIE_OPERATION_PENDING："):
		return "operation_duplicate_pending"
	case strings.HasPrefix(message, "YIJIE_GUARD_CAPACITY："):
		return "operation_guard_capacity"
	case strings.HasPrefix(message, "YIJIE_UNCHANGED_FAILURE："):
		switch {
		case strings.Contains(message, "发生域名解析错误"):
			return "operation_retry_dns_unchanged"
		case strings.Contains(message, "发生网络连接错误"):
			return "operation_retry_network_unchanged"
		case strings.Contains(message, "返回权限或认证错误"):
			return "operation_retry_access_unchanged"
		default:
			return "operation_retry_unchanged"
		}
	default:
		return "runtime_notice"
	}
}
