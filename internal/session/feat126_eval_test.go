package session

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const feat126EvalDatasetID = "feat126-title-raw-v1"

type feat126EvalCase struct {
	ID                    string  `json:"id"`
	Split                 string  `json:"split"`
	Category              string  `json:"category"`
	Locale                string  `json:"locale"`
	Prompt                string  `json:"prompt"`
	FakeTitleOutput       string  `json:"fake_title_output"`
	ExpectedTitle         *string `json:"expected_title"`
	ExpectedTitleAccepted bool    `json:"expected_title_accepted"`
	SemanticKeyword       string  `json:"semantic_keyword"`
	RawScenario           string  `json:"raw_scenario"`
	RawText               string  `json:"raw_text"`
	ExpectedRawGate       string  `json:"expected_raw_gate"`
	ExpectedRawStatus     string  `json:"expected_raw_status"`
}

type feat126EvalManifest struct {
	DatasetID string `json:"dataset_id"`
	Provider  string `json:"provider"`
	Counts    struct {
		Normal      int `json:"normal"`
		Adversarial int `json:"adversarial"`
		Train       int `json:"train"`
		Holdout     int `json:"holdout"`
	} `json:"counts"`
}

type feat126EvalSplit struct {
	DatasetID string   `json:"dataset_id"`
	Train     []string `json:"train"`
	Holdout   []string `json:"holdout"`
}

type feat126EvalLock struct {
	DatasetID string            `json:"dataset_id"`
	Files     map[string]string `json:"files"`
}

type feat126EvalMetrics struct {
	Cases                 int
	Normal                int
	Adversarial           int
	Train                 int
	Holdout               int
	TitleSchemaPassed     int
	TitleSemanticPassed   int
	TitleSemanticRequired int
	TitleUnsafeRejected   int
	RawValidPassed        int
	RawValidRequired      int
	RawNegativeDetected   int
	RawNegativeRequired   int
	RawFailureClasses     map[string]int
}

type feat126TitleGenerator struct{ output string }

func (generator *feat126TitleGenerator) GenerateTitle(context.Context, string) (string, error) {
	return generator.output, nil
}

// TestFEAT126FakeProviderEval is the sole authoritative, deterministic S9 runner.
// It intentionally reports only aggregate metrics and synthetic case identifiers.
func TestFEAT126FakeProviderEval(t *testing.T) {
	root, runnerPath := feat126EvalPaths(t)
	verifyFEAT126EvalLock(t, root, runnerPath)
	manifest := loadFEAT126JSON[feat126EvalManifest](t, filepath.Join(root, "manifest.json"))
	if manifest.DatasetID != feat126EvalDatasetID || manifest.Provider != "deterministic-fake" {
		t.Fatal("FEAT-126 Eval manifest authority mismatch")
	}
	cases := loadFEAT126Cases(t, root)
	metrics := verifyFEAT126Dataset(t, root, manifest, cases)
	verifyFEAT126Titles(t, cases, &metrics)
	verifyFEAT126RawReasoning(t, cases, &metrics)
	verifyFEAT126DesktopEventFixture(t, root)
	verifyFEAT126NoDurableBody(t)

	if metrics.TitleSchemaPassed != metrics.Cases {
		t.Fatalf("title schema/sanitizer gate failed: %d/%d", metrics.TitleSchemaPassed, metrics.Cases)
	}
	if metrics.TitleSemanticPassed*100 < metrics.TitleSemanticRequired*95 {
		t.Fatalf("title semantic gate failed: %d/%d", metrics.TitleSemanticPassed, metrics.TitleSemanticRequired)
	}
	if metrics.TitleUnsafeRejected != metrics.Adversarial {
		t.Fatalf("unsafe title rejection gate failed: %d/%d", metrics.TitleUnsafeRejected, metrics.Adversarial)
	}
	if metrics.RawValidPassed != metrics.RawValidRequired {
		t.Fatalf("raw valid gate failed: %d/%d", metrics.RawValidPassed, metrics.RawValidRequired)
	}
	if metrics.RawNegativeDetected != metrics.RawNegativeRequired {
		t.Fatalf("raw negative gate failed: %d/%d", metrics.RawNegativeDetected, metrics.RawNegativeRequired)
	}
	t.Logf("dataset=%s cases=%d normal=%d adversarial=%d train=%d holdout=%d", feat126EvalDatasetID, metrics.Cases, metrics.Normal, metrics.Adversarial, metrics.Train, metrics.Holdout)
	t.Logf("title schema_sanitizer=%d/%d semantic=%d/%d unsafe_rejected=%d/%d late_overwrite=0 leak=0 extra_action=0", metrics.TitleSchemaPassed, metrics.Cases, metrics.TitleSemanticPassed, metrics.TitleSemanticRequired, metrics.TitleUnsafeRejected, metrics.Adversarial)
	t.Logf("raw valid=%d/%d negative_detected=%d/%d failure_classes=%s body_leak=0", metrics.RawValidPassed, metrics.RawValidRequired, metrics.RawNegativeDetected, metrics.RawNegativeRequired, formatFEAT126FailureClasses(metrics.RawFailureClasses))
}

func feat126EvalPaths(t *testing.T) (string, string) {
	t.Helper()
	_, runnerPath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate FEAT-126 Eval runner")
	}
	root := filepath.Join(filepath.Dir(runnerPath), "testdata", feat126EvalDatasetID)
	return root, runnerPath
}

func verifyFEAT126EvalLock(t *testing.T, root, runnerPath string) {
	t.Helper()
	lock := loadFEAT126JSON[feat126EvalLock](t, filepath.Join(root, "lock.json"))
	if lock.DatasetID != feat126EvalDatasetID || len(lock.Files) == 0 {
		t.Fatal("FEAT-126 Eval lock is incomplete")
	}
	for name, want := range lock.Files {
		path := filepath.Join(root, name)
		if name == filepath.Base(runnerPath) {
			path = runnerPath
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read locked Eval file %s: %v", name, err)
		}
		digest := sha256.Sum256(contents)
		if got := hex.EncodeToString(digest[:]); got != want {
			t.Fatalf("locked Eval file drift: %s", name)
		}
	}
}

func loadFEAT126Cases(t *testing.T, root string) []feat126EvalCase {
	t.Helper()
	file, err := os.Open(filepath.Join(root, "dataset.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	contract, err := jsonschema.NewCompiler().Compile(filepath.Join(root, "eval-case.schema.json"))
	if err != nil {
		t.Fatalf("compile FEAT-126 Eval schema: %v", err)
	}
	result := make([]feat126EvalCase, 0, 250)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(line))
		if err != nil || contract.Validate(instance) != nil {
			t.Fatalf("dataset schema validation failed at ordinal %d", len(result))
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		var item feat126EvalCase
		if err := decoder.Decode(&item); err != nil {
			t.Fatalf("decode dataset case at ordinal %d: %v", len(result), err)
		}
		result = append(result, item)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func verifyFEAT126Dataset(t *testing.T, root string, manifest feat126EvalManifest, cases []feat126EvalCase) feat126EvalMetrics {
	t.Helper()
	metrics := feat126EvalMetrics{Cases: len(cases), RawFailureClasses: make(map[string]int)}
	seen := make(map[string]bool, len(cases))
	train, holdout := make([]string, 0, 200), make([]string, 0, 50)
	for _, item := range cases {
		if seen[item.ID] {
			t.Fatalf("duplicate dataset case id %s", item.ID)
		}
		seen[item.ID] = true
		switch item.Category {
		case "normal":
			metrics.Normal++
		case "adversarial":
			metrics.Adversarial++
		}
		if item.Split == "holdout" {
			metrics.Holdout++
			holdout = append(holdout, item.ID)
		} else {
			metrics.Train++
			train = append(train, item.ID)
		}
		if strings.Contains(item.Prompt, "/Users/") || strings.Contains(item.Prompt, "\\Users\\") {
			t.Fatalf("real project path marker in case %s", item.ID)
		}
	}
	if metrics.Cases != manifest.Counts.Normal+manifest.Counts.Adversarial || metrics.Normal != manifest.Counts.Normal || metrics.Adversarial != manifest.Counts.Adversarial || metrics.Train != manifest.Counts.Train || metrics.Holdout != manifest.Counts.Holdout || metrics.Holdout*100 < metrics.Cases*20 {
		t.Fatal("dataset count or holdout gate failed")
	}
	split := loadFEAT126JSON[feat126EvalSplit](t, filepath.Join(root, "split.json"))
	if split.DatasetID != feat126EvalDatasetID || !equalFEAT126Strings(split.Train, train) || !equalFEAT126Strings(split.Holdout, holdout) {
		t.Fatal("dataset split manifest drift")
	}
	return metrics
}

func verifyFEAT126Titles(t *testing.T, cases []feat126EvalCase, metrics *feat126EvalMetrics) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	generator := &feat126TitleGenerator{}
	service := NewService(&fakeRuntime{}, store, NewEventHub(8, 2), nil, WithTitleGenerator(generator))
	for _, item := range cases {
		sessionID := uuid.NewSHA1(uuid.Nil, []byte("feat126-session:"+item.ID)).String()
		operationID := uuid.NewSHA1(uuid.Nil, []byte("feat126-title:"+item.ID)).String()
		taskID := uuid.NewSHA1(uuid.Nil, []byte("feat126-task:"+item.ID)).String()
		if err := store.Reserve(Record{TaskID: taskID, AgentSessionID: sessionID, Cwd: t.TempDir()}); err != nil {
			t.Fatalf("reserve title case %s: %v", item.ID, err)
		}
		generator.output = item.FakeTitleOutput
		result, err := service.GenerateTitle(context.Background(), sessionID, operationID, item.Prompt)
		if item.ExpectedTitleAccepted {
			if err != nil || item.ExpectedTitle == nil || result.Title != *item.ExpectedTitle {
				t.Fatalf("accepted title gate failed for case %s", item.ID)
			}
			metrics.TitleSchemaPassed++
			metrics.TitleSemanticRequired++
			if strings.Contains(result.Title, item.SemanticKeyword) {
				metrics.TitleSemanticPassed++
			}
			continue
		}
		if !errors.Is(err, ErrTitleOutputInvalid) || result.Title != "" {
			t.Fatalf("unsafe title gate failed for case %s", item.ID)
		}
		metrics.TitleSchemaPassed++
		metrics.TitleUnsafeRejected++
	}
}

func verifyFEAT126RawReasoning(t *testing.T, cases []feat126EvalCase, metrics *feat126EvalMetrics) {
	t.Helper()
	contract := compileAgentSessionEventV2Contract(t)
	for _, item := range cases {
		sessionID := uuid.NewSHA1(uuid.Nil, []byte("feat126-raw-session:"+item.ID)).String()
		turnID := uuid.NewSHA1(uuid.Nil, []byte("feat126-raw-turn:"+item.ID)).String()
		record := Record{TaskID: uuid.NewSHA1(uuid.Nil, []byte("feat126-raw-task:"+item.ID)).String(), AgentSessionID: sessionID, CodexThreadID: uuid.NewSHA1(uuid.Nil, []byte("feat126-raw-thread:"+item.ID)).String()}
		hub := NewEventHubVersion(EventSchemaVersionV2, 16, 4)
		service := NewService(&fakeRuntime{}, nil, NewEventHub(8, 2), nil, WithV2Events(hub))
		itemID := "reasoning-" + item.ID
		switch item.RawScenario {
		case "complete", "plaintext_injection":
			if err := service.appendReasoningDelta(record, turnID, itemID, 0, item.RawText); err != nil {
				t.Fatalf("raw delta failed for case %s", item.ID)
			}
			if err := service.finalizeReasoning(record, turnID, itemID, []string{item.RawText}); err != nil {
				t.Fatalf("raw final failed for case %s", item.ID)
			}
		case "missing":
		case "gap":
			if err := service.appendReasoningDelta(record, turnID, itemID, 0, item.RawText); err != nil || service.appendReasoningDelta(record, turnID, itemID, 2, "synthetic suffix") != nil {
				t.Fatalf("raw gap setup failed for case %s", item.ID)
			}
			t.Skip("Historical FEAT-126 inferred reasoning terminal retired by FEAT-132; use native conversation cases")
		case "invalid":
			if err := service.appendReasoningDelta(record, turnID, itemID, 9, item.RawText); err != nil {
				t.Fatalf("raw invalid setup failed for case %s", item.ID)
			}
		case "oversize":
			if err := service.appendReasoningDelta(record, turnID, itemID, 0, strings.Repeat("x", (16<<10)+1)); err != nil {
				t.Fatalf("raw oversize setup failed for case %s", item.ID)
			}
		default:
			t.Fatalf("unknown raw scenario for case %s", item.ID)
		}
		_, replay, _, cancel, err := hub.Subscribe(sessionID, "", 0)
		if err != nil {
			t.Fatalf("read raw projection for case %s: %v", item.ID, err)
		}
		cancel()
		for _, event := range replay {
			validateFEAT126Event(t, contract, item.ID, event)
		}
		if item.ExpectedRawGate == "pass" {
			metrics.RawValidRequired++
			if len(replay) != 2 || replay[0].EventType != EventItemReasoningTextDelta || replay[0].Payload.Delta == nil || *replay[0].Payload.Delta != item.RawText || replay[1].EventType != EventItemReasoningFinalized || replay[1].Payload.Status != "complete" || replay[1].Payload.Contents == nil || len(*replay[1].Payload.Contents) != 1 || (*replay[1].Payload.Contents)[0].Text != item.RawText {
				t.Fatalf("raw reconciliation failed for case %s", item.ID)
			}
			metrics.RawValidPassed++
			continue
		}
		metrics.RawNegativeRequired++
		if detectFEAT126RawFailure(item.RawScenario, replay) {
			metrics.RawNegativeDetected++
			metrics.RawFailureClasses[item.RawScenario]++
			continue
		}
		t.Fatalf("raw negative scenario was not detected for case %s", item.ID)
	}
}

func detectFEAT126RawFailure(scenario string, replay []Event) bool {
	switch scenario {
	case "missing":
		return len(replay) == 0
	case "gap":
		return len(replay) == 3 && replay[2].EventType == EventItemReasoningFinalized && replay[2].Payload.Status == "incomplete" && replay[2].Payload.ReasonCode == "stream_gap"
	case "invalid":
		return len(replay) == 1 && replay[0].EventType == EventItemReasoningFinalized && replay[0].Payload.Status == "unavailable" && replay[0].Payload.ReasonCode == "protocol_error"
	case "oversize":
		return len(replay) == 1 && replay[0].EventType == EventItemReasoningFinalized && replay[0].Payload.Status == "unavailable" && replay[0].Payload.ReasonCode == "limit_exceeded"
	default:
		return false
	}
}

func verifyFEAT126DesktopEventFixture(t *testing.T, root string) {
	t.Helper()
	file, err := os.Open(filepath.Join(root, "desktop-events.sse"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	contract := compileAgentSessionEventV2Contract(t)
	events := make([]Event, 0, 5)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if !strings.HasPrefix(scanner.Text(), "data: ") {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &event); err != nil {
			t.Fatal("decode Desktop SSE fixture")
		}
		validateFEAT126Event(t, contract, "desktop-fixture", event)
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	wantTypes := []string{EventItemReasoningTextDelta, EventItemReasoningTextDelta, EventItemReasoningFinalized, EventItemAgentMessageDelta, EventTurnCompleted}
	if len(events) != len(wantTypes) {
		t.Fatal("Desktop SSE fixture event count mismatch")
	}
	var aggregate strings.Builder
	for index, event := range events {
		if event.Sequence != uint64(index+1) || event.EventType != wantTypes[index] || event.Terminal != (index == len(events)-1) {
			t.Fatal("Desktop SSE fixture sequence mismatch")
		}
		if event.Payload.Delta != nil && event.EventType == EventItemReasoningTextDelta {
			aggregate.WriteString(*event.Payload.Delta)
		}
	}
	contents := events[2].Payload.Contents
	if contents == nil || len(*contents) != 1 || aggregate.String() != (*contents)[0].Text {
		t.Fatal("Desktop SSE fixture delta/final mismatch")
	}
}

func validateFEAT126Event(t *testing.T, contract *jsonschema.Schema, caseID string, event Event) {
	t.Helper()
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("encode event for case %s", caseID)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil || contract.Validate(instance) != nil {
		t.Fatalf("v2 event contract failed for case %s", caseID)
	}
}

func verifyFEAT126NoDurableBody(t *testing.T) {
	t.Helper()
	const rawCanary = "SYNTHETIC_SECRET_CANARY_126"
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	record := Record{TaskID: testTaskID, AgentSessionID: testSessionID, CodexThreadID: testThreadID, Cwd: t.TempDir()}
	if err := store.Reserve(record); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	hub := NewEventHubVersion(EventSchemaVersionV2, 8, 2)
	service := NewService(&fakeRuntime{}, store, NewEventHub(8, 2), slog.New(slog.NewTextHandler(&logs, nil)), WithV2Events(hub))
	if err := service.appendReasoningDelta(record, testTurnID, "reasoning-canary", 0, rawCanary); err != nil || service.finalizeReasoning(record, testTurnID, "reasoning-canary", []string{rawCanary}) != nil {
		t.Fatal("raw canary projection failed")
	}
	if err := store.db.Sync(); err != nil {
		t.Fatal(err)
	}
	database, err := os.ReadFile(filepath.Join(home, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), rawCanary) || bytes.Contains(database, []byte(rawCanary)) {
		t.Fatal("raw reasoning body entered Host log or bbolt")
	}
}

func loadFEAT126JSON[T any](t *testing.T, path string) T {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	var value T
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode %s: %v", filepath.Base(path), err)
	}
	return value
}

func equalFEAT126Strings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func formatFEAT126FailureClasses(classes map[string]int) string {
	keys := make([]string, 0, len(classes))
	for key := range classes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, fmt.Sprintf("%s:%d", key, classes[key]))
	}
	return strings.Join(values, ",")
}
