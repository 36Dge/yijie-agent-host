package codex

import (
	"context"
	"encoding/json"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/scheduledraft"
	"strings"
	"testing"
)

func TestFEAT1554BDraftMeterDoesNotInheritApprovalPolicy(t *testing.T) {
	config := DefaultConfig()
	config.RuntimePermissionsEnabled = true
	config.PermissionVerificationBaseURL = "http://127.0.0.1:18083/v1"
	config.PermissionVerificationPolicy = "Ordinary verification policy"
	m := NewManager(config, nil)
	for _, thread := range []string{"", "existing-thread"} {
		params, err := m.scheduledDraftThreadParams(t.TempDir(), thread)
		if err != nil {
			t.Fatal(err)
		}
		p := params["config"].(map[string]any)
		if p["model_providers.minimax.base_url"] != config.PermissionVerificationBaseURL ||
			p["model_providers.minimax.request_max_retries"] != 0 || p["model_providers.minimax.stream_max_retries"] != 0 ||
			p["input_only"] != true || p["approval_policy"] != "never" || p["auto_review.policy"] != nil {
			t.Fatal("meter must preserve the restricted policy for start and resume")
		}
	}
	if m.permissionVerificationConfig()["auto_review.policy"] != config.PermissionVerificationPolicy {
		t.Fatal("ordinary permission verification changed")
	}
	config.RuntimePermissionsEnabled = false
	if _, err := NewManager(config, nil).scheduledDraftThreadParams(t.TempDir(), ""); err == nil {
		t.Fatal("configured but unauthorized meter must not fall back")
	}
	config.PermissionVerificationBaseURL = ""
	p, err := NewManager(config, nil).scheduledDraftThreadParams(t.TempDir(), "")
	if err != nil || p["config"].(map[string]any)["model_providers.minimax.base_url"] != nil {
		t.Fatal("ordinary non-verification provider must remain unchanged")
	}
}

func TestFEAT155DraftFixedConfigAndMissingQualificationClosesBeforeIO(t *testing.T) {
	root := t.TempDir()
	start, e := scheduledDraftStartParams(root, "")
	if e != nil {
		t.Fatal(e)
	}
	resume, e := scheduledDraftStartParams(root, "thread")
	if e != nil {
		t.Fatal(e)
	}
	a, _ := json.Marshal(start["config"])
	b, _ := json.Marshal(resume["config"])
	if string(a) != string(b) {
		t.Fatal("policy changed")
	}
	if start["sandbox"] != nil || resume["sandbox"] != nil {
		t.Fatal("legacy sandbox override")
	}
	if start["baseInstructions"] != resume["baseInstructions"] ||
		!strings.HasSuffix(start["baseInstructions"].(string), string(wire.OutputSchema())) {
		t.Fatal("start/resume must guide output with the identical canonical schema")
	}
	config := start["config"].(map[string]any)
	features := config["features"].(map[string]any)
	for _, name := range []string{"shell_tool", "request_permissions_tool", "unified_exec", "memories", "multi_agent", "plugins", "image_generation", "apps", "skill_mcp_dependency_install"} {
		if features[name] != false {
			t.Fatal(name)
		}
	}
	turn := scheduledDraftTurnParams(context.Background(), "thread", []any{map[string]any{"type": "text", "text": "合成草案"}})
	if turn["sandboxPolicy"] != nil {
		t.Fatal("legacy turn policy")
	}
	schema, _ := json.Marshal(turn["outputSchema"])
	if string(schema) != string(wire.OutputSchema()) {
		var a, b any
		json.Unmarshal(schema, &a)
		json.Unmarshal(wire.OutputSchema(), &b)
		aa, _ := json.Marshal(a)
		bb, _ := json.Marshal(b)
		if string(aa) != string(bb) {
			t.Fatal("not canonical schema")
		}
	}
	m := &Manager{}
	if m.ScheduledDraftReady() {
		t.Fatal("unqualified")
	}
	ctx := WithScheduledDraft(context.Background(), "workspace")
	if _, e = m.StartThread(ctx, root); e != errDraftUnqualified {
		t.Fatal(e)
	}
	if _, e = m.ResumeThread(ctx, "thread"); e != errDraftUnqualified {
		t.Fatal(e)
	}
	if _, e = m.startDraftTurn(ctx, "thread", nil); e != errDraftUnqualified {
		t.Fatal(e)
	}
}
