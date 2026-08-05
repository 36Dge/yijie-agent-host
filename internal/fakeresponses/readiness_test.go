package fakeresponses

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestFrozenReadinessAuthoritySeparatesDatasetAndFixtureCase(t *testing.T) {
	authority, err := FrozenReadinessAuthority()
	if err != nil {
		t.Fatal(err)
	}
	if authority.SchemaVersion != 1 || authority.DatasetID != session.FEAT126FixtureDatasetID ||
		authority.FixtureCaseID != codex.FEAT126FakeFixtureID || authority.DatasetID == authority.FixtureCaseID ||
		authority.DatasetSHA256 != "523609b44fd244fff18b930c992375999276c2e0d5786efadfd8858ec623b308" {
		t.Fatalf("unexpected readiness authority: %#v", authority)
	}
}

func TestProbeReadinessOwnsHeadersAndReturnsClosedIdentity(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != fakeReadinessURL || request.Header.Get(RunIDHeader) != testRunID ||
			request.Header.Get(FixtureIDHeader) != codex.FEAT126FakeFixtureID ||
			request.Header.Get(FixtureIDHeader) == session.FEAT126FixtureDatasetID {
			t.Fatalf("probe did not use the Host-owned authority: %s %#v", request.URL, request.Header)
		}
		return readinessResponse(http.StatusOK,
			`{"status":"ready","dataset_id":"feat126-title-raw-v1","fixture_case_id":"normal-000","dataset_sha256":"523609b44fd244fff18b930c992375999276c2e0d5786efadfd8858ec623b308"}`), nil
	})}
	result, err := probeReadiness(context.Background(), client, fakeReadinessURL, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ready" || result.RunID != testRunID ||
		result.DatasetID != session.FEAT126FixtureDatasetID || result.FixtureCaseID != codex.FEAT126FakeFixtureID {
		t.Fatalf("unexpected readiness result: %#v", result)
	}
}

func TestProbeReadinessFailsClosedOnIdentityAndPayloadDrift(t *testing.T) {
	tests := []struct {
		name string
		code int
		body string
		want string
	}{
		{name: "forbidden", code: http.StatusForbidden, body: `{}`, want: "fake_provider_not_ready"},
		{name: "dataset used as case", code: http.StatusOK, body: `{"status":"ready","dataset_id":"feat126-title-raw-v1","fixture_case_id":"feat126-title-raw-v1","dataset_sha256":"523609b44fd244fff18b930c992375999276c2e0d5786efadfd8858ec623b308"}`, want: "fake_identity_mismatch"},
		{name: "dataset drift", code: http.StatusOK, body: `{"status":"ready","dataset_id":"other","fixture_case_id":"normal-000","dataset_sha256":"523609b44fd244fff18b930c992375999276c2e0d5786efadfd8858ec623b308"}`, want: "fake_identity_mismatch"},
		{name: "digest drift", code: http.StatusOK, body: `{"status":"ready","dataset_id":"feat126-title-raw-v1","fixture_case_id":"normal-000","dataset_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, want: "fake_identity_mismatch"},
		{name: "unknown field", code: http.StatusOK, body: `{"status":"ready","dataset_id":"feat126-title-raw-v1","fixture_case_id":"normal-000","dataset_sha256":"523609b44fd244fff18b930c992375999276c2e0d5786efadfd8858ec623b308","fixture_id":"normal-000"}`, want: "fake_readiness_invalid"},
		{name: "oversize", code: http.StatusOK, body: `{"status":"ready","padding":"` + strings.Repeat("x", 4096) + `"}`, want: "fake_readiness_invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return readinessResponse(test.code, test.body), nil
			})}
			_, err := probeReadiness(context.Background(), client, fakeReadinessURL, testRunID)
			var probeError *ReadinessProbeError
			if !errorsAs(err, &probeError) || probeError.Code != test.want {
				t.Fatalf("expected %s, got %v", test.want, err)
			}
		})
	}
}

func TestProbeReadinessRejectsOperatorControlledEndpointAndRun(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid authority must fail before network access")
		return nil, nil
	})}
	for _, test := range []struct {
		endpoint string
		runID    string
		want     string
	}{
		{endpoint: "http://localhost:18082/healthz", runID: testRunID, want: "fake_endpoint_invalid"},
		{endpoint: "http://127.0.0.1:18082/healthz?fixture=normal-000", runID: testRunID, want: "fake_endpoint_invalid"},
		{endpoint: fakeReadinessURL, runID: "not-a-run", want: "fake_run_id_invalid"},
	} {
		_, err := probeReadiness(context.Background(), client, test.endpoint, test.runID)
		var probeError *ReadinessProbeError
		if !errorsAs(err, &probeError) || probeError.Code != test.want {
			t.Fatalf("expected %s, got %v", test.want, err)
		}
	}
}

func readinessResponse(code int, body string) *http.Response {
	return &http.Response{
		StatusCode: code,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func errorsAs(err error, target any) bool {
	return err != nil && errors.As(err, target)
}
