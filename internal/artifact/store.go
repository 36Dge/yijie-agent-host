package artifact

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	DefaultSessionLimit = int64(256 << 20)
	DefaultGlobalLimit  = int64(1 << 30)
	DefaultTTL          = 24 * time.Hour
	imageLimit          = int64(20 << 20)
	otherLimit          = int64(64 << 20)
)

var (
	ErrInvalid          = errors.New("artifact input is invalid")
	ErrNotFound         = errors.New("artifact not found")
	ErrExpired          = errors.New("artifact expired")
	ErrNotReady         = errors.New("artifact is not ready")
	ErrManifestMismatch = errors.New("artifact manifest mismatch")
	ErrAckConflict      = errors.New("artifact acknowledgement conflict")
	ErrLimitExceeded    = errors.New("artifact staging limit exceeded")
	ErrUnavailable      = errors.New("artifact resource unavailable")
)

type ResourceKind string

const (
	Content ResourceKind = "content"
	Poster  ResourceKind = "poster"
)

type Manifest struct {
	SessionID   string
	ArtifactID  string
	Kind        string
	DisplayName string
	MediaType   string
	Content     []byte
	PosterName  string
	PosterType  string
	Poster      []byte
	StagedAt    time.Time
}

type Resource struct {
	Bytes       []byte
	MediaType   string
	DisplayName string
	SHA256      string
}

type Acknowledgement struct {
	AckID            string
	SizeBytes        int64
	SHA256           string
	LocalCommittedAt time.Time
}

type Receipt struct {
	ArtifactID     string
	AckID          string
	CleanupStatus  string
	AcknowledgedAt time.Time
}

type Options struct {
	SessionLimit int64
	GlobalLimit  int64
	TTL          time.Duration
	Now          func() time.Time
}

type sealedResource struct {
	path        string
	mediaType   string
	displayName string
	size        int64
	sha256      string
}

type ackRecord struct {
	id          string
	requestHash [32]byte
	receipt     Receipt
}

type entry struct {
	sessionID  string
	artifactID string
	kind       string
	content    sealedResource
	poster     *sealedResource
	stagedAt   time.Time
	expired    bool
	ack        *ackRecord
}

type Store struct {
	mu           sync.Mutex
	root         string
	aead         cipher.AEAD
	sessionLimit int64
	globalLimit  int64
	ttl          time.Duration
	now          func() time.Time
	entries      map[string]*entry
	sessionBytes map[string]int64
	totalBytes   int64
	closed       bool
}

func OpenStore(root string, options Options) (*Store, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || filepath.Base(root) != "artifact-spool" || filepath.Dir(root) == root {
		return nil, fmt.Errorf("%w: spool root must be canonical and absolute", ErrInvalid)
	}
	if options.SessionLimit <= 0 {
		options.SessionLimit = DefaultSessionLimit
	}
	if options.GlobalLimit <= 0 {
		options.GlobalLimit = DefaultGlobalLimit
	}
	if options.TTL <= 0 {
		options.TTL = DefaultTTL
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if err := os.RemoveAll(root); err != nil {
		return nil, fmt.Errorf("clear stale artifact spool: %w", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create artifact spool: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("secure artifact spool: %w", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("create artifact spool key: %w", err)
	}
	block, err := aes.NewCipher(key)
	for index := range key {
		key[index] = 0
	}
	if err != nil {
		return nil, fmt.Errorf("create artifact cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create artifact AEAD: %w", err)
	}
	return &Store{
		root: root, aead: aead, sessionLimit: options.SessionLimit, globalLimit: options.GlobalLimit,
		ttl: options.TTL, now: options.Now, entries: make(map[string]*entry), sessionBytes: make(map[string]int64),
	}, nil
}

func (s *Store) Put(manifest Manifest) error {
	if err := validateManifest(manifest); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrUnavailable
	}
	s.cleanupExpiredLocked(s.now().UTC())
	key := artifactKey(manifest.SessionID, manifest.ArtifactID)
	if _, exists := s.entries[key]; exists {
		return ErrManifestMismatch
	}
	bytesRequired := int64(len(manifest.Content) + len(manifest.Poster))
	if s.sessionBytes[manifest.SessionID]+bytesRequired > s.sessionLimit || s.totalBytes+bytesRequired > s.globalLimit {
		return ErrLimitExceeded
	}
	directory := filepath.Join(s.root, manifest.SessionID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return ErrUnavailable
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return ErrUnavailable
	}
	content, err := s.writeSealed(directory, manifest.ArtifactID+".content.sealed", manifest.Content, manifest.MediaType, manifest.DisplayName)
	if err != nil {
		_ = os.RemoveAll(directory)
		return err
	}
	var poster *sealedResource
	if len(manifest.Poster) > 0 {
		value, writeErr := s.writeSealed(directory, manifest.ArtifactID+".poster.sealed", manifest.Poster, manifest.PosterType, manifest.PosterName)
		if writeErr != nil {
			_ = os.Remove(content.path)
			return writeErr
		}
		poster = &value
	}
	stagedAt := manifest.StagedAt.UTC()
	if stagedAt.IsZero() {
		stagedAt = s.now().UTC()
	}
	s.entries[key] = &entry{
		sessionID: manifest.SessionID, artifactID: manifest.ArtifactID, kind: manifest.Kind,
		content: content, poster: poster, stagedAt: stagedAt,
	}
	s.sessionBytes[manifest.SessionID] += bytesRequired
	s.totalBytes += bytesRequired
	return nil
}

func (s *Store) Get(sessionID, artifactID string, kind ResourceKind) (Resource, error) {
	if !canonicalUUID(sessionID) || !canonicalUUID(artifactID) || (kind != Content && kind != Poster) {
		return Resource{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Resource{}, ErrUnavailable
	}
	s.cleanupExpiredLocked(s.now().UTC())
	value := s.entries[artifactKey(sessionID, artifactID)]
	if value == nil {
		return Resource{}, ErrNotFound
	}
	if value.expired {
		return Resource{}, ErrExpired
	}
	resource := &value.content
	if kind == Poster {
		resource = value.poster
		if resource == nil {
			return Resource{}, ErrNotFound
		}
	}
	content, err := s.readSealed(*resource)
	if err != nil {
		s.expireEntryLocked(value)
		return Resource{}, err
	}
	return Resource{Bytes: content, MediaType: resource.mediaType, DisplayName: resource.displayName, SHA256: resource.sha256}, nil
}

func (s *Store) Acknowledge(sessionID, artifactID string, request Acknowledgement) (Receipt, error) {
	if !canonicalUUID(sessionID) || !canonicalUUID(artifactID) || !canonicalUUID(request.AckID) ||
		request.SizeBytes < 1 || !lowerHexDigest(request.SHA256) || request.LocalCommittedAt.IsZero() {
		return Receipt{}, ErrInvalid
	}
	hash := acknowledgementHash(request)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Receipt{}, ErrUnavailable
	}
	s.cleanupExpiredLocked(s.now().UTC())
	value := s.entries[artifactKey(sessionID, artifactID)]
	if value == nil {
		return Receipt{}, ErrNotFound
	}
	if value.ack != nil {
		if value.ack.id == request.AckID && subtle.ConstantTimeCompare(value.ack.requestHash[:], hash[:]) == 1 {
			return value.ack.receipt, nil
		}
		if value.ack.id == request.AckID {
			return Receipt{}, ErrAckConflict
		}
		return Receipt{}, ErrExpired
	}
	if value.expired {
		return Receipt{}, ErrExpired
	}
	if request.SizeBytes != value.content.size || subtle.ConstantTimeCompare([]byte(request.SHA256), []byte(value.content.sha256)) != 1 {
		return Receipt{}, ErrManifestMismatch
	}
	now := s.now().UTC()
	receipt := Receipt{ArtifactID: artifactID, AckID: request.AckID, CleanupStatus: "completed", AcknowledgedAt: now}
	value.ack = &ackRecord{id: request.AckID, requestHash: hash, receipt: receipt}
	s.expireEntryLocked(value)
	return receipt, nil
}

func (s *Store) DeleteSession(sessionID string) {
	if !canonicalUUID(sessionID) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, value := range s.entries {
		if value.sessionID == sessionID {
			s.removeEntryLocked(key, value)
		}
	}
	_ = os.RemoveAll(filepath.Join(s.root, sessionID))
}

func (s *Store) CleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.cleanupExpiredLocked(s.now().UTC())
	}
}

func (s *Store) RunJanitor(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.CleanupExpired()
		}
	}
}

func (s *Store) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.entries = make(map[string]*entry)
	s.sessionBytes = make(map[string]int64)
	s.totalBytes = 0
	s.mu.Unlock()
	return os.RemoveAll(s.root)
}

func (s *Store) writeSealed(directory, name string, plaintext []byte, mediaType, displayName string) (sealedResource, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return sealedResource{}, ErrUnavailable
	}
	sealed := s.aead.Seal(nil, nonce, plaintext, nil)
	encoded := append([]byte{1}, nonce...)
	encoded = append(encoded, sealed...)
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return sealedResource{}, ErrUnavailable
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = os.Remove(path)
		return sealedResource{}, ErrUnavailable
	}
	digest := sha256.Sum256(plaintext)
	return sealedResource{
		path: path, mediaType: mediaType, displayName: displayName, size: int64(len(plaintext)), sha256: hex.EncodeToString(digest[:]),
	}, nil
}

func (s *Store) readSealed(resource sealedResource) ([]byte, error) {
	encoded, err := os.ReadFile(resource.path)
	if err != nil || len(encoded) < 1+s.aead.NonceSize()+s.aead.Overhead() || encoded[0] != 1 {
		return nil, ErrUnavailable
	}
	nonceEnd := 1 + s.aead.NonceSize()
	plaintext, err := s.aead.Open(nil, encoded[1:nonceEnd], encoded[nonceEnd:], nil)
	if err != nil || int64(len(plaintext)) != resource.size {
		return nil, ErrUnavailable
	}
	digest := sha256.Sum256(plaintext)
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(digest[:])), []byte(resource.sha256)) != 1 {
		return nil, ErrUnavailable
	}
	return plaintext, nil
}

func (s *Store) cleanupExpiredLocked(now time.Time) {
	for key, value := range s.entries {
		if value.ack != nil {
			if !now.Before(value.stagedAt.Add(s.ttl)) {
				s.removeEntryLocked(key, value)
			}
			continue
		}
		if !value.expired && !now.Before(value.stagedAt.Add(s.ttl)) {
			s.expireEntryLocked(value)
		}
	}
}

func (s *Store) expireEntryLocked(value *entry) {
	if value.expired {
		return
	}
	bytesReleased := value.content.size
	_ = os.Remove(value.content.path)
	if value.poster != nil {
		bytesReleased += value.poster.size
		_ = os.Remove(value.poster.path)
	}
	_ = os.Remove(filepath.Dir(value.content.path))
	value.expired = true
	s.sessionBytes[value.sessionID] -= bytesReleased
	if s.sessionBytes[value.sessionID] <= 0 {
		delete(s.sessionBytes, value.sessionID)
	}
	s.totalBytes -= bytesReleased
}

func (s *Store) removeEntryLocked(key string, value *entry) {
	if !value.expired {
		s.expireEntryLocked(value)
	}
	delete(s.entries, key)
}

func validateManifest(value Manifest) error {
	if !canonicalUUID(value.SessionID) || !canonicalUUID(value.ArtifactID) ||
		!validKind(value.Kind) || !safeDisplayName(value.DisplayName) || !mediaAllowed(value.Kind, value.MediaType, value.Content) || len(value.Content) == 0 {
		return ErrInvalid
	}
	limit := otherLimit
	if value.Kind == "image" {
		limit = imageLimit
	}
	if int64(len(value.Content)) > limit {
		return ErrLimitExceeded
	}
	if len(value.Poster) > 0 {
		if value.Kind != "video" || !safeDisplayName(value.PosterName) || !mediaAllowed("image", value.PosterType, value.Poster) || int64(len(value.Poster)) > imageLimit {
			return ErrInvalid
		}
	} else if value.PosterName != "" || value.PosterType != "" {
		return ErrInvalid
	}
	return nil
}

func canonicalUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value
}

func validKind(value string) bool {
	return value == "image" || value == "video" || value == "file" || value == "report"
}

func safeDisplayName(value string) bool {
	if value == "" || len([]byte(value)) > 255 || !utf8.ValidString(value) || strings.ContainsAny(value, "/\\\x00\r\n\"") {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return value != "." && value != ".."
}

func mediaAllowed(kind, mediaType string, content []byte) bool {
	if len(content) == 0 || len(mediaType) > 128 || strings.ContainsAny(mediaType, "\r\n\x00") {
		return false
	}
	switch kind {
	case "image":
		switch mediaType {
		case "image/png":
			return bytes.HasPrefix(content, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
		case "image/jpeg":
			return len(content) >= 3 && content[0] == 0xff && content[1] == 0xd8 && content[2] == 0xff
		case "image/webp":
			return len(content) >= 12 && string(content[:4]) == "RIFF" && string(content[8:12]) == "WEBP"
		}
	case "video":
		return mediaType == "video/mp4" && len(content) >= 12 && string(content[4:8]) == "ftyp"
	case "file":
		switch mediaType {
		case "text/plain", "text/csv":
			return utf8.Valid(content) && !bytes.Contains(content, []byte{0})
		case "application/json":
			return json.Valid(content)
		case "application/pdf":
			return bytes.HasPrefix(content, []byte("%PDF-"))
		case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
			return bytes.HasPrefix(content, []byte{'P', 'K', 0x03, 0x04})
		}
	case "report":
		return mediaType == "application/vnd.yijie.report+json;version=1" && json.Valid(content)
	}
	return false
}

func lowerHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func acknowledgementHash(value Acknowledgement) [32]byte {
	canonical := fmt.Sprintf("%s\n%d\n%s\n%s", value.AckID, value.SizeBytes, value.SHA256, value.LocalCommittedAt.UTC().Format(time.RFC3339Nano))
	return sha256.Sum256([]byte(canonical))
}

func artifactKey(sessionID, artifactID string) string {
	return sessionID + "/" + artifactID
}
