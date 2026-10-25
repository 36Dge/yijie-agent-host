package codex

import (
	"errors"
	"fmt"
)

// Apply the already-selected local meter at the process provider layer as well
// as thread overrides. Native children, Guardian and compaction cannot fall
// back to a direct Provider URL. The public env loader remains fixed at18083.
func (m *Manager) kimiK31MeterArguments() ([]string, error) {
	if !m.config.KimiK31Candidate || m.config.PermissionVerificationBaseURL == "" {
		return nil, nil
	}
	if !m.config.RuntimePermissionsEnabled || !draftVerificationLoopback(m.config.PermissionVerificationBaseURL) {
		return nil, errors.New("candidate meter requires the local verification route")
	}
	content := ""
	for _, provider := range []string{"kimi", "minimax"} {
		content += fmt.Sprintf("[model_providers.%s]\nbase_url = %q\nrequest_max_retries = 0\nstream_max_retries = 0\n", provider, m.config.PermissionVerificationBaseURL)
	}
	return nativeConfigArguments([]byte(content))
}
