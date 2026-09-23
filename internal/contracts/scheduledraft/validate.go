package scheduledraft

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schema.json
var schemaBytes []byte

//go:embed output.schema.json
var outputBytes []byte
var once sync.Once
var schemas map[string]*jsonschema.Schema
var schemaError error

func initialize() {
	schemas = make(map[string]*jsonschema.Schema)
	var doc map[string]any
	if schemaError = json.Unmarshal(schemaBytes, &doc); schemaError != nil {
		return
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	id := doc["$id"].(string)
	if schemaError = c.AddResource(id, doc); schemaError != nil {
		return
	}
	for name := range doc["$defs"].(map[string]any) {
		var s *jsonschema.Schema
		s, schemaError = c.Compile(id + "#/$defs/" + name)
		if schemaError != nil {
			return
		}
		schemas[name] = s
	}
}
func Decode(name string, data []byte, target any) error {
	once.Do(initialize)
	if schemaError != nil {
		return errors.New("draft schema unavailable")
	}
	s, ok := schemas[name]
	if !ok {
		return errors.New("unknown draft schema")
	}
	v, e := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if e != nil || s.Validate(v) != nil {
		return errors.New("invalid draft value")
	}
	return json.Unmarshal(data, target)
}
func Validate(name string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	var out any
	return Decode(name, b, &out)
}
func OutputSchema() json.RawMessage { return append(json.RawMessage(nil), outputBytes...) }
