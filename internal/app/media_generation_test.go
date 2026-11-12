package app

import (
	"context"
	"encoding/json"
	media "github.com/36Dge/yijie-agent-host/internal/contracts/mediageneration"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type mediaRouteService struct {
	SessionService
	calls int
}

func (s *mediaRouteService) GenerateImage(_ context.Context, input media.GenerateRequest) media.GenerateResult {
	s.calls++
	return media.GenerateResult{Status: media.GenerateStatusBlocked, Code: media.GenerateCodeExpertRequired, Source: "builtin", NextAction: media.NextActionUseExpert}
}

func TestMediaGenerationBridgeUsesDedicatedAuthorityAndClosedInput(t *testing.T) {
	s := &mediaRouteService{}
	h := &sessionHandler{service: s, apiToken: strings.Repeat("m", 64)}
	handler := h.authorize(http.HandlerFunc(h.generateImage))
	const valid = `{"thread_id":"17200000-0000-4000-8000-000000000001","turn_id":"17200000-0000-4000-8000-000000000002","input":{"prompt":"A blue ceramic cup","use_current_reference":false}}`
	for _, tc := range []struct {
		name, body, token string
		status            int
	}{{"normal", valid, strings.Repeat("m", 64), 200}, {"missing authorization", valid, "", 401}, {"reference choice required", strings.Replace(valid, `,"use_current_reference":false`, "", 1), strings.Repeat("m", 64), 400}} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, media.PrivatePath, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("status=%d", response.Code)
			}
			if tc.status == 200 {
				var result media.GenerateResult
				if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Validate() != nil || result.Code != media.GenerateCodeExpertRequired {
					t.Fatal("selected expert receipt lost")
				}
			}
		})
	}
	if s.calls != 1 {
		t.Fatal("unexpected service calls", s.calls)
	}
}
