package session

import (
	"path/filepath"
	"strings"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
)

// Transient selected fields of canonical ThreadItem.FileChange. No new tool
// producer or lifecycle is inferred here; the Runtime supplies the facts.
type nativeWireFileChange struct {
	Path string `json:"path"`
	Kind struct {
		Type string `json:"type"`
	} `json:"kind"`
	Diff string `json:"diff"`
}

func nativeOperationDisplay(out *native.NativeItem, title, source string) {
	out.ToolLabel = nativeString(title)
	out.ToolDisplay = &native.NativeToolDisplay{Provenance: native.NativeToolDisplayProvenance("registeredTool"), Title: nativeString(title), SourceLabel: nativeString(source)}
}

func projectNativeFileChange(wire nativeWireItem, cwd string, out *native.NativeItem) {
	nativeOperationDisplay(out, "修改文件", "文件")
	labels := []string{}
	var detail strings.Builder
	for index, change := range wire.Changes {
		if index >= 64 {
			out.Availability = native.NativeItemAvailabilityPartial
			break
		}
		file := change.Path
		if file != "" && !filepath.IsAbs(file) && filepath.IsAbs(cwd) {
			file = filepath.Join(cwd, file)
		}
		location := v5CommandCwd(cwd, &file)
		label := "工作区外文件"
		if location.Kind == "workspace_root" {
			label = "当前项目"
		} else if location.Kind == "workspace_relative" {
			label = strings.Join(location.Segments, "/")
		}
		action := map[string]string{"add": "新增", "update": "修改", "delete": "删除"}[change.Kind.Type]
		if action == "" {
			action = "修改"
		}
		label = action + " " + label
		if safe := traceLabel(label, 512, out); safe != nil {
			labels = append(labels, *safe)
		}
		if detail.Len() < nativeTextLimit {
			value := label + "\n" + redactV5CommandText(change.Diff) + "\n"
			bounded, cut := truncateV5UTF8(value, nativeTextLimit-detail.Len())
			detail.WriteString(bounded)
			if cut {
				out.Availability = native.NativeItemAvailabilityPartial
			}
		}
	}
	if len(labels) == 0 {
		out.Availability = native.NativeItemAvailabilityPartial
		out.Text = nativeString("文件变更明细不可用。")
	} else {
		out.Text = nativeString(strings.Join(labels, "；"))
	}
	if detail.Len() > 0 {
		out.OutputText = nativeString(detail.String())
	}
}

func projectNativeAgentActivity(wire nativeWireItem, out *native.NativeItem) {
	title := map[string]string{"started": "启动子任务", "interacted": "与子任务交互", "interrupted": "中断子任务"}[wire.Kind]
	if title == "" {
		title = "子任务活动"
		out.Availability = native.NativeItemAvailabilityPartial
	}
	nativeOperationDisplay(out, title, "子任务")
	// AgentPath is a task identity, not a filesystem path. Present the relative
	// task name so the generic path redactor does not turn it into a disk label.
	if task := traceLabel(strings.TrimPrefix(wire.AgentPath, "/root/"), 512, out); task != nil {
		out.Text = nativeString(title + " " + *task)
	} else {
		out.Text = nativeString(title)
	}
	// A completed activity Item does not assert that the child task completed.
}
