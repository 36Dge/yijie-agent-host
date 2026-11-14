package skillmarket

import (
	"context"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/runtimeskillmarket"
)

// The pinned N2 native inventory owns the 512 installation bound. Bundled offers
// do not occupy installation slots. Retained journal history is never counted
// as an active installation or deleted to make room for a release.
const maxNativeInstallationSlots = 512

func installationSlots(states []native.PluginState, reserved []string) map[string]bool {
	slots := map[string]bool{}
	for _, state := range states {
		if !absent(state) {
			slots[state.PluginId] = true
		}
	}
	for _, id := range reserved {
		slots[id] = true
	}
	return slots
}
func (s *Service) requireInstallationSlot(ctx context.Context, target string) error {
	states, err := s.config.Runtime.ReadSkillPluginStates(ctx, nil, []string{"yijie-builtin", "yijie-user"}, "")
	if err != nil {
		return err
	}
	entries, err := s.entries()
	if err != nil {
		return err
	}
	reserved := []string{target}
	for _, e := range entries {
		if e.ActiveOperation != "" {
			reserved = append(reserved, e.pluginID())
		}
	}
	records, err := s.migrations()
	if err != nil {
		return err
	}
	for _, m := range records {
		if m.Rollback || m.Phase == "complete" {
			continue
		}
		var e entry
		if err = s.get("entries", m.ID, &e); err != nil {
			return err
		}
		reserved = append(reserved, e.pluginID())
	}
	if len(installationSlots(states, reserved)) > maxNativeInstallationSlots {
		return fail("skill_inventory_full")
	}
	return nil
}
