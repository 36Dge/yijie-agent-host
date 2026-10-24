package session

import (
	"context"
	"errors"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodelsv2"
)

type modelV2TestRuntime struct {
	modelTestRuntime
	ready bool
}

func (*modelV2TestRuntime) ChatModelsV2Enabled() bool { return true }
func (r *modelV2TestRuntime) ChatModelsV2Ready() bool { return r.ready }
func (*modelV2TestRuntime) ModelAvailable(id string) bool {
	return id == codex.KimiK31ProfileID || id == "minimax-m3-high-v1"
}
func TestFEAT164RetiredReadPreservesApplicationFacts(t *testing.T) {
	s, _ := modelService(t)
	s.runtime = &modelV2TestRuntime{ready: true}
	if _, err := s.store.update(testSessionID, func(r *Record) error {
		r.ModelProfile = "kimi-k3-max-v1"
		r.ModelSelectionState = "ready"
		r.ModelRevision = 3
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	value, err := s.ReadChatModelV2(testSessionID)
	if err != nil || value.Availability != wire.SelectionAvailabilityRetired || value.State != wire.SelectionStateReady || value.Revision != 3 {
		t.Fatal(value, err)
	}
	before, _ := s.store.Get(testSessionID)
	if err = s.checkModelRequest(codex.WithModelProfile(context.Background(), "kimi-k3-max-v1"), before); !errors.Is(err, ErrModelUnavailable) {
		t.Fatal("retired execution was admitted", err)
	}
	if err = s.checkModelRequest(context.Background(), Record{}); !errors.Is(err, ErrModelUnavailable) {
		t.Fatal("legacy headerless start bypassed candidate admission")
	}
	after, _ := s.store.Get(testSessionID)
	if before != after {
		t.Fatal("read changed durable model facts")
	}
	if wire.Validate("Catalog", s.ChatModelCatalogV2()) != nil {
		t.Fatal("catalog not conformant")
	}
}

func TestFEAT164ModelCatalogDoesNotMistakeConfiguredForRuntimeReady(t *testing.T) {
	s, _ := modelService(t)
	rt := &modelV2TestRuntime{}
	s.runtime = rt
	for _, entry := range s.ChatModelCatalogV2().Models {
		if entry.Available || entry.Reason != wire.ModelAvailabilityReasonRuntimeUnavailable {
			t.Fatal("unready Runtime advertised as ready")
		}
	}
	rt.ready = true
	for _, entry := range s.ChatModelCatalogV2().Models {
		if !entry.Available || entry.Reason != wire.ModelAvailabilityReasonReady {
			t.Fatal("ready Runtime not available")
		}
	}
}
