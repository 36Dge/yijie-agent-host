package codex

import (
	"encoding/json"
	"errors"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/runtimedocuments"
	"github.com/pelletier/go-toml/v2"
	"os"
	"path/filepath"
)

func documentRuntimeArguments() ([]string, error) {
	var profile struct {
		Config map[string]any `json:"native_config"`
	}
	if json.Unmarshal(native.Profile, &profile) != nil || profile.Config["exec_shutdown_policy"] != "cooperative_only" {
		return nil, errors.New("document Runtime configuration unavailable")
	}
	if values := documentShellEnvironment(); len(values) > 0 {
		profile.Config["shell_environment_policy"] = map[string]any{"set": values}
	}
	bytes, err := toml.Marshal(profile.Config)
	if err != nil {
		return nil, err
	}
	return nativeConfigArguments(bytes)
}
func documentShellEnvironment() map[string]string {
	root := os.Getenv("YIJIE_DOCUMENT_STATE_ROOT")
	canonical, err := filepath.EvalSymlinks(root)
	info, statErr := os.Stat(root)
	if err != nil || statErr != nil || !filepath.IsAbs(root) || canonical != root || !info.IsDir() || info.Mode().Perm() != 0700 {
		return map[string]string{}
	}
	return map[string]string{"YIJIE_DOCUMENT_STATE_ROOT": root, "PYTHONDONTWRITEBYTECODE": "1"}
}
func (m *Manager) documentInstructions() string {
	if !m.config.DocumentRuntimeEnabled {
		return ""
	}
	return " For document/data file tasks (TXT, Markdown, HTML, CSV, JSON, DOCX, PPTX, XLSX, PDF), use the built-in load_document_environment tool first and read the relevant bundled guide. Use its application-owned interpreter/helpers; do not fall back to system programs, install packages, or require a market expert/skill/connector. Generate and validate a new version inside the workspace, use native view_image for rendered-page QA, then publish_artifact. staged is not durable delivery: only get_artifact_status committed confirms the Native ACK in the current generation; unknown never authorizes replay. Preserve user originals and distinguish generation, chat delivery and external saving. Document tools do not expand native permissions."
}
