package codex

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// BuiltinWebOwner proves both native ancestry and membership in the current
// root turn. An old child is never assigned to whichever root turn is active.
// Native spawn Items are the evidence; the model supplies no ownership facts.
func (m *Manager) BuiltinWebOwner(ctx context.Context, thread, turn string) (string, string, error) {
	m.mu.Lock()
	ready := m.status.Ready && !m.shutdownRequested
	m.mu.Unlock()
	if !ready {
		return "", "", errors.New("web runtime unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	root, err := m.nativeApprovalOwner(ctx, thread, turn)
	if err != nil {
		return "", "", err
	}
	c := &m.permissionCallbacks
	c.mu.Lock()
	rootTurn := c.activeTurns[root]
	c.mu.Unlock()
	if rootTurn == "" {
		return "", "", errors.New("web root inactive")
	}
	if root != thread {
		queue := []string{root}
		seen := map[string]bool{}
		proven := false
		for len(queue) > 0 && len(seen) < 16 && !proven {
			current := queue[0]
			queue = queue[1:]
			if seen[current] {
				continue
			}
			seen[current] = true
			var raw json.RawMessage
			if m.request(ctx, "thread/read", map[string]any{"threadId": current, "includeTurns": true}, &raw) != nil {
				return "", "", errors.New("web ancestry unavailable")
			}
			var response map[string]json.RawMessage
			var identity map[string]json.RawMessage
			var turns []map[string]json.RawMessage
			if json.Unmarshal(raw, &response) != nil || json.Unmarshal(response["thread"], &identity) != nil || json.Unmarshal(identity["turns"], &turns) != nil {
				return "", "", errors.New("web ancestry unavailable")
			}
			var returned string
			_ = json.Unmarshal(identity["id"], &returned)
			if returned != current {
				return "", "", errors.New("web ancestry mismatch")
			}
			for _, nativeTurn := range turns {
				var id string
				_ = json.Unmarshal(nativeTurn["id"], &id)
				if current == root && id != rootTurn {
					continue
				}
				var items []map[string]json.RawMessage
				_ = json.Unmarshal(nativeTurn["items"], &items)
				for _, item := range items {
					var kind, tool, sender, activity, child string
					var receivers []string
					_ = json.Unmarshal(item["type"], &kind)
					_ = json.Unmarshal(item["tool"], &tool)
					_ = json.Unmarshal(item["senderThreadId"], &sender)
					if kind == "subAgentActivity" {
						_ = json.Unmarshal(item["kind"], &activity)
						_ = json.Unmarshal(item["agentThreadId"], &child)
						if activity != "started" || child == "" {
							continue
						}
						receivers = []string{child}
					} else {
						if kind != "collabAgentToolCall" || tool != "spawnAgent" || sender != current {
							continue
						}
						_ = json.Unmarshal(item["receiverThreadIds"], &receivers)
					}
					for _, child := range receivers {
						if child == thread {
							proven = true
						}
						if child != "" && len(queue) < 16 {
							queue = append(queue, child)
						}
					}
				}
			}
		}
		if !proven {
			return "", "", errors.New("child not bound to current web root turn")
		}
	}
	c.mu.Lock()
	unchanged := c.activeTurns[root] == rootTurn && c.activeTurns[thread] == turn
	c.mu.Unlock()
	m.mu.Lock()
	ready = m.status.Ready && !m.shutdownRequested
	m.mu.Unlock()
	if !ready || !unchanged || ctx.Err() != nil {
		return "", "", errors.New("web turn ended")
	}
	return root, rootTurn, nil
}
