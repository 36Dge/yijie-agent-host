package codex

import (
	"strings"
	"testing"
)

func TestFEAT164AuxiliaryMeterLayer(t *testing.T) {
	cfg := DefaultConfig()
	cfg.KimiK31Candidate = true
	cfg.RuntimePermissionsEnabled = true
	cfg.PermissionVerificationBaseURL = "http://127.0.0.1:19001/v1"
	args, e := NewManager(cfg, nil).kimiK31MeterArguments()
	if e != nil || len(args) != 12 {
		t.Fatal(args, e)
	}
	joined := strings.Join(args, "\n")
	if strings.Count(joined, "http://127.0.0.1:19001/v1") != 2 {
		t.Fatal("both provider URLs must use the exact meter")
	}
	for _, provider := range []string{"kimi", "minimax"} {
		for _, expected := range []string{"model_providers." + provider + ".base_url=", "model_providers." + provider + ".request_max_retries=0", "model_providers." + provider + ".stream_max_retries=0"} {
			if !strings.Contains(joined, expected) {
				t.Fatal("meter layer missing", expected)
			}
		}
	}
	cfg.PermissionVerificationBaseURL = "https://example.invalid/v1"
	if _, e = NewManager(cfg, nil).kimiK31MeterArguments(); e == nil {
		t.Fatal("non-loopback meter admitted")
	}
	cfg.KimiK31Candidate = false
	if args, e = NewManager(cfg, nil).kimiK31MeterArguments(); e != nil || len(args) != 0 {
		t.Fatal("ordinary profile changed")
	}
	cfg.KimiK31Candidate = true
	cfg.PermissionVerificationBaseURL = ""
	if args, e = NewManager(cfg, nil).kimiK31MeterArguments(); e != nil || len(args) != 0 {
		t.Fatal("non-verification profile changed")
	}
}
