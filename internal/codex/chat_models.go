package codex

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"

	models "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodels"
)

const KimiRuntimeEnvKey = "KIMI_API_KEY"
const chatModelsCatalogName = "yijie-chat-models-v1.json"
const chatModelsConfigName = "yijie-chat-models-v1.toml"

type modelProfileKey struct{}

func WithModelProfile(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, modelProfileKey{}, id)
}
func ModelProfileID(ctx context.Context) string {
	v, _ := ctx.Value(modelProfileKey{}).(string)
	return v
}
func ModelProfile(id string) (models.ModelDefinition, bool) {
	for _, p := range models.Definitions() {
		if string(p.ProfileId) == id {
			return p, true
		}
	}
	return models.ModelDefinition{}, false
}
func (m *Manager) ChatModelsEnabled() bool { return m.config.ChatModelsEnabled }
func (m *Manager) ModelAvailable(id string) bool {
	p, ok := ModelProfile(id)
	if !ok || !m.config.ChatModelsEnabled {
		return false
	}
	if p.Provider == "kimi" {
		return m.config.KimiAPIKey != ""
	}
	return m.config.MiniMax.Enabled
}
func (m *Manager) modelForContext(ctx context.Context) (models.ModelDefinition, error) {
	id := ModelProfileID(ctx)
	if id == "" {
		p, _ := ModelProfile("minimax-m3-high-v1")
		return p, nil
	}
	p, ok := ModelProfile(id)
	if !ok || !m.ModelAvailable(id) {
		return p, errors.New("selected model is not configured")
	}
	return p, nil
}
func applyModelConfig(config map[string]any, p models.ModelDefinition) map[string]any {
	if config == nil {
		config = make(map[string]any)
	}
	config["model_reasoning_effort"] = p.Effort
	config["model_context_window"] = p.ContextWindow
	return config
}
func chatModelCatalog() ([]byte, error) { return chatModelCatalogVersion(false) }
func chatModelCatalogVersion(earlyCandidate bool) ([]byte, error) {
	b, e := miniMaxModelCatalog()
	if e != nil {
		return nil, e
	}
	var catalog map[string]any
	if e = json.Unmarshal(b, &catalog); e != nil {
		return nil, e
	}
	baseline := catalog["models"].([]any)[0].(map[string]any)
	all := make([]any, 0, 2)
	for _, p := range models.Definitions() {
		entry := make(map[string]any, len(baseline))
		for k, v := range baseline {
			entry[k] = v
		}
		entry["slug"] = p.Model
		entry["display_name"] = p.Label
		entry["description"] = p.Label + " managed Responses profile"
		entry["default_reasoning_level"] = p.Effort
		if p.Provider == "kimi" || earlyCandidate {
			entry["supported_reasoning_levels"] = []any{map[string]any{"effort": p.Effort, "description": p.Effort}}
		}
		entry["context_window"] = p.ContextWindow
		entry["max_context_window"] = p.ContextWindow
		if p.Provider == "kimi" || earlyCandidate {
			entry["base_instructions"] = "You are Codex. Follow the current task instructions and native permission policy."
		}
		all = append(all, entry)
	}
	catalog["models"] = all
	return json.MarshalIndent(catalog, "", "  ")
}
func chatModelConfig(home string) []byte {
	return []byte("# Managed by yijie-agent-host FEAT-156.\n" +
		"model = \"kimi-k3\"\nmodel_provider = \"kimi\"\nmodel_reasoning_effort = \"max\"\nmodel_reasoning_summary = \"none\"\n" +
		"model_catalog_json = " + strconv.Quote(filepath.Join(home, chatModelsCatalogName)) + "\n" +
		"[model_providers.kimi]\nname = \"Kimi\"\nbase_url = \"https://api.moonshot.cn/v1\"\nenv_key = \"KIMI_API_KEY\"\nwire_api = \"responses\"\nrequires_openai_auth = false\nsupports_websockets = false\nrequest_max_retries = 0\nstream_max_retries = 0\n" +
		"[shell_environment_policy]\ninherit = \"core\"\nexclude = [\"YIJIE_FEAT144_SORFTIME_ACCOUNT_SK\", \"YIJIE_FEAT144_SORFTIME_ENABLED\", \"MINIMAX_API_KEY\", \"KIMI_API_KEY\", \"MOONSHOT_API_KEY\", \"YIJIE_KIMI_API_KEY\", \"YIJIE_KIMI_API_KEY_FILE\"]\nset = {}\n")
}
func prepareChatModelLayer(a *managedCodexHomeAuthority) ([]string, error) {
	catalog, e := chatModelCatalog()
	if e != nil {
		return nil, e
	}
	config := chatModelConfig(a.path)
	previous, e := chatModelCatalogVersion(true)
	if e != nil {
		return nil, e
	}
	cp, e := a.preflightManagedFile(chatModelsCatalogName, catalog, previous)
	if e != nil {
		return nil, e
	}
	fp, e := a.preflightManagedFile(chatModelsConfigName, config, config)
	if e != nil {
		return nil, e
	}
	for _, plan := range []managedFilePlan{cp, fp} {
		if e = a.applyManagedFile(plan); e != nil {
			return nil, e
		}
	}
	return nativeConfigArguments(config)
}
func validateChatModelLayer(a *managedCodexHomeAuthority) error {
	b, e := chatModelCatalog()
	if e != nil {
		return e
	}
	if e = a.validateManagedFileExact(chatModelsCatalogName, b); e != nil {
		return e
	}
	return a.validateManagedFileExact(chatModelsConfigName, chatModelConfig(a.path))
}

// SwitchThread uses only the fixed native lifecycle. It never sends a prompt,
// forks a thread, kills a process or rewrites native history.
func (m *Manager) SwitchThread(ctx context.Context, threadID string) (ThreadInfo, error) {
	m.sorftime.operation.Lock()
	defer m.sorftime.operation.Unlock()
	if !m.ModelAvailable(ModelProfileID(ctx)) {
		return ThreadInfo{}, errors.New("selected model is unavailable")
	}
	state, err := m.ReadThreadStatus(ctx, threadID)
	// The pinned native tracker reports systemError only after running and
	// pending approval/input facts have cleared. It is a terminal status that
	// may be resumed normally; active and unknown statuses still fail closed.
	if err != nil || (state != "idle" && state != "notLoaded" && state != "systemError") {
		return ThreadInfo{}, errors.New("native thread is not idle")
	}
	pending := false
	for _, a := range m.ListRuntimeApprovals(threadID) {
		if a.Status == "pending" {
			pending = true
		}
	}
	if pending {
		return ThreadInfo{}, errors.New("native thread has an approval")
	}
	if err = m.request(ctx, RuntimeMethodThreadUnsubscribe, map[string]any{"threadId": threadID}, &struct{}{}); err != nil {
		return ThreadInfo{}, err
	}
	if isScheduledDraft(ctx) {
		var response struct {
			Thread threadWire `json:"thread"`
		}
		if err = m.request(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": false}, &response); err != nil {
			return ThreadInfo{}, err
		}
		return m.startDraftThreadLocked(ctx, response.Thread.Cwd, threadID)
	}
	return m.resumeThread(ctx, threadID)
}
