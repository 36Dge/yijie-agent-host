package codex

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFEAT164ActivationAndRetiredAdmission(t *testing.T) {
	t.Run("frozen_predecessors_are_the_pinned_legacy_bytes", func(t *testing.T) {
		current, early, config := frozenKimiK31Predecessor("/ordinary-home")
		want, _ := chatModelCatalogVersion(false)
		old, _ := chatModelCatalogVersion(true)
		if !bytes.Equal(current, want) || !bytes.Equal(early, old) || !bytes.Equal(config, chatModelConfig("/ordinary-home")) {
			t.Fatal("legacy source changed; review frozen predecessor compatibility explicitly")
		}
	})
	for _, early := range []bool{false, true} {
		name := "current_predecessor"
		if early {
			name = "early_predecessor"
		}
		t.Run(name, func(t *testing.T) {
			home := canonicalOwnedTempDir(t)
			a := acquireTestCodexHomeAuthority(t, home)
			current, previous, config := frozenKimiK31Predecessor(home)
			if early {
				current = previous
			}
			writeS1File(t, home, chatModelsCatalogName, current)
			writeS1File(t, home, chatModelsConfigName, config)
			trust := []byte("# ordinary preserved user configuration\n[projects.\"/ordinary-workspace\"]\ntrust_level = \"trusted\"\n")
			writeS1File(t, home, "config.toml", trust)
			if _, err := prepareKimiK31Layer(a); err == nil {
				t.Fatal("implicit predecessor activation")
			}
			for i := 0; i < 2; i++ {
				if _, err := prepareKimiK31LayerWithPredecessor(a, true); err != nil {
					t.Fatal(err)
				}
			}
			for file, want := range map[string][]byte{chatModelsCatalogName: current, chatModelsConfigName: config, "config.toml": trust} {
				got, err := os.ReadFile(filepath.Join(home, file))
				if err != nil || !bytes.Equal(got, want) {
					t.Fatal("predecessor/trust changed", file, err)
				}
			}
			m := NewManager(Config{ChatModelsEnabled: true, KimiK31Candidate: true, KimiK31AllowManagedPredecessor: true}, nil)
			if err := m.validateSelectedChatModelLayer(a); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("reachable_pair_finishes_before_runtime", func(t *testing.T) {
		home := canonicalOwnedTempDir(t)
		a := acquireTestCodexHomeAuthority(t, home)
		catalog, err := kimiK31Catalog()
		if err != nil {
			t.Fatal(err)
		}
		writeS1File(t, home, kimiK31CatalogName, catalog)
		m := NewManager(Config{KimiK31Candidate: true}, nil)
		if m.validateSelectedChatModelLayer(a) == nil {
			t.Fatal("partial pair was ready")
		}
		if _, err = prepareKimiK31Layer(a); err != nil {
			t.Fatal(err)
		}
		if err = m.validateSelectedChatModelLayer(a); err != nil {
			t.Fatal(err)
		}
	})
	for _, which := range []string{"legacy_catalog_only", "legacy_config_only", "unknown_legacy", "candidate_config_only", "unknown_candidate"} {
		t.Run(which, func(t *testing.T) {
			home := canonicalOwnedTempDir(t)
			a := acquireTestCodexHomeAuthority(t, home)
			catalog, _, config := frozenKimiK31Predecessor(home)
			switch which {
			case "legacy_catalog_only":
				writeS1File(t, home, chatModelsCatalogName, catalog)
			case "legacy_config_only":
				writeS1File(t, home, chatModelsConfigName, config)
			case "unknown_legacy":
				writeS1File(t, home, chatModelsCatalogName, append(catalog, '\n'))
				writeS1File(t, home, chatModelsConfigName, config)
			case "candidate_config_only":
				writeS1File(t, home, kimiK31ConfigName, kimiK31Config(home))
			case "unknown_candidate":
				writeS1File(t, home, kimiK31CatalogName, []byte(`{"models":[]}`))
			}
			before := s1Files(t, home)
			if _, err := prepareKimiK31LayerWithPredecessor(a, true); err == nil {
				t.Fatal("unknown combination admitted")
			}
			after := s1Files(t, home)
			if len(before) != len(after) {
				t.Fatal("rejection wrote new files")
			}
			for k, v := range before {
				if !bytes.Equal(v, after[k]) {
					t.Fatal("rejection changed file", k)
				}
			}
		})
	}
	t.Run("active_identity_and_same_generation_receipt", func(t *testing.T) {
		c := DefaultConfig()
		c.ChatModelsEnabled = true
		c.KimiK31Candidate = true
		c.MiniMax.Enabled = true
		c.KimiAPIKey = "ordinary-fixture"
		m := NewManager(c, nil)
		ctx := WithModelProfile(context.Background(), KimiK31ProfileID)
		for _, id := range []string{"", "kimi-k3-max-v1", "unknown"} {
			bad := WithModelProfile(context.Background(), id)
			if _, err := m.modelForContext(bad); err == nil {
				t.Fatal("nonactive identity admitted", id)
			}
			if _, err := m.StartTurn(bad, "thread", "ordinary input", "high"); err == nil {
				t.Fatal("legacy entry bypass")
			}
			if _, err := m.StartTurnV2(bad, "thread", nil, "max"); err == nil {
				t.Fatal("V2 entry bypass")
			}
		}
		if m.admitKimiK31Turn(ctx, "thread", "max") == nil {
			t.Fatal("unconfirmed thread admitted")
		}
		m.bindKimiK31Thread(ctx, ThreadInfo{ID: "thread"})
		if err := m.admitKimiK31Turn(ctx, "thread", "max"); err != nil {
			t.Fatal(err)
		}
		if m.admitKimiK31Turn(WithModelProfile(context.Background(), "minimax-m3-high-v1"), "thread", "high") == nil {
			t.Fatal("wrong model receipt admitted")
		}
		for _, effort := range []string{"", "none", "high"} {
			if m.admitKimiK31Turn(ctx, "thread", effort) == nil {
				t.Fatal("candidate reasoning effort override admitted", effort)
			}
		}
		m.forgetKimiK31Thread("thread")
		if m.admitKimiK31Turn(ctx, "thread", "max") == nil {
			t.Fatal("stale receipt admitted")
		}
		c.KimiK31ExecutionPaused = true
		paused := NewManager(c, nil)
		if paused.ModelAvailable(KimiK31ProfileID) || paused.ModelAvailable("kimi-k3-max-v1") || !paused.ModelAvailable("minimax-m3-high-v1") || !paused.ChatModelsV2Enabled() {
			t.Fatal("pause disabled reads/MiniMax or retained Kimi execution")
		}
	})
}

func writeS1File(t *testing.T, home, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, name), data, 0600); err != nil {
		t.Fatal(err)
	}
}
func s1Files(t *testing.T, home string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			b, err := os.ReadFile(filepath.Join(home, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			out[e.Name()] = b
		}
	}
	return out
}
