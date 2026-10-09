// Generated from market-broker-control source and declared borrowed authorities; DO NOT EDIT.
package marketselection

import (
	"bytes"
	"encoding/json"
	"errors"
	market "github.com/36Dge/yijie-agent-host/internal/contracts/marketconnectors"
	"io"
	"regexp"
)

var wirePattern0 = regexp.MustCompile("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
var wirePattern1 = regexp.MustCompile("^[0-9a-f]{64}$")

type CanonicalId = market.CanonicalId

type SelectionDigestValue string

func (v SelectionDigestValue) Validate() error {
	if !wirePattern1.MatchString(string(v)) {
		return errors.New("invalid broker pattern")
	}
	return nil
}

type Selection []SelectionRef

func (v Selection) Validate() error {
	if v == nil {
		return errors.New("null broker collection")
	}
	if len(v) > 58 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	for i, item := range v {
		for _, prior := range v[:i] {
			if item == prior {
				return errors.New("duplicate broker collection item")
			}
		}
	}
	return nil
}

type SelectionSnapshot struct {
	SchemaVersion   int64                `json:"schemaVersion"`
	TurnOperationId CanonicalId          `json:"turnOperationId"`
	Selection       Selection            `json:"selection"`
	SelectionDigest SelectionDigestValue `json:"selectionDigest"`
}

func (v SelectionSnapshot) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.TurnOperationId.Validate(); err != nil {
		return err
	}
	if err := v.Selection.Validate(); err != nil {
		return err
	}
	if err := v.SelectionDigest.Validate(); err != nil {
		return err
	}
	for i := 1; i < len(v.Selection); i++ {
		if v.Selection[i-1].InstallationId >= v.Selection[i].InstallationId {
			return errors.New("selection snapshot is not canonical")
		}
	}
	digest, err := SelectionDigest(string(v.TurnOperationId), v.Selection)
	if err != nil {
		return err
	}
	if digest != string(v.SelectionDigest) {
		return errors.New("selection snapshot digest mismatch")
	}
	return nil
}
func (v *SelectionSnapshot) UnmarshalJSON(b []byte) error {
	type wire SelectionSnapshot
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schemaVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schemaVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["turnOperationId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["turnOperationId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["selection"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["selection"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["selectionDigest"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["selectionDigest"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := SelectionSnapshot(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}
