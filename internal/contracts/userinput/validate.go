package userinput

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"sync"
)

//go:embed schema.json
var schemaBytes []byte
var once sync.Once
var schemas map[string]*jsonschema.Schema
var schemaError error

func initialize() {
	schemas = make(map[string]*jsonschema.Schema)
	var document map[string]any
	if schemaError = json.Unmarshal(schemaBytes, &document); schemaError != nil {
		return
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	const id = "https://schemas.yijie.ai/user-input/v1"
	if schemaError = c.AddResource(id, document); schemaError != nil {
		return
	}
	for name := range document["definitions"].(map[string]any) {
		schemas[name], schemaError = c.Compile(id + "#/definitions/" + name)
		if schemaError != nil {
			return
		}
	}
}
func Decode(name string, data []byte, target any) error {
	once.Do(initialize)
	if schemaError != nil {
		return errors.New("user input schema unavailable")
	}
	schema, ok := schemas[name]
	if !ok {
		return errors.New("user input schema unavailable")
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil || schema.Validate(value) != nil {
		return errors.New("invalid user input")
	}
	return json.Unmarshal(data, target)
}
func Validate(name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return errors.New("invalid user input")
	}
	var out any
	return Decode(name, data, &out)
}
