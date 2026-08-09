package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

type evidenceRuntime struct {
	evidence codex.RuntimeEvidence
}

func (runtime evidenceRuntime) Snapshot() codex.Status {
	return codex.Status{State: codex.StateReady, Ready: true}
}

func (runtime evidenceRuntime) RuntimeEvidence(string, string, string) (codex.RuntimeEvidence, error) {
	return runtime.evidence, nil
}

func TestPrivateRuntimeEvidenceIsSameRunAndNonceBound(t *testing.T) {
	const runID = "019fbd88-cbc3-4bf1-934d-7b05cd693f80"
	const nonce = "019fbd88-cbc3-4bf1-934d-7b05cd693f81"
	runtime := evidenceRuntime{evidence: codex.RuntimeEvidence{
		SchemaVersion: 1, RunID: runID, Role: "runtime", PID: 11, PPID: 10,
		BinarySHA256: strings.Repeat("a", 64), ManifestSHA256: strings.Repeat("b", 64),
		Nonce: nonce, Profile: "feat-126-s10-local-lab", State: codex.StateReady, Ready: true,
	}}
	config := Config{Environment: "local", FEAT126TestRunID: runID, FEAT126TestProfile: "feat-126-s10-local-lab", InstanceNonce: nonce}
	request := httptest.NewRequest(http.MethodGet, "/v1/feat126/runtime-evidence", nil)
	request.Header.Set("X-Yijie-Feat126-Run-Id", runID)
	request.Header.Set("X-Yijie-Feat126-Nonce", nonce)
	response := httptest.NewRecorder()
	NewHandler(config, runtime, nil, "").ServeHTTP(response, request)
	var projection codex.RuntimeEvidence
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &projection) != nil {
		t.Fatalf("unexpected runtime evidence: %d %s", response.Code, response.Body.String())
	}
	if projection.RunID != runID || projection.Nonce != nonce || projection.PID != 11 || projection.PPID != 10 {
		t.Fatalf("runtime evidence authority mismatch: %#v", projection)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/feat126/runtime-evidence", nil)
	request.Header.Set("X-Yijie-Feat126-Run-Id", runID)
	response = httptest.NewRecorder()
	NewHandler(config, runtime, nil, "").ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing nonce must fail closed: %d", response.Code)
	}
}
