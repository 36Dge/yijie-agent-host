package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeToolsRuntimePatchApprovalRejected(t *testing.T) {
	h := newNativeToolsHarness(t, func(h *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		if n == 1 {
			patch := "*** Begin Patch\n*** Add File: " + filepath.Join(h.root, "ordinary-external-note.txt") + "\n+An ordinary note that requires approval.\n*** End Patch\n"
			return feat164AuxTool("", "apply_patch", "denied_patch169", map[string]any{"input": patch})
		}
		if !strings.Contains(strings.ToLower(nativeToolsOutput(body, "denied_patch169")), "reject") {
			t.Error("native patch rejection missing", nativeToolsOutput(body, "denied_patch169"))
		}
		return feat164AuxText("用户拒绝了文件变更；文件未创建。")
	})
	thread, err := h.manager.StartThread(feat164AuxCtx(), h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	decided := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			for _, approval := range h.manager.ListRuntimeApprovals(thread.ID) {
				if approval.Status == "pending" && approval.Kind == "file_change" {
					_, err := h.manager.DecideRuntimeApproval(context.Background(), thread.ID, approval.ID, "reject")
					decided <- err
					return
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		decided <- fmt.Errorf("native patch approval was not observed")
	}()
	h.turn(t, KimiK31ProfileID, thread.ID, "在我确认后再向所选的外部目录写入普通笔记。")
	if err := <-decided; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(h.root, "ordinary-external-note.txt")); !os.IsNotExist(err) {
		t.Fatal("rejected patch created a file", err)
	}
}

func TestNativeToolsRuntimeMemoryReopen(t *testing.T) {
	const filename = "2026-11-07T12-30-00-preference.md"
	const note = "Use blue for the ordinary sample."
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		switch n {
		case 1:
			return feat164AuxTool("", "memories__add_ad_hoc_note", "persist169", map[string]any{"filename": filename, "note": note})
		case 2:
			return feat164AuxText("已保存普通偏好。")
		case 3:
			return feat164AuxTool("", "memories__read", "reopen169", map[string]any{"path": "extensions/ad_hoc/notes/" + filename})
		case 4:
			if !strings.Contains(nativeToolsOutput(body, "reopen169"), note) {
				t.Error("native note did not survive normal reopen")
			}
			return feat164AuxTool("", "memories__add_ad_hoc_note", "duplicate169", map[string]any{"filename": filename, "note": "A second ordinary note."})
		case 5:
			if !strings.Contains(strings.ToLower(nativeToolsOutput(body, "duplicate169")), "exist") {
				t.Error("duplicate note was not rejected", nativeToolsOutput(body, "duplicate169"))
			}
			return feat164AuxText("已读回原笔记；同名笔记没有覆盖。")
		default:
			t.Error("unexpected background model call")
			return feat164AuxText("正常结束。")
		}
	})
	thread, err := h.manager.StartThread(feat164AuxCtx(), h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "记住这个普通示例偏好。")
	h.restart(t, h.manager.config)
	if _, err = h.manager.ResumeThread(feat164AuxCtx(), thread.ID); err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "读取刚才的笔记，再尝试保存同名普通笔记。")
	actual, err := os.ReadFile(filepath.Join(h.home, "memories/extensions/ad_hoc/notes", filename))
	if err != nil || strings.TrimSpace(string(actual)) != note || h.calls.Load() != 5 {
		t.Fatal("memory persistence or startup isolation differs", err, h.calls.Load())
	}
}

func TestNativeToolsRuntimeCodeCancel(t *testing.T) {
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		switch n {
		case 1:
			return feat164AuxTool("", "exec", "cancel_cell169", map[string]any{"input": "// @exec: {\"yield_time_ms\": 1}\nawait new Promise(resolve => setTimeout(resolve, 10000)); text(42);"})
		case 2:
			match := regexp.MustCompile(`Script running with cell ID ([A-Za-z0-9_-]+)`).FindStringSubmatch(nativeToolsOutput(body, "cancel_cell169"))
			if len(match) != 2 {
				t.Error("missing running V8 cell")
				return feat164AuxText("未获得运行标识。")
			}
			return feat164AuxTool("", "wait", "terminate169", map[string]any{"cell_id": match[1], "terminate": true})
		case 3:
			if !strings.Contains(strings.ToLower(nativeToolsOutput(body, "terminate169")), "terminat") {
				t.Error("V8 normal termination not observed", nativeToolsOutput(body, "terminate169"))
			}
			return feat164AuxText("已按请求结束等待中的原生代码单元。")
		default:
			t.Error("unexpected Code Mode continuation")
			return feat164AuxText("正常结束。")
		}
	})
	thread, err := h.manager.StartThread(feat164AuxCtx(), h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "开始一个普通 JavaScript 定时等待，再用原生 wait 结束它。")
}

func TestNativeToolsRuntimeResumePriorGeneration(t *testing.T) {
	previousBinary := os.Getenv("YIJIE_CODEX_NATIVE_TOOLS_PREVIOUS_BINARY")
	if previousBinary == "" {
		t.Skip("set canonical retained previous Runtime binary")
	}
	for _, profile := range []string{KimiK31ProfileID, "minimax-m3-high-v1"} {
		for _, previousVersion := range []string{"v1", "disabled"} {
			t.Run(profile+"/"+previousVersion, func(t *testing.T) {
				var upgraded atomic.Bool
				h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, _ int64) map[string]any {
					if upgraded.Load() {
						nativeToolsDeclarations(t, body)
					}
					return feat164AuxText("Ordinary persisted conversation.")
				}, func(cfg *Config) {
					cfg.NativeToolsEnabled = false
					cfg.HostImagesEnabled = true
					cfg.BinaryPath = previousBinary
					cfg.ManifestPath = filepath.Join(filepath.Dir(previousBinary), "runtime-manifest.json")
				})
				ctx := WithPermissionMode(WithModelProfile(context.Background(), profile), PermissionAsk)
				legacyCtx := WithMarketPrepared(ctx, &MarketPrepared{Config: map[string]any{"features": map[string]any{"multi_agent": previousVersion == "v1", "multi_agent_v2": false}}})
				thread, err := h.manager.StartThread(legacyCtx, h.cwd)
				if err != nil {
					t.Fatal(err)
				}
				h.turn(t, profile, thread.ID, "Persist an ordinary original conversation.")
				raw, err := h.manager.ReadThread(ctx, thread.ID)
				if err != nil {
					t.Fatal(err)
				}
				var saved struct {
					Path string `json:"path"`
				}
				if err = json.Unmarshal(raw, &saved); err != nil || saved.Path == "" {
					t.Fatal("native history path missing", err)
				}
				before, err := os.ReadFile(saved.Path)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(before), `"multi_agent_version":"`+previousVersion+`"`) {
					t.Fatal("retained Runtime did not establish the requested prior agent generation", previousVersion)
				}
				cfg := h.manager.config
				cfg.BinaryPath, cfg.ManifestPath = integrationArtifactPaths(t)
				cfg.NativeToolsEnabled, cfg.HostImagesEnabled = true, false
				h.restart(t, cfg)
				upgraded.Store(true)
				if _, err = h.manager.ResumeThread(ctx, thread.ID); err != nil {
					t.Fatal(err)
				}
				h.turn(t, profile, thread.ID, "Continue the original conversation with the native V2 tool profile.")
				after, err := os.ReadFile(saved.Path)
				if err != nil || !bytes.HasPrefix(after, before) {
					t.Fatal("original native history was rewritten", err)
				}
				if h.calls.Load() != 2 {
					t.Fatal("unexpected restart/background inference", h.calls.Load())
				}
			})
		}
	}
}
