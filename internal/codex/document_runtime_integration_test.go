package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func documentTestConfig(t *testing.T) func(*Config) {
	t.Helper()
	root := integrationPrivateTempDir(t)
	t.Setenv("YIJIE_DOCUMENT_STATE_ROOT", root)
	return func(c *Config) {
		c.InteractionToolsEnabled = true
		c.ToolSearchEnabled = true
		c.DocumentRuntimeEnabled = true
	}
}

func TestDocumentRuntimeManagedOfficeWithNativeApproval(t *testing.T) {
	bundle := os.Getenv("YIJIE_DOCUMENT_TEST_BUNDLE")
	if bundle == "" {
		t.Skip("requires built document bundle")
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	python := filepath.Join(bundle, "python/bin/python3.12")
	helper := filepath.Join(bundle, "helper/document_cli.py")
	prefix := quote(python) + " -I -B " + quote(helper)
	configure := documentTestConfig(t)
	phase := 0
	lastCall, collected := "", ""
	h := newNativeToolsHarness(t, func(h *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		if lastCall != "" {
			value := nativeToolsOutput(body, lastCall)
			collected += value
			if match := regexp.MustCompile(`Script running with cell ID ([A-Za-z0-9_-]+)`).FindStringSubmatch(value); len(match) == 2 {
				lastCall = fmt.Sprintf("document-wait-%d", n)
				return feat164AuxTool("", "wait", lastCall, map[string]any{"cell_id": match[1], "yield_time_ms": 1000})
			}
			if phase == 1 && !strings.Contains(collected, "generated") {
				t.Error("managed Python generation failed", collected)
			}
			if phase == 2 && !strings.Contains(collected, "native_platform_approval_required") {
				t.Error("missing explicit platform approval boundary", collected)
			}
			if phase == 4 && !strings.Contains(collected, "input_image") {
				t.Error("native view_image did not return model image content")
			}
		}
		if phase < 4 {
			phase++
			if phase == 4 {
				imagePath, _ := json.Marshal(filepath.Join(h.cwd, "native-qa/page-001.png"))
				lastCall = "document-view-image"
				collected = ""
				return feat164AuxTool("", "exec", lastCall, map[string]any{"input": "const result=await tools.view_image({path:" + string(imagePath) + "});image(result);"})
			}
			command := prefix + " render native.docx --output native-qa"
			if phase == 1 {
				command = prefix + " create --format docx --spec spec.json --output native.docx"
			}
			args := map[string]any{"cmd": command, "login": false, "yield_time_ms": 1000}
			if phase == 3 {
				args["sandbox_permissions"] = "require_escalated"
				args["justification"] = "Use the bundled Office engine for this ordinary local document preview, after native approval."
			}
			encoded, _ := json.Marshal(args)
			lastCall = fmt.Sprintf("document-sandbox-%d", phase)
			collected = ""
			return feat164AuxTool("", "exec", lastCall, map[string]any{"input": `let r=await tools.exec_command(` + string(encoded) + `);text(r);for(let i=0;r.session_id&&i<30;i++){r=await tools.write_stdin({session_id:r.session_id,chars:"",yield_time_ms:1000});text(r);}`})
		}
		raw, err := os.ReadFile(filepath.Join(h.cwd, "native-qa/receipt.json"))
		if err != nil {
			logs, _ := filepath.Glob(filepath.Join(os.Getenv("YIJIE_DOCUMENT_STATE_ROOT"), "job-*", "*.log"))
			for _, path := range logs {
				data, _ := os.ReadFile(path)
				if len(data) > 8192 {
					data = data[len(data)-8192:]
				}
				t.Log(filepath.Base(path), string(data))
			}
			t.Error("managed Office render did not complete", err, nativeToolsOutput(body, "document-sandbox-2"))
		} else {
			var receipt map[string]any
			_ = json.Unmarshal(raw, &receipt)
			if receipt["pages"] != float64(1) {
				t.Error("missing native page")
			}
		}
		return feat164AuxText("本地专用环境生成与页图检查完成。")
	}, configure)
	if err := os.WriteFile(filepath.Join(h.cwd, "spec.json"), []byte(`{"title":"普通文档","paragraphs":["专用环境的正常页面。"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	thread, err := h.manager.StartThread(feat164AuxCtx(), h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	var effective struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	if err := h.manager.request(context.Background(), "config/read", map[string]any{"cwd": h.cwd, "includeLayers": false}, &effective); err != nil {
		t.Fatal(err)
	}
	if err := validateMarketEffectiveIsolationForTools(effective.Config, true, true); err != nil {
		t.Fatal("document variables changed optional market isolation", err)
	}
	if err := validateMarketEffectiveIsolationForTools(effective.Config, true); err == nil {
		t.Fatal("legacy profile silently accepted new document variables")
	}
	approvalContext, cancelApproval := context.WithCancel(context.Background())
	defer cancelApproval()
	approved := make(chan struct{}, 1)
	go func() {
		for {
			select {
			case <-approvalContext.Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
			for _, view := range h.manager.ListRuntimeApprovals(thread.ID) {
				if view.Status == "pending" {
					if !strings.Contains(view.Summary, python) {
						t.Error("unexpected command approval")
						return
					}
					receipt, err := h.manager.DecideRuntimeApproval(approvalContext, thread.ID, view.ID, "approve_once")
					if err != nil || receipt.Status != "approved" {
						t.Error("native one-command approval failed", err)
						return
					}
					approved <- struct{}{}
					return
				}
			}
		}
	}()
	h.turn(t, KimiK31ProfileID, thread.ID, "生成并渲染一个普通 Word 文档。")
	select {
	case <-approved:
	default:
		t.Error("managed conversion did not use the existing native approval")
	}
}

// Uses the genuine canonical Runtime and bounded ordinary commands. Responses
// come from the local HTTP harness, never a supplier/model or a fake executable.
func TestDocumentRuntimeNormalExecAndShutdown(t *testing.T) {
	configure := documentTestConfig(t)
	h := newNativeToolsHarness(t, func(h *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		if n == 1 {
			return feat164AuxTool("", "exec", "document171", map[string]any{"input": "let r=await tools.exec_command({cmd:\"/bin/echo document-runtime-ready\", login:false, yield_time_ms:1000}); text(r);"})
		}
		if n != 2 || !strings.Contains(nativeToolsOutput(body, "document171"), "document-runtime-ready") {
			t.Error("native document command did not complete", nativeToolsOutput(body, "document171"))
		}
		return feat164AuxText("本地原生执行完成。")
	}, configure)
	ctx := feat164AuxCtx()
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "运行一个普通本地只读命令。")
	if h.calls.Load() != 2 {
		t.Fatal("unexpected requests", h.calls.Load())
	}
}

func TestDocumentRuntimeNormalCancellation(t *testing.T) {
	configure := documentTestConfig(t)
	h := newNativeToolsHarness(t, func(h *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		if n == 1 {
			return feat164AuxTool("", "exec", "cancel171", map[string]any{"input": "text(await tools.exec_command({cmd:\"printf ready > normal-command-started; /bin/sleep 2; printf finished\", login:false, yield_time_ms:1000}));"})
		}
		return feat164AuxText("正常收尾。")
	}, configure)
	thread, err := h.manager.StartThread(feat164AuxCtx(), h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := TextUserInput("开始一个有界的普通命令，然后由用户取消。")
	turn, err := h.manager.StartTurnV2(feat164AuxCtx(), thread.ID, []UserInput{input}, "max")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(h.cwd, "normal-command-started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("normal command did not start")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err := h.manager.InterruptTurn(context.Background(), thread.ID, turn.ID); err != nil {
		t.Fatal(err)
	}
	status, _, err := h.notifications.waitForTurn(turn.ID, 30*time.Second)
	if err != nil || status != "interrupted" {
		t.Fatalf("normal cancellation status=%s err=%v", status, err)
	}
}
