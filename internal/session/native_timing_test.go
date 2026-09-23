package session

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	timing "github.com/36Dge/yijie-agent-host/internal/contracts/nativetiming"
	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

type timingReadRuntime struct {
	Runtime // No executable stand-in: unimplemented operations must never be called.
	raw     json.RawMessage
	err     error
	calls   int
	thread  string
}

func (r *timingReadRuntime) ReadThread(ctx context.Context, id string) (json.RawMessage, error) {
	r.calls++
	r.thread = id
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 3*time.Second {
		return nil, errors.New("unbounded read")
	}
	return r.raw, r.err
}

func TestFEAT155TimingReadOnlyExactTurnAndReopen(t *testing.T) {
	home := t.TempDir()
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	sid, tid, turn := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err = store.Reserve(Record{TaskID: uuid.NewString(), AgentSessionID: sid, Cwd: home}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.BindThread(sid, tid, "", "", ""); err != nil {
		t.Fatal(err)
	}
	r := &timingReadRuntime{}
	s := NewService(r, store, NewEventHub(8, 8), nil)
	set := func(turns string) { r.raw = json.RawMessage(`{"id":"` + tid + `","turns":` + turns + `}`) }
	set(`[{"id":"` + uuid.NewString() + `","durationMs":99},{"id":"` + turn + `","startedAt":0,"completedAt":0,"durationMs":250}]`)
	var before int
	_ = store.db.View(func(tx *bolt.Tx) error { before = tx.ID(); return nil })
	v, err := s.ReadNativeTurnTiming(context.Background(), sid, turn)
	if err != nil || v.StartedAt.Value == nil || *v.StartedAt.Value != 0 || *v.DurationMs.Value != 250 || r.thread != tid {
		t.Fatalf("exact native time: %+v %v", v, err)
	}
	_ = store.db.View(func(tx *bolt.Tx) error {
		if tx.ID() != before {
			t.Fatal("timing wrote Store")
		}
		return nil
	})
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	s = NewService(r, store, NewEventHub(8, 8), nil)
	again, err := s.ReadNativeTurnTiming(context.Background(), sid, turn)
	if err != nil || !reflect.DeepEqual(v, again) {
		t.Fatal("reopen changed native read", err)
	}
	set(`[{"id":"` + turn + `","startedAt":null,"durationMs":-1}]`)
	v, err = s.ReadNativeTurnTiming(context.Background(), sid, turn)
	if err != nil || v.StartedAt.State != timing.Unknown || v.CompletedAt.State != timing.Unknown || v.DurationMs.State != timing.Invalid || v.DurationMs.Value != nil {
		t.Fatal("invented time", err)
	}
	for _, c := range []struct {
		raw       string
		sid, turn string
		err       error
		want      timing.TimingErrorCode
	}{
		{string(r.raw), "invalid", turn, nil, timing.InvalidRequest},
		{string(r.raw), uuid.NewString(), turn, nil, timing.SessionNotFound},
		{string(r.raw), sid, uuid.NewString(), nil, timing.TurnNotFound},
		{`{"id":"` + uuid.NewString() + `","turns":[]}`, sid, turn, nil, timing.NativeTimingIdentityMismatch},
		{string(r.raw), sid, turn, context.DeadlineExceeded, timing.NativeTimingTimeout},
	} {
		r.raw = json.RawMessage(c.raw)
		r.err = c.err
		_, err = s.ReadNativeTurnTiming(context.Background(), c.sid, c.turn)
		var typed NativeTimingError
		if !errors.As(err, &typed) || typed.Code != c.want {
			t.Fatalf("%s: %v", c.want, err)
		}
	}
}

func TestFEAT155TimingBoundedHistoryAndFieldNumbers(t *testing.T) {
	for _, c := range []struct {
		raw   string
		state timing.TimeFieldState
	}{{"0", timing.Known}, {"null", timing.Unknown}, {"", timing.Unknown}, {"0.25", timing.Invalid}, {"9007199254740992", timing.Invalid}, {"-1", timing.Invalid}} {
		state, _ := timingNumber(json.RawMessage(c.raw), 0, 9007199254740991)
		if state != c.state {
			t.Fatal(c.raw, state)
		}
	}
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sid, tid, turn := uuid.NewString(), uuid.NewString(), uuid.NewString()
	_ = store.Reserve(Record{TaskID: uuid.NewString(), AgentSessionID: sid})
	_, err = store.BindThread(sid, tid, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turns := make([]map[string]string, 1025)
	for i := range turns {
		turns[i] = map[string]string{"id": turn}
	}
	raw, _ := json.Marshal(map[string]any{"id": tid, "turns": turns})
	r := &timingReadRuntime{raw: raw}
	s := NewService(r, store, NewEventHub(8, 8), nil)
	_, err = s.ReadNativeTurnTiming(context.Background(), sid, turn)
	var typed NativeTimingError
	if !errors.As(err, &typed) || typed.Code != timing.NativeTimingLimitExceeded {
		t.Fatal(err)
	}
}
