package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	timing "github.com/36Dge/yijie-agent-host/internal/contracts/nativetiming"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/google/uuid"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type timingHTTPService struct {
	SessionService
	value timing.NativeTurnTiming
	calls int
}

func (s *timingHTTPService) ReadNativeTurnTiming(context.Context, string, string) (timing.NativeTurnTiming, error) {
	s.calls++
	return s.value, nil
}

func TestFEAT155TimingHTTPContractAndLocalCandidate(t *testing.T) {
	store, err := session.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id := uuid.New()
	zero := int64(0)
	s := &timingHTTPService{SessionService: session.NewService(nil, store, session.NewEventHub(8, 8), nil), value: timing.NativeTurnTiming{SchemaVersion: timing.N1, Source: timing.RuntimeRead, AgentSessionId: id, ThreadId: id, TurnId: id, StartedAt: timing.UnixSecondsFact{State: timing.Known, Value: &zero}, CompletedAt: timing.UnixSecondsFact{State: timing.Unknown}, DurationMs: timing.DurationMsFact{State: timing.Unknown}}}
	file, _ := filepath.Abs("../../api/schemas/native-turn-timing.schema.json")
	compile := func(name string) *jsonschema.Schema {
		v, e := jsonschema.NewCompiler().Compile(file + "#/$defs/" + name)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	success, failure := compile("NativeTurnTiming"), compile("TimingError")
	config := Config{Environment: "local", ScheduledRecoveryEnabled: true, ScheduledCandidate: &ScheduledCandidateConfig{SchemaVersion: 1}}
	h := NewHandler(config, staticRuntimeStatus{}, s, recoveryTestToken)
	url := "/v1/agent-sessions/" + id.String() + "/turns/" + id.String() + "/timing"
	recoveryRequest(t, h, url, recoveryTestToken, "", 200, success)
	recoveryRequest(t, h, url, "", "", 401, failure)
	recoveryRequest(t, h, url+"?unexpected=1", recoveryTestToken, "", 400, failure)
	if s.calls != 1 {
		t.Fatal("unauthorized/invalid read invoked service")
	}
	for _, c := range []Config{{Environment: "local", ScheduledRecoveryEnabled: true}, {Environment: "production", ScheduledRecoveryEnabled: true, ScheduledCandidate: config.ScheduledCandidate}} {
		r := httptest.NewRequest(http.MethodGet, url, nil)
		r.Header.Set("Authorization", "Bearer "+recoveryTestToken)
		w := httptest.NewRecorder()
		NewHandler(c, staticRuntimeStatus{}, s, recoveryTestToken).ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatal("timing active outside candidate", w.Code)
		}
	}
}
