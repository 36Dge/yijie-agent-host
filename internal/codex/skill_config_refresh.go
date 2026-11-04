package codex

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	runtimeprojection "github.com/36Dge/yijie-agent-host/internal/contracts/runtimekimik31pro"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type nativeSkillRefreshSchemaPair struct {
	request  *jsonschema.Schema
	response *jsonschema.Schema
}

var nativeSkillRefreshSchemas = sync.OnceValues(func() (nativeSkillRefreshSchemaPair, error) {
	compiler := jsonschema.NewCompiler()
	const request = "https://schemas.yijie.ai/pinned-runtime/config-batch-write"
	const response = "https://schemas.yijie.ai/pinned-runtime/config-write-response"
	for name, data := range map[string][]byte{
		request:  runtimeprojection.NativeConfigBatchWriteSchema,
		response: runtimeprojection.NativeConfigWriteResponseSchema,
	} {
		var document any
		if err := json.Unmarshal(data, &document); err != nil {
			return nativeSkillRefreshSchemaPair{}, err
		}
		if err := compiler.AddResource(name, document); err != nil {
			return nativeSkillRefreshSchemaPair{}, err
		}
	}
	in, err := compiler.Compile(request)
	if err != nil {
		return nativeSkillRefreshSchemaPair{}, err
	}
	out, err := compiler.Compile(response)
	return nativeSkillRefreshSchemaPair{request: in, response: out}, err
})

func (m *Manager) refreshNativeSkillConfig(ctx context.Context) error {
	// The additional method is admitted by the current immutable candidate.
	// Historical profiles keep their previously reviewed method surface.
	if !m.config.KimiK31Candidate {
		return nil
	}
	schemas, err := nativeSkillRefreshSchemas()
	params := map[string]any{"edits": []any{}, "reloadUserConfig": true}
	if err != nil || schemas.request.Validate(params) != nil {
		return errors.New("native Skill refresh schema is unavailable")
	}
	var response json.RawMessage
	if err := m.request(ctx, runtimeprojection.NativeConfigReloadMethod, params, &response); err != nil {
		return err
	}
	var value map[string]any
	if json.Unmarshal(response, &value) != nil || schemas.response.Validate(value) != nil || value["status"] != "ok" {
		return errors.New("native Skill configuration refresh was not confirmed")
	}
	return nil
}
