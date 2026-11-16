package session

import (
	"context"
	"encoding/json"
	"sync"

	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
)

// Disposable display snapshots of the existing native notification stream.
// No approval, lease, retry or execution state is owned here.
type marketSnapshotKey struct{ session, thread, turn, scope string }
type marketSnapshotEntry struct {
	data           []byte
	revision, used uint64
	complete       bool
}
type marketSnapshotVersion struct{ revision, invalidations uint64 }
type marketToolSnapshots struct {
	mu            sync.Mutex
	entries       map[marketSnapshotKey]marketSnapshotEntry
	clock         uint64
	invalidations uint64
	bytes         int
}

func marketSnapshotIdentity(record Record, turn string) marketSnapshotKey {
	return marketSnapshotKey{record.AgentSessionID, record.CodexThreadID, turn, record.MarketScopeHash}
}
func (c *marketToolSnapshots) version(key marketSnapshotKey) marketSnapshotVersion {
	c.mu.Lock()
	defer c.mu.Unlock()
	return marketSnapshotVersion{c.entries[key].revision, c.invalidations}
}
func (c *marketToolSnapshots) invalidate(key marketSnapshotKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bytes -= len(c.entries[key].data)
	delete(c.entries, key)
	c.invalidations++
}
func (c *marketToolSnapshots) get(key marketSnapshotKey) (wire.ToolSnapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || !entry.complete {
		return wire.ToolSnapshot{}, false
	}
	var snapshot wire.ToolSnapshot
	if json.Unmarshal(entry.data, &snapshot) != nil {
		return wire.ToolSnapshot{}, false
	}
	removeConflictingMarketLinks(snapshot.Items)
	c.clock++
	entry.used = c.clock
	c.entries[key] = entry
	return snapshot, true
}
func (c *marketToolSnapshots) putLocked(key marketSnapshotKey, snapshot wire.ToolSnapshot, complete bool) {
	// Keep the original validated links internally so repeated notifications
	// cannot revive a link that conflicted with another native Item. Filter only
	// the returned view; no caller can read this private serialized snapshot.
	snapshot.Items = append([]wire.ToolObservation{}, snapshot.Items...)
	data, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	// Retain native headers and links if the aggregate display exceeds the cache budget.
	for i := 0; len(data) > wire.MaxLiveToolBytes && i < len(snapshot.Items); i++ {
		snapshot.Items[i].ResultText, snapshot.Items[i].StructuredResultText = nil, nil
		snapshot.Items[i].ResultTextSource = nil
		snapshot.Items[i].Truncated, snapshot.Truncated = true, true
		data, err = json.Marshal(snapshot)
		if err != nil {
			return
		}
	}
	if len(data) > wire.MaxLiveToolBytes {
		return
	}
	if c.entries == nil {
		c.entries = make(map[marketSnapshotKey]marketSnapshotEntry)
	}
	c.bytes -= len(c.entries[key].data)
	c.clock++
	c.entries[key] = marketSnapshotEntry{data: data, revision: c.clock, used: c.clock, complete: complete}
	c.bytes += len(data)
	for len(c.entries) > wire.MaxLiveToolTurns || c.bytes > wire.MaxLiveToolBytes {
		var oldest marketSnapshotKey
		var used uint64
		for candidate, entry := range c.entries {
			if used == 0 || entry.used < used {
				oldest, used = candidate, entry.used
			}
		}
		c.bytes -= len(c.entries[oldest].data)
		delete(c.entries, oldest)
		c.invalidations++
	}
}
func (c *marketToolSnapshots) seed(key marketSnapshotKey, snapshot wire.ToolSnapshot, expected marketSnapshotVersion) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// A history read must never overwrite a notification that arrived during the read.
	if c.entries[key].revision != expected.revision || c.invalidations != expected.invalidations {
		return
	}
	c.putLocked(key, snapshot, true)
}

func hasMarketReceipt(raw json.RawMessage) bool {
	var result struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	return json.Unmarshal(raw, &result) == nil && len(result.Meta[broker.GatewayResultKey]) > 0
}
func (c *marketToolSnapshots) observe(key marketSnapshotKey, item *wire.ToolObservation, started bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, exists := c.entries[key]
	snapshot := wire.ToolSnapshot{AgentSessionId: wire.CanonicalId(key.session), NativeTurnId: wire.NativeId(key.turn), Items: []wire.ToolObservation{}}
	if exists && json.Unmarshal(entry.data, &snapshot) != nil {
		return
	}
	if item != nil {
		found := false
		for i := range snapshot.Items {
			if snapshot.Items[i].NativeItemId == item.NativeItemId {
				// A read can observe completion before an already-queued started
				// notification is delivered. Keep that exact native terminal fact.
				if snapshot.Items[i].State == "in_progress" || item.State != "in_progress" {
					snapshot.Items[i] = *item
				}
				found = true
				break
			}
		}
		if !found {
			if len(snapshot.Items) < wire.MaxToolItems {
				snapshot.Items = append(snapshot.Items, *item)
			} else {
				snapshot.Truncated = true
			}
		}
	}
	c.putLocked(key, snapshot, entry.complete || started)
}

func (s *Service) observeMarketToolNotification(record Record, method, turn string, rawItem json.RawMessage, rawTurn nativeWireTurn) {
	if record.Purpose != purposeMarket {
		return
	}
	if method == RuntimeNotificationTurnStarted || method == RuntimeNotificationTurnCompleted {
		turn = rawTurn.ID
	}
	if wire.NativeId(turn).Validate() != nil {
		return
	}
	key := marketSnapshotIdentity(record, turn)
	observe := func(item nativeWireItem) {
		if projected := s.projectMarketTool(record, turn, item); projected != nil {
			s.marketTools.observe(key, projected, false)
			// Completion may precede the final submission-ledger acknowledgement.
			// Resolve from actual history on the next read, never cache a guessed link.
			if projected.Correlation == nil && hasMarketReceipt(item.Result) {
				s.marketTools.invalidate(key)
			}
		}
	}
	switch method {
	case RuntimeNotificationTurnStarted:
		s.marketTools.observe(key, nil, true)
	case RuntimeNotificationItemStarted, RuntimeNotificationItemCompleted:
		var item nativeWireItem
		if json.Unmarshal(rawItem, &item) == nil {
			observe(item)
		}
	case RuntimeNotificationTurnCompleted:
		for _, raw := range rawTurn.Items {
			var item nativeWireItem
			if json.Unmarshal(raw, &item) == nil {
				observe(item)
			}
		}
	}
}

func (s *Service) ReadMarketToolEvents(ctx context.Context, id, turn string) (wire.ToolSnapshot, error) {
	if wire.NativeId(turn).Validate() != nil {
		return wire.ToolSnapshot{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	record, err := s.marketRecord(id)
	if err != nil {
		return wire.ToolSnapshot{}, err
	}
	if !marketRuntimeReady(s.runtime) {
		return wire.ToolSnapshot{}, marketFail(wire.ErrorCodeNotReady)
	}
	key := marketSnapshotIdentity(record, turn)
	if snapshot, ok := s.marketTools.get(key); ok {
		return snapshot, nil
	}
	snapshot, err := s.ReadMarketTools(ctx, id, turn)
	if err != nil {
		return wire.ToolSnapshot{}, err
	}
	if latest, ok := s.marketTools.get(key); ok {
		if marketSnapshotCovers(latest, snapshot) {
			return latest, nil
		}
		// A delayed turn/started can create an empty "complete" cache during the
		// history read. It is not evidence that the real native Items disappeared.
		// Discard this incomplete baseline so the next read can seed it again.
		s.marketTools.invalidate(key)
	}
	return snapshot, nil
}

func marketSnapshotCovers(latest, history wire.ToolSnapshot) bool {
	items := make(map[wire.NativeId]wire.ToolObservation, len(latest.Items))
	for _, item := range latest.Items {
		items[item.NativeItemId] = item
	}
	for _, previous := range history.Items {
		current, ok := items[previous.NativeItemId]
		if !ok || previous.State != "in_progress" && current.State == "in_progress" {
			return false
		}
	}
	return true
}
