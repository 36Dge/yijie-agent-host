package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os/exec"
	"testing"
)

func TestRetirementRuntimePolicyMatchesImmutableAuthority(t *testing.T) {
	const commit = "4d3f967938dde1c86ca34003a0a5628717f96262"
	const digest = "67d7dfe8d539668a366ed744d92483d39597208259fe929d32d6d811c79ffbb8"
	content, err := exec.Command("git", "-C", "../../../yijie-contracts", "show", commit+":docs/retirements/FEAT-137.json").Output()
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(content)
	if hex.EncodeToString(hash[:]) != digest {
		t.Fatal("retirement authority digest changed")
	}
	var authority struct {
		Status           string `json:"status"`
		Permanent        bool   `json:"permanent"`
		AcceptancePassed bool   `json:"acceptance_passed"`
		Runtime          struct {
			Binary   string `json:"binary_sha256"`
			Size     int64  `json:"binary_size_bytes"`
			Manifest string `json:"manifest_sha256"`
			Schema   string `json:"schema_tree_sha256"`
			Patches  int    `json:"patch_count"`
		} `json:"runtime"`
	}
	if err := json.Unmarshal(content, &authority); err != nil {
		t.Fatal(err)
	}
	if authority.Status != "terminated" || !authority.Permanent || authority.AcceptancePassed {
		t.Fatal("retirement must preserve unaccepted permanent termination")
	}
	policy := retiredApprovalBaselinePolicy
	if authority.Runtime.Binary != policy.runtimeSHA256 || authority.Runtime.Size != policy.runtimeSize ||
		authority.Runtime.Manifest != policy.manifestSHA256 || authority.Runtime.Schema != policy.schemaTreeSHA256 ||
		authority.Runtime.Patches != len(policy.patches) {
		t.Fatal("active gate-off Runtime differs from the frozen retirement authority")
	}
}
