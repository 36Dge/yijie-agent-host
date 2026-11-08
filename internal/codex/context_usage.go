package codex

// Read-only adapter over the pinned native rollout metadata. Counts come from
// TokenUsageInfo, exactly as native resume does; this never tokenizes content,
// resumes a thread, sends a turn, or changes Runtime configuration.
import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/contextwindow"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

var errContextUsageUnavailable = errors.New("native context statistics unavailable")

// NativeContextUsage is an internal read result, not a second wire protocol.
type NativeContextUsage struct {
	ThreadID, TurnID, Model, WindowKey string
	ObservedAt                         time.Time
	Usage                              wire.ThreadTokenUsage
}

func (m *Manager) ReadContextUsage(ctx context.Context, threadID string) (*NativeContextUsage, error) {
	if id, err := uuid.Parse(threadID); err != nil || id == uuid.Nil || id.String() != threadID {
		return nil, errContextUsageUnavailable
	}
	var response struct {
		Thread struct {
			ID   string  `json:"id"`
			Path *string `json:"path"`
		} `json:"thread"`
	}
	// This existing read surface works on unloaded threads and performs no model
	// operation. Do not use thread/resume just to make statistics available.
	if err := m.request(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": false}, &response); err != nil {
		return nil, errContextUsageUnavailable
	}
	if response.Thread.ID != threadID {
		return nil, errContextUsageUnavailable
	}
	if response.Thread.Path == nil {
		return nil, nil
	}
	return readNativeContextUsageFile(ctx, m.config.CodexHome, *response.Thread.Path, threadID)
}

func readNativeContextUsageFile(ctx context.Context, home, path, threadID string) (*NativeContextUsage, error) {
	rel, err := filepath.Rel(home, path)
	if err != nil || !filepath.IsAbs(path) || !filepath.IsLocal(rel) {
		return nil, errContextUsageUnavailable
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 || (parts[0] != "sessions" && parts[0] != "archived_sessions") || !strings.HasSuffix(parts[len(parts)-1], threadID+".jsonl") {
		return nil, errContextUsageUnavailable
	}
	root, rootInfo, err := openManagedCodexHome(home)
	if err != nil {
		return nil, errContextUsageUnavailable
	}
	opened := []*os.File{root}
	identities := []os.FileInfo{rootInfo}
	defer func() {
		for i := len(opened) - 1; i >= 0; i-- {
			_ = opened[i].Close()
		}
	}()
	for i, name := range parts {
		if validateManagedChildName(name) != nil {
			return nil, errContextUsageUnavailable
		}
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if i != len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		fd, openErr := unix.Openat(int(opened[len(opened)-1].Fd()), name, flags, 0)
		if openErr != nil {
			return nil, errContextUsageUnavailable
		}
		file := os.NewFile(uintptr(fd), name)
		opened = append(opened, file)
		info, statErr := file.Stat()
		// Native creates 0755/0644 descendants inside the owner-only Home. Do not
		// chmod them or impose managed-config 0700/0600 modes on native history.
		if statErr != nil || !currentUserOwns(info) || info.Mode().Perm()&0022 != 0 || (i == len(parts)-1 && !info.Mode().IsRegular()) || (i != len(parts)-1 && !info.IsDir()) {
			return nil, errContextUsageUnavailable
		}
		identities = append(identities, info)
	}
	file := opened[len(opened)-1]
	before, err := file.Stat()
	if err != nil {
		return nil, errContextUsageUnavailable
	}
	result, err := parseNativeContextUsage(ctx, io.LimitReader(file, before.Size()), threadID)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, errContextUsageUnavailable
	}
	for i := 1; i < len(opened); i++ {
		if !sameNativeContextEntry(opened[i-1], parts[i-1], identities[i]) {
			return nil, errContextUsageUnavailable
		}
	}
	currentRoot, currentRootInfo, err := openManagedCodexHome(home)
	if err != nil {
		return nil, errContextUsageUnavailable
	}
	_ = currentRoot.Close()
	if !os.SameFile(rootInfo, currentRootInfo) {
		return nil, errContextUsageUnavailable
	}
	return result, nil
}

func sameNativeContextEntry(parent *os.File, name string, expected os.FileInfo) bool {
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if expected.IsDir() {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Openat(int(parent.Fd()), name, flags, 0)
	if err != nil {
		return false
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	info, err := file.Stat()
	return err == nil && currentUserOwns(info) && info.Mode().Perm()&0022 == 0 && os.SameFile(info, expected)
}

func parseNativeContextUsage(ctx context.Context, input io.Reader, threadID string) (*NativeContextUsage, error) {
	reader := bufio.NewReaderSize(input, 64<<10)
	var candidate *NativeContextUsage
	var lastUsage *wire.ThreadTokenUsage
	var model, turn string
	windowKey := threadID
	matchedThread := false
	compacting := false
	var recordNumber int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, oversized, readErr := readContextMetadataLine(reader)
		recordNumber++
		if readErr != nil && readErr != io.EOF {
			return nil, errContextUsageUnavailable
		}
		if oversized {
			// Large response bodies are not statistics. A later native count may
			// restore the sample; never return a stale candidate after skipped data.
			candidate = nil
			windowKey = threadID + ":unparsed-record:" + strconv.FormatInt(recordNumber, 10)
		} else if len(line) > 0 {
			var row struct {
				Timestamp string          `json:"timestamp"`
				Type      string          `json:"type"`
				Payload   json.RawMessage `json:"payload"`
			}
			if json.Unmarshal(line, &row) != nil {
				return nil, errContextUsageUnavailable
			}
			switch row.Type {
			case "session_meta":
				var meta struct {
					ID      string `json:"id"`
					Version string `json:"cli_version"`
				}
				if json.Unmarshal(row.Payload, &meta) != nil || meta.ID != threadID || meta.Version != "0.144.6" {
					return nil, errContextUsageUnavailable
				}
				matchedThread = true
			case "turn_context":
				var context struct {
					TurnID string `json:"turn_id"`
					Model  string `json:"model"`
				}
				if json.Unmarshal(row.Payload, &context) != nil {
					return nil, errContextUsageUnavailable
				}
				model, turn = context.Model, context.TurnID
			case "compacted":
				var compact struct {
					WindowID string `json:"window_id"`
				}
				if json.Unmarshal(row.Payload, &compact) != nil {
					return nil, errContextUsageUnavailable
				}
				candidate = nil
				compacting = false
				windowKey = compact.WindowID
				if windowKey == "" {
					windowKey = threadID + ":compacted-record:" + strconv.FormatInt(recordNumber, 10)
				}
			case "event_msg":
				var event struct {
					Type   string          `json:"type"`
					TurnID string          `json:"turn_id"`
					Info   json.RawMessage `json:"info"`
					Item   struct {
						Type string `json:"type"`
					} `json:"item"`
				}
				if json.Unmarshal(row.Payload, &event) != nil {
					return nil, errContextUsageUnavailable
				}
				switch event.Type {
				case "task_started":
					turn = event.TurnID
				case "item_started":
					if event.Item.Type == "contextCompaction" {
						candidate = nil
						compacting = true
					}
				case "item_completed", "context_compacted":
					if event.Type == "context_compacted" || event.Item.Type == "contextCompaction" {
						compacting = false
					}
				case "thread_rolled_back":
					candidate = nil
					model = ""
					turn = ""
				case "token_count":
					if len(event.Info) == 0 || string(event.Info) == "null" {
						break
					}
					usage, err := nativePersistedUsage(event.Info, threadID, turn)
					if err != nil {
						return nil, err
					}
					duplicate := lastUsage != nil && equalNativeContextUsage(*lastUsage, usage)
					lastUsage = &usage
					// Rate-limit events repeat old info under a newer turn context.
					// Do not relabel those counts as a new model's measurement.
					if duplicate || compacting {
						break
					}
					at, err := time.Parse(time.RFC3339Nano, row.Timestamp)
					if err != nil || model == "" || turn == "" || !matchedThread {
						return nil, errContextUsageUnavailable
					}
					if usage.Last.TotalTokens < 0 || usage.Last.TotalTokens > 9007199254740991 {
						return nil, errContextUsageUnavailable
					}
					display := usage
					if c := usage.ModelContextWindow; c != nil && (*c <= 0 || *c > 9007199254740991) {
						display.ModelContextWindow = nil
					}
					candidate = &NativeContextUsage{ThreadID: threadID, TurnID: turn, Model: model, WindowKey: windowKey, ObservedAt: at, Usage: display}
				}
			}
		}
		if readErr == io.EOF {
			break
		}
	}
	if !matchedThread {
		return nil, errContextUsageUnavailable
	}
	return candidate, nil
}

func readContextMetadataLine(reader *bufio.Reader) ([]byte, bool, error) {
	var line []byte
	oversized := false
	for {
		part, err := reader.ReadSlice('\n')
		if !oversized {
			if len(line)+len(part) > 16<<20 {
				oversized = true
				line = nil
			} else {
				line = append(line, part...)
			}
		}
		if err != bufio.ErrBufferFull {
			return line, oversized, err
		}
	}
}

func equalNativeContextUsage(a, b wire.ThreadTokenUsage) bool {
	return a.Last == b.Last && a.Total == b.Total && ((a.ModelContextWindow == nil && b.ModelContextWindow == nil) || (a.ModelContextWindow != nil && b.ModelContextWindow != nil && *a.ModelContextWindow == *b.ModelContextWindow))
}

func nativePersistedUsage(info json.RawMessage, thread, turn string) (wire.ThreadTokenUsage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(info, &fields) != nil {
		return wire.ThreadTokenUsage{}, errContextUsageUnavailable
	}
	convert := func(raw json.RawMessage) map[string]json.RawMessage {
		var values map[string]json.RawMessage
		if json.Unmarshal(raw, &values) != nil {
			return nil
		}
		out := make(map[string]json.RawMessage)
		for native, app := range map[string]string{"total_tokens": "totalTokens", "input_tokens": "inputTokens", "cached_input_tokens": "cachedInputTokens", "output_tokens": "outputTokens", "reasoning_output_tokens": "reasoningOutputTokens"} {
			if value, ok := values[native]; ok {
				out[app] = value
			}
		}
		return out
	}
	raw, err := json.Marshal(map[string]any{"threadId": thread, "turnId": turn, "tokenUsage": map[string]any{"last": convert(fields["last_token_usage"]), "total": convert(fields["total_token_usage"]), "modelContextWindow": fields["model_context_window"]}})
	if err != nil {
		return wire.ThreadTokenUsage{}, errContextUsageUnavailable
	}
	var decoded wire.ThreadTokenUsageUpdatedNotification
	if wire.Decode("NativeUsage", raw, &decoded) != nil {
		return wire.ThreadTokenUsage{}, errContextUsageUnavailable
	}
	return decoded.TokenUsage, nil
}
