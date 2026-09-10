package session

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv2"
	"github.com/getkin/kin-openapi/openapi3"
)

type nativeStatusRuntime struct {
	fakeRuntime
	called string
	value  string
	err    error
}

func (r *nativeStatusRuntime) ReadThreadStatus(_ context.Context, id string) (string, error) {
	r.called = id
	return r.value, r.err
}

func TestFEAT144NativeThreadStatusUsesExactBindingWithoutPersisting(t *testing.T) {
	spec, err := openapi3.NewLoader().LoadFromFile("../../api/openapi/native-conversation-v2.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"notLoaded", "idle", "systemError", "active"} {
		t.Run(value, func(t *testing.T) {
			store := openBoundFEAT134Store(t)
			// A prior known Turn remains untouched even when the current native
			// loaded-thread observation differs from the persisted session state.
			if _, err := store.BindTurn(testSessionID, testTurnID); err != nil {
				t.Fatal(err)
			}
			before, _ := store.Get(testSessionID)
			r := &nativeStatusRuntime{value: value}
			s := NewService(r, store, NewEventHub(8, 2), nil)
			view, err := s.ReadNativeThreadStatusV2(context.Background(), testSessionID)
			if err != nil {
				t.Fatal(err)
			}
			if r.called != testThreadID || view.ThreadId != testThreadID || string(view.Status) != value || view.Source != native.NativeThreadStatusSnapshotSourceRuntimeRead {
				t.Fatal("native status identity or source changed")
			}
			encoded, _ := json.Marshal(view)
			var decoded any
			_ = json.Unmarshal(encoded, &decoded)
			if err := spec.Components.Schemas["NativeThreadStatusSnapshot"].Value.VisitJSON(decoded); err != nil {
				t.Fatal(err)
			}
			after, _ := store.Get(testSessionID)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("native status read rewrote persisted identity or lifecycle")
			}
		})
	}
}

func TestFEAT144NativeThreadStatusDoesNotGuessMissingOrFailedObservations(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		runtimeErr  error
	}{
		{"missing", "", nil},
		{"unknown", "futureStatus", nil},
		{"read failed", "", errors.New("ordinary native error detail")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &nativeStatusRuntime{value: tc.value, err: tc.runtimeErr}
			s := NewService(r, openBoundFEAT134Store(t), NewEventHub(8, 2), nil)
			view, err := s.ReadNativeThreadStatusV2(context.Background(), testSessionID)
			if !errors.Is(err, ErrRuntimeRequest) || view != (native.NativeThreadStatusSnapshot{}) || strings.Contains(err.Error(), "detail") {
				t.Fatal("missing native observation was accepted or exposed")
			}
		})
	}
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	r := &nativeStatusRuntime{value: "idle"}
	s := NewService(r, store, NewEventHub(8, 2), nil)
	if _, err := s.ReadNativeThreadStatusV2(context.Background(), testSessionID); !errors.Is(err, ErrSessionNotUsable) || r.called != "" {
		t.Fatal("absent binding reached Runtime")
	}
	if _, err := s.ReadNativeThreadStatusV2(context.Background(), "not-a-session-id"); !errors.Is(err, ErrInvalidArgument) || r.called != "" {
		t.Fatal("invalid session reached Runtime")
	}
	if _, err := s.ReadNativeThreadStatusV2(context.Background(), testThreadID); !errors.Is(err, ErrNotFound) || r.called != "" {
		t.Fatal("absent session reached Runtime")
	}
	s = NewService(&fakeRuntime{}, openBoundFEAT134Store(t), NewEventHub(8, 2), nil)
	if _, err := s.ReadNativeThreadStatusV2(context.Background(), testSessionID); !errors.Is(err, ErrSessionNotUsable) {
		t.Fatal("missing native reader was accepted")
	}
}
