package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const contextReadThread = "10000000-0000-4000-8000-000000000001"
const contextReadTurn = "10000000-0000-4000-8000-000000000002"

func contextNativeLine(kind string, payload any) string {
	b, _ := json.Marshal(map[string]any{"timestamp": "2026-11-08T08:54:33.663Z", "type": kind, "payload": payload})
	return string(b) + "\n"
}
func contextNativeStart(model, turn string) string {
	return contextNativeLine("turn_context", map[string]any{"turn_id": turn, "model": model})
}
func contextNativeUsage(used, total int64, capacity any) string {
	counts := func(n int64) any {
		return map[string]any{"input_tokens": n, "cached_input_tokens": 0, "output_tokens": 0, "reasoning_output_tokens": 0, "total_tokens": n}
	}
	return contextNativeLine("event_msg", map[string]any{"type": "token_count", "info": map[string]any{"last_token_usage": counts(used), "total_token_usage": counts(total), "model_context_window": capacity}})
}
func contextNativeMeta() string {
	return contextNativeLine("session_meta", map[string]any{"id": contextReadThread, "cli_version": "0.144.6"})
}

func TestContextUsageRestoresNativeLastRatherThanCumulative(t *testing.T) {
	raw := contextNativeMeta() + contextNativeStart("kimi-k3.1-pro", contextReadTurn) + contextNativeUsage(19638, 76542, 996147)
	value, err := parseNativeContextUsage(context.Background(), strings.NewReader(raw), contextReadThread)
	if err != nil || value == nil || value.Usage.Last.TotalTokens != 19638 || *value.Usage.ModelContextWindow != 996147 || value.Model != "kimi-k3.1-pro" || value.TurnID != contextReadTurn {
		t.Fatal(value, err)
	}
	if value.ObservedAt.Format(time.RFC3339Nano) != "2026-11-08T08:54:33.663Z" {
		t.Fatal("read time replaced native observation time")
	}
}
func TestContextUsageRepeatedCountsKeepOriginalTurnAndModel(t *testing.T) {
	base := contextNativeMeta() + contextNativeStart("kimi-k3.1-pro", contextReadTurn) + contextNativeUsage(100, 100, 996147)
	base += contextNativeStart("MiniMax-M3", "10000000-0000-4000-8000-000000000003") + contextNativeUsage(100, 100, 996147)
	v, err := parseNativeContextUsage(context.Background(), strings.NewReader(base), contextReadThread)
	if err != nil || v == nil || v.Model != "kimi-k3.1-pro" || v.TurnID != contextReadTurn {
		t.Fatal("rate-limit replay acquired new identity", v, err)
	}
	v, err = parseNativeContextUsage(context.Background(), strings.NewReader(base+contextNativeUsage(120, 220, 950000)), contextReadThread)
	if err != nil || v == nil || v.Model != "MiniMax-M3" || *v.Usage.ModelContextWindow != 950000 {
		t.Fatal("fresh model measurement lost", v, err)
	}
}
func TestContextUsageCompactionInvalidatesUntilNativeRecount(t *testing.T) {
	base := contextNativeMeta() + contextNativeStart("kimi-k3.1-pro", contextReadTurn) + contextNativeUsage(100, 100, 996147)
	base += contextNativeLine("compacted", map[string]any{"message": "ordinary summary", "window_id": "10000000-0000-4000-8000-000000000004"})
	for _, raw := range []string{base, base + contextNativeUsage(100, 100, 996147)} {
		v, err := parseNativeContextUsage(context.Background(), strings.NewReader(raw), contextReadThread)
		if err != nil || v != nil {
			t.Fatal("pre-compaction count survived", v, err)
		}
	}
	v, err := parseNativeContextUsage(context.Background(), strings.NewReader(base+contextNativeUsage(20, 100, 996147)), contextReadThread)
	if err != nil || v == nil || v.Usage.Last.TotalTokens != 20 || v.WindowKey != "10000000-0000-4000-8000-000000000004" {
		t.Fatal(v, err)
	}
}
func TestContextUsageMissingAndUnknownCapacityAreNotZero(t *testing.T) {
	base := contextNativeMeta() + contextNativeStart("kimi-k3.1-pro", contextReadTurn)
	v, err := parseNativeContextUsage(context.Background(), strings.NewReader(base), contextReadThread)
	if err != nil || v != nil {
		t.Fatal(v, err)
	}
	v, err = parseNativeContextUsage(context.Background(), strings.NewReader(base+contextNativeUsage(0, 0, nil)), contextReadThread)
	if err != nil || v == nil || v.Usage.ModelContextWindow != nil || v.Usage.Last.TotalTokens != 0 {
		t.Fatal(v, err)
	}
}
func TestContextUsageNativeFilesReadWithoutPermissionChanges(t *testing.T) {
	home := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	home, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "sessions", "2026", "11", "08")
	if err = os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-ordinary-"+contextReadThread+".jsonl")
	raw := contextNativeMeta() + contextNativeStart("kimi-k3.1-pro", contextReadTurn) + contextNativeUsage(42, 100, 996147)
	if err = os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)
	v, err := readNativeContextUsageFile(context.Background(), home, path, contextReadThread)
	if err != nil || v == nil || v.Usage.Last.TotalTokens != 42 {
		t.Fatal(v, err)
	}
	after, _ := os.Stat(path)
	b, _ := os.ReadFile(path)
	if before.Mode() != after.Mode() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || string(b) != raw {
		t.Fatal("native history was modified")
	}
}
func TestContextUsageAuditExistingNativeFile(t *testing.T) {
	home, path, thread := os.Getenv("YIJIE_CONTEXT_USAGE_AUDIT_HOME"), os.Getenv("YIJIE_CONTEXT_USAGE_AUDIT_FILE"), os.Getenv("YIJIE_CONTEXT_USAGE_AUDIT_THREAD")
	if home == "" || path == "" || thread == "" {
		t.Skip("explicit read-only native capture required")
	}
	v, err := readNativeContextUsageFile(context.Background(), home, path, thread)
	if err != nil || v == nil {
		t.Fatal("native persisted statistics unavailable", err)
	}
	var capacity any
	if v.Usage.ModelContextWindow != nil {
		capacity = *v.Usage.ModelContextWindow
	}
	t.Logf("native persisted usage: model=%s last=%d total=%d capacity=%v turn=%s", v.Model, v.Usage.Last.TotalTokens, v.Usage.Total.TotalTokens, capacity, v.TurnID)
	if output := os.Getenv("YIJIE_CONTEXT_USAGE_AUDIT_OUTPUT"); output != "" {
		b, _ := json.MarshalIndent(v, "", "  ")
		if err = os.WriteFile(output, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestContextUsageNativeNormalReopenReadsWithoutResumeOrInference(t *testing.T) {
	if os.Getenv("YIJIE_CONTEXT_USAGE_NATIVE_TEST") != "true" {
		t.Skip("explicit pinned native loopback test required")
	}
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, _ map[string]any, n int64) map[string]any {
		if n == 2 {
			return feat164AuxTool("", "new_context", "ordinary-context-restore", map[string]any{})
		}
		return feat164AuxText("普通上下文恢复验证完成。")
	}, toolSearchConfig)
	thread, err := h.manager.StartThread(feat164AuxCtx(), h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "普通只读统计验证。")
	first, err := h.manager.ReadContextUsage(context.Background(), thread.ID)
	if err != nil || first == nil {
		t.Fatal(first, err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "使用已支持的原生新窗口工具一次。")
	before, err := h.manager.ReadContextUsage(context.Background(), thread.ID)
	if err != nil || before == nil || before.WindowKey == first.WindowKey {
		t.Fatal("native reset not reflected", before, err)
	}
	requests := h.calls.Load()
	h.restart(t, h.manager.config)
	state, err := h.manager.ReadThreadStatus(context.Background(), thread.ID)
	if err != nil || state != "notLoaded" {
		t.Fatal("expected unloaded native thread", state, err)
	}
	after, err := h.manager.ReadContextUsage(context.Background(), thread.ID)
	if err != nil || after == nil || !equalNativeContextUsage(after.Usage, before.Usage) || after.TurnID != before.TurnID || after.Model != before.Model || after.WindowKey != before.WindowKey || !after.ObservedAt.Equal(before.ObservedAt) {
		t.Fatal("normal reopen changed statistics", before, after, err)
	}
	state, err = h.manager.ReadThreadStatus(context.Background(), thread.ID)
	if err != nil || state != "notLoaded" || h.calls.Load() != requests {
		t.Fatal("statistics read resumed or invoked a model", state, err, h.calls.Load())
	}
}
