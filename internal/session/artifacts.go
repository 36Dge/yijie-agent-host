package session

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
)

const SyntheticArtifactManifest = "feat128-artifact-v1"

const (
	syntheticVideoRawSize   = 1642
	syntheticVideoRawSHA256 = "96ea070cac612d17927939c22f3c0c593fb26b171f62c4e9cee43fb596177dd5"
)

// syntheticVideoBase64 is a derived consumer snapshot of the immutable
// yijie-contracts FEAT-128 canonical resource. scripts/check-contracts.sh
// verifies it byte-for-byte against the pinned source before builds/tests.
//
//go:embed fixtures/synthetic-video-16x16.mp4.base64
var syntheticVideoBase64 string

type ArtifactResource = artifact.Resource
type ArtifactResourceKind = artifact.ResourceKind
type ArtifactAcknowledgement = artifact.Acknowledgement
type ArtifactReceipt = artifact.Receipt

const (
	ArtifactContent = artifact.Content
	ArtifactPoster  = artifact.Poster
)

func (s *Service) ReadArtifact(sessionID, artifactID string, kind ArtifactResourceKind) (ArtifactResource, error) {
	if s.artifacts == nil {
		return ArtifactResource{}, artifact.ErrNotFound
	}
	return s.artifacts.Get(sessionID, artifactID, kind)
}

func (s *Service) AcknowledgeArtifact(sessionID, artifactID string, request ArtifactAcknowledgement) (ArtifactReceipt, error) {
	if s.artifacts == nil {
		return ArtifactReceipt{}, artifact.ErrNotFound
	}
	return s.artifacts.Acknowledge(sessionID, artifactID, request)
}

type syntheticArtifact struct {
	kind        string
	displayName string
	mediaType   string
	content     []byte
	posterName  string
	posterType  string
	poster      []byte
}

func (s *Service) publishSyntheticArtifacts(sessionID, turnID string) error {
	if s.eventsV3 == nil || s.artifacts == nil {
		return ErrSessionNotUsable
	}
	record, err := s.store.Get(sessionID)
	if err != nil {
		return err
	}
	fixtures, err := deterministicArtifacts()
	if err != nil {
		return err
	}
	for ordinal, fixture := range fixtures {
		artifactID, err := newUUID()
		if err != nil {
			return err
		}
		itemID := "synthetic-artifact-" + artifactID
		ordinalValue := ordinal
		if err := s.publishV3(record, Event{
			TurnID: turnID, ItemID: itemID, EventType: EventItemArtifactStarted,
			Payload: EventPayload{ArtifactID: artifactID, Kind: fixture.kind, Provenance: "synthetic", Status: "in_progress", Ordinal: &ordinalValue, DisplayName: fixture.displayName},
		}); err != nil {
			return err
		}
		progress := 50
		if err := s.publishV3(record, Event{
			TurnID: turnID, ItemID: itemID, EventType: EventItemArtifactProgress,
			Payload: EventPayload{ArtifactID: artifactID, Kind: fixture.kind, Provenance: "synthetic", Status: "in_progress", Ordinal: &ordinalValue, Stage: "generating", Progress: &progress},
		}); err != nil {
			return err
		}
		if err := s.artifacts.Put(artifact.Manifest{
			SessionID: sessionID, ArtifactID: artifactID, Kind: fixture.kind, DisplayName: fixture.displayName,
			MediaType: fixture.mediaType, Content: fixture.content, PosterName: fixture.posterName,
			PosterType: fixture.posterType, Poster: fixture.poster,
		}); err != nil {
			retryable := false
			return s.publishV3(record, Event{
				TurnID: turnID, ItemID: itemID, EventType: EventItemArtifactFailed,
				Payload: EventPayload{ArtifactID: artifactID, Kind: fixture.kind, Provenance: "synthetic", Status: "failed", Ordinal: &ordinalValue, ErrorCode: artifactFailureCode(err), Retryable: &retryable, Message: stringPointer("synthetic artifact staging failed")},
			})
		}
		digest := sha256.Sum256(fixture.content)
		size := int64(len(fixture.content))
		baseHref := fmt.Sprintf("/v3/agent-sessions/%s/artifacts/%s", sessionID, artifactID)
		payload := EventPayload{
			ArtifactID: artifactID, Kind: fixture.kind, Provenance: "synthetic", Status: "ready",
			Ordinal: &ordinalValue, DisplayName: fixture.displayName, MediaType: fixture.mediaType,
			SizeBytes: &size, SHA256: fmt.Sprintf("%x", digest), ContentHref: baseHref + "/content",
		}
		if len(fixture.poster) > 0 {
			payload.PosterHref = baseHref + "/poster"
		}
		if err := s.publishV3(record, Event{TurnID: turnID, ItemID: itemID, EventType: EventItemArtifactCompleted, Payload: payload}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) publishV3(record Record, event Event) error {
	if s.eventsV3 == nil {
		return ErrSessionNotUsable
	}
	decorateEvent(record, &event)
	_, err := s.eventsV3.Publish(event)
	return err
}

func artifactFailureCode(err error) string {
	if err == artifact.ErrLimitExceeded {
		return "limit_exceeded"
	}
	return "resource_unavailable"
}

func deterministicArtifacts() ([]syntheticArtifact, error) {
	imageBytes, err := syntheticPNG()
	if err != nil {
		return nil, err
	}
	videoBytes, err := syntheticMP4()
	if err != nil {
		return nil, err
	}
	reportBytes, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"title":          "Synthetic local report",
		"generated_at":   "2026-08-20T02:00:00Z",
		"sections": []any{
			map[string]any{"id": "overview", "type": "summary", "required": true, "payload": map[string]any{"heading": "Overview", "text": "Deterministic local-only data."}},
			map[string]any{"id": "totals", "type": "metrics", "required": false, "payload": map[string]any{"items": []any{map[string]any{"label": "Artifacts", "value": 4, "unit": "count"}}}},
		},
	})
	if err != nil {
		return nil, err
	}
	return []syntheticArtifact{
		{kind: "image", displayName: "synthetic-preview.png", mediaType: "image/png", content: imageBytes},
		{kind: "video", displayName: "synthetic-clip.mp4", mediaType: "video/mp4", content: videoBytes, posterName: "synthetic-poster.png", posterType: "image/png", poster: imageBytes},
		{kind: "file", displayName: "synthetic-data.csv", mediaType: "text/csv", content: []byte("name,value\nlocal,4\n")},
		{kind: "report", displayName: "synthetic-report.json", mediaType: "application/vnd.yijie.report+json;version=1", content: reportBytes},
	}, nil
}

func syntheticPNG() ([]byte, error) {
	value := image.NewRGBA(image.Rect(0, 0, 1, 1))
	value.Set(0, 0, color.RGBA{R: 0x36, G: 0x8d, B: 0x7e, A: 0xff})
	var output bytes.Buffer
	if err := png.Encode(&output, value); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func syntheticMP4() ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(syntheticVideoBase64))
	if err != nil {
		return nil, fmt.Errorf("decode canonical synthetic video: %w", err)
	}
	digest := sha256.Sum256(decoded)
	if len(decoded) != syntheticVideoRawSize || fmt.Sprintf("%x", digest) != syntheticVideoRawSHA256 {
		return nil, errors.New("canonical synthetic video snapshot failed integrity verification")
	}
	return decoded, nil
}
