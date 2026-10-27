package codex

import (
	"context"
	"embed"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
)

// Frozen output from Host 5022435, not reconstructed using current Definitions.
//
//go:embed kimi_k31_predecessors/v1-current.json kimi_k31_predecessors/v1-early.json kimi_k31_predecessors/v1-feat163-reviewed.json kimi_k31_predecessors/v1.toml.tpl
var kimiK31Predecessors embed.FS

func frozenKimiK31Predecessor(home string) (current, early, config []byte) {
	current, _ = kimiK31Predecessors.ReadFile("kimi_k31_predecessors/v1-current.json")
	early, _ = kimiK31Predecessors.ReadFile("kimi_k31_predecessors/v1-early.json")
	template, _ := kimiK31Predecessors.ReadFile("kimi_k31_predecessors/v1.toml.tpl")
	config = []byte(strings.Replace(string(template), "{{CATALOG_PATH}}", strconv.Quote(filepath.Join(home, chatModelsCatalogName)), 1))
	return
}

// The old pair remains immutable. This grants only managed-file preparation,
// never database migration, thread selection, or permission to send a turn.
func validateKimiK31Predecessor(a *managedCodexHomeAuthority, allow bool) error {
	current, early, config := frozenKimiK31Predecessor(a.path)
	// Exact retained local producer, reviewed in FEAT-164 report62/63. This is
	// compatibility admission only; neither legacy files nor active prompts change.
	reviewed, _ := kimiK31Predecessors.ReadFile("kimi_k31_predecessors/v1-feat163-reviewed.json")
	cp, err := a.preflightManagedFile(chatModelsCatalogName, current, early, reviewed)
	if err != nil {
		return err
	}
	fp, err := a.preflightManagedFile(chatModelsConfigName, config)
	if err != nil {
		return err
	}
	if cp.existed != fp.existed {
		return errors.New("incomplete managed model predecessor")
	}
	if cp.existed && !allow {
		return errors.New("managed model predecessor requires explicit activation review")
	}
	return nil
}

func (m *Manager) ChatModelActivationRequired(id string) bool {
	return m.ChatModelsV2Enabled() && id == KimiK31ProfileID && m.config.KimiK31ExecutionPaused
}

// Only complete model receipts from this Manager generation admit a turn.
// threadOperation serializes this cache with start/resume/turn/switch. It is not
// persistent model truth, and thread/read never populates it.
func (m *Manager) bindKimiK31Thread(ctx context.Context, info ThreadInfo) {
	if !m.config.KimiK31Candidate {
		return
	}
	if m.kimiK31ThreadProfiles == nil {
		m.kimiK31ThreadProfiles = make(map[string]string)
	}
	m.kimiK31ThreadProfiles[info.ID] = ModelProfileID(ctx)
}

func (m *Manager) forgetKimiK31Thread(thread string) {
	if m.config.KimiK31Candidate {
		delete(m.kimiK31ThreadProfiles, thread)
	}
}

func (m *Manager) admitKimiK31Turn(ctx context.Context, thread, effort string) error {
	if !m.config.KimiK31Candidate {
		return nil
	}
	id := ModelProfileID(ctx)
	profile, known := ModelProfile(id)
	if id == "" || !known || !m.ModelAvailable(id) || m.kimiK31ThreadProfiles[thread] != id || effort != profile.Effort {
		return errors.New("candidate turn requires a current confirmed active model receipt")
	}
	return nil
}
