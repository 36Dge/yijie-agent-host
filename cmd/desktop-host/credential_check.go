package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/36Dge/yijie-agent-host/internal/imagegen"
)

// Operator-only local diagnostic, using the real Host executable identity.
// No Runtime, provider HTTP, database, key write or unlock. Only an explicitly
// invoked attended authorize action requests system UI; check never does.
// A separate test executable cannot qualify the Host's Keychain access rights.
func checkMediaCredential(authorize bool) int {
	home := os.Getenv("YIJIE_AGENT_HOST_HOME")
	canonical, err := filepath.EvalSymlinks(home)
	info, statErr := os.Stat(home)
	if os.Getenv("YIJIE_ENV") != "local" || os.Getenv("YIJIE_LOCAL_PROFILE") != "demo_fast" || !filepath.IsAbs(home) || err != nil || canonical != home || statErr != nil || !info.IsDir() {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"ready": false, "code": "invalid_local_scope"})
		return 2
	}
	lookup := imagegen.ArkCredential(home)
	if authorize {
		input, err := os.Stdin.Stat()
		if err != nil || input.Mode()&os.ModeCharDevice == 0 {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"ready": false, "code": "attended_operator_required"})
			return 2
		}
		lookup = imagegen.OperatorArkCredential(home)
	}
	key, err := lookup(context.Background())
	ready := err == nil && key != "" && strings.TrimSpace(key) == key && !strings.ContainsAny(key, "\x00\r\n")
	key = ""
	code := "credential_unavailable"
	if ready {
		code = "ready"
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"ready": ready, "code": code, "provider_requests": 0})
	if !ready {
		return 1
	}
	return 0
}
