package session

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
)

// Decode only known canonical display fields. No raw commands or tool args
// are used to infer actions; the existing Runtime owns command parsing.
type nativeWireAction struct {
	Type    string  `json:"type"`
	Command string  `json:"command"`
	Name    string  `json:"name"`
	Path    *string `json:"path"`
	Query   *string `json:"query"`
}

// Preview only the first display line of a complete native command/action.
// The Runtime supplies action kinds: this does not parse or classify shell code.
// Reuse the existing conservative redactor; when an entire line is hidden, only
// an allowlisted executable name can survive. No raw program body is forwarded.
func traceCommandPreview(value string, out *native.NativeItem) *string {
	if !utf8.ValidString(value) || len(value) > nativeTextLimit || strings.ContainsRune(value, 0) {
		out.Availability = native.NativeItemAvailabilityPartial
		return nil
	}
	first, _, multiline := strings.Cut(strings.TrimSpace(value), "\n")
	clean := strings.Join(strings.Fields(redactV5CommandText(first)), " ")
	if clean == "" {
		return nil
	}
	if clean == v5RedactedMarker {
		out.Availability = native.NativeItemAvailabilityPartial
		words := strings.Fields(first)
		if len(words) == 0 {
			return nil
		}
		name := filepath.Base(strings.Trim(words[0], "\"'"))
		switch name {
		case "python", "python3", "node", "pnpm", "npm", "rg", "grep", "tail", "head", "sed", "cat", "ls", "pwd", "find", "wc", "git", "cargo", "go", "date", "curl", "wget", "echo", "printf", "sh", "zsh", "bash":
			return nativeString(name + " · 参数已隐藏")
		default:
			return nil
		}
	}
	if multiline {
		clean += " …"
		out.Availability = native.NativeItemAvailabilityPartial
	}
	runes := []rune(clean)
	if len(runes) > 512 {
		clean = string(runes[:511]) + "…"
		out.Availability = native.NativeItemAvailabilityPartial
	}
	return nativeString(clean)
}

func projectNativeCommandPreview(wire nativeWireItem, out *native.NativeItem) {
	// Canonical actions already remove shell wrappers. Prefer their previews;
	// several actions still belong to the single parent execution.
	previews := []string{}
	for i, action := range wire.CommandActions {
		if i == 3 {
			if len(previews) > 0 {
				previews = append(previews, "…")
			}
			break
		}
		if preview := traceCommandPreview(action.Command, out); preview != nil {
			previews = append(previews, *preview)
		}
	}
	if len(previews) > 0 {
		out.CommandLabel = nativeString(strings.Join(previews, "；"))
		return
	}
	if preview := traceCommandPreview(wire.Command, out); preview != nil {
		out.CommandLabel = preview
	}
}

type nativeWireAppContext struct {
	AppName    string `json:"appName"`
	ActionName string `json:"actionName"`
}

func traceLabel(value string, limit int, out *native.NativeItem) *string {
	if !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		out.Availability = native.NativeItemAvailabilityPartial
		return nil
	}
	clean := strings.TrimSpace(redactV5CommandText(value))
	if clean != strings.TrimSpace(value) {
		out.Availability = native.NativeItemAvailabilityPartial
	}
	runes := []rune(clean)
	if len(runes) > limit {
		clean = string(runes[:limit])
		out.Availability = native.NativeItemAvailabilityPartial
	}
	if clean == "" {
		return nil
	}
	return &clean
}

func projectNativeActions(wire nativeWireItem, cwd string, out *native.NativeItem) {
	if wire.CommandActions == nil {
		return
	}
	actions := []native.NativeCommandActionDisplay{}
	for i, a := range wire.CommandActions {
		if i >= 64 {
			out.Availability = native.NativeItemAvailabilityPartial
			break
		}
		entry := native.NativeCommandActionDisplay{Index: i, Kind: native.NativeCommandActionDisplayKind(a.Type)}
		switch a.Type {
		case "read", "listFiles", "search":
			if a.Path != nil {
				path := *a.Path
				if path != "" && !filepath.IsAbs(path) && filepath.IsAbs(wire.Cwd) {
					path = filepath.Join(wire.Cwd, path)
				}
				location := v5CommandCwd(cwd, &path)
				switch location.Kind {
				case "workspace_root":
					entry.Label = nativeString("当前项目")
				case "workspace_relative":
					entry.Label = traceLabel(strings.Join(location.Segments, "/"), 512, out)
				default:
					entry.Label = nativeString("文件位置已隐藏")
				}
			}
			if a.Type == "search" && a.Query != nil {
				entry.Query = traceLabel(*a.Query, 256, out)
			}
		case "unknown":
			entry.Label = traceCommandPreview(a.Command, out)
		default:
			entry.Kind = native.NativeCommandActionDisplayKind("unknown")
			out.Availability = native.NativeItemAvailabilityPartial
		}
		actions = append(actions, entry)
	}
	out.CommandActions = &actions
}

func projectNativeToolDisplay(wire nativeWireItem, out *native.NativeItem) {
	var label, title *string
	provenance := native.NativeToolDisplayProvenance("nativeAppContext")
	if wire.AppContext != nil {
		label = traceLabel(wire.AppContext.AppName, 160, out)
		title = traceLabel(wire.AppContext.ActionName, 160, out)
	}
	// This Host-owned registered tool already has a bounded display label.
	if wire.Server == "yijie_artifacts" && wire.Tool == "publish_artifact" {
		title = out.ToolLabel
		label = nativeString("易界 AI")
		provenance = native.NativeToolDisplayProvenance("registeredTool")
	}
	// Allow the explicit presentation title only on this known native UI tool.
	// Do not inspect code or use arbitrary tools' parameters to invent a title.
	if wire.Server == "cua_repl" && wire.Tool == "js" {
		var args struct {
			Title string `json:"title"`
		}
		if json.Unmarshal(wire.Arguments, &args) == nil {
			if supplied := traceLabel(args.Title, 160, out); supplied != nil {
				title = supplied
				provenance = native.NativeToolDisplayProvenance("allowedTitle")
			}
		}
		if label == nil {
			label = nativeToolSurfaceLabel(wire.Result, out)
			if label != nil && title == nil {
				// A registered CUA presenter selected this canonical result field.
				provenance = native.NativeToolDisplayProvenance("registeredTool")
			}
		}
	}
	if label == nil && title == nil {
		return
	}
	out.ToolDisplay = &native.NativeToolDisplay{Provenance: provenance, SourceLabel: label, Title: title}
	if title != nil {
		out.ToolLabel = title
	}
}

// codex/toolSurface is native CUA result metadata. Only display fields are
// selected; screenshots, URLs, app IDs, code and arbitrary metadata stay here.
func nativeToolSurfaceLabel(result json.RawMessage, out *native.NativeItem) *string {
	var value struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if json.Unmarshal(result, &value) != nil {
		return nil
	}
	var surface struct {
		Kind string `json:"kind"`
		App  struct {
			Kind        string `json:"kind"`
			DisplayName string `json:"displayName"`
			AppID       string `json:"appId"`
		} `json:"app"`
	}
	if json.Unmarshal(value.Meta["codex/toolSurface"], &surface) != nil {
		return nil
	}
	switch surface.Kind {
	case "browserUse":
		return nativeString("浏览器")
	case "computerUse":
		if surface.App.Kind == "displayName" {
			return traceLabel(surface.App.DisplayName, 160, out)
		}
		if surface.App.Kind == "appId" && surface.App.AppID == "com.yijie.ai" {
			return nativeString("易界 AI")
		}
	}
	return nil
}
