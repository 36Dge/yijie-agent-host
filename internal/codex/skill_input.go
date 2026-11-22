package codex

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"

	runtimeprojection "github.com/36Dge/yijie-agent-host/internal/contracts/runtimekimik31pro"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// The native loader supplies the selected SKILL.md and the Runtime owns the
// turn's tools and permissions. Keep product guidance out of the user's text.
const explicitSkillUseInstructions = `When the user explicitly invokes a skill in the current turn, use the loaded skill instructions, its declared tool dependencies and the requested task to determine what can actually run. In a brief public chat reply, name the skill and say which requested work is possible now. Check the capabilities available to this turn, including discoverable tools, relevant connections, permissions and observed results. Installation alone does not prove access, and a difference in tool names does not prove a capability is missing. If a prerequisite is unverified, identify the affected step and check it through normal authorized work before claiming a result.

Use information already supplied in the conversation and accessible attachments. Ask together for the specific missing inputs the skill requires for this task, offering alternatives the skill accepts. If the user supplied only the skill, ask for the task or the minimum starting information from its instructions. For a confirmed tool, data or authorization limitation, explain which step it affects, how to unblock it and what can still be done. Use the existing approval or connection flow for authorization, never ask for secrets in chat. When requirements are met, proceed in the same turn without another confirmation. Apply this feedback to each explicitly requested skill. Mere discussion of a skill, implicit matches and skills used only in previous turns do not call for a readiness report.`

var nativeUserInputSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	var document any
	if err := json.Unmarshal(runtimeprojection.NativeTurnStartSchema, &document); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	const resource = "https://schemas.yijie.ai/pinned-runtime/turn-start"
	if err := compiler.AddResource(resource, document); err != nil {
		return nil, err
	}
	return compiler.Compile(resource + "#/definitions/UserInput")
})

func validateNativeSkillInput(value map[string]any) error {
	schema, err := nativeUserInputSchema()
	if err != nil || schema.Validate(value) != nil {
		return errors.New("native Skill input is invalid")
	}
	return nil
}

// SkillUserInput accepts only a path resolved by the managed asset service.
// Runtime owns instruction reading, matching, precedence and deduplication.
func SkillUserInput(name, path string) (UserInput, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return UserInput{}, errors.New("managed Skill path is unavailable")
	}
	input := UserInput{kind: "skill", skillName: name, skillPath: path}
	if _, err := input.wireValue(); err != nil {
		return UserInput{}, err
	}
	return input, nil
}
