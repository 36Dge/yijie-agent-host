package session

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/contextwindow"
	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

func (s *Service) restoreContextWindow(ctx context.Context, record Record) {
	runtime, ok := s.runtime.(interface {
		ReadContextUsage(context.Context, string) (*codex.NativeContextUsage, error)
	})
	if !ok {
		return
	}
	s.contextWindowMu.Lock()
	entry := s.contextObservation(record)
	if entry.sample != nil || entry.restoreBlocked || entry.restoreInFlight || time.Now().Before(entry.restoreAfter) {
		s.contextWindowMu.Unlock()
		return
	}
	entry.restoreInFlight = true
	epoch, revision := entry.epoch, entry.observationRevision
	s.contextWindowMu.Unlock()
	value, readErr := runtime.ReadContextUsage(ctx, record.CodexThreadID)
	current, storeErr := s.store.Get(record.AgentSessionID)
	valid := readErr == nil && storeErr == nil && value != nil && current.CodexThreadID == record.CodexThreadID && current.ModelRevision == record.ModelRevision && recordProfile(current) == recordProfile(record) && current.RuntimeSessionID == record.RuntimeSessionID
	if valid {
		valid = s.contextUsageMatchesRecord(current, value)
	}
	s.contextWindowMu.Lock()
	defer s.contextWindowMu.Unlock()
	entry.restoreInFlight = false
	entry.restoreAfter = time.Now().Add(5 * time.Second)
	if !valid || s.contextWindows[record.CodexThreadID] != entry || entry.epoch != epoch || entry.observationRevision != revision || entry.restoreBlocked || entry.sample != nil {
		return
	}
	entry.lastUsage = &value.Usage
	entry.epoch = uuid.NewSHA1(uuid.NameSpaceOID, []byte(value.ThreadID+":"+value.WindowKey+":"+strconv.FormatInt(current.ModelRevision, 10)))
	entry.sample = &wire.ContextWindowSample{UsedTokens: value.Usage.Last.TotalTokens, CapacityTokens: value.Usage.ModelContextWindow, TurnId: value.TurnID, WindowId: entry.epoch, ObservedAt: value.ObservedAt}
}

func (s *Service) contextUsageMatchesRecord(record Record, value *codex.NativeContextUsage) bool {
	if value.ThreadID != record.CodexThreadID || (value.TurnID != record.LastTurnID && value.TurnID != record.ActiveTurnID) || (record.ModelSelectionState != "" && record.ModelSelectionState != "ready") {
		return false
	}
	profile, ok := codex.ModelProfile(recordProfile(record))
	if !ok || profile.Model != value.Model {
		return false
	}
	// Older unswitched sessions have no revision stamp. Their initial binding,
	// native measured turn and native model still provide an unambiguous match.
	if record.ModelRevision <= 1 && record.ModelSelectionOperation == "" {
		return true
	}
	matched := false
	err := s.store.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(turnOperationsBucket)
		prefix := append([]byte(record.AgentSessionID), 0)
		cursor := bucket.Cursor()
		for key, raw := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, raw = cursor.Next() {
			var op TurnOperation
			if json.Unmarshal(raw, &op) != nil {
				return ErrModelUnknown
			}
			if op.State == TurnOperationStateAccepted && op.TurnID == value.TurnID && op.ModelProfile == recordProfile(record) && op.ContextModelRevision == record.ModelRevision {
				matched = true
			}
		}
		return nil
	})
	return err == nil && matched
}
