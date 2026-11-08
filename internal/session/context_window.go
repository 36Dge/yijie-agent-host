package session

import (
	"context"
	"encoding/json"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/contextwindow"
	"github.com/google/uuid"
)

const tokenUsageNotification = "thread/tokenUsage/updated"
const maxContextObservations = 512

// Disposable display observation only. Never used to admit, compact, reset,
// budget or execute a turn. No prompt/content, credential or billing retention.
type contextObservation struct {
	profile, runtimeSession, turn   string
	revision                        int64
	epoch                           uuid.UUID
	sample                          *wire.ContextWindowSample
	compactions                     []wire.ContextCompaction
	compacting                      string
	modelItemCompleted              bool
	lastUsage                       *wire.ThreadTokenUsage
	touched                         time.Time
	restoreBlocked, restoreInFlight bool
	restoreAfter                    time.Time
	observationRevision             uint64
}

func (s *Service) contextObservation(record Record) *contextObservation {
	if s.contextWindows == nil {
		s.contextWindows = make(map[string]*contextObservation)
	}
	entry := s.contextWindows[record.CodexThreadID]
	if entry == nil {
		if len(s.contextWindows) >= maxContextObservations {
			var oldest string
			for id, value := range s.contextWindows {
				if oldest == "" || value.touched.Before(s.contextWindows[oldest].touched) {
					oldest = id
				}
			}
			delete(s.contextWindows, oldest)
		}
		entry = &contextObservation{epoch: uuid.New(), compactions: []wire.ContextCompaction{}, profile: recordProfile(record), revision: record.ModelRevision, runtimeSession: record.RuntimeSessionID}
		s.contextWindows[record.CodexThreadID] = entry
	}
	if entry.profile != recordProfile(record) || entry.revision != record.ModelRevision || entry.runtimeSession != record.RuntimeSessionID || (record.ModelSelectionState != "" && record.ModelSelectionState != "ready") {
		entry.sample, entry.turn, entry.compacting, entry.modelItemCompleted = nil, "", "", false
		entry.epoch = uuid.New()
		entry.restoreBlocked = true
		entry.profile, entry.revision, entry.runtimeSession = recordProfile(record), record.ModelRevision, record.RuntimeSessionID
	}
	entry.touched = time.Now()
	return entry
}

func validContextID(id string) bool { return id != "" && len(id) <= 512 }

func (s *Service) observeContextNotification(method string, raw json.RawMessage) {
	if method != tokenUsageNotification && method != RuntimeNotificationTurnStarted && method != RuntimeNotificationTurnCompleted && method != RuntimeNotificationItemStarted && method != RuntimeNotificationItemCompleted {
		return
	}
	var ids struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Turn     struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
		Item struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"item"`
	}
	if json.Unmarshal(raw, &ids) != nil {
		return
	}
	record, err := s.store.GetByThread(ids.ThreadID)
	if err != nil {
		return
	}
	s.contextWindowMu.Lock()
	defer s.contextWindowMu.Unlock()
	entry := s.contextObservation(record)
	entry.observationRevision++
	if record.ModelSelectionState != "" && record.ModelSelectionState != "ready" {
		return
	}
	if method == RuntimeNotificationTurnStarted {
		if validContextID(ids.Turn.ID) {
			entry.turn = ids.Turn.ID
			entry.modelItemCompleted = false
		}
		return
	}
	if method == RuntimeNotificationTurnCompleted {
		for i := range entry.compactions {
			if entry.compactions[i].TurnId == ids.Turn.ID && entry.compactions[i].State == wire.InProgress {
				entry.compactions[i].State = wire.Unknown
				if ids.Turn.Status == "failed" || ids.Turn.Status == "interrupted" {
					entry.compactions[i].State = wire.Interrupted
				}
				entry.sample = nil
			}
		}
		entry.compacting, entry.modelItemCompleted = "", false
		return
	}
	if !validContextID(ids.TurnID) {
		return
	}
	if method == RuntimeNotificationItemStarted || method == RuntimeNotificationItemCompleted {
		if ids.Item.Type == "contextCompaction" && validContextID(ids.Item.ID) {
			index := -1
			for i, item := range entry.compactions {
				if item.ItemId == ids.Item.ID && item.TurnId == ids.TurnID {
					index = i
					break
				}
			}
			if index < 0 {
				entry.compactions = append(entry.compactions, wire.ContextCompaction{ItemId: ids.Item.ID, TurnId: ids.TurnID, State: wire.Unknown})
				index = len(entry.compactions) - 1
				entry.sample, entry.epoch, entry.modelItemCompleted = nil, uuid.New(), false
				entry.restoreBlocked = true
			}
			if method == RuntimeNotificationItemStarted && entry.compactions[index].State == wire.Unknown {
				entry.compactions[index].State, entry.compacting = wire.InProgress, ids.Item.ID
				entry.turn = ids.TurnID
			} else if method == RuntimeNotificationItemCompleted {
				entry.compactions[index].State, entry.compacting = wire.Completed, ""
			}
			if len(entry.compactions) > 128 {
				entry.compactions = entry.compactions[len(entry.compactions)-128:]
			}
		} else if ids.TurnID == entry.turn && method == RuntimeNotificationItemCompleted && (ids.Item.Type == "agentMessage" || ids.Item.Type == "reasoning" || ids.Item.Type == "mcpToolCall" || ids.Item.Type == "commandExecution" || ids.Item.Type == "webSearch") {
			entry.modelItemCompleted = true
		}
		return
	}
	var notification wire.ThreadTokenUsageUpdatedNotification
	if wire.Decode("NativeUsage", raw, &notification) != nil {
		entry.sample = nil
		return
	}
	usage := notification.TokenUsage
	previous := entry.lastUsage
	entry.lastUsage = &usage
	// Resume/fork and rate-limit updates can replay the last request's sample.
	// Only a newly observed model result or native window recomputation admits it.
	if notification.TurnId != entry.turn {
		return
	}
	recomputed := entry.compacting != "" && !sameContextUsage(previous, &usage) && usage.Last.InputTokens == 0 && usage.Last.OutputTokens == 0 && usage.Last.CachedInputTokens == 0 && usage.Last.ReasoningOutputTokens == 0
	newRequestUsage := previous != nil && usage.Total != previous.Total
	firstRequest := previous == nil && record.LastTurnID == "" && record.ModelRevision <= 1
	if !recomputed && (entry.compacting != "" || sameContextUsage(previous, &usage) || (!entry.modelItemCompleted && !newRequestUsage && !firstRequest)) {
		return
	}
	if usage.Last.TotalTokens < 0 || usage.Last.TotalTokens > 9007199254740991 {
		entry.sample = nil
		return
	}
	capacity := usage.ModelContextWindow
	if capacity != nil && (*capacity <= 0 || *capacity > 9007199254740991) {
		capacity = nil
	}
	entry.sample = &wire.ContextWindowSample{UsedTokens: usage.Last.TotalTokens, CapacityTokens: capacity, TurnId: notification.TurnId, WindowId: entry.epoch, ObservedAt: time.Now().UTC()}
	entry.modelItemCompleted = false
}

func sameContextUsage(a, b *wire.ThreadTokenUsage) bool {
	return a != nil && a.Last == b.Last && a.Total == b.Total && ((a.ModelContextWindow == nil && b.ModelContextWindow == nil) || (a.ModelContextWindow != nil && b.ModelContextWindow != nil && *a.ModelContextWindow == *b.ModelContextWindow))
}

func (s *Service) ReadContextWindow(ctx context.Context, sessionID string, includeHistory bool) (wire.ContextWindowSnapshot, error) {
	if err := requireUUID("agent_session_id", sessionID); err != nil {
		return wire.ContextWindowSnapshot{}, err
	}
	record, err := s.store.Get(sessionID)
	if err != nil {
		return wire.ContextWindowSnapshot{}, err
	}
	if status, ok := s.runtime.(interface{ Snapshot() codex.Status }); ok && !status.Snapshot().Ready {
		return wire.ContextWindowSnapshot{}, ErrRuntimeRequest
	}
	s.restoreContextWindow(ctx, record)
	if includeHistory {
		if runtime, ok := s.runtime.(interface {
			ReadThread(context.Context, string) (json.RawMessage, error)
		}); ok {
			raw, readErr := runtime.ReadThread(ctx, record.CodexThreadID)
			if readErr == nil {
				s.observeContextHistory(record, raw)
			}
		}
	}
	// Re-read after the asynchronous history query: model selection may change.
	record, err = s.store.Get(sessionID)
	if err != nil {
		return wire.ContextWindowSnapshot{}, err
	}
	s.contextWindowMu.Lock()
	defer s.contextWindowMu.Unlock()
	entry := s.contextObservation(record)
	profile := entry.profile
	result := wire.ContextWindowSnapshot{SchemaVersion: 1, ThreadId: &record.CodexThreadID, ModelProfile: &profile, ModelRevision: record.ModelRevision, Sample: entry.sample, Compactions: append([]wire.ContextCompaction{}, entry.compactions...)}
	if profile == "" {
		result.ModelProfile = nil
	}
	return result, nil
}

// Pinned native thread_history::handle_context_compacted creates these read
// items only from ContextCompacted. It ignores the started Item lifecycle.
// Read IDs are native history IDs and are never matched to different live IDs.
func (s *Service) observeContextHistory(record Record, raw json.RawMessage) {
	var thread struct {
		ID    string           `json:"id"`
		Turns []nativeWireTurn `json:"turns"`
	}
	if json.Unmarshal(raw, &thread) != nil || thread.ID != record.CodexThreadID {
		return
	}
	s.contextWindowMu.Lock()
	defer s.contextWindowMu.Unlock()
	// An asynchronous read may complete after a model switch. It can add
	// historical identities, but must not rebind or erase a newer usage sample.
	entry := s.contextWindows[record.CodexThreadID]
	if entry == nil {
		entry = s.contextObservation(record)
	}
	for _, turn := range thread.Turns {
		for _, itemRaw := range turn.Items {
			var item struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			}
			if json.Unmarshal(itemRaw, &item) != nil || item.Type != "contextCompaction" || !validContextID(item.ID) || !validContextID(turn.ID) {
				continue
			}
			found := false
			for _, observed := range entry.compactions {
				if observed.ItemId == item.ID && observed.TurnId == turn.ID {
					found = true
					break
				}
			}
			if !found {
				entry.compactions = append(entry.compactions, wire.ContextCompaction{ItemId: item.ID, TurnId: turn.ID, State: wire.Completed})
			}
		}
	}
	if len(entry.compactions) > 128 {
		entry.compactions = entry.compactions[len(entry.compactions)-128:]
	}
}
