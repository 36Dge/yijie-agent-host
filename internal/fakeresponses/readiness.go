package fakeresponses

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

const (
	fakeReadinessURL      = "http://127.0.0.1:18082/healthz"
	maxReadinessBodyBytes = int64(4096)
)

// ReadinessProbeError exposes only a stable, content-free failure class.
type ReadinessProbeError struct {
	Code string
}

func (e *ReadinessProbeError) Error() string { return e.Code }

type ReadinessAuthority struct {
	SchemaVersion int    `json:"schema_version"`
	DatasetID     string `json:"dataset_id"`
	FixtureCaseID string `json:"fixture_case_id"`
	DatasetSHA256 string `json:"dataset_sha256"`
}

type ReadinessResult struct {
	SchemaVersion int    `json:"schema_version"`
	Status        string `json:"status"`
	RunID         string `json:"run_id"`
	DatasetID     string `json:"dataset_id"`
	FixtureCaseID string `json:"fixture_case_id"`
	DatasetSHA256 string `json:"dataset_sha256"`
}

type readinessPayload struct {
	Status        string `json:"status"`
	DatasetID     string `json:"dataset_id"`
	FixtureCaseID string `json:"fixture_case_id"`
	DatasetSHA256 string `json:"dataset_sha256"`
}

func FrozenReadinessAuthority() (ReadinessAuthority, error) {
	fixture, err := session.LoadFEAT126FakeFixture(codex.FEAT126FakeFixtureID)
	if err != nil {
		return ReadinessAuthority{}, &ReadinessProbeError{Code: "fake_authority_invalid"}
	}
	return ReadinessAuthority{
		SchemaVersion: 1,
		DatasetID:     session.FEAT126FixtureDatasetID,
		FixtureCaseID: fixture.ID,
		DatasetSHA256: fixture.DatasetSHA256,
	}, nil
}

func ProbeReadiness(ctx context.Context, runID string) (ReadinessResult, error) {
	transport := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   time.Second,
			KeepAlive: -1,
		}).DialContext,
		DisableKeepAlives: true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirects are forbidden")
		},
	}
	return probeReadiness(ctx, client, fakeReadinessURL, runID)
}

func probeReadiness(ctx context.Context, client *http.Client, endpoint, runID string) (ReadinessResult, error) {
	if !isCanonicalRFC4122UUIDv4(runID) {
		return ReadinessResult{}, &ReadinessProbeError{Code: "fake_run_id_invalid"}
	}
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil || parsedEndpoint.Scheme != "http" || parsedEndpoint.Host != "127.0.0.1:18082" ||
		parsedEndpoint.Path != "/healthz" || parsedEndpoint.RawQuery != "" || parsedEndpoint.Fragment != "" || parsedEndpoint.User != nil {
		return ReadinessResult{}, &ReadinessProbeError{Code: "fake_endpoint_invalid"}
	}
	authority, err := FrozenReadinessAuthority()
	if err != nil {
		return ReadinessResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ReadinessResult{}, &ReadinessProbeError{Code: "fake_probe_invalid"}
	}
	request.Header.Set(RunIDHeader, runID)
	request.Header.Set(FixtureIDHeader, authority.FixtureCaseID)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return ReadinessResult{}, &ReadinessProbeError{Code: "fake_provider_unavailable"}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ReadinessResult{}, &ReadinessProbeError{Code: "fake_provider_not_ready"}
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return ReadinessResult{}, &ReadinessProbeError{Code: "fake_readiness_invalid"}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxReadinessBodyBytes+1))
	if err != nil || len(body) == 0 || int64(len(body)) > maxReadinessBodyBytes {
		return ReadinessResult{}, &ReadinessProbeError{Code: "fake_readiness_invalid"}
	}
	var payload readinessPayload
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return ReadinessResult{}, &ReadinessProbeError{Code: "fake_readiness_invalid"}
	}
	if payload.Status != "ready" || payload.DatasetID != authority.DatasetID ||
		payload.FixtureCaseID != authority.FixtureCaseID || payload.DatasetSHA256 != authority.DatasetSHA256 {
		return ReadinessResult{}, &ReadinessProbeError{Code: "fake_identity_mismatch"}
	}
	return ReadinessResult{
		SchemaVersion: 1,
		Status:        "ready",
		RunID:         runID,
		DatasetID:     authority.DatasetID,
		FixtureCaseID: authority.FixtureCaseID,
		DatasetSHA256: authority.DatasetSHA256,
	}, nil
}
