package session

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/google/uuid"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	canonicalSyntheticVideoSize   = 1642
	canonicalSyntheticVideoSHA256 = "96ea070cac612d17927939c22f3c0c593fb26b171f62c4e9cee43fb596177dd5"
)

func TestSyntheticVideoFixtureCanonicalConformance(t *testing.T) {
	video, err := syntheticMP4()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("canonical identity", func(t *testing.T) {
		digest := sha256.Sum256(video)
		if len(video) != canonicalSyntheticVideoSize {
			t.Errorf("synthetic video size = %d, want %d", len(video), canonicalSyntheticVideoSize)
		}
		if got := fmt.Sprintf("%x", digest); got != canonicalSyntheticVideoSHA256 {
			t.Errorf("synthetic video SHA-256 = %s, want %s", got, canonicalSyntheticVideoSHA256)
		}
	})

	t.Run("playback and seek structure", func(t *testing.T) {
		metadata, err := inspectSyntheticVideo(video)
		if err != nil {
			t.Fatal(err)
		}
		if !metadata.frontMoov {
			t.Error("synthetic video does not place moov before media data")
		}
		if !metadata.avc1 || !metadata.avcC || metadata.profile != 100 {
			t.Errorf("synthetic video codec = avc1:%t avcC:%t profile:%d, want H.264 High", metadata.avc1, metadata.avcC, metadata.profile)
		}
		if metadata.width != 16 || metadata.height != 16 {
			t.Errorf("synthetic video dimensions = %dx%d, want 16x16", metadata.width, metadata.height)
		}
		if metadata.timescale != 1000 || metadata.duration != 120 {
			t.Errorf("synthetic video duration = %d/%d seconds, want 120/1000", metadata.duration, metadata.timescale)
		}
		if metadata.samples != 3 {
			t.Errorf("synthetic video samples = %d, want 3", metadata.samples)
		}
		if metadata.firstKeyframe != 1 {
			t.Errorf("synthetic video first keyframe = %d, want sample 1", metadata.firstKeyframe)
		}
	})
}

type syntheticVideoMetadata struct {
	frontMoov     bool
	avc1          bool
	avcC          bool
	profile       byte
	width         uint16
	height        uint16
	timescale     uint32
	duration      uint32
	samples       uint32
	firstKeyframe uint32
}

type boundedMP4Box struct {
	kind    string
	payload []byte
}

func inspectSyntheticVideo(data []byte) (syntheticVideoMetadata, error) {
	top, err := parseBoundedMP4Boxes(data)
	if err != nil {
		return syntheticVideoMetadata{}, err
	}
	if len(top) < 2 || top[0].kind != "ftyp" {
		return syntheticVideoMetadata{}, errors.New("synthetic video is missing the leading ftyp box")
	}
	metadata := syntheticVideoMetadata{frontMoov: top[1].kind == "moov"}
	moov, ok := findBoundedMP4Box(top, "moov")
	if !ok {
		return metadata, errors.New("synthetic video is missing the moov box")
	}
	moovChildren, err := parseBoundedMP4Boxes(moov.payload)
	if err != nil {
		return metadata, fmt.Errorf("parse moov: %w", err)
	}
	mvhd, ok := findBoundedMP4Box(moovChildren, "mvhd")
	if !ok || len(mvhd.payload) < 20 || mvhd.payload[0] != 0 {
		return metadata, errors.New("synthetic video is missing a bounded version-0 mvhd box")
	}
	metadata.timescale = binary.BigEndian.Uint32(mvhd.payload[12:16])
	metadata.duration = binary.BigEndian.Uint32(mvhd.payload[16:20])

	trak, ok := findBoundedMP4Box(moovChildren, "trak")
	if !ok {
		return metadata, errors.New("synthetic video is missing a video track")
	}
	trakChildren, err := parseBoundedMP4Boxes(trak.payload)
	if err != nil {
		return metadata, fmt.Errorf("parse trak: %w", err)
	}
	mdia, ok := findBoundedMP4Box(trakChildren, "mdia")
	if !ok {
		return metadata, errors.New("synthetic video track is missing mdia")
	}
	mdiaChildren, err := parseBoundedMP4Boxes(mdia.payload)
	if err != nil {
		return metadata, fmt.Errorf("parse mdia: %w", err)
	}
	minf, ok := findBoundedMP4Box(mdiaChildren, "minf")
	if !ok {
		return metadata, errors.New("synthetic video track is missing minf")
	}
	minfChildren, err := parseBoundedMP4Boxes(minf.payload)
	if err != nil {
		return metadata, fmt.Errorf("parse minf: %w", err)
	}
	stbl, ok := findBoundedMP4Box(minfChildren, "stbl")
	if !ok {
		return metadata, errors.New("synthetic video track is missing stbl")
	}
	stblChildren, err := parseBoundedMP4Boxes(stbl.payload)
	if err != nil {
		return metadata, fmt.Errorf("parse stbl: %w", err)
	}
	stsd, ok := findBoundedMP4Box(stblChildren, "stsd")
	if !ok || len(stsd.payload) < 8 || binary.BigEndian.Uint32(stsd.payload[4:8]) != 1 {
		return metadata, errors.New("synthetic video is missing its single sample description")
	}
	sampleEntries, err := parseBoundedMP4Boxes(stsd.payload[8:])
	if err != nil {
		return metadata, fmt.Errorf("parse stsd entries: %w", err)
	}
	avc1, ok := findBoundedMP4Box(sampleEntries, "avc1")
	if !ok || len(avc1.payload) < 78 {
		return metadata, errors.New("synthetic video is missing a bounded avc1 sample entry")
	}
	metadata.avc1 = true
	metadata.width = binary.BigEndian.Uint16(avc1.payload[24:26])
	metadata.height = binary.BigEndian.Uint16(avc1.payload[26:28])
	avc1Children, err := parseBoundedMP4Boxes(avc1.payload[78:])
	if err != nil {
		return metadata, fmt.Errorf("parse avc1 extensions: %w", err)
	}
	avcC, ok := findBoundedMP4Box(avc1Children, "avcC")
	if !ok || len(avcC.payload) < 2 {
		return metadata, errors.New("synthetic video is missing avcC configuration")
	}
	metadata.avcC = true
	metadata.profile = avcC.payload[1]

	stsz, ok := findBoundedMP4Box(stblChildren, "stsz")
	if !ok || len(stsz.payload) < 12 {
		return metadata, errors.New("synthetic video is missing sample sizes")
	}
	metadata.samples = binary.BigEndian.Uint32(stsz.payload[8:12])
	stss, ok := findBoundedMP4Box(stblChildren, "stss")
	if !ok || len(stss.payload) < 12 || binary.BigEndian.Uint32(stss.payload[4:8]) < 1 {
		return metadata, errors.New("synthetic video is missing sync samples")
	}
	metadata.firstKeyframe = binary.BigEndian.Uint32(stss.payload[8:12])
	return metadata, nil
}

func parseBoundedMP4Boxes(data []byte) ([]boundedMP4Box, error) {
	const maximumBoxes = 128
	boxes := make([]boundedMP4Box, 0, 8)
	for offset := 0; offset < len(data); {
		if len(boxes) == maximumBoxes {
			return nil, errors.New("synthetic video exceeds the bounded box count")
		}
		if len(data)-offset < 8 {
			return nil, errors.New("synthetic video contains a truncated box header")
		}
		size := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		if size < 8 || size > len(data)-offset {
			return nil, errors.New("synthetic video contains an invalid box size")
		}
		boxes = append(boxes, boundedMP4Box{kind: string(data[offset+4 : offset+8]), payload: data[offset+8 : offset+size]})
		offset += size
	}
	return boxes, nil
}

func findBoundedMP4Box(boxes []boundedMP4Box, kind string) (boundedMP4Box, bool) {
	for _, box := range boxes {
		if box.kind == kind {
			return box, true
		}
	}
	return boundedMP4Box{}, false
}

func TestSyntheticArtifactsPublishCanonicalV3LifecycleAndResources(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
		t.Fatal(err)
	}
	spool, err := artifact.OpenStore(filepath.Join(home, "artifact-spool"), artifact.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = spool.Close() })
	v3 := NewEventHubVersion(EventSchemaVersionV3, 64, 16)
	service := NewService(&fakeRuntime{}, store, NewEventHub(16, 8), nil, WithV3Artifacts(v3, spool, true))
	if err := service.publishSyntheticArtifacts(testSessionID, testTurnID); err != nil {
		t.Fatal(err)
	}
	_, replay, _, cancel, err := service.SubscribeEventsV3(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != 12 {
		t.Fatalf("expected four three-event lifecycles, got %d", len(replay))
	}
	contract := compileAgentSessionEventV3Contract(t)
	seenKinds := make(map[string]bool)
	for index, event := range replay {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		if err := contract.Validate(instance); err != nil {
			t.Fatalf("v3 event %d violates contract: %v\n%s", index, err, encoded)
		}
		if event.SchemaVersion != 3 || event.Terminal {
			t.Fatalf("unexpected v3 lifecycle identity: %+v", event)
		}
		seenKinds[event.Payload.Kind] = true
		if event.EventType != EventItemArtifactCompleted {
			continue
		}
		resource, err := service.ReadArtifact(testSessionID, event.Payload.ArtifactID, ArtifactContent)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(resource.Bytes)
		if event.Payload.SizeBytes == nil || *event.Payload.SizeBytes != int64(len(resource.Bytes)) || event.Payload.SHA256 != bytesToHex(digest[:]) {
			t.Fatalf("completed manifest mismatch: %+v resource=%+v", event.Payload, resource)
		}
		if event.Payload.Kind == "video" {
			if _, err := service.ReadArtifact(testSessionID, event.Payload.ArtifactID, ArtifactPoster); err != nil {
				t.Fatalf("video poster unavailable: %v", err)
			}
		}
		if event.Payload.Kind == "report" {
			instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(resource.Bytes))
			if err != nil {
				t.Fatal(err)
			}
			if err := compileReportDocumentV1Contract(t).Validate(instance); err != nil {
				t.Fatalf("synthetic report violates ReportDocumentV1: %v\n%s", err, resource.Bytes)
			}
		}
	}
	for _, kind := range []string{"image", "video", "file", "report"} {
		if !seenKinds[kind] {
			t.Fatalf("synthetic lifecycle omitted %s", kind)
		}
	}
	if _, err := service.ReadArtifact("019fbd88-cbc3-7bf1-934d-7b05cd693f54", replay[2].Payload.ArtifactID, ArtifactContent); !errors.Is(err, artifact.ErrNotFound) {
		t.Fatalf("cross-session read leaked resource: %v", err)
	}
}

func TestArtifactAcknowledgementDelegatesToEncryptedStore(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
		t.Fatal(err)
	}
	spool, err := artifact.OpenStore(filepath.Join(home, "artifact-spool"), artifact.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = spool.Close() })
	service := NewService(&fakeRuntime{}, store, NewEventHub(8, 4), nil,
		WithV3Artifacts(NewEventHubVersion(EventSchemaVersionV3, 32, 8), spool, true))
	if err := service.publishSyntheticArtifacts(testSessionID, testTurnID); err != nil {
		t.Fatal(err)
	}
	_, replay, _, cancel, err := service.SubscribeEventsV3(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	completed := replay[2]
	receipt, err := service.AcknowledgeArtifact(testSessionID, completed.Payload.ArtifactID, ArtifactAcknowledgement{
		AckID: uuid.NewString(), SizeBytes: *completed.Payload.SizeBytes, SHA256: completed.Payload.SHA256,
		LocalCommittedAt: time.Now().UTC(),
	})
	if err != nil || receipt.CleanupStatus != "completed" {
		t.Fatalf("acknowledgement mismatch: %+v err=%v", receipt, err)
	}
	if _, err := service.ReadArtifact(testSessionID, completed.Payload.ArtifactID, ArtifactContent); !errors.Is(err, artifact.ErrExpired) {
		t.Fatalf("acknowledged content remained readable: %v", err)
	}
}

func TestV3PreservesV2ReasoningAndLifecycleVariants(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
		t.Fatal(err)
	}
	v3 := NewEventHubVersion(EventSchemaVersionV3, 32, 8)
	service := NewService(&fakeRuntime{}, store, NewEventHub(16, 8), nil, WithV3Artifacts(v3, nil, false))
	service.HandleNotification(RuntimeNotificationReasoningTextDelta, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID, "itemId": "reasoning-v3", "contentIndex": 0, "delta": "safe reasoning",
	}))
	service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{"id": "reasoning-v3", "type": "reasoning", "content": []string{"safe reasoning"}},
	}))
	_, replay, _, cancel, err := service.SubscribeEventsV3(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != 3 || replay[0].EventType != EventItemReasoningTextDelta || replay[1].EventType != EventItemReasoningFinalized || replay[2].EventType != EventItemCompleted {
		t.Fatalf("v3 did not preserve v2 reasoning lifecycle: %+v", replay)
	}
	contract := compileAgentSessionEventV3Contract(t)
	for _, event := range replay {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		if err := contract.Validate(instance); err != nil {
			t.Fatalf("preserved v3 event violates contract: %v\n%s", err, encoded)
		}
	}
}

func compileAgentSessionEventV3Contract(t *testing.T) *jsonschema.Schema {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate Agent session v3 contract test source")
	}
	contract, err := jsonschema.NewCompiler().Compile(filepath.Clean(filepath.Join(
		filepath.Dir(sourceFile), "..", "..", "api", "jsonschema", "agent-session-event-v3.schema.json",
	)))
	if err != nil {
		t.Fatalf("compile AgentSessionEventV3 JSON Schema: %v", err)
	}
	return contract
}

func compileReportDocumentV1Contract(t *testing.T) *jsonschema.Schema {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate report v1 contract test source")
	}
	contract, err := jsonschema.NewCompiler().Compile(filepath.Clean(filepath.Join(
		filepath.Dir(sourceFile), "..", "..", "api", "jsonschema", "report-document-v1.schema.json",
	)))
	if err != nil {
		t.Fatalf("compile ReportDocumentV1 JSON Schema: %v", err)
	}
	return contract
}

func bytesToHex(value []byte) string {
	const digits = "0123456789abcdef"
	encoded := make([]byte, len(value)*2)
	for index, item := range value {
		encoded[index*2] = digits[item>>4]
		encoded[index*2+1] = digits[item&0x0f]
	}
	return string(encoded)
}
