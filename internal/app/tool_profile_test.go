package app

import (
	"net/http/httptest"
	"strings"
	"testing"

	agenthost "github.com/36Dge/yijie-agent-host/internal/contracts"
)

func TestNewChatToolProfileKeepsOmittedAndExplicitValuesDistinct(t *testing.T) {
	for _, test := range []struct {
		body  string
		valid bool
		value string
	}{
		{`{"cwd":"/ordinary"}`, true, ""},
		{`{"cwd":"/ordinary","tool_profile":"full"}`, true, "full"},
		{`{"cwd":"/ordinary","tool_profile":"light"}`, true, "light"},
		{`{"cwd":"/ordinary","tool_profile":null}`, false, ""},
	} {
		var request agenthost.StartSessionRequest
		err := decodeRequest(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader(test.body)), &request)
		if (err == nil) != test.valid {
			t.Fatalf("normal input validity: %v", err)
		}
		if test.valid {
			var value string
			if request.ToolProfile != nil {
				value = string(*request.ToolProfile)
			}
			if value != test.value {
				t.Fatal("tool profile changed during decoding")
			}
		}
	}
}
