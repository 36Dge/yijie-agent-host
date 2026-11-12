package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/videogeneration"
	"github.com/36Dge/yijie-agent-host/internal/videogen"
	bolt "go.etcd.io/bbolt"
)

type videoProviderFixture struct {
	creates, queries, downloads atomic.Int64
	create                      func(context.Context, videogen.Request) (videogen.Task, error)
	query                       func(context.Context, string) (videogen.Task, error)
	download                    func(context.Context, videogen.Task) (videogen.Result, error)
	clip                        []byte
}

func (f *videoProviderFixture) Create(c context.Context, r videogen.Request) (videogen.Task, error) {
	f.creates.Add(1)
	if f.create != nil {
		return f.create(c, r)
	}
	return videogen.Task{ID: "cgt-normal-video"}, nil
}
func (f *videoProviderFixture) Get(c context.Context, id string) (videogen.Task, error) {
	f.queries.Add(1)
	if f.query != nil {
		return f.query(c, id)
	}
	return videogen.Task{ID: id, Status: wire.ProviderStatusSucceeded, VideoURL: "https://ordinary-result.invalid/clip.mp4"}, nil
}
func (f *videoProviderFixture) Download(c context.Context, r videogen.Task) (videogen.Result, error) {
	f.downloads.Add(1)
	if f.download != nil {
		return f.download(c, r)
	}
	return videogen.Result{Bytes: f.clip}, nil
}
func videoFixture(t *testing.T) (*Service, *videoProviderFixture, *int) {
	t.Helper()
	s, imageCalls := mediaFixture(t, false)
	clip, err := base64.StdEncoding.DecodeString(strings.TrimSpace(syntheticVideoBase64))
	if err != nil {
		t.Fatal(err)
	}
	provider := &videoProviderFixture{clip: clip}
	WithVideoGenerator(provider)(s)
	return s, provider, imageCalls
}
func videoGenerateRequest(root string) wire.GenerateRequest {
	return wire.GenerateRequest{ThreadId: testThreadID, TurnId: root, Input: wire.GenerateInput{Prompt: "A ceramic cup with a slow camera move", ImageMode: wire.ImageModeNone, ImageIndices: []int64{}}}
}
func videoGetRequest(root string) wire.GetRequest {
	return wire.GetRequest{ThreadId: testThreadID, TurnId: root, Input: wire.GetInput{}}
}
func checkedVideo(t *testing.T, r wire.VideoResult) wire.VideoResult {
	t.Helper()
	if err := r.Validate(); err != nil {
		t.Fatalf("invalid generated receipt: code=%s status=%s error=%v", r.Code, r.Status, err)
	}
	return r
}
func endVideoRoot(t *testing.T, s *Service, root, status string) {
	t.Helper()
	s.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{"threadId": testThreadID, "turn": map[string]any{"id": root, "status": status}}))
}
func beginVideoRoot(t *testing.T, s *Service, root string) {
	t.Helper()
	record, err := s.store.PrepareTurn(testSessionID, TraceContext{})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.beginImageTurn(context.Background(), record, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.BindTurn(testSessionID, root); err != nil {
		t.Fatal(err)
	}
	if err = s.bindImageTurn(testThreadID, root); err != nil {
		t.Fatal(err)
	}
}

func TestVideoGenerationCreatesOncePublishesAndUsesExistingACK(t *testing.T) {
	s, p, imageCalls := videoFixture(t)
	first := checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)))
	if first.Code != wire.VideoCodeProviderPending || first.VideoOperationId == nil || *first.VideoOperationId != testTurnID {
		t.Fatal(first)
	}
	duplicate := checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)))
	changed := videoGenerateRequest(testTurnID)
	changed.Input.Prompt = "A second scene"
	if duplicate.Code != wire.VideoCodeProviderPending || checkedVideo(t, s.GenerateVideo(context.Background(), changed)).Code != wire.VideoCodeOperationConflict || p.creates.Load() != 1 {
		t.Fatal("create replay changed dispatch")
	}
	got := checkedVideo(t, s.GetVideoResult(context.Background(), videoGetRequest(testTurnID)))
	if got.Status != wire.VideoStatusStaged || got.ArtifactId == nil {
		t.Fatal(got)
	}
	_, events, _, cancel, err := s.SubscribeMediaEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(events) != 2 || events[0].EventType != EventItemArtifactStarted || events[1].EventType != EventItemArtifactCompleted || events[1].Payload.Kind != "video" || events[1].Payload.Provenance != "provider" || events[1].Payload.MediaType != "video/mp4" {
		t.Fatal("video never entered the existing media stream")
	}
	data, err := s.ReadArtifact(testSessionID, *got.ArtifactId, ArtifactContent)
	if err != nil || !bytes.Equal(data.Bytes, p.clip) {
		t.Fatal("wrong artifact bytes", err)
	}
	repeated := checkedVideo(t, s.GetVideoResult(context.Background(), videoGetRequest(testTurnID)))
	if repeated.ArtifactId == nil || *repeated.ArtifactId != *got.ArtifactId {
		t.Fatal("repeated query produced another artifact")
	}
	if _, err = s.AcknowledgeArtifact(testSessionID, *got.ArtifactId, ArtifactAcknowledgement{AckID: *got.ArtifactId, SizeBytes: int64(len(data.Bytes)), SHA256: data.SHA256, LocalCommittedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	committed := checkedVideo(t, s.GetVideoResult(context.Background(), videoGetRequest(testTurnID)))
	if committed.Status != wire.VideoStatusCommitted || p.creates.Load() != 1 || p.queries.Load() != 1 || p.downloads.Load() != 1 {
		t.Fatal("ACK caused generation or download", committed)
	}
	if *imageCalls != 0 {
		t.Fatal("video used image provider")
	}
	imageResult := s.GenerateImage(context.Background(), providerMediaCall(map[string]any{"prompt": "A ceramic cup"}))
	if imageResult.Code != "ok" || *imageCalls != 1 {
		t.Fatal("video occupied the image slot")
	}
}

func TestVideoGenerationRestoresMissingHandleAndBlocksNewRootCreate(t *testing.T) {
	s, p, _ := videoFixture(t)
	_ = checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID))) // ordinary response not retained by caller
	endVideoRoot(t, s, testTurnID, "completed")
	beginVideoRoot(t, s, nextImageTurnID)
	blocked := checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(nextImageTurnID)))
	if blocked.Code != wire.VideoCodeExistingOperation || blocked.VideoOperationId == nil || *blocked.VideoOperationId != testTurnID || p.creates.Load() != 1 {
		t.Fatal(blocked)
	}
	endVideoRoot(t, s, testTurnID, "completed") // stale native terminal
	restored := checkedVideo(t, s.GetVideoResult(context.Background(), videoGetRequest(nextImageTurnID)))
	if restored.Status != wire.VideoStatusStaged || restored.DeliveryTurnId == nil || *restored.DeliveryTurnId != nextImageTurnID || *restored.VideoOperationId != testTurnID {
		t.Fatal(restored)
	}
	_, events, _, cancel, err := s.SubscribeMediaEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	for _, e := range events {
		if e.Payload.Kind == "video" && e.TurnID != nextImageTurnID {
			t.Fatal("restored video was published on the creation root")
		}
	}
}

func TestVideoGenerationUnknownSubmissionSurvivesNormalStoreReopen(t *testing.T) {
	s, p, _ := videoFixture(t)
	p.create = func(context.Context, videogen.Request) (videogen.Task, error) {
		return videogen.Task{}, &videogen.Error{Kind: videogen.Unavailable}
	}
	if checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID))).Code != wire.VideoCodeOutcomeUnknown {
		t.Fatal("unknown create was lost")
	}
	endVideoRoot(t, s, testTurnID, "completed")
	home := filepath.Dir(s.store.db.Path())
	before, err := s.store.videoOperation(testSessionID, testTurnID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	s.store = reopened
	after, err := reopened.videoOperation(testSessionID, testTurnID)
	if err != nil || before != after {
		t.Fatal("normal reopen changed video metadata", err)
	}
	// Existing session writers keep the additive private bucket intact.
	if _, err = reopened.UpdateTrace(testSessionID, TraceContext{}); err != nil {
		t.Fatal(err)
	}
	beginVideoRoot(t, s, nextImageTurnID)
	got := checkedVideo(t, s.GetVideoResult(context.Background(), videoGetRequest(nextImageTurnID)))
	blocked := checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(nextImageTurnID)))
	if got.Code != wire.VideoCodeOutcomeUnknown || blocked.Code != wire.VideoCodeExistingOperation || p.creates.Load() != 1 || p.queries.Load() != 0 {
		t.Fatal("reopen resubmitted an unknown create")
	}
	var encoded []byte
	_ = reopened.db.View(func(tx *bolt.Tx) error {
		encoded = bytes.Clone(tx.Bucket(videoOperationsBucket).Get(videoKey(testSessionID, testTurnID)))
		return nil
	})
	for _, forbidden := range []string{"A ceramic cup", "data:image/", "video_url", "ack_id", "content_href"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatal("private record contains content or a second artifact authority")
		}
	}
}

func TestVideoGenerationSelectedExpertBlocksCreationAndRecovery(t *testing.T) {
	s, p, _ := videoFixture(t)
	_ = checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)))
	endVideoRoot(t, s, testTurnID, "completed")
	beginVideoRoot(t, s, nextImageTurnID)
	err := s.store.db.Update(func(tx *bolt.Tx) error {
		if _, e := tx.CreateBucketIfNotExists(expertBindingsBucket); e != nil {
			return e
		}
		return putExpertBinding(tx, expertBinding{Version: 1, AgentSessionID: testSessionID, Revision: 1, Expert: feat158Ref()})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []wire.VideoResult{s.GetVideoResult(context.Background(), videoGetRequest(nextImageTurnID)), s.GenerateVideo(context.Background(), videoGenerateRequest(nextImageTurnID))} {
		if checkedVideo(t, result).Code != wire.VideoCodeExpertRequired {
			t.Fatal(result)
		}
	}
	if p.creates.Load() != 1 || p.queries.Load() != 0 || p.downloads.Load() != 0 {
		t.Fatal("selected expert fell back to builtin video")
	}
	binding, err := s.store.expertBinding(testSessionID)
	if err != nil || binding.Expert == nil {
		t.Fatal("expert selection removed")
	}
}

func TestVideoGenerationCancelledCreationRetainsLateKnownTask(t *testing.T) {
	s, p, _ := videoFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	p.create = func(context.Context, videogen.Request) (videogen.Task, error) {
		close(entered)
		<-release
		return videogen.Task{ID: "cgt-late-normal-task"}, nil
	}
	finished := make(chan wire.VideoResult, 1)
	go func() { finished <- s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)) }()
	<-entered
	endVideoRoot(t, s, testTurnID, "interrupted")
	beginVideoRoot(t, s, nextImageTurnID)
	close(release)
	if checkedVideo(t, <-finished).Code != wire.VideoCodeLocalCancelled {
		t.Fatal("cancelled root claimed success")
	}
	result := checkedVideo(t, s.GetVideoResult(context.Background(), videoGetRequest(nextImageTurnID)))
	if result.Status != wire.VideoStatusStaged || p.creates.Load() != 1 || *result.DeliveryTurnId != nextImageTurnID {
		t.Fatal("new observer inherited cancelled creation context", result)
	}
}

func TestVideoGenerationGetWindowPendingDoesNotCreateAgain(t *testing.T) {
	s, p, _ := videoFixture(t)
	p.query = func(_ context.Context, id string) (videogen.Task, error) {
		return videogen.Task{ID: id, Status: wire.ProviderStatusRunning}, nil
	}
	_ = checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	result := checkedVideo(t, s.GetVideoResult(ctx, videoGetRequest(testTurnID)))
	if result.Code != wire.VideoCodeProviderPending || result.Status != wire.VideoStatusPending || p.creates.Load() != 1 || p.queries.Load() != 1 || p.downloads.Load() != 0 {
		t.Fatal(result)
	}
}

func TestVideoGenerationHandoffIsNotReplayedAfterSpoolRestart(t *testing.T) {
	s, p, _ := videoFixture(t)
	_ = checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)))
	result := checkedVideo(t, s.GetVideoResult(context.Background(), videoGetRequest(testTurnID)))
	if result.ArtifactId == nil {
		t.Fatal(result)
	}
	if err := s.artifacts.Close(); err != nil {
		t.Fatal(err)
	}
	spool, err := artifact.OpenStore(filepath.Join(filepath.Dir(s.store.db.Path()), "artifact-spool"), artifact.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = spool.Close() })
	s.artifacts = spool
	got := checkedVideo(t, s.GetVideoResult(context.Background(), videoGetRequest(testTurnID)))
	if got.Code != wire.VideoCodeDeliveryUnconfirmed || got.ArtifactId == nil || *got.ArtifactId != *result.ArtifactId || p.creates.Load() != 1 || p.queries.Load() != 1 || p.downloads.Load() != 1 {
		t.Fatal("handoff was reconstructed", got)
	}
}

func TestVideoGenerationAmbiguousCompletedRecordsRequireSelection(t *testing.T) {
	s, p, _ := videoFixture(t)
	p.query = func(_ context.Context, id string) (videogen.Task, error) {
		return videogen.Task{ID: id, Status: wire.ProviderStatusFailed}, nil
	}
	for _, root := range []string{testTurnID, nextImageTurnID} {
		if root != testTurnID {
			beginVideoRoot(t, s, root)
		}
		_ = checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(root)))
		_ = checkedVideo(t, s.GetVideoResult(context.Background(), videoGetRequest(root)))
		endVideoRoot(t, s, root, "completed")
	}
	third := "019fbd88-cbc3-7bf1-934d-7b05cd693f98"
	beginVideoRoot(t, s, third)
	result := checkedVideo(t, s.GetVideoResult(context.Background(), videoGetRequest(third)))
	if result.Code != wire.VideoCodeSelectionRequired || result.Candidates == nil || len(*result.Candidates) != 2 || p.creates.Load() != 2 || p.queries.Load() != 2 {
		t.Fatal("ambiguous lookup guessed an operation", result)
	}
}

func TestVideoGenerationCurrentImageRolesAndPreflight(t *testing.T) {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 320, 320))); err != nil {
		t.Fatal(err)
	}
	ref := imageReference{mediaType: "image/png", dataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())}
	input := wire.GenerateInput{Prompt: "A cup", ImageMode: wire.ImageModeFirstLastFrames, ImageIndices: []int64{1, 1}}
	request, err := videoInput(input, []imageReference{ref})
	if err != nil || len(request.Images) != 2 || request.Images[0].Role != "first_frame" || request.Images[1].Role != "last_frame" || request.Images[0].DataURL != request.Images[1].DataURL {
		t.Fatal("same image frame roles lost", err)
	}
	s, p, _ := videoFixture(t)
	invalid := videoGenerateRequest(testTurnID)
	invalid.Input.ImageMode = wire.ImageModeFirstFrame
	invalid.Input.ImageIndices = []int64{1}
	if checkedVideo(t, s.GenerateVideo(context.Background(), invalid)).Code != wire.VideoCodeInputUnsupported || p.creates.Load() != 0 {
		t.Fatal("missing reference dispatched")
	}
	if _, err = s.store.videoOperation(testSessionID, testTurnID); !errors.Is(err, ErrNotFound) {
		t.Fatal("input rejection reserved a generation")
	}
}

func TestVideoGenerationConcurrentCallsShareOneCreation(t *testing.T) {
	s, p, _ := videoFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	p.create = func(context.Context, videogen.Request) (videogen.Task, error) {
		close(entered)
		<-release
		return videogen.Task{ID: "cgt-shared-creation"}, nil
	}
	first := make(chan wire.VideoResult, 1)
	go func() { first <- s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)) }()
	<-entered
	results := make(chan wire.VideoResult, 12)
	for i := 0; i < 12; i++ {
		go func() { results <- s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)) }()
	}
	for i := 0; i < 12; i++ {
		if got := checkedVideo(t, <-results); got.Code != wire.VideoCodeProviderPending {
			t.Fatal("duplicate did not observe original", got)
		}
	}
	close(release)
	checkedVideo(t, <-first)
	if p.creates.Load() != 1 {
		t.Fatal("concurrent duplicate creation")
	}
}

func TestVideoGenerationStopDuringDownloadDoesNotPublishIntoNextTurn(t *testing.T) {
	s, p, _ := videoFixture(t)
	checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)))
	entered, release := make(chan struct{}), make(chan struct{})
	p.download = func(context.Context, videogen.Task) (videogen.Result, error) {
		close(entered)
		<-release
		return videogen.Result{Bytes: p.clip}, nil
	}
	result := make(chan wire.VideoResult, 1)
	go func() { result <- s.GetVideoResult(context.Background(), videoGetRequest(testTurnID)) }()
	<-entered
	endVideoRoot(t, s, testTurnID, "interrupted")
	beginVideoRoot(t, s, nextImageTurnID)
	close(release)
	if checkedVideo(t, <-result).Code != wire.VideoCodeLocalCancelled {
		t.Fatal("stopped download claimed delivery")
	}
	_, events, _, cancel, err := s.SubscribeMediaEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	for _, event := range events {
		if event.EventType == EventItemArtifactStarted || event.EventType == EventItemArtifactCompleted {
			t.Fatal("late download published into another root")
		}
	}
	op, err := s.store.videoOperation(testSessionID, testTurnID)
	if err != nil || op.Handoff || op.TaskID == "" || !op.unresolved() {
		t.Fatal("original task no longer resumable", err)
	}
}

func TestVideoGenerationRetryAfterSurvivesNextToolCall(t *testing.T) {
	s, p, _ := videoFixture(t)
	checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)))
	p.query = func(context.Context, string) (videogen.Task, error) {
		return videogen.Task{}, &videogen.Error{Kind: videogen.Unavailable, RetryAfter: 30 * time.Second}
	}
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		got := checkedVideo(t, s.GetVideoResult(ctx, videoGetRequest(testTurnID)))
		cancel()
		if got.Code != wire.VideoCodeProviderPending {
			t.Fatal(got)
		}
	}
	if p.queries.Load() != 1 || p.creates.Load() != 1 {
		t.Fatal("new call bypassed Retry-After", p.queries.Load())
	}
	op, err := s.store.videoOperation(testSessionID, testTurnID)
	if err != nil || op.QueryNotBefore <= time.Now().Add(20*time.Second).UnixNano() || op.ProviderStatus != "" || op.ObservedAt != 0 {
		t.Fatal("throttle fabricated provider observation", err)
	}
}

func TestVideoGenerationCleanupRemovesMetadataAndLateWriteCannotRecreate(t *testing.T) {
	s, _, _ := videoFixture(t)
	checkedVideo(t, s.GenerateVideo(context.Background(), videoGenerateRequest(testTurnID)))
	op, err := s.store.videoOperation(testSessionID, testTurnID)
	if err != nil {
		t.Fatal(err)
	}
	endVideoRoot(t, s, testTurnID, "completed")
	const cleanupID = "17300000-0000-4000-8000-000000000099"
	if _, err = s.store.BeginCleanup(cleanupID, testSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.MarkCleanupRuntimeDeleted(cleanupID, testSessionID); err != nil {
		t.Fatal(err)
	}
	if err = s.store.DeleteSessionWithReceipt(cleanupID, testSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.videoOperation(testSessionID, testTurnID); !errors.Is(err, ErrNotFound) {
		t.Fatal("cleanup retained video metadata", err)
	}
	if _, err = s.store.updateVideo(op, func(v *videoOperation) error { v.TaskID = "cgt-late-after-cleanup"; return nil }); err == nil {
		t.Fatal("late response recreated deleted record")
	}
}
