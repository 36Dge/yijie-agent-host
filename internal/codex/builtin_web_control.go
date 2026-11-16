package codex

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
)

func (m *Manager) ConfigureBuiltinWeb(origin, admissionURL, admissionToken string, lifecycle func(BuiltinWebTurnEvent)) error {
	return m.ConfigureBuiltinWebProfile(origin, admissionURL, admissionToken, "product", lifecycle)
}
func (m *Manager) ConfigureBuiltinWebProfile(origin, admissionURL, admissionToken, profile string, lifecycle func(BuiltinWebTurnEvent)) error {
	u, err := url.Parse(origin)
	local := profile == "local_demo" && origin == wire.LocalAPIOrigin
	if (profile != "product" && !local) || err != nil || (u.Scheme != "https" && !local) || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("builtin web service origin unavailable")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started || !m.config.BuiltinWebEnabled {
		return errors.New("builtin web assembly unavailable")
	}
	m.builtinWeb.apiOrigin = strings.TrimSuffix(origin, "/")
	m.builtinWeb.profile = profile
	m.builtinWeb.admissionURL = admissionURL
	m.builtinWeb.admissionToken = admissionToken
	m.builtinWeb.lifecycle = lifecycle
	return nil
}
func (m *Manager) BuiltinWebControl(ctx context.Context, name string, raw []byte) (wire.ControlAck, error) {
	if !m.config.BuiltinWebEnabled {
		return wire.ControlAck{}, errors.New("builtin web unavailable")
	}
	owner, err := m.MarketOwner()
	if err != nil {
		return wire.ControlAck{}, err
	}
	if name == "WebCapabilityInstall" {
		m.mu.Lock()
		ready := m.status.Ready && !m.shutdownRequested
		m.mu.Unlock()
		if !ready {
			return wire.ControlAck{}, errors.New("builtin web runtime not ready")
		}
		var control wire.WebCapabilityInstall
		if wire.Validate(name, raw) != nil || json.Unmarshal(raw, &control) != nil {
			return wire.ControlAck{}, errors.New("builtin web control invalid")
		}
		m.mu.Lock()
		config := m.builtinWeb
		m.mu.Unlock()
		request := wire.WebWorkerInstall{Method: "web_worker_install", Control: control, ApiOrigin: config.apiOrigin, AdmissionUrl: config.admissionURL, AdmissionBearer: config.admissionToken}
		profile := wire.WebWorkerInstallProfile(config.profile)
		request.Profile = &profile
		raw, err = json.Marshal(request)
		if err != nil {
			return wire.ControlAck{}, err
		}
		name = "WebWorkerInstall"
	}
	ack, err := owner.BuiltinWebControl(ctx, name, raw)
	if err != nil || ack.State == "unavailable" {
		return ack, errors.New("builtin web control unavailable")
	}
	if name == "WebWorkerInstall" {
		token, err := owner.BuiltinWebToken()
		if err != nil {
			return ack, err
		}
		m.market.mu.Lock()
		endpoint := strings.TrimSuffix(m.market.gatewayURL, "/mcp") + "/builtin-web"
		m.market.mu.Unlock()
		m.mu.Lock()
		if !m.status.Ready || m.shutdownRequested {
			m.mu.Unlock()
			return ack, errors.New("builtin web runtime stopped")
		}
		m.builtinWeb.url = endpoint
		m.builtinWeb.token = token
		m.mu.Unlock()
	} else if name == "WebCapabilityClear" {
		m.mu.Lock()
		m.builtinWeb.url = ""
		m.mu.Unlock()
	}
	return ack, nil
}
