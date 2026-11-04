package codex

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Explicit, one-turn-per-model diagnostic of the existing native Responses
// path. Reuses the passive meter; requests/SSE are not rewritten. A successful
// test means observation completed, not that public progress quality qualified.
func TestPublicProgressWireDiagnostic(t *testing.T) {
	if os.Getenv("YIJIE_PUBLIC_PROGRESS_WIRE_DIAGNOSTIC") != "1" {
		t.Skip("requires current task's explicit paid diagnostic authorization")
	}
	binary, manifest := integrationArtifactPaths(t)
	out := os.Getenv("YIJIE_PUBLIC_PROGRESS_EVIDENCE_DIR")
	if !filepath.IsAbs(out) {
		t.Fatal("absolute diagnostic evidence directory required")
	}
	mini, err := integrationMiniMaxKey()
	if err != nil {
		t.Fatal("approved MiniMax credential source unavailable")
	}
	kimiPath := os.Getenv("YIJIE_KIMI_API_KEY_FILE")
	info, err := os.Lstat(kimiPath)
	if err != nil || !filepath.IsAbs(kimiPath) || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		t.Fatal("approved owner-only Kimi source unavailable")
	}
	rawKey, err := os.ReadFile(kimiPath)
	if err != nil {
		t.Fatal("Kimi source unreadable")
	}
	kimi := strings.TrimSpace(string(rawKey))
	if kimi == "" || strings.ContainsAny(kimi, "\r\n\x00") {
		t.Fatal("Kimi source invalid")
	}
	hostRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(out, "wire-meter-ledger.json")
	if _, err = os.Stat(ledger); err != nil {
		t.Fatal("pre-approved diagnostic ledger is required")
	}
	command := exec.Command("python3", "-B", filepath.Join(hostRoot, "scripts/chat-model-smoke-meter.py"),
		"--port", "0", "--ledger", ledger, "--key-file", os.Getenv("YIJIE_MINIMAX_API_KEY_FILE"), "--kimi-key-file", kimiPath,
		"--observe-public-progress", "--shutdown-on-stdin-eof")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.OpenFile(filepath.Join(out, "wire-meter-process.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = stderr
	if err = command.Start(); err != nil {
		t.Fatal("meter startup failed")
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		if err := command.Wait(); err != nil {
			t.Error("normal meter EOF shutdown failed")
		}
		_ = stderr.Close()
	})
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatal("meter readiness unavailable")
	}
	var ready struct {
		Ready bool `json:"ready"`
		Port  int  `json:"port"`
	}
	if json.Unmarshal(scanner.Bytes(), &ready) != nil || !ready.Ready || ready.Port <= 0 {
		t.Fatal("invalid local meter readiness")
	}
	var observations []map[string]any
	for _, profile := range []struct{ ID, Model, Effort string }{{"kimi-k3-1-pro-max-v1", "kimi-k3.1-pro", "max"}, {"minimax-m3-high-v1", "MiniMax-M3", "high"}} {
		t.Run(profile.Model, func(t *testing.T) {
			root, err := os.MkdirTemp(out, "wire-native-")
			if err != nil {
				t.Fatal(err)
			}
			root, err = filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			home, cwd := filepath.Join(root, "runtime"), filepath.Join(root, "workspace")
			for _, p := range []string{home, cwd} {
				if err = os.Mkdir(p, 0700); err != nil {
					t.Fatal(err)
				}
			}
			cfg := integrationConfig(binary, manifest, home, mini)
			cfg.ChatModelsEnabled, cfg.KimiK31Candidate, cfg.RuntimePermissionsEnabled = true, true, true
			cfg.KimiAPIKey = kimi
			cfg.PermissionVerificationBaseURL = fmt.Sprintf("http://127.0.0.1:%d/v1", ready.Port)
			manager := NewManager(cfg, nil)
			notifications := newIntegrationNotifications()
			var mu sync.Mutex
			messages := []map[string]any{}
			if err = manager.SetNotificationHandler(func(method string, raw json.RawMessage) {
				if method == "item/completed" {
					var event struct {
						Item struct {
							ID, Type, Text string
							Phase          *string
						} `json:"item"`
					}
					if json.Unmarshal(raw, &event) == nil && event.Item.Type == "agentMessage" {
						idHash, textHash := sha256.Sum256([]byte(event.Item.ID)), sha256.Sum256([]byte(event.Item.Text))
						mu.Lock()
						messages = append(messages, map[string]any{"item_sha256": hex.EncodeToString(idHash[:]), "text_sha256": hex.EncodeToString(textHash[:]), "text_bytes": len([]byte(event.Item.Text)), "phase": event.Item.Phase})
						mu.Unlock()
					}
				}
				notifications.handle(method, raw)
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if manager.Shutdown(ctx) != nil {
					t.Error("normal native shutdown pending; evidence retained")
				}
			})
			if manager.Start(context.Background()) != nil {
				t.Fatal("managed native startup failed")
			}
			ctx := WithPermissionMode(WithModelProfile(context.Background(), profile.ID), PermissionAsk)
			thread, err := manager.StartThread(ctx, cwd)
			if err != nil {
				t.Fatal("native thread startup failed")
			}
			input, _ := TextUserInput("向非技术同事比较同步协作与异步协作，给出适用场景和组合办法。不调用工具，正文约350字。")
			turn, err := manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, profile.Effort)
			if err != nil {
				t.Fatal("native submit failed")
			}
			status, _, waitErr := notifications.waitForTurn(turn.ID, 4*time.Minute)
			if waitErr != nil {
				_ = manager.InterruptTurn(ctx, thread.ID, turn.ID)
			}
			mu.Lock()
			row := map[string]any{"model": profile.Model, "case": "N02-wire-audit", "runtime_home": home, "thread_id": thread.ID, "turn_id": turn.ID, "native_status": status, "observation_timed_out": waitErr != nil, "public_messages": messages, "scope": "Passive provider wire vs native event metadata; not App UI or quality qualification; no existing session data modified."}
			mu.Unlock()
			observations = append(observations, row)
			bytes, _ := json.MarshalIndent(observations, "", "  ")
			if os.WriteFile(filepath.Join(out, "wire-native-observations.json"), append(bytes, '\n'), 0600) != nil {
				t.Error("could not save diagnostic metadata")
			}
			t.Logf("%s one native diagnostic turn: status=%s public_items=%d; no generation quality PASS implied", profile.Model, status, len(messages))
		})
	}
}
