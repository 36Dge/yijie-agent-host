package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

type multimodalTestRuntime struct {
	fakeRuntime
	startV2 func(string, []codex.UserInput, string) (codex.TurnInfo, error)
}

func (runtime *multimodalTestRuntime) StartTurnV2(
	_ context.Context,
	threadID string,
	inputs []codex.UserInput,
	effort string,
) (codex.TurnInfo, error) {
	return runtime.startV2(threadID, inputs, effort)
}

func TestServiceStartsOrderedMultimodalTurnWithoutPersistingAttachmentContent(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reserveBoundSession(t, store)

	var logs bytes.Buffer
	runtimeCalls := 0
	runtime := &multimodalTestRuntime{startV2: func(threadID string, inputs []codex.UserInput, effort string) (codex.TurnInfo, error) {
		runtimeCalls++
		if threadID != testThreadID || len(inputs) != 3 || effort != "high" {
			t.Fatalf("unexpected Runtime mapping: thread=%q inputs=%d effort=%q", threadID, len(inputs), effort)
		}
		return codex.TurnInfo{ID: testTurnID, Status: "inProgress"}, nil
	}}
	service := NewService(runtime, store, NewEventHub(16, 8), slog.New(slog.NewTextHandler(&logs, nil)))
	blocks := validMultimodalBlocks()
	input := StartTurnV2Input{
		AgentSessionID:  testSessionID,
		OperationID:     testTurnOperationID,
		ContentBlocks:   blocks,
		ReasoningEffort: "high",
		Trace:           TraceContext{TraceID: "trace-v2", RequestID: "request-v2"},
	}
	turn, err := service.StartTurnV2(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if turn.ID != testTurnID {
		t.Fatalf("unexpected turn: %+v", turn)
	}
	record, err := store.Get(testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != StateActive || record.ActiveTurnID != testTurnID {
		t.Fatalf("turn was not bound: %+v", record)
	}
	input.Trace = TraceContext{TraceID: "replay-trace", RequestID: "replay-request"}
	replayed, err := service.StartTurnV2(context.Background(), input)
	if err != nil || replayed.ID != testTurnID || runtimeCalls != 1 {
		t.Fatalf("accepted turn was not replayed exactly: turn=%+v err=%v", replayed, err)
	}
	changed := input
	changed.ContentBlocks = validMultimodalBlocks()
	changed.ContentBlocks[0].Text = "different canonical input"
	if _, err := service.StartTurnV2(context.Background(), changed); !errors.Is(err, ErrTurnOperationConflict) || runtimeCalls != 1 {
		t.Fatalf("operation id reuse with changed input did not conflict: %v", err)
	}

	fileContext := formatFileContext(*blocks[2].File)
	if !strings.Contains(fileContext, "ATTACHMENT-CONTEXT-CANARY") ||
		!strings.Contains(fileContext, "\"name\":\"report.txt\"") ||
		strings.Contains(fileContext, "/private/") {
		t.Fatalf("file context mapping is unsafe or incomplete: %q", fileContext)
	}
	if err := store.db.Sync(); err != nil {
		t.Fatal(err)
	}
	database, err := os.ReadFile(filepath.Join(home, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"ATTACHMENT-CONTEXT-CANARY", blocks[1].Image.DataURL, "report.txt"} {
		if bytes.Contains(database, []byte(forbidden)) {
			t.Fatalf("attachment content entered Host persistence: %q", forbidden)
		}
		if strings.Contains(logs.String(), forbidden) {
			t.Fatalf("attachment content entered Host logs: %q", forbidden)
		}
	}
}

func TestServiceTurnV2AcceptedReplaySurvivesRestartWithKeyedDigest(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	reserveBoundSession(t, store)
	runtimeCalls := 0
	runtime := &multimodalTestRuntime{startV2: func(string, []codex.UserInput, string) (codex.TurnInfo, error) {
		runtimeCalls++
		return codex.TurnInfo{ID: testTurnID, Status: "inProgress"}, nil
	}}
	input := StartTurnV2Input{
		AgentSessionID: testSessionID, OperationID: testTurnOperationID, ContentBlocks: validMultimodalBlocks(),
	}
	service := NewService(runtime, store, NewEventHub(16, 8), nil)
	if _, err := service.StartTurnV2(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restartedService := NewService(runtime, reopened, NewEventHub(16, 8), nil)
	replayed, err := restartedService.StartTurnV2(context.Background(), input)
	if err != nil || replayed.ID != testTurnID || runtimeCalls != 1 {
		t.Fatalf("accepted replay after restart mismatch: turn=%+v calls=%d err=%v", replayed, runtimeCalls, err)
	}
}

func TestTurnV2ValidationRejectsCrossBlockAndContentMismatches(t *testing.T) {
	t.Run("unknown block", func(t *testing.T) {
		blocks := validMultimodalBlocks()
		blocks[0].Type = "audio"
		assertInvalidTurnV2Blocks(t, blocks)
	})
	t.Run("image digest mismatch", func(t *testing.T) {
		blocks := validMultimodalBlocks()
		blocks[1].Image.SHA256 = strings.Repeat("0", 64)
		assertInvalidTurnV2Blocks(t, blocks)
	})
	t.Run("image magic mismatch", func(t *testing.T) {
		blocks := validMultimodalBlocks()
		content := encodedTestImage("image/gif")
		digest := sha256.Sum256(content)
		blocks[1].Image.SizeBytes = int64(len(content))
		blocks[1].Image.SHA256 = hex.EncodeToString(digest[:])
		blocks[1].Image.DataURL = "data:image/png;base64," + base64.StdEncoding.EncodeToString(content)
		assertInvalidTurnV2Blocks(t, blocks)
	})
	t.Run("unsafe file name", func(t *testing.T) {
		blocks := validMultimodalBlocks()
		blocks[2].File.Name = "/private/report.txt"
		assertInvalidTurnV2Blocks(t, blocks)
	})
	t.Run("non NFC file name", func(t *testing.T) {
		blocks := validMultimodalBlocks()
		blocks[2].File.Name = "e\u0301.txt"
		assertInvalidTurnV2Blocks(t, blocks)
	})
	t.Run("blank file chunk", func(t *testing.T) {
		blocks := validMultimodalBlocks()
		blocks[2].File.ContextChunks = []string{" \n\t"}
		assertInvalidTurnV2Blocks(t, blocks)
	})
	t.Run("legacy doc media type", func(t *testing.T) {
		blocks := validMultimodalBlocks()
		blocks[2].File.MediaType = "application/msword"
		assertInvalidTurnV2Blocks(t, blocks)
	})
	t.Run("too many attachments", func(t *testing.T) {
		file := *validMultimodalBlocks()[2].File
		blocks := make([]TurnContentBlock, 11)
		for index := range blocks {
			copy := file
			blocks[index] = TurnContentBlock{Type: ContentBlockFile, File: &copy}
		}
		assertInvalidTurnV2Blocks(t, blocks)
	})
	t.Run("aggregate image bytes", func(t *testing.T) {
		content := pngWithAncillaryPayload((5 << 20) + 1)
		image := imageBlockFor(content, "image/png")
		blocks := []TurnContentBlock{
			{Type: ContentBlockImage, Image: &image},
			{Type: ContentBlockImage, Image: &image},
		}
		assertInvalidTurnV2Blocks(t, blocks)
	})
	t.Run("aggregate file context bytes", func(t *testing.T) {
		fileOne := *validMultimodalBlocks()[2].File
		fileTwo := fileOne
		chunk := strings.Repeat("x", maxFileContextChunkRunes)
		fileOne.ContextChunks = make([]string, 8)
		fileTwo.ContextChunks = make([]string, 9)
		for index := range fileOne.ContextChunks {
			fileOne.ContextChunks[index] = chunk
		}
		for index := range fileTwo.ContextChunks[:8] {
			fileTwo.ContextChunks[index] = chunk
		}
		fileTwo.ContextChunks[8] = "x"
		assertInvalidTurnV2Blocks(t, []TurnContentBlock{
			{Type: ContentBlockFile, File: &fileOne},
			{Type: ContentBlockFile, File: &fileTwo},
		})
	})
}

func TestTurnV2RasterImageValidationRejectsTruncationAndOversizedDimensions(t *testing.T) {
	for _, mediaType := range []string{"image/jpeg", "image/png", "image/gif"} {
		mediaType := mediaType
		t.Run("valid "+mediaType, func(t *testing.T) {
			content := encodedTestImage(mediaType)
			block := imageBlockFor(content, mediaType)
			if _, err := validateImageBlock(block); err != nil {
				t.Fatalf("valid %s image was rejected: %v", mediaType, err)
			}
		})
		t.Run("truncated "+mediaType, func(t *testing.T) {
			content := truncatedAfterDecodableConfig(encodedTestImage(mediaType))
			block := imageBlockFor(content, mediaType)
			if _, err := validateImageBlock(block); !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("truncated %s image was accepted: %v", mediaType, err)
			}
		})
	}

	basePNG := encodedTestImage("image/png")
	pixelOverflowHeight := uint32(maxImageTotalPixels/int64(maxImageEdgePixels) + 1)
	tests := []struct {
		name   string
		width  uint32
		height uint32
	}{
		{name: "zero width", width: 0, height: 1},
		{name: "edge limit", width: maxImageEdgePixels + 1, height: 1},
		{name: "pixel limit", width: maxImageEdgePixels, height: pixelOverflowHeight},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			content := pngWithDeclaredDimensions(basePNG, test.width, test.height)
			block := imageBlockFor(content, "image/png")
			if _, err := validateImageBlock(block); !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("oversized image dimensions were accepted: %dx%d err=%v", test.width, test.height, err)
			}
		})
	}
}

func TestTurnV2WebPValidationChecksRIFFChunksHeadersAndDimensions(t *testing.T) {
	vp8 := []byte{0, 0, 0, 0x9d, 0x01, 0x2a, 1, 0, 1, 0}
	vp8l := []byte{0x2f, 0, 0, 0, 0}
	vp8x := make([]byte, 10)
	animatedVP8X := make([]byte, 10)
	animatedVP8X[0] = 0x02
	animationFrame := append(make([]byte, 16), webPChunkBytes(testWebPChunk{Type: "VP8L", Payload: vp8l})...)
	valid := []struct {
		name   string
		chunks []testWebPChunk
	}{
		{name: "vp8", chunks: []testWebPChunk{{Type: "VP8 ", Payload: vp8}}},
		{name: "vp8l", chunks: []testWebPChunk{{Type: "VP8L", Payload: vp8l}}},
		{name: "vp8x", chunks: []testWebPChunk{{Type: "VP8X", Payload: vp8x}, {Type: "VP8L", Payload: vp8l}}},
		{name: "animated vp8x", chunks: []testWebPChunk{
			{Type: "VP8X", Payload: animatedVP8X},
			{Type: "ANIM", Payload: make([]byte, 6)},
			{Type: "ANMF", Payload: animationFrame},
		}},
	}
	for _, test := range valid {
		t.Run("valid "+test.name, func(t *testing.T) {
			content := webPWithChunks(test.chunks...)
			block := imageBlockFor(content, "image/webp")
			if _, err := validateImageBlock(block); err != nil {
				t.Fatalf("valid %s WebP container was rejected: %v", test.name, err)
			}
		})
	}

	incompleteHeader := make([]byte, 16)
	copy(incompleteHeader[:4], "RIFF")
	binary.LittleEndian.PutUint32(incompleteHeader[4:8], uint32(len(incompleteHeader)-8))
	copy(incompleteHeader[8:12], "WEBP")
	copy(incompleteHeader[12:16], "VP8L")

	missingPadding := webPWithChunks(testWebPChunk{Type: "VP8L", Payload: vp8l})
	missingPadding = append([]byte(nil), missingPadding[:len(missingPadding)-1]...)
	binary.LittleEndian.PutUint32(missingPadding[4:8], uint32(len(missingPadding)-8))

	nonzeroPadding := webPWithChunks(testWebPChunk{Type: "VP8L", Payload: vp8l})
	nonzeroPadding[len(nonzeroPadding)-1] = 1

	trailingPartialChunk := append(webPWithChunks(testWebPChunk{Type: "VP8L", Payload: vp8l}), []byte("EXIF")...)
	binary.LittleEndian.PutUint32(trailingPartialChunk[4:8], uint32(len(trailingPartialChunk)-8))

	oversizedVP8X := make([]byte, 10)
	storedWidth := uint32(maxImageEdgePixels)
	oversizedVP8X[4] = byte(storedWidth)
	oversizedVP8X[5] = byte(storedWidth >> 8)
	oversizedVP8X[6] = byte(storedWidth >> 16)

	invalid := []struct {
		name    string
		content []byte
	}{
		{name: "incomplete chunk header", content: incompleteHeader},
		{name: "vp8x without image payload", content: webPWithChunks(testWebPChunk{Type: "VP8X", Payload: vp8x})},
		{name: "missing odd payload padding", content: missingPadding},
		{name: "nonzero payload padding", content: nonzeroPadding},
		{name: "trailing partial chunk", content: trailingPartialChunk},
		{name: "invalid vp8 sync code", content: webPWithChunks(testWebPChunk{Type: "VP8 ", Payload: make([]byte, 10)})},
		{name: "invalid vp8l signature", content: webPWithChunks(testWebPChunk{Type: "VP8L", Payload: make([]byte, 5)})},
		{name: "invalid vp8x reserved bit", content: webPWithChunks(testWebPChunk{Type: "VP8X", Payload: []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0}})},
		{name: "oversized vp8x canvas", content: webPWithChunks(testWebPChunk{Type: "VP8X", Payload: oversizedVP8X})},
		{name: "animated frame without anim header", content: webPWithChunks(
			testWebPChunk{Type: "VP8X", Payload: animatedVP8X},
			testWebPChunk{Type: "ANMF", Payload: animationFrame},
		)},
		{name: "animated frame without image payload", content: webPWithChunks(
			testWebPChunk{Type: "VP8X", Payload: animatedVP8X},
			testWebPChunk{Type: "ANIM", Payload: make([]byte, 6)},
			testWebPChunk{Type: "ANMF", Payload: make([]byte, 16)},
		)},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			block := imageBlockFor(test.content, "image/webp")
			if _, err := validateImageBlock(block); !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("invalid WebP was accepted: %v", err)
			}
		})
	}
}

func TestTurnV2InputDigestNormalizesEffortAndCommitsOrderAndContent(t *testing.T) {
	var key [sha256.Size]byte
	copy(key[:], []byte("synthetic-turn-digest-key-one"))
	baseBlocks := validMultimodalBlocks()
	base, err := turnV2InputDigest(key, baseBlocks, "")
	if err != nil {
		t.Fatal(err)
	}
	none, err := turnV2InputDigest(key, validMultimodalBlocks(), "none")
	if err != nil || base != none {
		t.Fatalf("empty and none reasoning effort produced different digests: empty=%q none=%q err=%v", base, none, err)
	}

	high, err := turnV2InputDigest(key, validMultimodalBlocks(), "high")
	if err != nil || high == base {
		t.Fatalf("reasoning effort was not committed by digest: high=%q err=%v", high, err)
	}
	ordered := validMultimodalBlocks()
	ordered[0], ordered[1] = ordered[1], ordered[0]
	reordered, err := turnV2InputDigest(key, ordered, "none")
	if err != nil || reordered == base {
		t.Fatalf("block order was not committed by digest: reordered=%q err=%v", reordered, err)
	}
	changed := validMultimodalBlocks()
	changed[2].File.ContextChunks[0] = "different bounded context"
	changedContent, err := turnV2InputDigest(key, changed, "none")
	if err != nil || changedContent == base {
		t.Fatalf("block content was not committed by digest: changed=%q err=%v", changedContent, err)
	}
	otherKey := key
	otherKey[0] ^= 0xff
	otherHostDigest, err := turnV2InputDigest(otherKey, validMultimodalBlocks(), "none")
	if err != nil || otherHostDigest == base {
		t.Fatalf("digest was not keyed by the owner-only Host secret: other=%q err=%v", otherHostDigest, err)
	}
}

func TestServiceTurnV2ConcurrentDuplicateInvokesRuntimeOnce(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reserveBoundSession(t, store)

	entered := make(chan struct{})
	release := make(chan struct{})
	runtimeCalls := 0
	runtime := &multimodalTestRuntime{startV2: func(string, []codex.UserInput, string) (codex.TurnInfo, error) {
		runtimeCalls++
		close(entered)
		<-release
		return codex.TurnInfo{ID: testTurnID, Status: "inProgress"}, nil
	}}
	service := NewService(runtime, store, NewEventHub(16, 8), nil)
	input := StartTurnV2Input{
		AgentSessionID: testSessionID, OperationID: testTurnOperationID, ContentBlocks: validMultimodalBlocks(),
	}
	firstResult := make(chan error, 1)
	go func() {
		_, err := service.StartTurnV2(context.Background(), input)
		firstResult <- err
	}()
	<-entered
	if _, err := service.StartTurnV2(context.Background(), input); !errors.Is(err, ErrSessionNotUsable) {
		t.Fatalf("concurrent duplicate was not rejected while the result was pending: %v", err)
	}
	close(release)
	if err := <-firstResult; err != nil {
		t.Fatalf("first operation failed: %v", err)
	}
	if runtimeCalls != 1 {
		t.Fatalf("concurrent duplicate invoked Runtime %d times", runtimeCalls)
	}
}

func TestServiceTurnV2UnknownOutcomeFailsClosedUntilExplicitResume(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	reserveBoundSession(t, store)

	calls := 0
	runtime := &multimodalTestRuntime{}
	runtime.resume = func(threadID string) (codex.ThreadInfo, error) {
		return codex.ThreadInfo{
			ID: threadID, RuntimeSession: "runtime-session-resumed",
			Model: codex.MiniMaxModel, ModelProvider: codex.MiniMaxProviderID,
		}, nil
	}
	runtime.startV2 = func(string, []codex.UserInput, string) (codex.TurnInfo, error) {
		calls++
		if calls == 1 {
			return codex.TurnInfo{}, errors.New("synthetic lost response")
		}
		return codex.TurnInfo{ID: testTurnID, Status: "inProgress"}, nil
	}
	service := NewService(runtime, store, NewEventHub(16, 8), nil)
	input := StartTurnV2Input{
		AgentSessionID: testSessionID, OperationID: testTurnOperationID, ContentBlocks: validMultimodalBlocks(),
	}
	if _, err := service.StartTurnV2(context.Background(), input); !errors.Is(err, ErrRuntimeRequest) {
		t.Fatalf("unknown Runtime outcome was not surfaced: %v", err)
	}
	record, err := store.Get(testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != StateFailed || record.FailureCode != "turn_start_failed" {
		t.Fatalf("unknown outcome did not fail closed: %+v", record)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	service = NewService(runtime, reopened, NewEventHub(16, 8), nil)
	if _, err := service.StartTurnV2(context.Background(), input); !errors.Is(err, ErrSessionNotUsable) || calls != 1 {
		t.Fatalf("turn was automatically retried after restart: calls=%d err=%v", calls, err)
	}
	if _, err := service.ResumeSession(context.Background(), testSessionID, TraceContext{TraceID: "resync"}); err != nil {
		t.Fatalf("explicit resync failed: %v", err)
	}
	if _, err := service.StartTurnV2(context.Background(), input); !errors.Is(err, ErrSessionNotUsable) || calls != 1 {
		t.Fatalf("uncertain operation replayed after explicit resync: calls=%d err=%v", calls, err)
	}
	input.OperationID = testTurnOperationID2
	if _, err := service.StartTurnV2(context.Background(), input); err != nil || calls != 2 {
		t.Fatalf("new operation did not recover after explicit resync: calls=%d err=%v", calls, err)
	}
}

func reserveBoundSession(t *testing.T, store *Store) {
	t.Helper()
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(
		testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID,
	); err != nil {
		t.Fatal(err)
	}
}

func validMultimodalBlocks() []TurnContentBlock {
	png := encodedTestImage("image/png")
	image := imageBlockFor(png, "image/png")
	return []TurnContentBlock{
		{Type: ContentBlockText, Text: "inspect the attachment"},
		{Type: ContentBlockImage, Image: &image},
		{Type: ContentBlockFile, File: &TurnFileBlock{
			AttachmentID:  "019c0123-4567-7abc-8123-456789abcdf1",
			Name:          "report.txt",
			MediaType:     "text/plain",
			SizeBytes:     42,
			SHA256:        strings.Repeat("a", 64),
			ContextChunks: []string{"ATTACHMENT-CONTEXT-CANARY"},
		}},
	}
}

type testWebPChunk struct {
	Type    string
	Payload []byte
}

func encodedTestImage(mediaType string) []byte {
	source := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	var output bytes.Buffer
	var err error
	switch mediaType {
	case "image/gif":
		err = gif.Encode(&output, source, nil)
	case "image/jpeg":
		err = jpeg.Encode(&output, source, nil)
	case "image/png":
		err = png.Encode(&output, source)
	default:
		panic("unsupported test image media type: " + mediaType)
	}
	if err != nil {
		panic(err)
	}
	return output.Bytes()
}

func truncatedAfterDecodableConfig(content []byte) []byte {
	for end := len(content) - 1; end > 0; end-- {
		candidate := content[:end]
		if _, _, err := image.DecodeConfig(bytes.NewReader(candidate)); err != nil {
			continue
		}
		if _, _, err := image.Decode(bytes.NewReader(candidate)); err != nil {
			return append([]byte(nil), candidate...)
		}
	}
	panic("test image has no truncation that preserves a decodable config")
}

func pngWithDeclaredDimensions(content []byte, width, height uint32) []byte {
	if len(content) < 33 || string(content[:8]) != "\x89PNG\r\n\x1a\n" || string(content[12:16]) != "IHDR" {
		panic("test PNG does not start with IHDR")
	}
	mutated := append([]byte(nil), content...)
	binary.BigEndian.PutUint32(mutated[16:20], width)
	binary.BigEndian.PutUint32(mutated[20:24], height)
	binary.BigEndian.PutUint32(mutated[29:33], crc32.ChecksumIEEE(mutated[12:29]))
	return mutated
}

func pngWithAncillaryPayload(payloadSize int) []byte {
	base := encodedTestImage("image/png")
	if len(base) < 12 || string(base[len(base)-8:len(base)-4]) != "IEND" {
		panic("test PNG does not end with IEND")
	}
	chunk := make([]byte, payloadSize+12)
	binary.BigEndian.PutUint32(chunk[:4], uint32(payloadSize))
	copy(chunk[4:8], "ruSt")
	binary.BigEndian.PutUint32(chunk[8+payloadSize:], crc32.ChecksumIEEE(chunk[4:8+payloadSize]))
	content := make([]byte, 0, len(base)+len(chunk))
	content = append(content, base[:len(base)-12]...)
	content = append(content, chunk...)
	return append(content, base[len(base)-12:]...)
}

func webPWithChunks(chunks ...testWebPChunk) []byte {
	body := []byte("WEBP")
	for _, chunk := range chunks {
		body = append(body, webPChunkBytes(chunk)...)
	}
	content := make([]byte, 8, 8+len(body))
	copy(content[:4], "RIFF")
	binary.LittleEndian.PutUint32(content[4:8], uint32(len(body)))
	return append(content, body...)
}

func webPChunkBytes(chunk testWebPChunk) []byte {
	if len(chunk.Type) != 4 {
		panic("WebP test chunk type must contain four bytes")
	}
	content := make([]byte, 8, 8+len(chunk.Payload)+1)
	copy(content[:4], chunk.Type)
	binary.LittleEndian.PutUint32(content[4:8], uint32(len(chunk.Payload)))
	content = append(content, chunk.Payload...)
	if len(chunk.Payload)%2 != 0 {
		content = append(content, 0)
	}
	return content
}

func imageBlockFor(content []byte, mediaType string) TurnImageBlock {
	digest := sha256.Sum256(content)
	return TurnImageBlock{
		AttachmentID: "019c0123-4567-7abc-8123-456789abcdf0",
		MediaType:    mediaType,
		SizeBytes:    int64(len(content)),
		SHA256:       hex.EncodeToString(digest[:]),
		DataURL:      "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(content),
	}
}

func assertInvalidTurnV2Blocks(t *testing.T, blocks []TurnContentBlock) {
	t.Helper()
	if _, err := validateAndMapTurnV2Blocks(blocks); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid blocks were accepted: %v", err)
	}
}
