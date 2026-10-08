package session

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
)

func (s *Service) marketRecord(id string) (Record, error) {
	if wire.CanonicalId(id).Validate() != nil {
		return Record{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	owner, e := s.marketReadableOwner()
	if e != nil {
		return Record{}, e
	}
	r, e := s.store.Get(id)
	if e != nil {
		return Record{}, marketFail(wire.ErrorCodeSessionNotFound)
	}
	if r.Purpose != purposeMarket || r.MarketScopeHash != owner || r.CodexThreadID == "" {
		return Record{}, marketFail(wire.ErrorCodeAuthorityMismatch)
	}
	return r, nil
}
func (s *Service) ReadMarketApprovals(_ context.Context, id string) (wire.ApprovalSnapshot, error) {
	r, e := s.marketRecord(id)
	if e != nil {
		return wire.ApprovalSnapshot{}, e
	}
	rt, ok := s.runtime.(interface {
		ListMarketApprovals(string) []wire.MarketApproval
	})
	if !ok {
		return wire.ApprovalSnapshot{}, marketFail(wire.ErrorCodeNotReady)
	}
	out := wire.ApprovalSnapshot{AgentSessionId: wire.CanonicalId(id), Requests: rt.ListMarketApprovals(r.CodexThreadID)}
	if out.Validate() != nil {
		return wire.ApprovalSnapshot{}, marketFail(wire.ErrorCodeTemporarilyUnavailable)
	}
	return out, nil
}
func (s *Service) DecideMarketApproval(ctx context.Context, input wire.ApprovalDecidePayload) (wire.MarketApproval, error) {
	if input.Validate() != nil {
		return wire.MarketApproval{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	s.modelOperationMu.Lock()
	defer s.modelOperationMu.Unlock()
	if e := s.ValidateMarketScope(input.HostInstanceId, input.Scope, true); e != nil {
		return wire.MarketApproval{}, e
	}
	r, e := s.marketRecord(string(input.AgentSessionId))
	if e != nil {
		return wire.MarketApproval{}, e
	}
	rt, ok := s.runtime.(interface {
		DecideMarketApproval(context.Context, string, wire.ApprovalDecidePayload) (wire.MarketApproval, error)
	})
	if !ok {
		return wire.MarketApproval{}, marketFail(wire.ErrorCodeNotReady)
	}
	result, e := rt.DecideMarketApproval(ctx, r.CodexThreadID, input)
	if e != nil {
		return wire.MarketApproval{}, marketFail(wire.ErrorCodeApprovalStale)
	}
	return result, nil
}

// This is only a bounded projection of Runtime-owned complete MCP Items. The
// elicitation carries no item ID, so a callRef/service is intentionally absent
// unless the native protocol eventually supplies an explicit binding.
func (s *Service) ReadMarketTools(ctx context.Context, id, turnID string) (wire.ToolSnapshot, error) {
	if wire.NativeId(turnID).Validate() != nil {
		return wire.ToolSnapshot{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	r, e := s.marketRecord(id)
	if e != nil {
		return wire.ToolSnapshot{}, e
	}
	rt, ok := s.runtime.(interface {
		ReadThread(context.Context, string) (json.RawMessage, error)
	})
	if !ok {
		return wire.ToolSnapshot{}, marketFail(wire.ErrorCodeNotReady)
	}
	raw, e := rt.ReadThread(ctx, r.CodexThreadID)
	if e != nil {
		return wire.ToolSnapshot{}, marketFail(wire.ErrorCodeTemporarilyUnavailable)
	}
	var native struct {
		ID    string           `json:"id"`
		Turns []nativeWireTurn `json:"turns"`
	}
	if len(raw) > 32<<20 || json.Unmarshal(raw, &native) != nil || native.ID != r.CodexThreadID {
		return wire.ToolSnapshot{}, marketFail(wire.ErrorCodeTemporarilyUnavailable)
	}
	out := wire.ToolSnapshot{AgentSessionId: wire.CanonicalId(id), NativeTurnId: wire.NativeId(turnID), Items: []wire.ToolObservation{}}
	for _, turn := range native.Turns {
		if turn.ID != turnID {
			continue
		}
		for _, rawItem := range turn.Items {
			var item nativeWireItem
			if json.Unmarshal(rawItem, &item) != nil || item.Type != "mcpToolCall" || item.Server != "yijie_market" {
				continue
			}
			if len(out.Items) >= wire.MaxToolItems {
				out.Truncated = true
				break
			}
			if wire.NativeId(item.ID).Validate() != nil || len(item.Tool) > 128 || !nativeMcpIdentifier.MatchString(item.Tool) || v5SensitiveCommandLine(item.Tool) {
				continue
			}
			state := "in_progress"
			if item.Status != nil {
				switch *item.Status {
				case "completed":
					state = "completed"
				case "failed":
					state = "failed"
				}
			}
			view := wire.ToolObservation{NativeItemId: wire.NativeId(item.ID), NativeThreadId: wire.NativeId(native.ID), NativeTurnId: wire.NativeId(turnID), ServerName: item.Server, ToolName: item.Tool, State: state}
			var result struct {
				Content    []json.RawMessage `json:"content"`
				Structured json.RawMessage   `json:"structuredContent"`
				IsError    bool              `json:"isError"`
			}
			if len(item.Result) > 0 && string(item.Result) != "null" {
				if json.Unmarshal(item.Result, &result) != nil {
					code := "unsupported_result"
					view.ErrorCode = &code
				} else {
					if result.IsError {
						view.State = "failed"
					}
					var texts []string
					remaining := wire.MaxResultTextBytes
					for index, content := range result.Content {
						if index >= 32 {
							view.Truncated = true
							break
						}
						var block struct {
							Type string  `json:"type"`
							Text *string `json:"text"`
						}
						if json.Unmarshal(content, &block) != nil || block.Type != "text" || block.Text == nil {
							view.Truncated = true
							continue
						}
						clean := redactV5CommandText(*block.Text)
						if strings.ContainsRune(clean, 0) {
							view.Truncated = true
							continue
						}
						if len(texts) > 0 {
							remaining--
						}
						if remaining < 0 {
							view.Truncated = true
							break
						}
						bounded, truncated := truncateV5UTF8(clean, remaining)
						view.Truncated = view.Truncated || truncated
						texts = append(texts, bounded)
						remaining -= len(bounded)
					}
					if len(result.Structured) > 0 && string(result.Structured) != "null" &&
						(view.Truncated || len(texts) != 1 || !sameMarketJSON(texts[0], result.Structured)) {
						view.Truncated = true
					}
					if len(texts) > 0 {
						text := strings.Join(texts, "\n")
						view.ResultText = &text
					}
				}
			}
			if view.Validate() == nil {
				out.Items = append(out.Items, view)
			} else {
				out.Truncated = true
			}
		}
		return out, nil
	}
	return wire.ToolSnapshot{}, marketFail(wire.ErrorCodeSessionNotFound)
}

// rmcp's structured result also includes a JSON text block. Omitting that
// identical second representation does not omit any result information.
func sameMarketJSON(text string, structured json.RawMessage) bool {
	var left, right bytes.Buffer
	return json.Compact(&left, []byte(text)) == nil && json.Compact(&right, structured) == nil && bytes.Equal(left.Bytes(), right.Bytes())
}
