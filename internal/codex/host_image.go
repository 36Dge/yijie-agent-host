package codex

import (
	"context"
	"encoding/json"
	"errors"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/runtimehostimages"
)

func (m *Manager) imageToolInstructions(ctx context.Context) string {
	return m.imageOnlyToolInstructions(ctx) + m.videoToolInstructions(ctx)
}

func (m *Manager) imageOnlyToolInstructions(ctx context.Context) string {
	if !m.config.BuiltinMediaEnabled || isScheduledDraft(ctx) {
		return ""
	}
	if ImageRoute(ctx) != "builtin" {
		return "\nYiJie images: the explicitly selected expert owns this conversation's image capability. Use only its configured MCP tools. If the expert, tool, credential or required capability is unavailable, or execution fails or has an unknown outcome, report that status and retain the expert selection. Never switch to YiJie's builtin image generation, another provider, or an image CLI. Do not ask for provider keys."
	}
	return "\nYiJie images: use yijie_media.generate_image for an explicit image creation request, discovering it with native tool search when its definition is not visible. Do not use an image CLI or ask for provider keys. Set use_current_reference only for the user's single current-turn reference, never for image analysis. A staged image is not yet committed; use the existing get_artifact_status to check delivery. Failed or unknown results do not authorize automatic resubmission."
}

type HostImageHandler func(context.Context, wire.HostImageGenerateParams) bool

func (m *Manager) SetHostImageHandler(handler HostImageHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started || handler == nil || !m.config.HostImagesEnabled {
		return errors.New("cannot bind managed image handler")
	}
	m.hostImageHandler = handler
	return nil
}

func (m *Manager) handleHostImage(ctx context.Context, req serverRequest) serverRequestResult {
	failed := serverRequestResult{respond: true, value: wire.HostImageGenerateResponse{Success: false}}
	m.mu.Lock()
	handler := m.hostImageHandler
	enabled := m.config.HostImagesEnabled && m.artifact.supportsHostImages()
	m.mu.Unlock()
	if !enabled || handler == nil || !req.exactEnvelope || len(req.params) > 16<<10 {
		return failed
	}
	// A missing required boolean cannot be interpreted as a text-only request.
	keys, err := decodeUniqueJSONObject(req.params, map[string]struct{}{"threadId": {}, "turnId": {}, "callId": {}, "prompt": {}, "useCurrentReference": {}})
	if err != nil || len(keys) != 5 || (string(keys["useCurrentReference"]) != "true" && string(keys["useCurrentReference"]) != "false") {
		return failed
	}
	var call wire.HostImageGenerateParams
	if json.Unmarshal(req.params, &call) != nil || ctx.Err() != nil {
		return failed
	}
	return serverRequestResult{respond: true, value: wire.HostImageGenerateResponse{Success: handler(ctx, call)}}
}

func (m *Manager) videoToolInstructions(ctx context.Context) string {
	if !m.config.BuiltinVideoEnabled || isScheduledDraft(ctx) {
		return ""
	}
	if ImageRoute(ctx) != "builtin" {
		return "\nYiJie videos: retain the explicitly selected expert and use only its configured tools. Unavailable, failed, unsupported or unknown expert outcomes never authorize builtin video generation/recovery, another provider, or a video CLI. Do not ask for provider keys."
	}
	return "\nYiJie videos: for an explicit new text/image-to-video request discover yijie_media.generate_video, then await get_video_result for the SAME operation. Use native exec/wait for long calls. For 'continue', 'retry' or a missing result call get_video_result, not generate_video; omitting the operation ID can recover this conversation's local record. Existing/unknown operations do not authorize another paid creation. Provider defaults and model are fixed; never add --parameter suffixes. Builtin video accepts text or current-turn images only. Video/audio references, editing or extending an existing clip are unavailable; never promise an in-place edit or exact output duration, ratio, resolution or audio control. A requested revision requires explicit intent for a new generation. Current-turn image indices are 1-based; first/last can use the same index. Never use a CLI, provider URLs/keys or publish_artifact to duplicate delivery. After staged, check get_artifact_status; committed confirms delivery, not playback. Pending means the original task is still being observed. Stopping only stops local waiting and delivery, not the remote task. A handoff with unconfirmed delivery is not permission to generate or publish again."
}
