package session

import (
	"encoding/json"

	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
)

// Decode only the owned namespace of a native MCP result. The original native
// Item supplies the result identity; model text and supplier content do not.
func (s *Service) marketResultCorrelation(record Record, item nativeWireItem, turnID string, meta json.RawMessage) *wire.GatewayCallReceipt {
	var envelope map[string]json.RawMessage
	if item.Server != "yijie_market" || json.Unmarshal(meta, &envelope) != nil {
		return nil
	}
	raw := envelope[broker.GatewayResultKey]
	if len(raw) == 0 || len(raw) > broker.GatewayResultMaxMetadataBytes {
		return nil
	}
	var receipt wire.GatewayCallReceipt
	if json.Unmarshal(raw, &receipt) != nil || receipt.Validate() != nil ||
		string(receipt.NativeThreadId) != record.CodexThreadID || string(receipt.NativeTurnId) != turnID || string(receipt.ToolName) != item.Tool {
		return nil
	}
	entry, err := s.store.readMarketOperation(string(receipt.TurnOperationId))
	if err != nil || entry.ScopeHash != record.MarketScopeHash {
		return nil
	}
	original := s.reconcileMarketReceipt(entry)
	if string(original.TaskId) != record.TaskID || original.AgentSessionId == nil || string(*original.AgentSessionId) != record.AgentSessionID ||
		original.NativeThreadId == nil || *original.NativeThreadId != receipt.NativeThreadId ||
		original.NativeTurnId == nil || *original.NativeTurnId != receipt.NativeTurnId ||
		original.TurnOperationId != receipt.TurnOperationId || original.SelectionDigest != receipt.SelectionDigest {
		return nil
	}
	return &receipt
}

// Distinct native Items cannot silently claim one Gateway call. A duplicate
// observation of the same Item is harmless; ambiguous links are omitted only.
func removeConflictingMarketLinks(items []wire.ToolObservation) {
	seen := make(map[wire.CanonicalId]wire.NativeId)
	conflicts := make(map[wire.CanonicalId]bool)
	for _, item := range items {
		if item.Correlation == nil {
			continue
		}
		ref := item.Correlation.CallRef
		if prior, ok := seen[ref]; ok && prior != item.NativeItemId {
			conflicts[ref] = true
		}
		seen[ref] = item.NativeItemId
	}
	for i := range items {
		if items[i].Correlation != nil && conflicts[items[i].Correlation.CallRef] {
			items[i].Correlation, items[i].CallRef = nil, nil
		}
	}
}
