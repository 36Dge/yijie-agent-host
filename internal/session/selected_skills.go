package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	agenthostcontract "github.com/36Dge/yijie-agent-host/internal/contracts"
)

type SelectedSkillResources interface {
	ResolveSelectedSkills(context.Context, []agenthostcontract.SelectedSkillRef) ([]codex.UserInput, error)
}

func WithSelectedSkillResources(resources SelectedSkillResources) ServiceOption {
	return func(s *Service) { s.selectedSkills = resources }
}

func selectedSkillReferences(blocks []TurnContentBlock) ([]agenthostcontract.SelectedSkillRef, error) {
	var refs []agenthostcontract.SelectedSkillRef
	seen := make(map[string]bool)
	for _, block := range blocks {
		if len(block.Skills) > 0 && block.Type != ContentBlockText {
			return nil, fmt.Errorf("%w: selected Skills require text input", ErrInvalidArgument)
		}
		for _, ref := range block.Skills {
			if strings.TrimSpace(ref.Id) == "" || len([]rune(ref.Id)) > 256 ||
				strings.TrimSpace(ref.Version) == "" || len([]rune(ref.Version)) > 64 || seen[ref.Id] || len(refs) >= 256 {
				return nil, fmt.Errorf("%w: selected Skill identity is invalid", ErrInvalidArgument)
			}
			seen[ref.Id] = true
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

func (s *Service) resolveSelectedSkillInputs(ctx context.Context, blocks []TurnContentBlock, draft bool) ([]codex.UserInput, error) {
	refs, err := selectedSkillReferences(blocks)
	if err != nil || len(refs) == 0 {
		return nil, err
	}
	if draft || s.selectedSkills == nil {
		return nil, fmt.Errorf("%w: selected Skills are unavailable", ErrInvalidArgument)
	}
	inputs, err := s.selectedSkills.ResolveSelectedSkills(ctx, refs)
	if err != nil {
		return nil, fmt.Errorf("%w: selected Skill installation or version is unavailable", ErrInvalidArgument)
	}
	return inputs, nil
}
