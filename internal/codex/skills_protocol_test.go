package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"
)

type skillsProtocolTestRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type skillsProtocolTestHarness struct {
	manager   *Manager
	requests  <-chan skillsProtocolTestRequest
	responses chan<- json.RawMessage
	logs      *bytes.Buffer
}

func newSkillsProtocolTestHarness(t *testing.T) skillsProtocolTestHarness {
	t.Helper()
	clientOutput, runtimeInput := io.Pipe()
	runtimeOutput, clientInput := io.Pipe()
	requests := make(chan skillsProtocolTestRequest, 8)
	responses := make(chan json.RawMessage, 8)
	logs := &bytes.Buffer{}
	client := NewClient(runtimeInput, runtimeOutput, 1<<20, 8, nil, nil)
	client.Start()

	go func() {
		decoder := json.NewDecoder(clientOutput)
		encoder := json.NewEncoder(clientInput)
		for {
			var request skillsProtocolTestRequest
			if err := decoder.Decode(&request); err != nil {
				return
			}
			requests <- request
			result, ok := <-responses
			if !ok {
				return
			}
			if err := encoder.Encode(struct {
				ID     json.RawMessage `json:"id"`
				Result json.RawMessage `json:"result"`
			}{ID: request.ID, Result: result}); err != nil {
				return
			}
		}
	}()

	config := DefaultConfig()
	config.RequestTimeout = 2 * time.Second
	manager := NewManager(config, slog.New(slog.NewTextHandler(logs, nil)))
	manager.mu.Lock()
	manager.client = client
	manager.status.State = StateReady
	manager.status.Ready = true
	manager.mu.Unlock()

	t.Cleanup(func() {
		close(responses)
		_ = client.CloseInput()
		_ = clientInput.Close()
	})
	return skillsProtocolTestHarness{
		manager: manager, requests: requests, responses: responses, logs: logs,
	}
}

func (h skillsProtocolTestHarness) nextRequest(t *testing.T) skillsProtocolTestRequest {
	t.Helper()
	select {
	case request := <-h.requests:
		return request
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Runtime Skills request")
		return skillsProtocolTestRequest{}
	}
}

func TestSupportedSkillsRuntimeMethods(t *testing.T) {
	want := []string{
		RuntimeMethodSkillsList,
		RuntimeMethodSkillsExtraRootsSet,
		RuntimeMethodSkillsConfigWrite,
	}
	got := SupportedSkillsRuntimeMethods()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected Runtime Skills methods: %v", got)
	}
	got[0] = "changed"
	if reflect.DeepEqual(SupportedSkillsRuntimeMethods(), got) {
		t.Fatal("SupportedSkillsRuntimeMethods returned mutable package state")
	}
	if RuntimeNotificationSkillsChanged != "skills/changed" {
		t.Fatalf("unexpected Skills notification: %q", RuntimeNotificationSkillsChanged)
	}
}

func TestListSkillsMapsPinnedRuntimeWire(t *testing.T) {
	harness := newSkillsProtocolTestHarness(t)
	type result struct {
		data []SkillsListEntry
		err  error
	}
	resultDone := make(chan result, 1)
	go func() {
		data, err := harness.manager.ListSkills(context.Background(), []string{"/workspace"}, true)
		resultDone <- result{data: data, err: err}
	}()

	request := harness.nextRequest(t)
	if request.Method != RuntimeMethodSkillsList {
		t.Fatalf("unexpected method: %q", request.Method)
	}
	var params map[string]any
	if err := json.Unmarshal(request.Params, &params); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(params, map[string]any{
		"cwds":        []any{"/workspace"},
		"forceReload": true,
	}) {
		t.Fatalf("unexpected skills/list params: %s", request.Params)
	}

	harness.responses <- json.RawMessage(`{
		"data":[{
			"cwd":"/workspace",
			"skills":[{
				"name":"copywriting",
				"description":"Write concise product copy",
				"shortDescription":"Product copy",
				"interface":{
					"displayName":"Copywriting",
					"shortDescription":null,
					"iconSmall":"/managed/copywriting/icon-small.png",
					"iconLarge":null,
					"brandColor":"#123456",
					"defaultPrompt":"Draft copy"
				},
				"dependencies":{"tools":[{
					"type":"mcp",
					"value":"search",
					"description":"Search catalog",
					"transport":"stdio",
					"command":"search-server",
					"url":null
				}]},
				"path":"/managed/copywriting/SKILL.md",
				"scope":"user",
				"enabled":true
			}],
			"errors":[{"path":"relative/broken/SKILL.md","message":"invalid metadata"}]
		}]
	}`)

	got := <-resultDone
	if got.err != nil {
		t.Fatalf("list skills: %v", got.err)
	}
	if len(got.data) != 1 || got.data[0].CWD != "/workspace" ||
		len(got.data[0].Skills) != 1 || len(got.data[0].Errors) != 1 {
		t.Fatalf("unexpected skills/list result: %+v", got.data)
	}
	skill := got.data[0].Skills[0]
	if skill.Name != "copywriting" || skill.Path != "/managed/copywriting/SKILL.md" ||
		skill.Scope != SkillScopeUser || !skill.Enabled || skill.ShortDescription == nil ||
		*skill.ShortDescription != "Product copy" || skill.Interface == nil ||
		skill.Interface.IconSmall == nil || *skill.Interface.IconSmall != "/managed/copywriting/icon-small.png" ||
		skill.Dependencies == nil || len(skill.Dependencies.Tools) != 1 ||
		skill.Dependencies.Tools[0].Value != "search" {
		t.Fatalf("unexpected Runtime skill metadata: %+v", skill)
	}
}

func TestListSkillsOmitsPinnedRuntimeDefaults(t *testing.T) {
	harness := newSkillsProtocolTestHarness(t)
	done := make(chan error, 1)
	go func() {
		data, err := harness.manager.ListSkills(context.Background(), nil, false)
		if err == nil && data == nil {
			err = io.ErrUnexpectedEOF
		}
		done <- err
	}()
	request := harness.nextRequest(t)
	if request.Method != RuntimeMethodSkillsList || string(request.Params) != "{}" {
		t.Fatalf("unexpected default skills/list frame: method=%q params=%s", request.Method, request.Params)
	}
	harness.responses <- json.RawMessage(`{"data":[]}`)
	if err := <-done; err != nil {
		t.Fatalf("list default skills: %v", err)
	}
}

func TestSetSkillsExtraRootsMapsPinnedRuntimeWire(t *testing.T) {
	harness := newSkillsProtocolTestHarness(t)
	for _, test := range []struct {
		name  string
		roots []string
		want  string
	}{
		{name: "set", roots: []string{"/managed/skills"}, want: `{"extraRoots":["/managed/skills"]}`},
		{name: "clear", roots: nil, want: `{"extraRoots":[]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			done := make(chan error, 1)
			go func() {
				done <- harness.manager.SetSkillsExtraRoots(context.Background(), test.roots)
			}()
			request := harness.nextRequest(t)
			if request.Method != RuntimeMethodSkillsExtraRootsSet || string(request.Params) != test.want {
				t.Fatalf("unexpected extra roots frame: method=%q params=%s", request.Method, request.Params)
			}
			harness.responses <- json.RawMessage(`{}`)
			if err := <-done; err != nil {
				t.Fatalf("set Runtime Skill extra roots: %v", err)
			}
		})
	}
}

func TestWriteSkillConfigMapsPinnedRuntimeWire(t *testing.T) {
	harness := newSkillsProtocolTestHarness(t)
	type result struct {
		enabled bool
		err     error
	}
	done := make(chan result, 1)
	go func() {
		enabled, err := harness.manager.WriteSkillConfig(
			context.Background(), "/managed/copywriting/SKILL.md", false,
		)
		done <- result{enabled: enabled, err: err}
	}()
	request := harness.nextRequest(t)
	if request.Method != RuntimeMethodSkillsConfigWrite {
		t.Fatalf("unexpected method: %q", request.Method)
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(request.Params, &params); err != nil {
		t.Fatal(err)
	}
	if len(params) != 3 || string(params["path"]) != `"/managed/copywriting/SKILL.md"` ||
		string(params["name"]) != "null" || string(params["enabled"]) != "false" {
		t.Fatalf("unexpected skills/config/write params: %s", request.Params)
	}
	harness.responses <- json.RawMessage(`{"effectiveEnabled":false}`)
	got := <-done
	if got.err != nil || got.enabled {
		t.Fatalf("write Runtime Skill config: enabled=%t err=%v", got.enabled, got.err)
	}
}

func TestRuntimeSkillsInputValidationDoesNotExposePaths(t *testing.T) {
	manager := &Manager{}
	secret := "relative/private/merchant/SKILL.md"
	if _, err := manager.ListSkills(context.Background(), []string{secret}, false); err == nil ||
		strings.Contains(err.Error(), secret) {
		t.Fatalf("unexpected relative cwd validation error: %v", err)
	}
	nonCleanSecret := "/private/merchant/../secret"
	if _, err := manager.ListSkills(context.Background(), []string{nonCleanSecret}, false); err == nil ||
		strings.Contains(err.Error(), nonCleanSecret) {
		t.Fatalf("unexpected non-clean cwd validation error: %v", err)
	}
	if _, err := manager.ListSkills(context.Background(), []string{""}, false); err == nil {
		t.Fatal("expected empty cwd entry to fail")
	}
	if err := manager.SetSkillsExtraRoots(context.Background(), []string{secret}); err == nil ||
		strings.Contains(err.Error(), secret) {
		t.Fatalf("unexpected extra root validation error: %v", err)
	}
	if _, err := manager.WriteSkillConfig(context.Background(), secret, true); err == nil ||
		strings.Contains(err.Error(), secret) {
		t.Fatalf("unexpected Skill config validation error: %v", err)
	}
	if _, err := manager.WriteSkillConfig(context.Background(), "/managed/not-a-skill.txt", true); err == nil {
		t.Fatal("expected non-SKILL.md config path to fail")
	}
}

func TestListSkillsRejectsMalformedResponsesWithoutPathDisclosure(t *testing.T) {
	harness := newSkillsProtocolTestHarness(t)
	secret := "/private/merchant/secret/SKILL.md"
	tests := []struct {
		name     string
		response string
	}{
		{name: "unknown top-level field", response: `{"data":[],"/private/merchant/secret/SKILL.md":true}`},
		{name: "missing data", response: `{}`},
		{name: "null data", response: `{"data":null}`},
		{name: "missing entry field", response: `{"data":[{"skills":[],"errors":[]}]}`},
		{name: "unknown nested field", response: `{"data":[{"cwd":"/repo","skills":[],"errors":[],"secret":"/private/merchant/secret/SKILL.md"}]}`},
		{name: "relative Skill path", response: `{"data":[{"cwd":"/repo","skills":[{"name":"bad","description":"bad","path":"relative/SKILL.md","scope":"user","enabled":true}],"errors":[]}]}`},
		{name: "invalid scope", response: `{"data":[{"cwd":"/repo","skills":[{"name":"bad","description":"bad","path":"/private/merchant/secret/SKILL.md","scope":"other","enabled":true}],"errors":[]}]}`},
		{name: "missing enabled", response: `{"data":[{"cwd":"/repo","skills":[{"name":"bad","description":"bad","path":"/private/merchant/secret/SKILL.md","scope":"user"}],"errors":[]}]}`},
		{name: "missing dependency tools", response: `{"data":[{"cwd":"/repo","skills":[{"name":"bad","description":"bad","dependencies":{},"path":"/private/merchant/secret/SKILL.md","scope":"user","enabled":true}],"errors":[]}]}`},
		{name: "relative icon", response: `{"data":[{"cwd":"/repo","skills":[{"name":"bad","description":"bad","interface":{"iconSmall":"relative.png"},"path":"/private/merchant/secret/SKILL.md","scope":"user","enabled":true}],"errors":[]}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			done := make(chan error, 1)
			go func() {
				_, err := harness.manager.ListSkills(context.Background(), nil, false)
				done <- err
			}()
			request := harness.nextRequest(t)
			if request.Method != RuntimeMethodSkillsList {
				t.Fatalf("unexpected method: %q", request.Method)
			}
			harness.responses <- json.RawMessage(test.response)
			err := <-done
			if err == nil || !strings.Contains(err.Error(), "skills/list response is invalid") {
				t.Fatalf("expected strict response failure, got %v", err)
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(harness.logs.String(), secret) {
				t.Fatalf("Runtime path leaked through error or log: err=%v logs=%q", err, harness.logs.String())
			}
		})
	}
}

func TestRuntimeSkillsRejectsMalformedAcknowledgements(t *testing.T) {
	harness := newSkillsProtocolTestHarness(t)

	extraDone := make(chan error, 1)
	go func() {
		extraDone <- harness.manager.SetSkillsExtraRoots(context.Background(), nil)
	}()
	_ = harness.nextRequest(t)
	harness.responses <- json.RawMessage(`{"path":"/private/merchant/secret"}`)
	if err := <-extraDone; err == nil || strings.Contains(err.Error(), "/private/merchant/secret") {
		t.Fatalf("unexpected extra roots response error: %v", err)
	}

	configDone := make(chan error, 1)
	go func() {
		_, err := harness.manager.WriteSkillConfig(context.Background(), "/managed/skill/SKILL.md", true)
		configDone <- err
	}()
	_ = harness.nextRequest(t)
	harness.responses <- json.RawMessage(`{"effectiveEnabled":false}`)
	if err := <-configDone; err == nil || !strings.Contains(err.Error(), "inconsistent") {
		t.Fatalf("unexpected config response error: %v", err)
	}

	configDone = make(chan error, 1)
	go func() {
		_, err := harness.manager.WriteSkillConfig(context.Background(), "/managed/skill/SKILL.md", true)
		configDone <- err
	}()
	_ = harness.nextRequest(t)
	harness.responses <- json.RawMessage(`{}`)
	if err := <-configDone; err == nil || !strings.Contains(err.Error(), "response is invalid") {
		t.Fatalf("unexpected missing config response field error: %v", err)
	}
}

func TestValidateSkillsChangedNotification(t *testing.T) {
	if err := ValidateSkillsChangedNotification(json.RawMessage(`{}`)); err != nil {
		t.Fatalf("validate skills/changed: %v", err)
	}
	for _, raw := range []string{
		`null`,
		`[]`,
		`{"path":"/private/merchant/secret/SKILL.md"}`,
		`{} {}`,
	} {
		err := ValidateSkillsChangedNotification(json.RawMessage(raw))
		if err == nil {
			t.Fatalf("expected invalid skills/changed payload %s to fail", raw)
		}
		if strings.Contains(err.Error(), "/private/merchant/secret/SKILL.md") {
			t.Fatalf("notification error leaked Runtime path: %v", err)
		}
	}
}
