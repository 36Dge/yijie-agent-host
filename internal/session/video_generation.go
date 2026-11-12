package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"strings"
	"time"

	media "github.com/36Dge/yijie-agent-host/internal/contracts/mediageneration"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/videogeneration"
	"github.com/36Dge/yijie-agent-host/internal/videogen"
)

func WithVideoGenerator(provider videogen.Provider) ServiceOption {
	return func(s *Service) {
		s.videoGenerator = provider
		s.videoCalls = make(map[string]bool)
		s.videoDownloads = make(chan struct{}, wire.MaxDownloads)
	}
}
func videoResult(code wire.VideoCode) wire.VideoResult {
	result := wire.VideoResult{Source: "builtin", Code: code, Status: wire.VideoStatusBlocked, NextAction: wire.NextActionReportStatus}
	switch code {
	case wire.VideoCodeProviderPending:
		result.Status = wire.VideoStatusPending
	case wire.VideoCodeExpertRequired:
		result.NextAction = wire.NextActionUseExpert
	case wire.VideoCodeOutcomeUnknown, wire.VideoCodeDeliveryUnconfirmed:
		result.Status = wire.VideoStatusUnknown
	case wire.VideoCodeProviderFailed, wire.VideoCodeProviderExpired, wire.VideoCodeDeliveryRejected:
		result.Status = wire.VideoStatusFailed
	}
	return result
}
func videoWithOperation(result wire.VideoResult, o videoOperation) wire.VideoResult {
	if validUUID(o.RootID) {
		result.VideoOperationId = stringPointer(o.RootID)
	}
	if o.ProviderStatus != "" {
		state, observed := o.ProviderStatus, time.Unix(0, o.ObservedAt).Unix()
		result.ProviderStatus = &state
		result.ProviderObservedAt = &observed
	}
	return result
}
func videoAdmissionCode(err error) wire.VideoCode {
	if errors.Is(err, errVideoExpert) {
		return wire.VideoCodeExpertRequired
	}
	if errors.Is(err, ErrSessionNotUsable) || errors.Is(err, ErrTurnNotActive) || errors.Is(err, ErrNotFound) {
		return wire.VideoCodeLocalCancelled
	}
	return wire.VideoCodeServiceUnavailable
}

// Uses the same prepared root/binding/reference/cancellation authority as images.
func (s *Service) videoGate(ctx context.Context, thread, root string) (Record, *imageTurn, wire.VideoCode) {
	if s.videoGenerator == nil || s.artifacts == nil || s.media == nil {
		return Record{}, nil, wire.VideoCodeServiceUnavailable
	}
	if ctx.Err() != nil {
		return Record{}, nil, wire.VideoCodeLocalCancelled
	}
	record, err := s.store.GetByThread(thread)
	if err != nil || record.Purpose == purposeDraft || record.CleanupOperationID != "" {
		return Record{}, nil, wire.VideoCodeLocalCancelled
	}
	binding, err := s.store.expertBinding(record.AgentSessionID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Record{}, nil, wire.VideoCodeServiceUnavailable
	}
	if binding.Expert != nil || binding.PendingOperation != "" {
		return record, nil, wire.VideoCodeExpertRequired
	}
	if code := s.awaitImageTurnBinding(ctx, record.AgentSessionID, thread, root); code != "" {
		if code == media.GenerateCodeStatePending {
			return record, nil, wire.VideoCodeProviderPending
		}
		return record, nil, wire.VideoCodeLocalCancelled
	}
	s.imageMu.Lock()
	defer s.imageMu.Unlock()
	turn := s.imageTurns[thread]
	if turn == nil || turn.sessionID != record.AgentSessionID || turn.turnID != root || turn.stopped || turn.context.Err() != nil || ctx.Err() != nil {
		return record, nil, wire.VideoCodeLocalCancelled
	}
	if turn.route != "builtin" {
		return record, nil, wire.VideoCodeExpertRequired
	}
	record, err = s.store.videoCurrent(record.AgentSessionID, thread, root)
	if err != nil {
		return record, nil, videoAdmissionCode(err)
	}
	return record, turn, ""
}
func (s *Service) acquireVideoCall(session, id string, creating bool) bool {
	s.videoMu.Lock()
	defer s.videoMu.Unlock()
	key := string(videoKey(session, id))
	if _, busy := s.videoCalls[key]; busy {
		return false
	}
	s.videoCalls[key] = creating
	return true
}
func (s *Service) releaseVideoCall(session, id string) {
	s.videoMu.Lock()
	delete(s.videoCalls, string(videoKey(session, id)))
	s.videoMu.Unlock()
}
func (s *Service) videoCreating(o videoOperation) bool {
	s.videoMu.Lock()
	defer s.videoMu.Unlock()
	return s.videoCalls[string(videoKey(o.SessionID, o.RootID))]
}
func videoContext(caller context.Context, turn *imageTurn, timeout time.Duration) (context.Context, func()) {
	deadline := time.Now().Add(timeout)
	if remaining, ok := caller.Deadline(); ok && remaining.Before(deadline) {
		deadline = remaining
	}
	ctx, cancel := context.WithDeadline(turn.context, deadline)
	stop := context.AfterFunc(caller, cancel)
	if caller.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}
func videoStopped(caller context.Context, turn *imageTurn) bool {
	return turn.context.Err() != nil || errors.Is(caller.Err(), context.Canceled)
}

func videoPendingResult(caller context.Context, turn *imageTurn, o videoOperation) wire.VideoResult {
	if videoStopped(caller, turn) {
		return videoWithOperation(videoResult(wire.VideoCodeLocalCancelled), o)
	}
	result := videoWithOperation(videoResult(wire.VideoCodeProviderPending), o)
	result.NextAction = wire.NextActionGetVideoResult
	return result
}

// A received cooldown remains authoritative even if this observer just ended.
// Reuse the existing private timestamp; do not manufacture a provider status.
func (s *Service) retainVideoBackoff(o videoOperation, providerErr error) (videoOperation, error) {
	delay := videogen.RetryAfter(providerErr)
	if delay <= 0 {
		return o, nil
	}
	next := time.Now().Add(delay)
	if maximum := time.Unix(0, 1<<63-1); next.After(maximum) {
		next = maximum
	}
	return s.store.updateVideo(o, func(value *videoOperation) error {
		if next.UnixNano() > value.QueryNotBefore {
			value.QueryNotBefore = next.UnixNano()
		}
		return nil
	})
}

func (s *Service) videoReceipt(o videoOperation) wire.VideoResult {
	if o.Handoff {
		result := videoWithOperation(videoResult(wire.VideoCodeDeliveryUnconfirmed), o)
		result.ArtifactId = stringPointer(o.ArtifactID)
		result.DeliveryTurnId = stringPointer(o.DeliveryTurnID)
		switch s.artifacts.Status(o.SessionID, o.ArtifactID) {
		case "committed":
			result.Status = wire.VideoStatusCommitted
			result.Code = wire.VideoCodeOk
			result.NextAction = wire.NextActionReportStatus
		case "staged":
			if o.FailureCode != wire.VideoCodeDeliveryUnconfirmed {
				result.Status = wire.VideoStatusStaged
				result.Code = wire.VideoCodeOk
				result.NextAction = wire.NextActionCheckArtifact
			}
		}
		return result
	}
	code := wire.VideoCodeProviderPending
	switch o.Phase {
	case videoReserved, videoSending:
		if !s.videoCreating(o) {
			code = wire.VideoCodeOutcomeUnknown
		}
	case videoUnknown:
		code = wire.VideoCodeOutcomeUnknown
	case videoCancelled:
		code = wire.VideoCodeLocalCancelled
	case videoRejected, videoDeliveryRejected:
		code = o.FailureCode
		if code == "" {
			code = wire.VideoCodeProviderFailed
		}
	}
	switch o.ProviderStatus {
	case wire.ProviderStatusFailed, wire.ProviderStatusCancelled:
		code = wire.VideoCodeProviderFailed
	case wire.ProviderStatusExpired:
		code = wire.VideoCodeProviderExpired
	}
	result := videoWithOperation(videoResult(code), o)
	if code == wire.VideoCodeProviderPending {
		result.NextAction = wire.NextActionGetVideoResult
	}
	return result
}

func videoInput(input wire.GenerateInput, references []imageReference) (videogen.Request, error) {
	if input.Validate() != nil {
		return videogen.Request{}, ErrInvalidArgument
	}
	request := videogen.Request{Prompt: input.Prompt}
	for position, n := range input.ImageIndices {
		if n < 1 || n > int64(len(references)) {
			return request, ErrInvalidArgument
		}
		ref := references[n-1]
		if _, ok := supportedImageMediaTypes[ref.mediaType]; !ok {
			return request, ErrInvalidArgument
		}
		prefix := "data:" + ref.mediaType + ";base64,"
		if !strings.HasPrefix(ref.dataURL, prefix) {
			return request, ErrInvalidArgument
		}
		encoded := strings.TrimPrefix(ref.dataURL, prefix)
		if base64.StdEncoding.DecodedLen(len(encoded)) > int(maxAttachmentBytes)+2 {
			return request, ErrInvalidArgument
		}
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(data) > int(maxAttachmentBytes) {
			return request, ErrInvalidArgument
		}
		var width, height int
		if ref.mediaType == "image/webp" {
			var ok bool
			width, height, ok = validWebP(data)
			if !ok {
				return request, ErrInvalidArgument
			}
		} else {
			config, _, e := image.DecodeConfig(bytes.NewReader(data))
			if e != nil {
				return request, ErrInvalidArgument
			}
			width, height = config.Width, config.Height
		}
		if width < 300 || height < 300 || width > 6000 || height > 6000 || width*5 < height*2 || width*2 > height*5 {
			return request, ErrInvalidArgument
		}
		role := "reference_image"
		if input.ImageMode == wire.ImageModeFirstFrame || input.ImageMode == wire.ImageModeFirstLastFrames {
			role = "first_frame"
			if position == 1 {
				role = "last_frame"
			}
		}
		request.Images = append(request.Images, videogen.Reference{DataURL: ref.dataURL, Role: role})
	}
	// Bound the actual serialized request before a paid reservation/dispatch.
	// The provider independently enforces the same limit while encoding.
	size := len(input.Prompt)*6 + 512
	for _, ref := range request.Images {
		size += len(ref.DataURL) + 128
	}
	if size > 64_000_000 {
		return request, ErrInvalidArgument
	}
	return request, nil
}

func (s *Service) GenerateVideo(caller context.Context, req wire.GenerateRequest) wire.VideoResult {
	if req.Validate() != nil || requireUUID("thread_id", req.ThreadId) != nil || requireUUID("turn_id", req.TurnId) != nil {
		return videoResult(wire.VideoCodeInputUnsupported)
	}
	caller, cancelBudget := context.WithTimeout(caller, wire.GenerateTimeoutSeconds*time.Second)
	defer cancelBudget()
	record, turn, code := s.videoGate(caller, req.ThreadId, req.TurnId)
	if code != "" {
		return videoResult(code)
	}
	if !s.acquireVideoCall(record.AgentSessionID, req.TurnId, true) {
		o, err := s.store.videoOperation(record.AgentSessionID, req.TurnId)
		if err == nil {
			encoded, _ := json.Marshal(req.Input)
			digest := fmt.Sprintf("%x", sha256.Sum256(encoded))
			if digest != o.Digest {
				return videoResult(wire.VideoCodeOperationConflict)
			}
			return s.videoReceipt(o)
		}
		return videoResult(wire.VideoCodeProviderPending)
	}
	defer s.releaseVideoCall(record.AgentSessionID, req.TurnId)
	ctx, done := videoContext(caller, turn, wire.GenerateTimeoutSeconds*time.Second)
	defer done()
	s.imageMu.Lock()
	if turn.stopped || turn.context.Err() != nil || ctx.Err() != nil {
		s.imageMu.Unlock()
		return videoResult(wire.VideoCodeLocalCancelled)
	}
	references := append([]imageReference(nil), turn.references...)
	s.imageMu.Unlock()
	input, err := videoInput(req.Input, references)
	if err != nil {
		return videoResult(wire.VideoCodeInputUnsupported)
	}
	encoded, _ := json.Marshal(req.Input)
	digest := fmt.Sprintf("%x", sha256.Sum256(encoded))
	s.imageMu.Lock()
	if turn.stopped || ctx.Err() != nil {
		s.imageMu.Unlock()
		return videoResult(wire.VideoCodeLocalCancelled)
	}
	o, created, err := s.store.reserveVideo(record.AgentSessionID, req.ThreadId, req.TurnId, digest, req.Input.ImageMode)
	s.imageMu.Unlock()
	if err != nil {
		if errors.Is(err, ErrTurnOperationConflict) {
			return videoResult(wire.VideoCodeOperationConflict)
		}
		return videoResult(videoAdmissionCode(err))
	}
	if !created {
		if o.RootID != req.TurnId {
			result := videoWithOperation(videoResult(wire.VideoCodeExistingOperation), o)
			result.NextAction = wire.NextActionGetVideoResult
			return result
		}
		// Our current map entry is not evidence that a retained submission is live.
		if o.Phase == videoReserved || o.Phase == videoSending {
			return videoWithOperation(videoResult(wire.VideoCodeOutcomeUnknown), o)
		}
		return s.videoReceipt(o)
	}
	if _, _, code = s.videoGate(ctx, req.ThreadId, req.TurnId); code != "" {
		_, _ = s.store.updateVideo(o, func(v *videoOperation) error { v.Phase = videoCancelled; return nil })
		return videoWithOperation(videoResult(code), o)
	}
	o, err = s.store.updateVideo(o, func(v *videoOperation) error { v.Phase = videoSending; return nil })
	if err != nil {
		return videoResult(wire.VideoCodeServiceUnavailable)
	}
	if ctx.Err() != nil {
		if updated, e := s.store.updateVideo(o, func(v *videoOperation) error { v.Phase = videoCancelled; return nil }); e == nil {
			o = updated
		}
		return videoWithOperation(videoResult(wire.VideoCodeLocalCancelled), o)
	}
	task, providerErr := s.videoGenerator.Create(ctx, input)
	o, err = s.store.updateVideo(o, func(v *videoOperation) error {
		if providerErr == nil && videogen.ValidTaskID(task.ID) {
			v.TaskID = task.ID
			v.Phase = videoSubmitted
			return nil
		}
		v.Phase = videoUnknown
		v.FailureCode = wire.VideoCodeOutcomeUnknown
		switch videogen.Kind(providerErr) {
		case videogen.Authentication:
			v.Phase = videoRejected
			v.FailureCode = wire.VideoCodeServiceUnavailable
		case videogen.Rejected, videogen.LimitExceeded:
			v.Phase = videoRejected
			v.FailureCode = wire.VideoCodeProviderFailed
		}
		return nil
	})
	if err != nil {
		result := videoResult(wire.VideoCodeOutcomeUnknown)
		result.VideoOperationId = stringPointer(req.TurnId)
		return result
	}
	if videoStopped(caller, turn) {
		return videoWithOperation(videoResult(wire.VideoCodeLocalCancelled), o)
	}
	return s.videoReceipt(o)
}

func (s *Service) videoCandidate(o videoOperation) wire.OperationCandidate {
	phase := wire.OperationPhasePending
	if o.Handoff {
		phase = wire.OperationPhaseHandedOff
	} else if o.Phase == videoUnknown || ((o.Phase == videoReserved || o.Phase == videoSending) && !s.videoCreating(o)) {
		phase = wire.OperationPhaseUnknown
	} else if o.Phase == videoReserved || o.Phase == videoSending {
		phase = wire.OperationPhaseSubmitting
	} else if !o.unresolved() {
		phase = wire.OperationPhaseFailed
	} else if o.ProviderStatus == wire.ProviderStatusSucceeded {
		phase = wire.OperationPhaseSucceeded
	}
	return wire.OperationCandidate{VideoOperationId: o.RootID, CreatedAt: o.CreatedAt, ImageMode: o.ImageMode, Phase: phase}
}
func waitVideo(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}

func (s *Service) GetVideoResult(caller context.Context, req wire.GetRequest) wire.VideoResult {
	if req.Validate() != nil || requireUUID("thread_id", req.ThreadId) != nil || requireUUID("turn_id", req.TurnId) != nil {
		return videoResult(wire.VideoCodeInputUnsupported)
	}
	// Binding admission is part of the same bridge budget as polling/download.
	caller, cancelBudget := context.WithTimeout(caller, wire.GetTimeoutSeconds*time.Second)
	defer cancelBudget()
	record, turn, code := s.videoGate(caller, req.ThreadId, req.TurnId)
	if code != "" {
		return videoResult(code)
	}
	o, candidates, err := s.store.resolveVideo(record.AgentSessionID, req.ThreadId, req.TurnId, req.Input.VideoOperationId)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return videoResult(wire.VideoCodeOperationNotFound)
		}
		return videoResult(videoAdmissionCode(err))
	}
	if o == nil {
		result := videoResult(wire.VideoCodeSelectionRequired)
		result.NextAction = wire.NextActionSelectOperation
		complete := len(candidates) <= 5
		if !complete {
			candidates = candidates[:5]
		}
		values := make([]wire.OperationCandidate, 0, len(candidates))
		for _, entry := range candidates {
			values = append(values, s.videoCandidate(entry))
		}
		result.Candidates = &values
		result.CandidatesComplete = &complete
		return result
	}
	if !s.acquireVideoCall(o.SessionID, o.RootID, false) {
		return s.videoReceipt(*o)
	}
	defer s.releaseVideoCall(o.SessionID, o.RootID)
	// A previous observer may have completed handoff before this lease was acquired.
	current, err := s.store.videoOperation(o.SessionID, o.RootID)
	if err != nil {
		return videoResult(wire.VideoCodeServiceUnavailable)
	}
	o = &current
	if o.Handoff || !o.unresolved() || o.TaskID == "" {
		return s.videoReceipt(*o)
	}
	ctx, done := videoContext(caller, turn, wire.GetTimeoutSeconds*time.Second)
	defer done()
	pending := func() wire.VideoResult {
		return videoPendingResult(caller, turn, *o)
	}
	delay := time.Duration(0)
	if o.ObservedAt > 0 {
		delay = time.Until(time.Unix(0, o.ObservedAt).Add(wire.PollIntervalSeconds * time.Second))
	}
	if next := time.Until(time.Unix(0, o.QueryNotBefore)); next > delay {
		delay = next
	}
	for waitVideo(ctx, delay) {
		if _, _, code = s.videoGate(ctx, req.ThreadId, req.TurnId); code != "" {
			if code == wire.VideoCodeLocalCancelled && ctx.Err() != nil {
				return pending()
			}
			return videoWithOperation(videoResult(code), *o)
		}
		// Retain only a throttle timestamp, so a new tool call or normal reopen
		// cannot bypass the minimum interval or a previous Retry-After.
		value, e := s.store.updateVideo(*o, func(v *videoOperation) error {
			v.QueryNotBefore = time.Now().Add(wire.PollIntervalSeconds * time.Second).UnixNano()
			return nil
		})
		if e != nil {
			return videoWithOperation(videoResult(wire.VideoCodeServiceUnavailable), *o)
		}
		o = &value
		task, e := s.videoGenerator.Get(ctx, o.TaskID)
		if e != nil {
			retained, retentionErr := s.retainVideoBackoff(*o, e)
			if retentionErr != nil {
				return videoWithOperation(videoResult(videoAdmissionCode(retentionErr)), *o)
			}
			o = &retained
			if ctx.Err() != nil {
				return pending()
			}
			switch videogen.Kind(e) {
			case videogen.Authentication:
				return videoWithOperation(videoResult(wire.VideoCodeServiceUnavailable), *o)
			case videogen.Rejected:
				return videoWithOperation(videoResult(wire.VideoCodeOperationNotFound), *o)
			case videogen.InvalidResponse:
				return videoWithOperation(videoResult(wire.VideoCodeOutcomeUnknown), *o)
			}
			delay = wire.PollIntervalSeconds * time.Second
			if retry := videogen.RetryAfter(e); retry > delay {
				delay = retry
			}
			continue
		}
		if task.ID != o.TaskID || task.Status.Validate() != nil {
			return videoWithOperation(videoResult(wire.VideoCodeOutcomeUnknown), *o)
		}
		value, e = s.store.updateVideo(*o, func(v *videoOperation) error {
			v.ProviderStatus = task.Status
			v.ObservedAt = time.Now().UTC().UnixNano()
			return nil
		})
		if e != nil {
			return videoWithOperation(videoResult(wire.VideoCodeServiceUnavailable), *o)
		}
		o = &value
		if videoStopped(caller, turn) {
			return pending()
		}
		// Expiring this observation cannot undo a terminal fact just received.
		if !o.unresolved() {
			return s.videoReceipt(*o)
		}
		if ctx.Err() != nil {
			return pending()
		}
		switch task.Status {
		case wire.ProviderStatusQueued, wire.ProviderStatusRunning:
			delay = wire.PollIntervalSeconds * time.Second
			continue
		case wire.ProviderStatusSucceeded:
			select {
			case s.videoDownloads <- struct{}{}:
			case <-ctx.Done():
				return pending()
			}
			result := func() wire.VideoResult {
				defer func() { <-s.videoDownloads }()
				if ctx.Err() != nil {
					return pending()
				}
				if _, _, code = s.videoGate(ctx, req.ThreadId, req.TurnId); code != "" {
					if code == wire.VideoCodeLocalCancelled && ctx.Err() != nil {
						return pending()
					}
					return videoWithOperation(videoResult(code), *o)
				}
				content, e := s.videoGenerator.Download(ctx, task)
				if e != nil {
					retained, retentionErr := s.retainVideoBackoff(*o, e)
					if retentionErr != nil {
						return videoWithOperation(videoResult(videoAdmissionCode(retentionErr)), *o)
					}
					o = &retained
					if ctx.Err() != nil {
						return pending()
					}
					if videogen.Kind(e) == videogen.LimitExceeded || videogen.Kind(e) == videogen.InvalidResponse || videogen.Kind(e) == videogen.Rejected {
						_, _ = s.store.updateVideo(*o, func(v *videoOperation) error {
							v.Phase = videoDeliveryRejected
							v.FailureCode = wire.VideoCodeDeliveryRejected
							return nil
						})
						return videoWithOperation(videoResult(wire.VideoCodeDeliveryRejected), *o)
					}
					return pending()
				}
				return s.deliverVideo(ctx, caller, record, req.TurnId, turn, *o, content)
			}()
			return result
		}
	}
	return pending()
}
