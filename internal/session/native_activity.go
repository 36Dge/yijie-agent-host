package session

// Read-only projection of existing app-server Items. No execution, filesystem
// access, synthetic event, or additional lifecycle is introduced here.
import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv5"
)

func nativeActivityText(value string, limit int, out *native.NativeItem) *string {
	if value == "" {
		return nil
	}
	redacted := redactV5CommandText(value)
	bounded, complete := safeNativeText(redacted, limit)
	if redacted != value || !complete {
		out.Availability = native.NativeItemAvailabilityPartial
	}
	return &bounded
}

func nativeActivityPath(value, cwd string, out *native.NativeItem) *string {
	if value == "" {
		return nil
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(cwd, value)
	}
	location := v5CommandCwd(cwd, &value)
	if location.Kind == "workspace_root" {
		return nativeString(".")
	}
	label := strings.Join(location.Segments, "/")
	if location.Kind != "workspace_relative" || len(label) > 1024 {
		out.Availability = native.NativeItemAvailabilityPartial
		return nil
	}
	return &label
}

func projectNativeCommandActions(wire nativeWireItem, cwd string, out *native.NativeItem) {
	out.CommandPreview = nativeActivityText(wire.Command, 4096, out)
	if len(wire.CommandActions) == 0 || string(wire.CommandActions) == "null" {
		return
	}
	var actions []struct {
		Type, Command, Name string
		Path, Query         *string
	}
	if json.Unmarshal(wire.CommandActions, &actions) != nil {
		out.Availability = native.NativeItemAvailabilityPartial
		return
	}
	result := []native.NativeCommandActionDisplay{}
	remaining := 32 << 10
	for index, action := range actions {
		if index >= 32 {
			out.Availability = native.NativeItemAvailabilityPartial
			break
		}
		kind := native.NativeCommandActionDisplayType(action.Type)
		if !kind.Valid() {
			kind = native.NativeCommandActionDisplayTypeUnknown
			out.Availability = native.NativeItemAvailabilityPartial
		}
		entry := native.NativeCommandActionDisplay{Index: index, Type: kind}
		entry.Name = nativeActivityText(action.Name, 255, out)
		if action.Path != nil {
			path := *action.Path
			if path != "" && !filepath.IsAbs(path) && wire.Cwd != "" {
				path = filepath.Join(wire.Cwd, path)
			}
			entry.PathLabel = nativeActivityPath(path, cwd, out)
		}
		if action.Query != nil {
			entry.Query = nativeActivityText(*action.Query, 1024, out)
		}
		if remaining > 0 {
			entry.CommandPreview = nativeActivityText(action.Command, min(remaining, 4096), out)
			if entry.CommandPreview != nil {
				remaining -= len(*entry.CommandPreview)
			}
		} else if action.Command != "" {
			out.Availability = native.NativeItemAvailabilityPartial
		}
		result = append(result, entry)
	}
	out.CommandActions = &result
}

func projectNativeAppContext(wire nativeWireItem, out *native.NativeItem) {
	if len(wire.AppContext) == 0 || string(wire.AppContext) == "null" {
		return
	}
	var context struct {
		ConnectorID string `json:"connectorId"`
		AppName     string `json:"appName"`
		ActionName  string `json:"actionName"`
	}
	if json.Unmarshal(wire.AppContext, &context) != nil {
		out.Availability = native.NativeItemAvailabilityPartial
		return
	}
	display := native.NativeAppContextDisplay{
		ConnectorId: nativeActivityText(context.ConnectorID, 128, out),
		AppName:     nativeActivityText(context.AppName, 128, out),
		ActionName:  nativeActivityText(context.ActionName, 128, out),
	}
	if display.ConnectorId != nil || display.AppName != nil || display.ActionName != nil {
		out.AppContext = &display
	}
}

func projectNativeFileChange(wire nativeWireItem, cwd string, out *native.NativeItem) {
	var changes []struct {
		Path string `json:"path"`
		Kind struct {
			Type     string  `json:"type"`
			MovePath *string `json:"move_path"`
		} `json:"kind"`
		Diff *string `json:"diff"`
	}
	if len(wire.Changes) == 0 || string(wire.Changes) == "null" || json.Unmarshal(wire.Changes, &changes) != nil {
		out.Availability = native.NativeItemAvailabilityPartial
		return
	}
	display := &native.NativeFileChangeDisplay{Changes: []native.NativeFileUpdateDisplay{}}
	out.FileChange = display
	remaining := 64 << 10
	for index, change := range changes {
		if index >= 128 {
			out.Availability = native.NativeItemAvailabilityPartial
			break
		}
		kind := native.NativeFileUpdateDisplayKind(change.Kind.Type)
		if !kind.Valid() {
			kind = native.NativeFileUpdateDisplayKindUnknown
			out.Availability = native.NativeItemAvailabilityPartial
		}
		entry := native.NativeFileUpdateDisplay{Index: index, Kind: kind, PathLabel: nativeActivityPath(change.Path, cwd, out)}
		if change.Kind.MovePath != nil {
			entry.MovePathLabel = nativeActivityPath(*change.Kind.MovePath, cwd, out)
		}
		if change.Diff != nil && len(*change.Diff) <= remaining {
			// Counts use complete native content, before confidentiality projection.
			if wire.Status != nil && *wire.Status == "completed" {
				diff := *change.Diff
				if change.Kind.MovePath != nil {
					diff = strings.TrimSuffix(diff, "\n\nMoved to: "+*change.Kind.MovePath)
				}
				added, removed, ok := nativeDiffCounts(change.Kind.Type, diff)
				if ok {
					entry.AddedLines = &added
					entry.RemovedLines = &removed
				}
			}
			entry.DiffText = nativeActivityText(*change.Diff, remaining, out)
			remaining -= len(*change.Diff)
		} else {
			out.Availability = native.NativeItemAvailabilityPartial
			if change.Diff != nil && remaining > 0 {
				entry.DiffText = nativeActivityText(*change.Diff, remaining, out)
				remaining = 0
			}
		}
		display.Changes = append(display.Changes, entry)
	}
}

var nativeDiffHunk = regexp.MustCompile(`^@@ -[0-9]+(?:,([0-9]+))? \+[0-9]+(?:,([0-9]+))? @@(?:.*)$`)

// Match the pinned TUI's content-line (add/delete) and unified-hunk (update)
// semantics. Unlike its numeric fallback, an unparseable update stays unknown.
func nativeDiffCounts(kind, diff string) (added, removed int, ok bool) {
	lines := strings.Split(strings.TrimSuffix(diff, "\n"), "\n")
	if diff == "" {
		lines = nil
	}
	if kind == "add" {
		return len(lines), 0, true
	}
	if kind == "delete" {
		return 0, len(lines), true
	}
	if kind != "update" {
		return 0, 0, false
	}
	oldLeft, newLeft, hunks := 0, 0, 0
	header := 0
	for _, line := range lines {
		if match := nativeDiffHunk.FindStringSubmatch(line); match != nil {
			// Native apply_patch reports hunk-only diffs as well as full unified
			// diffs. A lone old-file header is incomplete; no headers is valid.
			if oldLeft != 0 || newLeft != 0 || header == 1 {
				return 0, 0, false
			}
			count := func(v string) int {
				if v == "" {
					return 1
				}
				n, e := strconv.Atoi(v)
				if e != nil {
					return -1
				}
				return n
			}
			oldLeft, newLeft = count(match[1]), count(match[2])
			if oldLeft < 0 || newLeft < 0 {
				return 0, 0, false
			}
			hunks++
			continue
		}
		if hunks == 0 {
			if header == 0 && strings.HasPrefix(line, "--- ") {
				header = 1
				continue
			}
			if header == 1 && strings.HasPrefix(line, "+++ ") {
				header = 2
				continue
			}
			return 0, 0, false
		}
		if line == "\\ No newline at end of file" {
			continue
		}
		if line == "" {
			return 0, 0, false
		}
		switch line[0] {
		case '+':
			added++
			newLeft--
		case '-':
			removed++
			oldLeft--
		case ' ':
			oldLeft--
			newLeft--
		default:
			return 0, 0, false
		}
		if oldLeft < 0 || newLeft < 0 {
			return 0, 0, false
		}
	}
	return added, removed, hunks > 0 && oldLeft == 0 && newLeft == 0
}
