package contextwindow

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

//go:embed native.schema.json
var nativeBytes []byte

var once sync.Once
var schemas map[string]*jsonschema.Schema
var schemaError error

func initialize() {
	schemas = make(map[string]*jsonschema.Schema)
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	for name, data := range map[string][]byte{"display": schemaBytes, "native": nativeBytes} {
		var doc map[string]any
		if schemaError = json.Unmarshal(data, &doc); schemaError != nil {
			return
		}
		id := "https://schemas.yijie.ai/context-window/" + name
		if schemaError = c.AddResource(id, doc); schemaError != nil {
			return
		}
		if name == "native" {
			schemas["NativeUsage"], schemaError = c.Compile(id)
		} else {
			schemas["ContextWindowSnapshot"], schemaError = c.Compile(id + "#/definitions/ContextWindowSnapshot")
		}
		if schemaError != nil {
			return
		}
	}
}

func Decode(name string, data []byte, target any) error {
	once.Do(initialize)
	if schemaError != nil || schemas[name] == nil {
		return errors.New("context schema unavailable")
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil || schemas[name].Validate(value) != nil {
		return errors.New("invalid context observation")
	}
	return json.Unmarshal(data, target)
}

func Validate(value ContextWindowSnapshot) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var out any
	return Decode("ContextWindowSnapshot", data, &out)
}
