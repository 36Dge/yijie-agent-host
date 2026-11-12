package session

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/videogeneration"
	"github.com/36Dge/yijie-agent-host/internal/videogen"
)

type videoDelivery struct {
	operation  videoOperation
	artifactID string
	itemID     string
	finished   bool
}

func (s *Service) finishVideoFailureLocked(record Record, turn *imageTurn, code string) {
	delivery := turn.video
	if delivery == nil || delivery.finished {
		return
	}
	delivery.finished = true
	_, _ = s.store.updateVideo(delivery.operation, func(o *videoOperation) error { o.FailureCode = wire.VideoCodeDeliveryUnconfirmed; return nil })
	ordinal, retry := 0, false
	_ = s.publishV3(record, Event{TurnID: turn.turnID, ItemID: delivery.itemID, EventType: EventItemArtifactFailed, Payload: EventPayload{
		ArtifactID: delivery.artifactID, Kind: "video", Provenance: "provider", Status: "failed", Ordinal: &ordinal, ErrorCode: code, Retryable: &retry, Message: stringPointer("Video delivery did not finish; do not create another generation to retry delivery."),
	}})
}
func (s *Service) unconfirmedVideo(o videoOperation) wire.VideoResult {
	result := videoWithOperation(videoResult(wire.VideoCodeDeliveryUnconfirmed), o)
	result.ArtifactId = stringPointer(o.ArtifactID)
	result.DeliveryTurnId = stringPointer(o.DeliveryTurnID)
	return result
}

// All network work has finished. The same short publication lock used by
// image termination arbitrates current-turn cancellation against these events.
func (s *Service) deliverVideo(ctx, caller context.Context, record Record, turnID string, turn *imageTurn, o videoOperation, content videogen.Result) wire.VideoResult {
	if len(content.Bytes) < 32 || len(content.Bytes) > wire.MaxVideoBytes || string(content.Bytes[4:8]) != "ftyp" {
		_, _ = s.store.updateVideo(o, func(v *videoOperation) error {
			v.Phase = videoDeliveryRejected
			v.FailureCode = wire.VideoCodeDeliveryRejected
			return nil
		})
		return videoWithOperation(videoResult(wire.VideoCodeDeliveryRejected), o)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(content.Bytes))
	id, err := newUUID()
	if err != nil {
		return videoWithOperation(videoResult(wire.VideoCodeServiceUnavailable), o)
	}
	s.imageMu.Lock()
	defer s.imageMu.Unlock()
	if s.imageTurns[record.CodexThreadID] != turn || turn.stopped || turn.context.Err() != nil {
		return videoWithOperation(videoResult(wire.VideoCodeLocalCancelled), o)
	}
	if ctx.Err() != nil {
		return videoPendingResult(caller, turn, o)
	}
	record, err = s.store.videoCurrent(record.AgentSessionID, record.CodexThreadID, turnID)
	if err != nil {
		return videoWithOperation(videoResult(videoAdmissionCode(err)), o)
	}
	o, err = s.store.prepareVideoArtifact(o, record.CodexThreadID, turnID, id)
	if err != nil {
		return videoWithOperation(videoResult(videoAdmissionCode(err)), o)
	}
	id = o.ArtifactID
	const name = "生成的视频.mp4"
	switch s.artifacts.Status(record.AgentSessionID, id) {
	case "staged":
		original, e := s.artifacts.Get(record.AgentSessionID, id, artifact.Content)
		if e != nil || original.MediaType != "video/mp4" || original.SHA256 != hash || len(original.Bytes) != len(content.Bytes) {
			return videoWithOperation(videoResult(wire.VideoCodeServiceUnavailable), o)
		}
	case "unknown":
		if err = s.artifacts.Put(artifact.Manifest{SessionID: record.AgentSessionID, ArtifactID: id, Kind: "video", DisplayName: name, MediaType: "video/mp4", Content: content.Bytes}); err != nil {
			if errors.Is(err, artifact.ErrInvalid) {
				_, _ = s.store.updateVideo(o, func(v *videoOperation) error {
					v.Phase = videoDeliveryRejected
					v.FailureCode = wire.VideoCodeDeliveryRejected
					return nil
				})
				return videoWithOperation(videoResult(wire.VideoCodeDeliveryRejected), o)
			}
			return videoWithOperation(videoResult(wire.VideoCodeServiceUnavailable), o)
		}
	default:
		return videoWithOperation(videoResult(wire.VideoCodeServiceUnavailable), o)
	}
	if ctx.Err() != nil || turn.context.Err() != nil {
		return videoPendingResult(caller, turn, o)
	}
	o, err = s.store.markVideoHandoff(o, record.CodexThreadID, turnID)
	if err != nil {
		return videoWithOperation(videoResult(videoAdmissionCode(err)), o)
	}
	fail := func() wire.VideoResult {
		_, _ = s.store.updateVideo(o, func(v *videoOperation) error { v.FailureCode = wire.VideoCodeDeliveryUnconfirmed; return nil })
		return s.unconfirmedVideo(o)
	}
	if ctx.Err() != nil {
		return fail()
	}
	itemID, ordinal := "provider-video-"+id, 0
	if s.publishV3(record, Event{TurnID: turnID, ItemID: itemID, EventType: EventItemArtifactStarted, Payload: EventPayload{ArtifactID: id, Kind: "video", Provenance: "provider", Status: "in_progress", Ordinal: &ordinal, DisplayName: name}}) != nil {
		return fail()
	}
	turn.video = &videoDelivery{operation: o, artifactID: id, itemID: itemID}
	if ctx.Err() != nil {
		s.finishVideoFailureLocked(record, turn, "turn_interrupted")
		return s.unconfirmedVideo(o)
	}
	size := int64(len(content.Bytes))
	// A partial publisher failure can already have reached a reader. Never send
	// a second, conflicting terminal after attempting completion.
	turn.video.finished = true
	if s.publishV3(record, Event{TurnID: turnID, ItemID: itemID, EventType: EventItemArtifactCompleted, Payload: EventPayload{
		ArtifactID: id, Kind: "video", Provenance: "provider", Status: "ready", Ordinal: &ordinal, DisplayName: name, MediaType: "video/mp4", SizeBytes: &size, SHA256: hash,
		ContentHref: fmt.Sprintf("/v3/agent-sessions/%s/artifacts/%s/content", record.AgentSessionID, id),
	}}) != nil {
		return fail()
	}
	return s.videoReceipt(o)
}
