package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

type feat164DraftRouteService struct {
	modelV2RouteService
	enabled bool
}

func (s *feat164DraftRouteService) ChatModelsV2Enabled() bool { return s.enabled }

// Exercise the actual v2 HTTP router, not a direct Runtime/Store call.
func TestFEAT164DraftV2RoutesFollowContract(t *testing.T) {
	const id = "00000000-0000-4000-8000-000000000164"
	const token = "feat164-local-draft-route-token"
	for _, tc := range []struct{ name, suffix, body string }{
		{"create", "", `{"schema_version":1,"policy_version":1,"task_id":"` + id + `","workspace_id":"` + id + `"}`},
		{"resume", "/" + id + "/resume", `{"schema_version":1,"policy_version":1}`},
		{"turn", "/" + id + "/turns", `{"schema_version":1,"policy_version":1,"operation_id":"` + id + `","text":"ordinary offline draft"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			service := &feat164DraftRouteService{enabled: true}
			calls := 0
			expectedProfile := ""
			legacy := "/v1/scheduled-plan-draft-sessions" + tc.suffix
			mux.HandleFunc("POST "+legacy, func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != tc.body || r.URL.Path != legacy || codex.ModelProfileID(r.Context()) != expectedProfile {
					t.Error("original draft payload, path or admitted model changed")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"schema_version":1,"policy_version":1}`))
			})
			registerChatModelsV2(mux, &sessionHandler{service: service, apiToken: token})
			send := func(profile string, auth bool) int {
				r := httptest.NewRequest("POST", "/v2/model-chat/scheduled-plan-draft-sessions"+tc.suffix, strings.NewReader(tc.body))
				if auth {
					r.Header.Set("Authorization", "Bearer "+token)
				}
				if profile != "" {
					r.Header.Set("X-Yijie-Model-Profile", profile)
				}
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				return w.Code
			}
			for _, profile := range []string{codex.KimiK31ProfileID, "minimax-m3-high-v1"} {
				expectedProfile = profile
				if code := send(profile, true); code != 200 {
					t.Fatalf("contracted route unavailable: status=%d", code)
				}
			}
			if calls != 2 {
				t.Fatalf("delegated calls=%d", calls)
			}
			for _, v := range []struct {
				profile string
				auth    bool
				status  int
			}{
				{codex.KimiK31ProfileID, false, 401}, {"", true, 400}, {"kimi-k3-max-v1", true, 409},
			} {
				if code := send(v.profile, v.auth); code != v.status {
					t.Fatalf("admission status=%d want=%d", code, v.status)
				}
			}
			service.enabled = false
			if code := send(codex.KimiK31ProfileID, true); code != 503 {
				t.Fatalf("disabled status=%d", code)
			}
			if calls != 2 {
				t.Fatal("unavailable or unauthorized request reached draft handler")
			}
		})
	}
}
