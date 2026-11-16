package session

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
	"github.com/google/uuid"
)

type builtinWebAuthority struct {
	mu               sync.Mutex
	process, binding uuid.UUID
	installed        bool
	closed           bool
	stopped          map[[2]string]bool
}
type builtinWebRuntime interface {
	BuiltinWebControl(context.Context, string, []byte) (wire.ControlAck, error)
	BuiltinWebOwner(context.Context, string, string) (string, string, error)
}

func (s *Service) BuiltinWebControl(ctx context.Context, request wire.WebControlRequest) (wire.WebControlResponse, error) {
	runtime, ok := s.runtime.(builtinWebRuntime)
	if !ok || s.market == nil {
		return wire.WebControlResponse{}, errors.New("web unavailable")
	}
	raw, err := request.Payload.MarshalJSON()
	if err != nil {
		return wire.WebControlResponse{}, err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	var process uuid.UUID
	_ = json.Unmarshal(fields["process_epoch"], &process)
	s.market.mu.Lock()
	valid := !s.market.closed && s.market.hello != nil && string(s.market.hello.NativeProcessEpoch) == process.String()
	s.market.mu.Unlock()
	if !valid {
		return wire.WebControlResponse{}, errors.New("web authority unavailable")
	}
	name := map[wire.WebControlRequestMethod]string{"web_capability_install": "WebCapabilityInstall", "web_capability_clear": "WebCapabilityClear", "web_turn_revoke": "WebTurnRevoke", "web_call_cancel": "WebCallCancel"}[request.Method]
	if name == "" || wire.Validate(name, raw) != nil {
		return wire.WebControlResponse{}, errors.New("web control invalid")
	}
	s.builtinWeb.mu.Lock()
	closed := s.builtinWeb.closed
	var clear wire.WebCapabilityClear
	if name == "WebCapabilityClear" {
		_ = json.Unmarshal(raw, &clear)
		closed = closed || (s.builtinWeb.installed && s.builtinWeb.binding != clear.BindingEpoch)
	}
	s.builtinWeb.mu.Unlock()
	if closed {
		return wire.WebControlResponse{}, errors.New("web authority closed or replaced")
	}
	ack, err := runtime.BuiltinWebControl(ctx, name, raw)
	if err != nil {
		return wire.WebControlResponse{}, err
	}
	s.builtinWeb.mu.Lock()
	defer s.builtinWeb.mu.Unlock()
	if s.builtinWeb.closed {
		return wire.WebControlResponse{}, errors.New("web authority closed")
	}
	if name == "WebCapabilityInstall" {
		var install wire.WebCapabilityInstall
		_ = json.Unmarshal(raw, &install)
		if s.builtinWeb.binding != install.BindingEpoch {
			s.builtinWeb.stopped = make(map[[2]string]bool)
		}
		s.builtinWeb.process = process
		s.builtinWeb.binding = install.BindingEpoch
		s.builtinWeb.installed = true
	}
	if name == "WebCapabilityClear" {
		s.builtinWeb.installed = false
	}
	return wire.WebControlResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: ack}, nil
}
func (s *Service) AdmitBuiltinWeb(ctx context.Context, q wire.WebAdmissionRequest) (wire.WebAdmissionReceipt, error) {
	if s.market == nil {
		return wire.WebAdmissionReceipt{}, errors.New("web authority unavailable")
	}
	s.market.mu.Lock()
	closed := s.market.closed || s.market.hello == nil
	s.market.mu.Unlock()
	if closed {
		return wire.WebAdmissionReceipt{}, errors.New("web authority closed")
	}
	runtime, ok := s.runtime.(builtinWebRuntime)
	if !ok {
		return wire.WebAdmissionReceipt{}, errors.New("web runtime unavailable")
	}
	a := &s.builtinWeb
	a.mu.Lock()
	valid := !a.closed && a.installed && a.process == q.ProcessEpoch && a.binding == q.BindingEpoch
	a.mu.Unlock()
	if !valid {
		return wire.WebAdmissionReceipt{}, errors.New("web binding unavailable")
	}
	root, turn, err := runtime.BuiltinWebOwner(ctx, q.ThreadId, q.TurnId)
	if err != nil {
		return wire.WebAdmissionReceipt{}, err
	}
	a.mu.Lock()
	stopped := a.stopped[[2]string{root, turn}] || a.stopped[[2]string{q.ThreadId, q.TurnId}]
	a.mu.Unlock()
	if stopped {
		return wire.WebAdmissionReceipt{}, errors.New("web turn stopped")
	}
	record, err := s.store.GetByThread(root)
	if err != nil || record.Purpose == purposeDraft || record.ActiveTurnID != turn || record.State != StateActive || record.CleanupOperationID != "" {
		return wire.WebAdmissionReceipt{}, errors.New("web root inactive")
	}
	resolved, err := s.contextForStoredExpert(ctx, record.AgentSessionID)
	if err != nil || !codex.ExpertWebAllowed(resolved) {
		return wire.WebAdmissionReceipt{}, errors.New("expert web disabled")
	}
	current, err := s.store.GetByThread(root)
	s.market.mu.Lock()
	closed = s.market.closed
	s.market.mu.Unlock()
	a.mu.Lock()
	valid = !a.closed && a.installed && a.process == q.ProcessEpoch && a.binding == q.BindingEpoch && !a.stopped[[2]string{root, turn}] && !a.stopped[[2]string{q.ThreadId, q.TurnId}]
	a.mu.Unlock()
	if err != nil || closed || !valid || current.ActiveTurnID != turn || current.State != StateActive || current.CleanupOperationID != "" || ctx.Err() != nil {
		return wire.WebAdmissionReceipt{}, errors.New("web turn ended")
	}
	return wire.WebAdmissionReceipt{OperationId: q.OperationId, RootThreadId: root, RootTurnId: turn, DeadlineUnixMs: int(time.Now().Add(45 * time.Second).UnixMilli())}, nil
}
func (s *Service) ObserveBuiltinWeb(event codex.BuiltinWebTurnEvent) {
	if event.State == "started" {
		return
	}
	a := &s.builtinWeb
	a.mu.Lock()
	process, binding, installed := a.process, a.binding, a.installed
	if a.stopped == nil {
		a.stopped = make(map[[2]string]bool)
	}
	if event.ThreadID != "" && event.TurnID != "" {
		a.stopped[[2]string{event.ThreadID, event.TurnID}] = true
	}
	if len(a.stopped) > 1024 {
		a.installed = false
	}
	if event.State == "closed" {
		a.installed = false
		a.closed = true
	}
	a.mu.Unlock()
	if !installed {
		return
	}
	runtime, ok := s.runtime.(builtinWebRuntime)
	if !ok {
		return
	}
	name := "WebTurnRevoke"
	var value any = wire.WebTurnRevoke{Method: "web_turn_revoke", ProcessEpoch: process, ThreadId: event.ThreadID, TurnId: event.TurnID}
	if event.State == "closed" {
		name = "WebCapabilityClear"
		value = wire.WebCapabilityClear{Method: "web_capability_clear", ProcessEpoch: process, BindingEpoch: binding}
	}
	raw, _ := json.Marshal(value)
	// Native's stdio reader must remain free to deliver the real terminal event.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = runtime.BuiltinWebControl(ctx, name, raw)
	}()
}
