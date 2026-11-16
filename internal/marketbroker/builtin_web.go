package marketbroker

import (
	"context"
	"encoding/json"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
)

// BuiltinWebControl uses the same single-reader owner pipe as market tools.
// The schema is separate because no marketplace selection grants this feature.
func (o *Owner) BuiltinWebControl(ctx context.Context, name string, payload []byte) (wire.ControlAck, error) {
	if wire.Validate(name, payload) != nil {
		return wire.ControlAck{}, ErrProtocol
	}
	if err := o.acquireControl(ctx); err != nil {
		return wire.ControlAck{}, err
	}
	defer func() { <-o.gate }()
	frame, err := o.exchange(ctx, payload, false)
	if err != nil {
		return wire.ControlAck{}, err
	}
	var ack wire.ControlAck
	if wire.Validate("ControlAck", frame) != nil || json.Unmarshal(frame, &ack) != nil {
		o.retire(ErrProtocol)
		return ack, ErrProtocol
	}
	return ack, nil
}
func (o *Owner) BuiltinWebToken() (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || !o.initialized || o.capability == "" {
		return "", ErrClosed
	}
	return o.capability, nil
}
