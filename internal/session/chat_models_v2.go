package session

import (
	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodelsv2"
)

func (s *Service) ChatModelsV2Enabled() bool {
	r, ok := s.runtime.(interface{ ChatModelsV2Enabled() bool })
	return ok && r.ChatModelsV2Enabled() && s.ChatModelsEnabled()
}
func (s *Service) chatModelsV2Ready() bool {
	r, ok := s.runtime.(interface{ ChatModelsV2Ready() bool })
	return ok && r.ChatModelsV2Ready()
}
func (s *Service) modelActivationRequired(id string) bool {
	r, ok := s.runtime.(interface{ ChatModelActivationRequired(string) bool })
	return ok && r.ChatModelActivationRequired(id)
}
func (s *Service) ChatModelCatalogV2() wire.Catalog {
	entries := make([]wire.ModelAvailability, 0, 2)
	rt, ok := s.runtime.(modelRuntime)
	for _, p := range wire.Definitions() {
		configured := ok && s.ChatModelsV2Enabled() && rt.ModelAvailable(string(p.ProfileId))
		available := configured && s.chatModelsV2Ready()
		reason := wire.ModelAvailabilityReasonNotConfigured
		if available {
			reason = wire.ModelAvailabilityReasonReady
		} else if s.modelActivationRequired(string(p.ProfileId)) {
			reason = wire.ModelAvailabilityReasonActivationRequired
		} else if configured {
			reason = wire.ModelAvailabilityReasonRuntimeUnavailable
		}
		entries = append(entries, wire.ModelAvailability{Profile: p, Available: available, Reason: reason})
	}
	return wire.Catalog{SchemaVersion: 2, DefaultProfile: codex.KimiK31ProfileID, Models: entries}
}
func (s *Service) ReadChatModelV2(id string) (wire.Selection, error) {
	if !s.ChatModelsV2Enabled() {
		return wire.Selection{}, ErrModelUnavailable
	}
	if !isCanonicalNonZeroUUID(id) {
		return wire.Selection{}, ErrInvalidArgument
	}
	r, err := s.store.Get(id)
	if err != nil {
		return wire.Selection{}, err
	}
	state := wire.SelectionState(r.ModelSelectionState)
	if state == "" {
		state = wire.SelectionStateReady
	}
	out := wire.Selection{SchemaVersion: 2, AgentSessionId: id, State: state, Revision: r.ModelRevision, Availability: wire.SelectionAvailabilitySelectionUnknown}
	profile := recordProfile(r)
	if profile != "" {
		p := wire.ProfileId(profile)
		out.ProfileId = &p
	} else {
		out.State = wire.SelectionStateUnknown
	}
	if r.ModelSelectionOperation != "" {
		out.OperationId = &r.ModelSelectionOperation
	}
	if profile == "kimi-k3-max-v1" {
		out.Availability = wire.SelectionAvailabilityRetired
	} else if out.State == wire.SelectionStateReady && profile != "" {
		out.Availability = wire.SelectionAvailabilityNotConfigured
		if s.modelActivationRequired(profile) {
			out.Availability = wire.SelectionAvailabilityActivationRequired
		}
		if s.runtime.(modelRuntime).ModelAvailable(profile) {
			out.Availability = wire.SelectionAvailabilityReady
			if !s.chatModelsV2Ready() {
				out.Availability = wire.SelectionAvailabilityRuntimeUnavailable
			}
		}
	}
	if wire.Validate("Selection", out) != nil {
		return wire.Selection{}, ErrModelUnknown
	}
	return out, nil
}
