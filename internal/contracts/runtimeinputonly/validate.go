package runtimeinputonly

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// These are byte-identical imports of the candidate Runtime's stable schemas.
//
//go:embed response.schema.json
var responseSchema []byte

var once sync.Once
var compiled *jsonschema.Schema
var compileErr error

func DecodePolicy(data []byte) (ThreadInputOnlyPolicyReadResponse, error) {
	once.Do(func() {
		var document any
		document, compileErr = jsonschema.UnmarshalJSON(bytes.NewReader(responseSchema))
		if compileErr != nil {
			return
		}
		compiler := jsonschema.NewCompiler()
		compileErr = compiler.AddResource("urn:yijie:runtime:input-only:response", document)
		if compileErr == nil {
			compiled, compileErr = compiler.Compile("urn:yijie:runtime:input-only:response")
		}
	})
	var result ThreadInputOnlyPolicyReadResponse
	if compileErr != nil {
		return result, errors.New("native input-only schema unavailable")
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil || compiled.Validate(value) != nil {
		return result, errors.New("invalid native input-only policy")
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, errors.New("invalid native input-only policy fields")
	}
	return result, nil
}
