package session

import (
	"encoding/json"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
	video "github.com/36Dge/yijie-agent-host/internal/contracts/videogeneration"
	"strings"
	"testing"
)

func TestNativeVideoProjectionDistinguishesWaitingStopAndHandoff(t *testing.T) {
	for _, tc := range []struct {
		code video.VideoCode
		want string
	}{
		{video.VideoCodeProviderPending, "正在准备，尚未确认受理"},
		{video.VideoCodeLocalCancelled, "远端视频任务可能继续"},
		{video.VideoCodeExpertRequired, "不会调用内置视频"},
		{video.VideoCodeDeliveryUnconfirmed, "交付状态暂未确认"},
	} {
		receipt := videoResult(tc.code)
		if tc.code == video.VideoCodeDeliveryUnconfirmed {
			receipt.VideoOperationId = stringPointer(testTurnID)
			receipt.ArtifactId = stringPointer(testTurnID)
			receipt.DeliveryTurnId = stringPointer(testTurnID)
		}
		raw, _ := json.Marshal(map[string]any{"structuredContent": receipt})
		out := &native.NativeItem{}
		projectNativeMcp(nativeWireItem{Server: "yijie_media", Tool: video.GetToolName, Result: raw}, out)
		if out.ResultSummary == nil || !strings.Contains(*out.ResultSummary, tc.want) || out.Mcp.ResultKind != native.Text {
			t.Fatalf("projection %s: %+v", tc.code, out)
		}
	}
}

func TestNativeVideoProjectionDoesNotCallDeliveryWaitGeneration(t *testing.T) {
	op := "17300000-0000-4000-8000-000000000001"
	observed := int64(1794490000)
	for _, status := range []video.ProviderStatus{video.ProviderStatusQueued, video.ProviderStatusRunning, video.ProviderStatusSucceeded} {
		receipt := video.VideoResult{Source: "builtin", Status: video.VideoStatusPending, Code: video.VideoCodeProviderPending, NextAction: video.NextActionGetVideoResult, VideoOperationId: &op, ProviderStatus: &status, ProviderObservedAt: &observed}
		raw, _ := json.Marshal(map[string]any{"structuredContent": receipt})
		out := &native.NativeItem{}
		projectNativeMcp(nativeWireItem{Server: "yijie_media", Tool: video.GetToolName, Result: raw}, out)
		if out.ResultSummary == nil {
			t.Fatal("missing video summary")
		}
		expected := "视频任务正在处理中"
		if status == video.ProviderStatusSucceeded {
			expected = "视频已生成，等待取回交付"
		}
		if *out.ResultSummary != expected {
			t.Fatal("state meaning lost", status, *out.ResultSummary)
		}
	}
}
