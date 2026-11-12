package session

import (
	"encoding/json"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
	video "github.com/36Dge/yijie-agent-host/internal/contracts/videogeneration"
)

// Project only the closed receipt, never prompts, provider task IDs or URLs.
func projectNativeVideo(wire nativeWireItem, out *native.NativeItem) bool {
	if wire.Server != "yijie_media" || (wire.Tool != video.GenerateToolName && wire.Tool != video.GetToolName) {
		return false
	}
	label := "生成视频"
	if wire.Tool == video.GetToolName {
		label = "查询视频结果"
	}
	nativeOperationDisplay(out, label, "易界内置")
	out.ArgumentsSummary = nativeString(label)
	if len(wire.Result) == 0 || string(wire.Result) == "null" {
		return true
	}
	var envelope struct {
		Structured json.RawMessage `json:"structuredContent"`
	}
	var receipt video.VideoResult
	if json.Unmarshal(wire.Result, &envelope) != nil || json.Unmarshal(envelope.Structured, &receipt) != nil || receipt.Validate() != nil {
		out.Mcp.ResultKind = native.Omitted
		out.ResultSummary = nativeString("视频状态暂未确认")
		return true
	}
	label = "视频服务暂不可用；请查询原任务"
	switch receipt.Code {
	case video.VideoCodeOk:
		label = "视频已生成，正在确认交付"
		if receipt.Status == video.VideoStatusCommitted {
			label = "视频已交付"
		}
	case video.VideoCodeProviderPending:
		label = "视频任务正在处理中"
		if receipt.VideoOperationId == nil {
			label = "视频请求正在准备，尚未确认受理"
		} else if receipt.ProviderStatus != nil && *receipt.ProviderStatus == video.ProviderStatusSucceeded {
			label = "视频已生成，等待取回交付"
		}
	case video.VideoCodeExistingOperation:
		label = "继续查询已有视频任务，未重复创建"
	case video.VideoCodeSelectionRequired:
		label = "有多个视频任务，请选择要查询的任务"
	case video.VideoCodeOperationNotFound:
		label = "当前聊天未找到对应的视频任务"
	case video.VideoCodeExpertRequired:
		label = "已选专家；不会调用内置视频"
	case video.VideoCodeInputUnsupported:
		label = "当前视频输入不受支持"
	case video.VideoCodeOperationConflict:
		label = "本轮已有不同的视频请求，未重新提交"
	case video.VideoCodeOutcomeUnknown:
		label = "生成结果暂未确认，未重复提交"
	case video.VideoCodeProviderFailed:
		label = "视频生成未完成，未重新提交"
	case video.VideoCodeProviderExpired:
		label = "视频任务已过期，未重新提交"
	case video.VideoCodeDeliveryRejected:
		label = "视频无法交付，未重新生成"
	case video.VideoCodeDeliveryUnconfirmed:
		label = "视频交付状态暂未确认，请查看原产物"
	case video.VideoCodeLocalCancelled:
		label = "已停止本地等待，远端视频任务可能继续"
	}
	out.Mcp.ResultKind = native.Text
	out.Mcp.Texts = []native.NativeMcpText{{Index: 0, Text: label}}
	out.ResultSummary = nativeString(label)
	return true
}
