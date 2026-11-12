package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/videogeneration"
	"github.com/36Dge/yijie-agent-host/internal/videogen"
	bolt "go.etcd.io/bbolt"
)

func TestVideoReauditUsesRemainingDownloadBudget(t *testing.T) {
	s, p, _ := videoFixture(t)
	checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p.download = func(c context.Context, _ videogen.Task) (videogen.Result, error) {
		deadline, ok := c.Deadline()
		if !ok || time.Until(deadline) > 5*time.Second {
			t.Error("download context escaped remaining call budget")
		}
		return videogen.Result{Bytes: p.clip}, nil
	}
	got := checkedVideo(t, s.GetVideoResult(ctx, videoGetRequest(testTurnID)))
	if got.Status != wire.VideoStatusStaged || p.creates.Load() != 1 || p.queries.Load() != 1 || p.downloads.Load() != 1 {
		t.Fatalf("available download budget was discarded: status=%s queries=%d downloads=%d", got.Status, p.queries.Load(), p.downloads.Load())
	}
}

func TestVideoReauditObservedTerminalSurvivesBudgetExpiry(t *testing.T) {
	for _, tc := range []struct {
		status wire.ProviderStatus
		code   wire.VideoCode
	}{
		{wire.ProviderStatusFailed, wire.VideoCodeProviderFailed},
		{wire.ProviderStatusExpired, wire.VideoCodeProviderExpired},
		{wire.ProviderStatusCancelled, wire.VideoCodeProviderFailed},
	} {
		t.Run(string(tc.status), func(t *testing.T) {
			s, p, _ := videoFixture(t)
			checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)))
			p.query = func(c context.Context, id string) (videogen.Task, error) {
				<-c.Done()
				return videogen.Task{ID: id, Status: tc.status}, nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			got := checkedVideo(t, s.GetVideoResult(ctx, videoGetRequest(testTurnID)))
			if got.Code != tc.code || got.Status != wire.VideoStatusFailed || p.downloads.Load() != 0 {
				t.Fatalf("known terminal became %s/%s", got.Code, got.Status)
			}
		})
	}
}

func TestVideoReauditRetryAfterSurvivesSimultaneousEnd(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "caller cancellation"}[stop], func(t *testing.T) {
			s, p, _ := videoFixture(t)
			checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)))
			var ctx context.Context
			var cancel context.CancelFunc
			if stop {
				ctx, cancel = context.WithCancel(context.Background())
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
			}
			defer cancel()
			p.query = func(c context.Context, _ string) (videogen.Task, error) {
				if stop {
					cancel()
				}
				<-c.Done()
				return videogen.Task{}, &videogen.Error{Kind: videogen.Unavailable, RetryAfter: 30 * time.Second}
			}
			got := checkedVideo(t, s.GetVideoResult(ctx, videoGetRequest(testTurnID)))
			want := wire.VideoCodeProviderPending
			if stop {
				want = wire.VideoCodeLocalCancelled
			}
			if got.Code != want {
				t.Fatal("observation end classification", got.Code)
			}
			op, err := s.store.videoOperation(testSessionID, testTurnID)
			if err != nil || op.QueryNotBefore < time.Now().Add(25*time.Second).UnixNano() || op.ProviderStatus != "" || op.ObservedAt != 0 {
				t.Fatal("received Retry-After was discarded or fabricated a status", err)
			}
		})
	}
}

func TestVideoReauditDownloadRetainsRetryAfter(t *testing.T) {
	s, p, _ := videoFixture(t)
	checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)))
	p.download = func(context.Context, videogen.Task) (videogen.Result, error) {
		return videogen.Result{}, &videogen.Error{Kind: videogen.Unavailable, RetryAfter: 30 * time.Second}
	}
	got := checkedVideo(t, s.GetVideoResult(context.Background(), videoGetRequest(testTurnID)))
	if got.Code != wire.VideoCodeProviderPending || p.downloads.Load() != 1 {
		t.Fatal("download response lost", got.Code)
	}
	op, err := s.store.videoOperation(testSessionID, testTurnID)
	if err != nil || op.QueryNotBefore < time.Now().Add(25*time.Second).UnixNano() {
		t.Fatal("download Retry-After was discarded", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	checkedVideo(t, s.GetVideoResult(ctx, videoGetRequest(testTurnID)))
	if p.queries.Load() != 1 || p.downloads.Load() != 1 {
		t.Fatal("new observation bypassed download throttle")
	}
}

func TestVideoReauditPreHandoffDeadlineIsNotUserStop(t *testing.T) {
	s, p, _ := videoFixture(t)
	checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)))
	record, turn, code := s.videoGate(context.Background(), testThreadID, testTurnID)
	if code != "" {
		t.Fatal(code)
	}
	op, err := s.store.videoOperation(testSessionID, testTurnID)
	if err != nil {
		t.Fatal(err)
	}
	op, err = s.store.updateVideo(op, func(v *videoOperation) error {
		v.ProviderStatus = wire.ProviderStatusSucceeded
		v.ObservedAt = time.Now().UnixNano()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Millisecond))
	defer cancel()
	got := checkedVideo(t, s.deliverVideo(ctx, ctx, record, testTurnID, turn, op, videogen.Result{Bytes: p.clip}))
	if got.Code != wire.VideoCodeProviderPending || got.NextAction != wire.NextActionGetVideoResult {
		t.Fatalf("budget expiry mislabeled as %s", got.Code)
	}
	after, err := s.store.videoOperation(testSessionID, testTurnID)
	if err != nil || after.Handoff {
		t.Fatal("expired observer handed off", err)
	}
}

func TestVideoReauditKnownTaskSurvivesNormalStoreAndServiceReopen(t *testing.T) {
	s, p, _ := videoFixture(t)
	checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)))
	endVideoRoot(t, s, testTurnID, "completed")
	home := filepath.Dir(s.store.db.Path())
	spoolRoot := filepath.Join(home, "artifact-spool")
	if err := s.store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.artifacts.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	spool, err := artifact.OpenStore(spoolRoot, artifact.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	fresh := NewService(s.runtime, store, NewEventHub(128, 64), nil, WithV3Artifacts(NewEventHubVersion(3, 128, 64), spool, false), WithArtifactMedia(), WithVideoGenerator(p))
	beginVideoRoot(t, fresh, nextImageTurnID)
	duplicate := checkedVideo(t, fresh.GenerateVideo(context.Background(), videoGenerateRequest(nextImageTurnID)))
	if duplicate.Code != wire.VideoCodeExistingOperation {
		t.Fatal("reopen forgot unresolved task", duplicate.Code)
	}
	result := checkedVideo(t, fresh.GetVideoResult(context.Background(), videoGetRequest(nextImageTurnID)))
	if result.Status != wire.VideoStatusStaged || result.VideoOperationId == nil || *result.VideoOperationId != testTurnID || result.DeliveryTurnId == nil || *result.DeliveryTurnId != nextImageTurnID || p.creates.Load() != 1 || p.queries.Load() != 1 || p.downloads.Load() != 1 {
		t.Fatal("known task lost across normal reopen", result.Code)
	}
}

func TestVideoReauditPendingExpertBlocksKnownTaskWithoutClearingSelection(t *testing.T) {
	s, p, _ := videoFixture(t)
	checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)))
	err := s.store.db.Update(func(tx *bolt.Tx) error {
		if _, e := tx.CreateBucketIfNotExists(expertBindingsBucket); e != nil {
			return e
		}
		return putExpertBinding(tx, expertBinding{Version: 1, AgentSessionID: testSessionID, Revision: 1, PendingOperation: "17300000-0000-4000-8000-000000000098"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range []wire.VideoResult{s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)), s.GetVideoResult(context.Background(), videoGetRequest(testTurnID))} {
		if checkedVideo(t, got).Code != wire.VideoCodeExpertRequired {
			t.Fatal("pending expert used builtin", got.Code)
		}
	}
	binding, err := s.store.expertBinding(testSessionID)
	if err != nil || binding.PendingOperation == "" || p.creates.Load() != 1 || p.queries.Load() != 0 || p.downloads.Load() != 0 {
		t.Fatal("pending selection or provider isolation lost", err)
	}
}
