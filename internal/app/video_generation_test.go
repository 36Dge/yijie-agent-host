package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	video "github.com/36Dge/yijie-agent-host/internal/contracts/videogeneration"
)

type videoRouteService struct {
	SessionService
	calls int
}

func (s *videoRouteService) GenerateVideo(_ context.Context, _ video.GenerateRequest) video.VideoResult {
	s.calls++
	return video.VideoResult{Status: video.VideoStatusBlocked, Code: video.VideoCodeExpertRequired, Source: "builtin", NextAction: video.NextActionUseExpert}
}
func (s *videoRouteService) GetVideoResult(_ context.Context, _ video.GetRequest) video.VideoResult {
	s.calls++
	return video.VideoResult{Status: video.VideoStatusBlocked, Code: video.VideoCodeOperationNotFound, Source: "builtin", NextAction: video.NextActionReportStatus}
}
func TestVideoGenerationBridgeUsesDedicatedAuthorityAndClosedInput(t *testing.T) {
	s := &videoRouteService{}
	mux := http.NewServeMux()
	registerVideoGeneration(mux, &sessionHandler{service: s, apiToken: strings.Repeat("v", 64)})
	const prefix = `{"thread_id":"17300000-0000-4000-8000-000000000001","turn_id":"17300000-0000-4000-8000-000000000002","input":`
	generate := prefix + `{"prompt":"A blue cup","image_mode":"none","image_indices":[]}}`
	get := prefix + `{}}`
	for _, tc := range []struct {
		name, path, body, token string
		status                  int
	}{
		{"create", video.GeneratePath, generate, strings.Repeat("v", 64), 200},
		{"find without handle", video.GetPath, get, strings.Repeat("v", 64), 200},
		{"missing authority", video.GetPath, get, "", 401},
		{"other authority", video.GeneratePath, generate, strings.Repeat("a", 64), 401},
		{"missing image choice", video.GeneratePath, prefix + `{"prompt":"A blue cup"}}`, strings.Repeat("v", 64), 400},
		{"optional provider fields forbidden", video.GeneratePath, prefix + `{"prompt":"A blue cup","image_mode":"none","image_indices":[],"duration":5}}`, strings.Repeat("v", 64), 400},
		{"missing current turn", video.GetPath, `{"thread_id":"17300000-0000-4000-8000-000000000001","input":{}}`, strings.Repeat("v", 64), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d", w.Code)
			}
			if tc.status == 200 {
				var result video.VideoResult
				if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Validate() != nil || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("invalid private receipt")
				}
			}
		})
	}
	if s.calls != 2 {
		t.Fatal("unexpected dispatch", s.calls)
	}
}

func TestVideoGenerationConfigDefaultsAndRollback(t *testing.T) {
	for _, key := range []string{"YIJIE_NATIVE_REASONING_ENABLED", "YIJIE_FEAT134_STREAMING_ENABLED", "YIJIE_FEAT136_COMMAND_TOOL_ITEMS_ENABLED", "YIJIE_FEAT137_COMMAND_APPROVAL_ENABLED", "YIJIE_FEAT126_S10_TEST_PROFILE_ENABLED", "YIJIE_MINIMAX_API_KEY_FILE", "YIJIE_FEAT128_SYNTHETIC_ENABLED", "YIJIE_FEAT128_SYNTHETIC_MANIFEST", "YIJIE_AGENT_HOST_V2_RAW_REASONING_ENABLED", skillBundleRootEnv, skillInstallRootEnv} {
		t.Setenv(key, "")
	}
	root := t.TempDir()
	for k, v := range map[string]string{
		"YIJIE_ENV": "local", "YIJIE_LOCAL_PROFILE": "demo_fast", "YIJIE_RUNTIME_PERMISSIONS_ENABLED": "true",
		"YIJIE_MODEL_PROVIDER": "minimax", "YIJIE_MINIMAX_API_KEY": "ordinary-local-only",
		"YIJIE_AGENT_HOST_HOME": root + "/host", "YIJIE_CODEX_HOME": root + "/runtime",
		"YIJIE_FEAT128_IMAGE_GENERATION_ENABLED": "false", "YIJIE_AGENT_HOST_V3_ARTIFACTS_ENABLED": "true", "YIJIE_AGENT_HOST_V2_MULTIMODAL_TURNS_ENABLED": "true",
		"YIJIE_FEAT169_NATIVE_TOOLS": "true", "YIJIE_ARTIFACT_MEDIA_ENABLED": "true", "YIJIE_FEAT173_VIDEO_MCP": "",
	} {
		t.Setenv(k, v)
	}
	for _, tc := range []struct {
		selector string
		enabled  bool
		invalid  bool
	}{{"", true, false}, {"true", true, false}, {"false", false, false}, {"TRUE", false, true}} {
		t.Run("selector="+tc.selector, func(t *testing.T) {
			t.Setenv("YIJIE_FEAT173_VIDEO_MCP", tc.selector)
			c, err := LoadConfig()
			if (err != nil) != tc.invalid {
				t.Fatal("selector validation", err)
			}
			if err == nil && (c.Runtime.BuiltinVideoEnabled != tc.enabled || !c.Runtime.BuiltinMediaEnabled || !c.ImageGenerationEnabled || c.Runtime.HostImagesEnabled) {
				t.Fatal("video altered existing image profile")
			}
		})
	}
	t.Setenv("YIJIE_ARTIFACT_MEDIA_ENABLED", "false")
	c, err := LoadConfig()
	if err != nil || c.Runtime.BuiltinVideoEnabled {
		t.Fatal("video escaped complete media profile", err)
	}
}
