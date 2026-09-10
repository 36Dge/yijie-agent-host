package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/getkin/kin-openapi/openapi3"
)

// Local HTTP serialization/authorization test only. No Runtime or tool starts.
type mcpRouteService struct {
	SessionService
	decisions, preparations int
}

func (*mcpRouteService) StartTurnV2(context.Context, session.StartTurnV2Input) (codex.TurnInfo, error) {
	return codex.TurnInfo{}, session.ErrSessionNotUsable
}
func (*mcpRouteService) ListRuntimeApprovals(string) ([]codex.RuntimeApproval, error) {
	return []codex.RuntimeApproval{{ID: "01440000-0000-4000-8000-000000000001", Kind: "mcp", Status: "pending", Summary: "查询商品资料", Scope: "sorftime/product_detail", Reason: "确认实际参数", Mcp: &codex.McpApprovalScope{Server: "sorftime", Tool: "product_detail", ASIN: "B07H9PZDQW", Marketplace: "US"}}}, nil
}
func (s *mcpRouteService) DecideRuntimeApproval(_ context.Context, id, approval, decision string) (codex.RuntimeApproval, error) {
	s.decisions++
	values, _ := s.ListRuntimeApprovals(id)
	value := values[0]
	value.ID = approval
	if decision == "cancel" {
		value.Status = "cancelled"
	}
	return value, nil
}
func (s *mcpRouteService) PrepareMcpPermissionScope(_ context.Context, mode codex.PermissionMode) (bool, bool, error) {
	s.preparations++
	return mode == codex.PermissionAsk, mode != codex.PermissionAsk, nil
}

func TestFEAT144McpRoutesKeepLegacyBoundariesAndOwnerAuthority(t *testing.T) {
	service := &mcpRouteService{}
	mux := http.NewServeMux()
	const token = "public-local-unit-placeholder"
	registerRuntimePermissions(mux, &sessionHandler{service: service, apiToken: token}, true)
	spec, err := openapi3.NewLoader().LoadFromFile("../../api/openapi/runtime-permissions-v2.yaml")
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string, authorized bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if authorized {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := request("POST", "/v2/runtime-mcp/permission-scope", `{"mode":"auto"}`, false); w.Code != 401 || service.preparations != 0 {
		t.Fatal("scope endpoint bypassed owner bearer")
	}
	for _, tc := range []struct{ method, path, body, schema string }{
		{"GET", "/v2/agent-sessions/session/runtime-approvals", "", "RuntimeApprovalSnapshot"},
		{"POST", "/v2/agent-sessions/session/runtime-approvals/01440000-0000-4000-8000-000000000001", `{"decision":"cancel"}`, "RuntimeApproval"},
		{"POST", "/v2/runtime-mcp/permission-scope", `{"mode":"auto"}`, "McpPermissionScopeResult"},
	} {
		w := request(tc.method, tc.path, tc.body, true)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("native route result unavailable")
		}
		var value any
		_ = json.Unmarshal(w.Body.Bytes(), &value)
		if err := spec.Components.Schemas[tc.schema].Value.VisitJSON(value); err != nil {
			t.Fatal(err)
		}
	}
	legacy := request("GET", "/v1/agent-sessions/session/runtime-approvals", "", true)
	if legacy.Code != 200 || strings.Contains(legacy.Body.String(), "B07H9PZDQW") {
		t.Fatal("v1 leaked an unsupported MCP callback")
	}
	prior := service.decisions
	legacy = request("POST", "/v1/agent-sessions/session/runtime-approvals/01440000-0000-4000-8000-000000000001", `{"decision":"approve_once"}`, true)
	if legacy.Code != 400 || service.decisions != prior {
		t.Fatal("v1 authorized the new callback kind")
	}
}
