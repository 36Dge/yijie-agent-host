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

func (s *Service) readContextWindowRecord(ctx context.Context, sessionID string) (Record, error) {
	runtime, ok := s.runtime.(interface {
		ReadContextUsage(context.Context, string) (*codex.NativeContextUsage, error)
	})
	status, reportsStatus := s.runtime.(interface{ Snapshot() codex.Status })
	ready := !reportsStatus || status.Snapshot().Ready
	s.contextWindowMu.Lock()
	// Read the binding under the same mutex as notification projection. An
	// earlier poll must never rebind an entry after a later model has reported.
	record, err := s.store.Get(sessionID)
	if err != nil || !ready {
		s.contextWindowMu.Unlock()
		if err != nil {
			return Record{}, err
		}
		return Record{}, ErrRuntimeRequest
	}
	entry := s.contextObservation(record)
	idleWithLast := entry.lastObservation != nil && record.ActiveTurnID == "" && entry.lastObservation.Sample.TurnId == record.LastTurnID
	if !ok || entry.sample != nil || idleWithLast || entry.restoreBlocked || entry.restoreInFlight || time.Now().Before(entry.restoreAfter) {
		s.contextWindowMu.Unlock()
		return record, nil
	}
	entry.restoreInFlight = true
	epoch, revision := entry.epoch, entry.observationRevision
	s.contextWindowMu.Unlock()
	value, readErr := runtime.ReadContextUsage(ctx, record.CodexThreadID)
	current, storeErr := s.store.Get(record.AgentSessionID)
	var verified, currentBinding bool
	if readErr == nil && storeErr == nil && value != nil && current.CodexThreadID == record.CodexThreadID && current.ModelRevision == record.ModelRevision && recordProfile(current) == recordProfile(record) && current.RuntimeSessionID == record.RuntimeSessionID && current.ModelSelectionState == record.ModelSelectionState {
		verified, currentBinding = s.contextUsageBinding(current, value)
	}
	s.contextWindowMu.Lock()
	defer s.contextWindowMu.Unlock()
	entry.restoreInFlight = false
	if s.contextWindows[record.CodexThreadID] != entry || entry.epoch != epoch || entry.observationRevision != revision || entry.restoreBlocked || entry.sample != nil {
		return record, nil
	}
	entry.restoreAfter = time.Now().Add(5 * time.Second)
	if !verified {
		return record, nil
	}
	entry.lastUsage = &value.Usage
	source := "model:" + value.Model
	if currentBinding {
		source = strconv.FormatInt(current.ModelRevision, 10)
	}
	windowID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(value.ThreadID+":"+value.WindowKey+":"+source))
	sample := wire.ContextWindowSample{UsedTokens: value.Usage.Last.TotalTokens, CapacityTokens: value.Usage.ModelContextWindow, TurnId: value.TurnID, WindowId: windowID, ObservedAt: value.ObservedAt}
	if currentBinding {
		entry.epoch, entry.sample, entry.lastObservation = windowID, &sample, nil
	} else {
		entry.lastObservation = &wire.ContextWindowObservation{Model: value.Model, Sample: sample}
	}
	return record, nil
}

// Thread/turn ownership permits a labelled observation. An unlabelled current
// sample additionally needs the exact active model binding that produced it.
func (s *Service) contextUsageBinding(record Record, value *codex.NativeContextUsage) (verified, current bool) {
	if value.ThreadID != record.CodexThreadID || !validContextID(value.TurnID) || value.Model == "" || len(value.Model) > 128 {
		return false, false
	}
	latest := value.TurnID == record.LastTurnID || value.TurnID == record.ActiveTurnID
	profile, ok := codex.ModelProfile(recordProfile(record))
	currentModel := latest && ok && profile.Model == value.Model && (record.ModelSelectionState == "" || record.ModelSelectionState == "ready")
	// Older unswitched sessions have no revision stamp. Their initial binding,
	// native measured turn and native model still provide an unambiguous match.
	if currentModel && record.ModelRevision <= 1 && record.ModelSelectionOperation == "" {
		return true, true
	}
	verified = latest
	err := s.store.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(turnOperationsBucket)
		prefix := append([]byte(record.AgentSessionID), 0)
		cursor := bucket.Cursor()
		for key, raw := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, raw = cursor.Next() {
			var op TurnOperation
			if json.Unmarshal(raw, &op) != nil {
				return ErrModelUnknown
			}
			if op.State != TurnOperationStateAccepted || op.SessionID != record.AgentSessionID || op.TurnID != value.TurnID {
				continue
			}
			if op.ModelProfile != "" {
				measured, ok := codex.ModelProfile(op.ModelProfile)
				if !ok || measured.Model != value.Model {
					return ErrModelUnknown
				}
			}
			verified = true
			current = currentModel && op.ModelProfile == recordProfile(record) && op.ContextModelRevision == record.ModelRevision
			break
		}
		return nil
	})
	return err == nil && verified, err == nil && current
}
