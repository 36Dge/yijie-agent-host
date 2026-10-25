package session

import (
	"context"
	"errors"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/google/uuid"
	"testing"
)

func TestFEAT164TitleRequiresConfirmedActiveSelection(t *testing.T) {
	s, _ := modelService(t)
	rt := &modelV2TestRuntime{ready: true, paused: true}
	s.runtime = rt
	g := &fakeTitleGenerator{title: "普通本机标题"}
	s.titleGenerator = g
	for _, profile := range []string{"", "kimi-k3-max-v1", codex.KimiK31ProfileID} {
		_, err := s.store.update(testSessionID, func(r *Record) error { r.ModelProfile = profile; r.ModelSelectionState = "ready"; return nil })
		if err != nil {
			t.Fatal(err)
		}
		before, _ := s.store.Get(testSessionID)
		// An incoming new identity must not relabel an unclassified old title.
		_, err = s.GenerateTitle(codex.WithModelProfile(context.Background(), codex.KimiK31ProfileID), testSessionID, uuid.NewString(), "ordinary text")
		if !errors.Is(err, ErrTitleUnavailable) || g.calls != 0 || len(s.titleOperations) != 0 {
			t.Fatal("title execution escaped admission", profile, err)
		}
		after, _ := s.store.Get(testSessionID)
		if before != after {
			t.Fatal("blocked title changed model history")
		}
	}
	for _, profile := range []string{"minimax-m3-high-v1", codex.KimiK31ProfileID} {
		rt.paused = profile == "minimax-m3-high-v1"
		_, err := s.store.update(testSessionID, func(r *Record) error { r.ModelProfile = profile; r.ModelSelectionState = "ready"; return nil })
		if err != nil {
			t.Fatal(err)
		}
		op := uuid.NewString()
		result, err := s.GenerateTitle(context.Background(), testSessionID, op, "ordinary active title")
		if err != nil {
			t.Fatal(err)
		}
		calls := g.calls
		rt.paused = true
		replay, err := s.GenerateTitle(context.Background(), testSessionID, op, "ordinary active title")
		if err != nil || replay != result || g.calls != calls {
			t.Fatal("completed title replay attempted execution", err)
		}
	}
}
