package codex

import (
	"context"
	"encoding/json"
	"errors"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/runtimehostimages"
)

func (m *Manager) imageToolInstructions(ctx context.Context) string {
	if !m.config.HostImagesEnabled || isScheduledDraft(ctx) {
		return ""
	}
	if ImageRoute(ctx) != "builtin" {
		return "\nFor an image creation request, use only the bound expert's configured image service under its normal approval policy. If unavailable, report that state."
	}
	return "\nYiJie image generation: when the user asks to create an image, directly call the native function generate_image as your first tool action. Its schema is the complete workflow for this deployment; skip the OpenAI imagegen skill files, command-line utilities, scripts and tool searches. Set use_current_reference=true only when the user wants to use their single attached reference in this turn. Do not call it for image analysis. The desktop displays the published image. If the call fails, report the failure and stop. A new paid attempt requires a new user request."
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
