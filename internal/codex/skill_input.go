package codex

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"

	runtimeprojection "github.com/36Dge/yijie-agent-host/internal/contracts/runtimekimik31pro"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

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
