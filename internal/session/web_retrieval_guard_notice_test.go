package session

import "testing"

func TestExecutionGuardWarningUsesExistingSafeDiagnosticChannel(t *testing.T) {
	for _, item := range []struct{ message, code string }{
		{"YIJIE_WEB_RETRIEVAL_ONLY：已拦截。", "web_retrieval_local_fetch_blocked"},
		{"YIJIE_OPERATION_PENDING：相同操作尚未收尾。", "operation_duplicate_pending"},
		{"YIJIE_UNCHANGED_FAILURE：上次相同操作发生域名解析错误；条件未变。", "operation_retry_dns_unchanged"},
		{"YIJIE_UNCHANGED_FAILURE：上次相同操作发生网络连接错误；条件未变。", "operation_retry_network_unchanged"},
		{"YIJIE_UNCHANGED_FAILURE：上次相同操作返回权限或认证错误；条件未变。", "operation_retry_access_unchanged"},
		{"YIJIE_UNCHANGED_FAILURE：上次相同操作返回失败结果；条件未变。", "operation_retry_unchanged"},
		{"YIJIE_GUARD_CAPACITY：本轮达到容量限制。", "operation_guard_capacity"},
		{"ordinary future warning https://example.com/", "runtime_notice"},
	} {
		s := NewService(&fakeRuntime{}, openBoundFEAT134Store(t), NewEventHub(32, 8), nil)
		s.HandleNotification(RuntimeNotificationWarning, rawJSON(t, map[string]any{"threadId": testThreadID, "message": item.message}))
		_, events, _, cancel, err := s.SubscribeNativeEvents(testSessionID, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		if len(events) != 1 || events[0].Payload.Native.Code == nil || *events[0].Payload.Native.Code != item.code {
			t.Fatalf("safe guard diagnostic lost: %s", item.code)
		}
		if events[0].Terminal {
			t.Fatal("warning changed the turn terminal state")
		}
	}
}
