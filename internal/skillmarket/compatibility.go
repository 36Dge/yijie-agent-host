package skillmarket

import "context"

type compatibilityToggle struct {
	ID                            string
	Epoch                         int64
	Version                       string
	WasEnabled, Applied, Restored bool
	ConfigVersion                 string // schema-1 reader; retained, never repurposed
}

func (s *Service) compatibilityState(ctx context.Context, change compatibilityToggle) (entry, bool, error) {
	var e entry
	if err := s.get("entries", change.ID, &e); err != nil {
		return e, false, err
	}
	state, err := s.readState(ctx, e)
	if err != nil {
		return e, false, err
	}
	if e.Epoch != change.Epoch || state.CacheState != "valid" || !state.UserConfigPresent || state.UserEnabled == nil || state.CacheVersion == nil || *state.CacheVersion != change.Version {
		return e, false, fail("migration_user_intent_changed")
	}
	return e, *state.UserEnabled, nil
}
func (s *Service) verifyCompatibilityToggle(ctx context.Context, change compatibilityToggle, enabled bool) error {
	_, actual, err := s.compatibilityState(ctx, change)
	if err != nil {
		return err
	}
	if actual != enabled {
		return fail("migration_user_intent_changed")
	}
	return nil
}
func (s *Service) applyCompatibilityToggle(ctx context.Context, change *compatibilityToggle, restore bool) error {
	e, actual, err := s.compatibilityState(ctx, *change)
	if err != nil {
		return err
	}
	target := restore && change.WasEnabled
	// Either side of this recorded native CAS is a valid restart checkpoint.
	// Neither a different epoch nor a different package can be adopted here.
	if actual != target {
		if err = s.config.Runtime.SetSkillPluginEnabled(ctx, e.pluginID(), target); err != nil {
			return err
		}
	}
	if err = s.verifyCompatibilityToggle(ctx, *change, target); err != nil {
		return err
	}
	if restore {
		change.Restored = true
	} else {
		change.Applied = true
	}
	return nil
}
