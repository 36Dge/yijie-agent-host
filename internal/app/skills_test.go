package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	agenthostcontract "github.com/36Dge/yijie-agent-host/internal/contracts"
	"github.com/36Dge/yijie-agent-host/internal/skills"
)

type skillServiceStub struct {
	snapshot       skills.Snapshot
	state          skills.State
	err            error
	listCalls      int
	scanInput      skills.ScanInput
	installInput   skills.InstallInput
	enabledInput   skills.EnabledInput
	uninstallInput skills.UninstallInput
}

func (s *skillServiceStub) List(context.Context) (skills.Snapshot, error) {
	s.listCalls++
	return s.snapshot, s.err
}

func (s *skillServiceStub) Scan(_ context.Context, input skills.ScanInput) (skills.Snapshot, error) {
	s.scanInput = input
	return s.snapshot, s.err
}

func (s *skillServiceStub) Install(_ context.Context, input skills.InstallInput) (skills.State, error) {
	s.installInput = input
	return s.state, s.err
}

func (s *skillServiceStub) SetEnabled(_ context.Context, input skills.EnabledInput) (skills.State, error) {
	s.enabledInput = input
	state := s.state
	state.Enabled = input.Enabled
	state.RuntimeVisible = input.Enabled
	return state, s.err
}

func (s *skillServiceStub) Uninstall(_ context.Context, input skills.UninstallInput) (skills.State, error) {
	s.uninstallInput = input
	state := s.state
	state.InstallationStatus = "not_installed"
	state.Enabled = false
	state.RuntimeVisible = false
	return state, s.err
}

func TestSkillRoutesAuthorizeBearerBeforeCapabilityAndBody(t *testing.T) {
	service := &skillServiceStub{}
	handler := NewHandler(
		Config{Skills: SkillFeatureConfig{}},
		staticRuntimeStatus{status: codex.Status{}},
		nil,
		"api-token",
		WithSkillService(service),
	)

	for _, route := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/v1/skills"},
		{method: http.MethodPost, path: "/v1/skills/scan-operations"},
		{method: http.MethodPost, path: "/v1/skills/yijie.content-marketing.copywriting/install-operations"},
		{method: http.MethodPut, path: "/v1/skills/yijie.content-marketing.copywriting/enabled"},
		{method: http.MethodPost, path: "/v1/skills/yijie.content-marketing.copywriting/uninstall-operations"},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			request := httptest.NewRequest(route.method, route.path, strings.NewReader("not-json"))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertSkillError(t, response, http.StatusUnauthorized, agenthostcontract.SkillErrorResponseErrorCodeUnauthorized)

			request = httptest.NewRequest(route.method, route.path, strings.NewReader("not-json"))
			request.Header.Set("Authorization", "Bearer api-token")
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertSkillError(t, response, http.StatusForbidden, agenthostcontract.SkillErrorResponseErrorCodeCapabilityDenied)
		})
	}
	if service.listCalls != 0 || service.scanInput.OperationID != "" || service.installInput.OperationID != "" ||
		service.enabledInput.OperationID != "" || service.uninstallInput.OperationID != "" {
		t.Fatal("authorization failure reached the Skill service")
	}

	handler = NewHandler(
		Config{Skills: SkillFeatureConfig{Read: true, Manage: true}},
		staticRuntimeStatus{status: codex.Status{}},
		nil,
		"api-token",
	)
	request := authorizedRequest(http.MethodGet, "/v1/skills", "")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertSkillError(t, response, http.StatusServiceUnavailable, agenthostcontract.SkillErrorResponseErrorCodeRuntimeUnavailable)
}

func TestSkillListMatchesCanonicalContractFixture(t *testing.T) {
	service := &skillServiceStub{snapshot: canonicalSkillSnapshot(t)}
	handler := skillTestHandler(service)
	request := authorizedRequest(http.MethodGet, "/v1/skills", "")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list status %d: %s", response.Code, response.Body.String())
	}
	assertOpenAPIJSON(t, "SkillListResponse", response.Body.Bytes())
	assertJSONFixtureEqual(t, response.Body.Bytes(), "list-response.json")
	if response.Header().Get("Cache-Control") != "no-store" || service.listCalls != 1 {
		t.Fatalf("unexpected list headers or call count: headers=%v calls=%d", response.Header(), service.listCalls)
	}
}

func TestSkillMutationRoutesConsumePinnedRequests(t *testing.T) {
	service := &skillServiceStub{
		snapshot: canonicalSkillSnapshot(t),
		state:    canonicalSkillState(true),
	}
	handler := skillTestHandler(service)

	scan := serveSkillFixture(t, handler, http.MethodPost, "/v1/skills/scan-operations", "scan-request.json")
	if scan.Code != http.StatusOK {
		t.Fatalf("scan status %d: %s", scan.Code, scan.Body.String())
	}
	assertOpenAPIJSON(t, "SkillScanResponse", scan.Body.Bytes())
	if service.scanInput.OperationID != "019fbd88-cbc3-7bf1-934d-7b05cd693f70" || service.scanInput.Reason != "page_open" {
		t.Fatalf("unexpected scan input: %#v", service.scanInput)
	}

	install := serveSkillFixture(t, handler, http.MethodPost, "/v1/skills/yijie.content-marketing.copywriting/install-operations", "install-request.json")
	if install.Code != http.StatusOK {
		t.Fatalf("install status %d: %s", install.Code, install.Body.String())
	}
	assertOpenAPIJSON(t, "SkillMutationResponse", install.Body.Bytes())
	assertJSONFixtureEqual(t, install.Body.Bytes(), "mutation-response.json")
	if service.installInput.SkillID != "yijie.content-marketing.copywriting" ||
		service.installInput.ExpectedVersion != "0.1.0" ||
		service.installInput.ExpectedArchiveSHA256 != "0be8fb3745a32207c6dedb46090044763d47aa64ccf0147653591cb1c9171032" ||
		service.installInput.CatalogRevision != "407d0760e1d06ef3446955fba19b39da26d1f740b2f63b601edae903b1664c18" {
		t.Fatalf("unexpected install input: %#v", service.installInput)
	}

	enabled := serveSkillFixture(t, handler, http.MethodPut, "/v1/skills/yijie.content-marketing.copywriting/enabled", "enabled-request.json")
	if enabled.Code != http.StatusOK || service.enabledInput.Enabled || service.enabledInput.OperationID != "019fbd88-cbc3-7bf1-934d-7b05cd693f72" {
		t.Fatalf("unexpected enabled result: status=%d input=%#v body=%s", enabled.Code, service.enabledInput, enabled.Body.String())
	}
	assertOpenAPIJSON(t, "SkillMutationResponse", enabled.Body.Bytes())

	uninstall := serveSkillFixture(t, handler, http.MethodPost, "/v1/skills/yijie.content-marketing.copywriting/uninstall-operations", "uninstall-request.json")
	if uninstall.Code != http.StatusOK || service.uninstallInput.OperationID != "019fbd88-cbc3-7bf1-934d-7b05cd693f73" {
		t.Fatalf("unexpected uninstall result: status=%d input=%#v body=%s", uninstall.Code, service.uninstallInput, uninstall.Body.String())
	}
	assertOpenAPIJSON(t, "SkillMutationResponse", uninstall.Body.Bytes())
}

func TestSkillRouteRejectsInvalidIDAndClosedJSON(t *testing.T) {
	service := &skillServiceStub{state: canonicalSkillState(true)}
	handler := skillTestHandler(service)
	for _, test := range []struct {
		name string
		path string
		body string
	}{
		{name: "invalid id", path: "/v1/skills/INVALID/install-operations", body: fixtureText(t, "install-request.json")},
		{name: "unknown field", path: "/v1/skills/yijie.content-marketing.copywriting/install-operations", body: `{"operation_id":"019fbd88-cbc3-7bf1-934d-7b05cd693f71","expected_version":"0.1.0","expected_archive_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","catalog_revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","path":"/tmp/escape"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := authorizedRequest(http.MethodPost, test.path, test.body)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertSkillError(t, response, http.StatusBadRequest, agenthostcontract.SkillErrorResponseErrorCodeInvalidRequest)
		})
	}
}

func TestSkillArchiveUnsafeErrorMatchesPinnedFixture(t *testing.T) {
	service := &skillServiceStub{err: &skills.ServiceError{Code: skills.CodeArchiveUnsafe}}
	handler := skillTestHandler(service)
	response := serveSkillFixture(t, handler, http.MethodPost, "/v1/skills/yijie.content-marketing.copywriting/install-operations", "install-request.json")
	assertSkillError(t, response, http.StatusUnprocessableEntity, agenthostcontract.SkillErrorResponseErrorCodeArchiveUnsafe)
	assertJSONFixtureEqual(t, response.Body.Bytes(), "error-archive-unsafe.json")
}

func TestSkillNotInstallableErrorMatchesPinnedFixture(t *testing.T) {
	service := &skillServiceStub{err: &skills.ServiceError{Code: skills.CodeNotInstallable}}
	handler := skillTestHandler(service)
	response := serveSkillFixture(t, handler, http.MethodPost, "/v1/skills/yijie.content-marketing.copywriting/install-operations", "install-request.json")
	assertSkillError(t, response, http.StatusUnprocessableEntity, agenthostcontract.SkillErrorResponseErrorCodeSkillNotInstallable)
	assertJSONFixtureEqual(t, response.Body.Bytes(), "error-skill-not-installable.json")
}

func TestSkillFirstUseStalePreconditionMapsToBadRequest(t *testing.T) {
	service := &skillServiceStub{err: &skills.ServiceError{Code: skills.CodeInvalidRequest}}
	handler := skillTestHandler(service)
	response := serveSkillFixture(t, handler, http.MethodPost, "/v1/skills/yijie.content-marketing.copywriting/install-operations", "install-request.json")
	assertSkillError(t, response, http.StatusBadRequest, agenthostcontract.SkillErrorResponseErrorCodeInvalidRequest)
}

func TestSkillServiceErrorsStayWithinEachRouteContract(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		fixture    string
		serviceErr skills.ErrorCode
		status     int
		code       agenthostcontract.SkillErrorResponseErrorCode
	}{
		{
			name: "list catalog failure is internal", method: http.MethodGet, path: "/v1/skills",
			serviceErr: skills.CodeBundleMissing, status: http.StatusInternalServerError,
			code: agenthostcontract.SkillErrorResponseErrorCodeInternalError,
		},
		{
			name: "list Runtime failure is unavailable", method: http.MethodGet, path: "/v1/skills",
			serviceErr: skills.CodeRuntimeSyncFailed, status: http.StatusServiceUnavailable,
			code: agenthostcontract.SkillErrorResponseErrorCodeRuntimeSyncFailed,
		},
		{
			name: "scan manifest failure is internal", method: http.MethodPost, path: "/v1/skills/scan-operations", fixture: "scan-request.json",
			serviceErr: skills.CodeManifestInvalid, status: http.StatusInternalServerError,
			code: agenthostcontract.SkillErrorResponseErrorCodeInternalError,
		},
		{
			name: "scan contention is conflict", method: http.MethodPost, path: "/v1/skills/scan-operations", fixture: "scan-request.json",
			serviceErr: skills.CodeBusy, status: http.StatusConflict,
			code: agenthostcontract.SkillErrorResponseErrorCodeSkillBusy,
		},
		{
			name: "install archive failure is unprocessable", method: http.MethodPost,
			path: "/v1/skills/yijie.content-marketing.copywriting/install-operations", fixture: "install-request.json",
			serviceErr: skills.CodeArchiveUnsafe, status: http.StatusUnprocessableEntity,
			code: agenthostcontract.SkillErrorResponseErrorCodeArchiveUnsafe,
		},
		{
			name: "enable blocked is bad request", method: http.MethodPut,
			path: "/v1/skills/yijie.content-marketing.copywriting/enabled", fixture: "enabled-request.json",
			serviceErr: skills.CodeNotInstallable, status: http.StatusBadRequest,
			code: agenthostcontract.SkillErrorResponseErrorCodeInvalidRequest,
		},
		{
			name: "enable Runtime failure is unavailable", method: http.MethodPut,
			path: "/v1/skills/yijie.content-marketing.copywriting/enabled", fixture: "enabled-request.json",
			serviceErr: skills.CodeRuntimeUnavailable, status: http.StatusServiceUnavailable,
			code: agenthostcontract.SkillErrorResponseErrorCodeRuntimeUnavailable,
		},
		{
			name: "uninstall catalog failure is internal", method: http.MethodPost,
			path: "/v1/skills/yijie.content-marketing.copywriting/uninstall-operations", fixture: "uninstall-request.json",
			serviceErr: skills.CodeBundleMissing, status: http.StatusInternalServerError,
			code: agenthostcontract.SkillErrorResponseErrorCodeInternalError,
		},
		{
			name: "uninstall unknown Skill is not found", method: http.MethodPost,
			path: "/v1/skills/yijie.content-marketing.copywriting/uninstall-operations", fixture: "uninstall-request.json",
			serviceErr: skills.CodeNotFound, status: http.StatusNotFound,
			code: agenthostcontract.SkillErrorResponseErrorCodeSkillNotFound,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &skillServiceStub{err: &skills.ServiceError{Code: test.serviceErr}}
			handler := skillTestHandler(service)
			body := ""
			if test.fixture != "" {
				body = fixtureText(t, test.fixture)
			}
			request := authorizedRequest(test.method, test.path, body)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertSkillError(t, response, test.status, test.code)
		})
	}
}

func skillTestHandler(service SkillService) http.Handler {
	return NewHandler(
		Config{Skills: SkillFeatureConfig{Read: true, Manage: true}},
		staticRuntimeStatus{status: codex.Status{}},
		nil,
		"api-token",
		WithSkillService(service),
	)
}

func canonicalSkillSnapshot(t *testing.T) skills.Snapshot {
	t.Helper()
	var fixture agenthostcontract.SkillListResponse
	if err := json.Unmarshal([]byte(fixtureText(t, "list-response.json")), &fixture); err != nil {
		t.Fatalf("decode canonical Skill list fixture: %v", err)
	}
	snapshot := skills.Snapshot{
		CatalogRevision: string(fixture.CatalogRevision),
		ScannedAt:       fixture.ScannedAt,
		Skills:          make([]skills.State, 0, len(fixture.Skills)),
	}
	for _, skill := range fixture.Skills {
		blockedReason := ""
		if skill.CatalogBlockedReason != nil {
			blockedReason = string(*skill.CatalogBlockedReason)
		}
		snapshot.Skills = append(snapshot.Skills, skills.State{
			ID:                   string(skill.Id),
			RuntimeName:          skill.RuntimeName,
			Version:              string(skill.Version),
			CatalogStatus:        string(skill.CatalogStatus),
			CatalogBlockedReason: blockedReason,
			MaintenanceStatus:    string(skill.MaintenanceStatus),
			CapabilityReadiness:  string(skill.CapabilityReadiness),
			InstallationStatus:   string(skill.InstallationStatus),
			Enabled:              skill.Enabled,
			RuntimeVisible:       skill.RuntimeVisible,
			FailureCode:          string(skill.FailureCode),
		})
	}
	return snapshot
}

func canonicalSkillState(installed bool) skills.State {
	state := skills.State{
		ID:                  "yijie.content-marketing.copywriting",
		RuntimeName:         "copywriting",
		Version:             "0.1.0",
		CatalogStatus:       "installable",
		MaintenanceStatus:   "maintained",
		CapabilityReadiness: "ready",
		InstallationStatus:  "not_installed",
	}
	if installed {
		state.InstallationStatus = "installed"
		state.Enabled = true
		state.RuntimeVisible = true
	}
	return state
}

func serveSkillFixture(t *testing.T, handler http.Handler, method, path, fixture string) *httptest.ResponseRecorder {
	t.Helper()
	request := authorizedRequest(method, path, fixtureText(t, fixture))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func fixtureText(t *testing.T, name string) string {
	t.Helper()
	encoded, err := os.ReadFile(contractFilePath(t, "fixtures", "agent", "host-skills-v1", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(encoded)
}

func assertSkillError(t *testing.T, response *httptest.ResponseRecorder, status int, code agenthostcontract.SkillErrorResponseErrorCode) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("error status %d, want %d: %s", response.Code, status, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Skill error omitted no-store: %v", response.Header())
	}
	var payload agenthostcontract.SkillErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode Skill error: %v", err)
	}
	if payload.Error.Code != code {
		t.Fatalf("error code %q, want %q", payload.Error.Code, code)
	}
	assertOpenAPIJSON(t, "SkillErrorResponse", response.Body.Bytes())
}

func assertJSONFixtureEqual(t *testing.T, actual []byte, fixture string) {
	t.Helper()
	var actualValue any
	if err := json.Unmarshal(actual, &actualValue); err != nil {
		t.Fatalf("decode actual JSON: %v", err)
	}
	var expectedValue any
	if err := json.Unmarshal([]byte(fixtureText(t, fixture)), &expectedValue); err != nil {
		t.Fatalf("decode fixture JSON: %v", err)
	}
	actualJSON, _ := json.Marshal(actualValue)
	expectedJSON, _ := json.Marshal(expectedValue)
	if string(actualJSON) != string(expectedJSON) {
		t.Fatalf("response differs from %s\nactual: %s\nexpected: %s", fixture, actualJSON, expectedJSON)
	}
}
