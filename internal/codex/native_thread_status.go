package codex

import (
	"context"
	"errors"
)

// ReadThreadStatus reads only the fixed native thread status. It does not load
// or resume the thread, read its turns, or update any Host execution state.
func (m *Manager) ReadThreadStatus(ctx context.Context, threadID string) (string, error) {
	if threadID == "" {
		return "", errors.New("Codex thread id is required")
	}
	var response struct {
		Thread struct {
			ID     string `json:"id"`
			Status struct {
				Type string `json:"type"`
			} `json:"status"`
		} `json:"thread"`
	}
	if err := m.request(ctx, "thread/read", struct {
		ThreadID     string `json:"threadId"`
		IncludeTurns bool   `json:"includeTurns"`
	}{threadID, false}, &response); err != nil {
		return "", err
	}
	if response.Thread.ID != threadID {
		return "", errors.New("thread/read returned an unexpected thread id")
	}
	switch response.Thread.Status.Type {
	case "notLoaded", "idle", "systemError", "active":
		return response.Thread.Status.Type, nil
	default:
		return "", errors.New("thread/read status is unavailable")
	}
}
