package session

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/artifactmedia"
)

// Only registration metadata is retained here; bytes live in the existing spool.
type mediaRegistration struct {
	digest  string
	receipt wire.PublishReceipt
}
type mediaAuthority struct {
	mu            sync.Mutex
	events        *EventHub
	filesEvents   *EventHub
	subscribers   map[string]int
	extended      map[string]bool
	legacyOrder   map[string]map[string]int
	registrations map[string]mediaRegistration
	ordinals      map[string]int
	orderMu       sync.Mutex
	order         map[string]map[string]int
}

func WithArtifactMedia() ServiceOption {
	return func(s *Service) {
		s.media = &mediaAuthority{events: NewEventHubVersion(9, 512, 64), filesEvents: NewEventHubVersion(12, 512, 64), subscribers: make(map[string]int), extended: make(map[string]bool), legacyOrder: make(map[string]map[string]int), registrations: make(map[string]mediaRegistration), ordinals: make(map[string]int), order: make(map[string]map[string]int)}
	}
}

func (s *Service) SubscribeMediaEvents(sessionID, streamID string, after uint64) (string, []Event, <-chan Event, func(), error) {
	if s.media == nil {
		return "", nil, nil, nil, ErrSessionNotUsable
	}
	if _, err := s.store.Get(sessionID); err != nil {
		return "", nil, nil, nil, err
	}
	return s.media.events.Subscribe(sessionID, streamID, after)
}

// The explicit v12 reader is the admission evidence for new file MIME values.
// This uses the same EventHub and resource/ACK implementation as legacy v9.
func (s *Service) SubscribeFileMediaEvents(sessionID, streamID string, after uint64) (string, []Event, <-chan Event, func(), error) {
	if s.media == nil {
		return "", nil, nil, nil, ErrSessionNotUsable
	}
	if _, err := s.store.Get(sessionID); err != nil {
		return "", nil, nil, nil, err
	}
	id, replay, events, cancel, err := s.media.filesEvents.Subscribe(sessionID, streamID, after)
	if err != nil {
		return id, replay, events, cancel, err
	}
	s.media.mu.Lock()
	s.media.subscribers[sessionID]++
	s.media.mu.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			cancel()
			s.media.mu.Lock()
			defer s.media.mu.Unlock()
			s.media.subscribers[sessionID]--
			if s.media.subscribers[sessionID] <= 0 {
				delete(s.media.subscribers, sessionID)
			}
		})
	}
	return id, replay, events, release, nil
}
func (s *Service) publishMedia(record Record, event Event) error {
	if s.media == nil {
		return nil
	}
	// Both media streams retain the v3 ordinary-event contract. Reuse the
	// existing compatibility projection before publishing newer lifecycle fields.
	event = legacyEvent(event)
	decorateEvent(record, &event)
	s.media.orderMu.Lock()
	defer s.media.orderMu.Unlock()
	publish := func(hub *EventHub, orders map[string]map[string]int) error {
		copy := event
		if copy.Payload.ArtifactID != "" && copy.Payload.Ordinal != nil {
			key := record.AgentSessionID + "/" + copy.TurnID
			order := orders[key]
			if order == nil {
				order = make(map[string]int)
				orders[key] = order
			}
			ordinal, ok := order[copy.Payload.ArtifactID]
			if !ok {
				ordinal = len(order)
				order[copy.Payload.ArtifactID] = ordinal
			}
			copy.Payload.Ordinal = &ordinal
		}
		_, err := hub.Publish(copy)
		return err
	}
	if err := publish(s.media.filesEvents, s.media.order); err != nil {
		return err
	}
	// Suppress the entire new artifact lifecycle, including started/failed, on v9.
	if s.media.extended[event.Payload.ArtifactID] {
		return nil
	}
	return publish(s.media.events, s.media.legacyOrder)
}

func (s *Service) clearMediaRegistrations(prefix string) {
	if s.media == nil {
		return
	}
	s.media.mu.Lock()
	defer s.media.mu.Unlock()
	for key := range s.media.registrations {
		if strings.HasPrefix(key, prefix) {
			delete(s.media.registrations, key)
		}
	}
	for key := range s.media.ordinals {
		if strings.HasPrefix(key+"/", prefix) {
			delete(s.media.ordinals, key)
		}
	}
	s.media.orderMu.Lock()
	defer s.media.orderMu.Unlock()
	for key := range s.media.order {
		if strings.HasPrefix(key+"/", prefix) {
			for id := range s.media.order[key] {
				delete(s.media.extended, id)
			}
			delete(s.media.order, key)
			delete(s.media.legacyOrder, key)
		}
	}
}

// Native thread/turn are supplied by the private MCP adapter, never tool arguments.
func (s *Service) PublishArtifact(ctx context.Context, request wire.PublishRequest) (wire.PublishReceipt, error) {
	fail := func() (wire.PublishReceipt, error) {
		return wire.PublishReceipt{}, errors.New("artifact registration unavailable")
	}
	if s.media == nil || s.artifacts == nil || ctx.Err() != nil {
		return fail()
	}
	input := request.Input
	if !utf8.ValidString(input.RelativePath) || len(input.RelativePath) > 1024 || !filepath.IsLocal(input.RelativePath) || path.Clean(input.RelativePath) != input.RelativePath || strings.ContainsAny(input.RelativePath, "\\\x00") || !validV4DisplayName(input.DisplayName, false) {
		return fail()
	}
	record, err := s.store.GetByThread(request.ThreadId)
	if err != nil || record.Purpose == purposeDraft || record.ActiveTurnID != request.TurnId || record.ActiveTurnID == "" || record.CleanupOperationID != "" {
		return fail()
	}
	root, err := os.OpenRoot(record.Cwd)
	if err != nil {
		return fail()
	}
	defer root.Close()
	// Check every component with the rooted authority; never follow a link.
	parts := strings.Split(input.RelativePath, "/")
	for i := range parts {
		info, e := root.Lstat(strings.Join(parts[:i+1], "/"))
		if e != nil || info.Mode()&os.ModeSymlink != 0 {
			return fail()
		}
	}
	f, err := root.Open(input.RelativePath)
	if err != nil {
		return fail()
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 64<<20 {
		return fail()
	}
	content, err := io.ReadAll(io.LimitReader(f, 64<<20+1))
	if err != nil || int64(len(content)) != info.Size() || ctx.Err() != nil {
		return fail()
	}
	kind, mime := artifactMediaType(input.RelativePath)
	if kind == "" || kind == "image" && len(content) > 20<<20 {
		return fail()
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(content))
	key := record.AgentSessionID + "/" + request.TurnId + "/" + input.RelativePath
	turnKey := record.AgentSessionID + "/" + request.TurnId
	manifestDigest, _ := json.Marshal([]string{sum, input.DisplayName, mime})
	s.terminalMu.Lock()
	defer s.terminalMu.Unlock()
	s.media.mu.Lock()
	defer s.media.mu.Unlock()
	if previous, ok := s.media.registrations[key]; ok {
		if previous.digest == string(manifestDigest) {
			return previous.receipt, nil
		}
		return fail()
	}
	if !wire.LegacyMediaAllowed(kind, mime) && s.media.subscribers[record.AgentSessionID] == 0 {
		return fail()
	}
	if len(s.media.registrations) >= 4096 || s.media.ordinals[turnKey] >= 12 {
		return fail()
	}
	current, err := s.store.Get(record.AgentSessionID)
	if err != nil || current.ActiveTurnID != request.TurnId || ctx.Err() != nil {
		return fail()
	}
	id, err := newUUID()
	if err != nil {
		return fail()
	}
	if !wire.LegacyMediaAllowed(kind, mime) {
		s.media.orderMu.Lock()
		s.media.extended[id] = true
		s.media.orderMu.Unlock()
	}
	ordinal := s.media.ordinals[turnKey]
	s.media.ordinals[turnKey]++
	payload := EventPayload{ArtifactID: id, Kind: kind, Provenance: "tool", Status: "in_progress", Ordinal: &ordinal, DisplayName: input.DisplayName}
	if err = s.publishMedia(record, Event{TurnID: request.TurnId, ItemID: "artifact-" + id, EventType: EventItemArtifactStarted, Payload: payload}); err != nil {
		return fail()
	}
	if err = s.artifacts.Put(artifact.Manifest{SessionID: record.AgentSessionID, ArtifactID: id, Kind: kind, DisplayName: input.DisplayName, MediaType: mime, Content: content}); err != nil {
		retry := false
		payload.Status = "failed"
		payload.ErrorCode = artifactFailureCode(err)
		payload.Retryable = &retry
		_ = s.publishMedia(record, Event{TurnID: request.TurnId, ItemID: "artifact-" + id, EventType: EventItemArtifactFailed, Payload: payload})
		return fail()
	}
	size := int64(len(content))
	payload.Status = "ready"
	payload.MediaType = mime
	payload.SizeBytes = &size
	payload.SHA256 = sum
	payload.ContentHref = "/v3/agent-sessions/" + record.AgentSessionID + "/artifacts/" + id + "/content"
	if err = s.publishMedia(record, Event{TurnID: request.TurnId, ItemID: "artifact-" + id, EventType: EventItemArtifactCompleted, Payload: payload}); err != nil {
		return fail()
	}
	receipt := wire.PublishReceipt{ArtifactId: id, Status: "staged"}
	s.media.registrations[key] = mediaRegistration{string(manifestDigest), receipt}
	return receipt, nil
}
func artifactMediaType(name string) (string, string) {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".htm":
		return "file", "text/html"
	case ".svg":
		return "image", "image/svg+xml"
	case ".mp4":
		return "video", "video/mp4"
	case ".png":
		return "image", "image/png"
	case ".jpg", ".jpeg":
		return "image", "image/jpeg"
	case ".webp":
		return "image", "image/webp"
	case ".md", ".markdown":
		return "file", "text/markdown"
	case ".docx":
		return "file", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".pptx":
		return "file", "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".txt":
		return "file", "text/plain"
	case ".csv":
		return "file", "text/csv"
	case ".json":
		return "file", "application/json"
	case ".pdf":
		return "file", "application/pdf"
	case ".xlsx":
		return "file", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	return "", ""
}
