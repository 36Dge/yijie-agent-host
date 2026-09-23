package runtimeinputonly

import (
	"encoding/json"
	"testing"
)

func TestNativeInputOnlyPolicyRejectsMissingOrUnknownFacts(t *testing.T) {
	policy := map[string]any{"version": 1, "cwd": "/ordinary-fixture", "fileReadRoots": []string{}, "fileWriteRoots": []string{}, "networkAccess": false, "toolNames": []string{}, "instructionSources": []string{}, "extensionContributorsEnabled": false}
	decode := func(value any) error {
		body, _ := json.Marshal(map[string]any{"threadId": "native-thread", "policy": value})
		_, err := DecodePolicy(body)
		return err
	}
	if err := decode(policy); err != nil {
		t.Fatal(err)
	}
	for key, value := range policy {
		delete(policy, key)
		if decode(policy) == nil {
			t.Errorf("missing %s accepted", key)
		}
		policy[key] = value
	}
	policy["futureField"] = true
	if decode(policy) == nil {
		t.Fatal("unknown field accepted")
	}
	delete(policy, "futureField")
	policy["toolNames"] = nil
	if decode(policy) == nil {
		t.Fatal("null tools accepted as empty")
	}
	if err := decode(nil); err != nil {
		t.Fatal("ordinary null receipt rejected", err)
	}
}
