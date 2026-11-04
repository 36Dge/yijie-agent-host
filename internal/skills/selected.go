package skills

import (
	"context"
	"errors"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	agenthostcontract "github.com/36Dge/yijie-agent-host/internal/contracts"
)

// ResolveSelectedSkills performs only managed installation/identity admission.
// Native discovery supplies names and paths; Runtime reads the actual SKILL.md.
func (s *Service) ResolveSelectedSkills(ctx context.Context, refs []agenthostcontract.SelectedSkillRef) ([]codex.UserInput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.runtime == nil || len(refs) == 0 || len(refs) > 256 {
		return nil, errorWithCode(CodeCapabilityUnavailable, errors.New("selected Skills unavailable"))
	}
	current, err := s.loadCurrentCatalog()
	if err != nil {
		return nil, err
	}
	entries, err := s.runtime.ListSkills(ctx, []string{s.managedRoot}, true)
	if err != nil {
		return nil, errorWithCode(CodeRuntimeUnavailable, err)
	}
	inputs := make([]codex.UserInput, 0, len(refs))
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if seen[ref.Id] {
			return nil, errorWithCode(CodeInvalidRequest, errors.New("duplicate selected Skill"))
		}
		seen[ref.Id] = true
		skill, err := manifestSkill(current, ref.Id)
		if err != nil || isExpertResource(skill) || !installableSkill(skill) || skill.Version != ref.Version {
			return nil, errorWithCode(CodeCapabilityUnavailable, errors.New("selected Skill version unavailable"))
		}
		installed := inspectInstallation(s.managed, skill)
		if !installed.valid || !installed.enabled {
			return nil, errorWithCode(CodeCapabilityUnavailable, errors.New("selected Skill installation unavailable"))
		}
		entrypoint, safe := safeEntrypointPath(s.managedRoot, s.managed, skill)
		if !safe {
			return nil, errorWithCode(CodeCapabilityUnavailable, errors.New("selected Skill entrypoint unavailable"))
		}
		matches := 0
		for _, entry := range entries {
			for _, visible := range entry.Skills {
				if visible.Path == entrypoint && visible.Name == skill.RuntimeName && visible.Enabled {
					matches++
				}
			}
		}
		if matches != 1 {
			return nil, errorWithCode(CodeRuntimeSyncFailed, errors.New("selected Skill native identity unavailable"))
		}
		input, err := codex.SkillUserInput(skill.RuntimeName, entrypoint)
		if err != nil {
			return nil, errorWithCode(CodeRuntimeSyncFailed, err)
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}
