package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/videogeneration"
	"github.com/36Dge/yijie-agent-host/internal/videogen"
	bolt "go.etcd.io/bbolt"
)

var videoOperationsBucket = []byte("video_operations_v1")
var errVideoExpert = errors.New("selected expert excludes builtin video")
var errVideoState = errors.New("video operation state unavailable")

const (
	videoReserved         = "reserved"
	videoSending          = "sending"
	videoSubmitted        = "submitted"
	videoUnknown          = "unknown"
	videoRejected         = "rejected"
	videoCancelled        = "cancelled_before_submit"
	videoDeliveryRejected = "delivery_rejected"
)

// Private adapter metadata only. No prompt, image, URL, manifest, ACK or bytes.
// The existing root UUID is the operation identity, scoped by its Host session.
type videoOperation struct {
	Version        int                 `json:"version"`
	SessionID      string              `json:"session_id"`
	RootID         string              `json:"root_id"`
	ThreadID       string              `json:"thread_id"`
	Digest         string              `json:"digest"`
	ImageMode      wire.ImageMode      `json:"image_mode"`
	CreatedAt      int64               `json:"created_at"`
	Phase          string              `json:"phase"`
	TaskID         string              `json:"task_id,omitempty"`
	ProviderStatus wire.ProviderStatus `json:"provider_status,omitempty"`
	ObservedAt     int64               `json:"observed_at,omitempty"`      // Host observation, Unix nanoseconds.
	QueryNotBefore int64               `json:"query_not_before,omitempty"` // Throttle only; not a provider status.
	FailureCode    wire.VideoCode      `json:"failure_code,omitempty"`
	ArtifactID     string              `json:"artifact_id,omitempty"`
	DeliveryTurnID string              `json:"delivery_turn_id,omitempty"`
	Handoff        bool                `json:"handoff_attempted,omitempty"`
}

func (o videoOperation) unresolved() bool {
	if o.Handoff || o.Phase == videoRejected || o.Phase == videoCancelled || o.Phase == videoDeliveryRejected {
		return false
	}
	switch o.ProviderStatus {
	case wire.ProviderStatusFailed, wire.ProviderStatusCancelled, wire.ProviderStatusExpired:
		return false
	}
	return true
}
func videoKey(session, id string) []byte { return []byte(session + "/" + id) }
func decodeVideoOperation(raw, key []byte) (videoOperation, error) {
	var o videoOperation
	if len(raw) > 8192 {
		return o, errVideoState
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&o) != nil || d.Decode(new(any)) != io.EOF || o.Version != 1 || !validUUID(o.SessionID) || !validUUID(o.RootID) || !validUUID(o.ThreadID) || !validSHA256(o.Digest) || o.ImageMode.Validate() != nil || o.CreatedAt < 0 || !bytes.Equal(key, videoKey(o.SessionID, o.RootID)) {
		return o, errVideoState
	}
	switch o.Phase {
	case videoReserved, videoSending, videoSubmitted, videoUnknown, videoRejected, videoCancelled, videoDeliveryRejected:
	default:
		return o, errVideoState
	}
	if o.TaskID != "" && !videogen.ValidTaskID(o.TaskID) {
		return o, errVideoState
	}
	if o.Phase == videoSubmitted && o.TaskID == "" {
		return o, errVideoState
	}
	if (o.ProviderStatus != "") != (o.ObservedAt > 0) || o.ProviderStatus != "" && (o.ProviderStatus.Validate() != nil || o.TaskID == "") {
		return o, errVideoState
	}
	if o.FailureCode != "" && o.FailureCode.Validate() != nil {
		return o, errVideoState
	}
	if o.QueryNotBefore < 0 {
		return o, errVideoState
	}
	if o.ArtifactID != "" && !validUUID(o.ArtifactID) {
		return o, errVideoState
	}
	if o.Handoff && (o.ArtifactID == "" || !validUUID(o.DeliveryTurnID) || o.TaskID == "" || o.ProviderStatus != wire.ProviderStatusSucceeded) {
		return o, errVideoState
	}
	if !o.Handoff && o.DeliveryTurnID != "" {
		return o, errVideoState
	}
	return o, nil
}
func readVideoOperation(tx *bolt.Tx, session, id string) (videoOperation, error) {
	bucket := tx.Bucket(videoOperationsBucket)
	if bucket == nil {
		return videoOperation{}, ErrNotFound
	}
	key := videoKey(session, id)
	raw := bucket.Get(key)
	if raw == nil {
		return videoOperation{}, ErrNotFound
	}
	return decodeVideoOperation(raw, key)
}
func writeVideoOperation(tx *bolt.Tx, o videoOperation) error {
	data, err := json.Marshal(o)
	if err != nil {
		return errVideoState
	}
	if _, err = decodeVideoOperation(data, videoKey(o.SessionID, o.RootID)); err != nil {
		return err
	}
	bucket, err := tx.CreateBucketIfNotExists(videoOperationsBucket)
	if err != nil {
		return err
	}
	return bucket.Put(videoKey(o.SessionID, o.RootID), data)
}
func videoOperations(tx *bolt.Tx, session string) ([]videoOperation, error) {
	out := []videoOperation{}
	bucket := tx.Bucket(videoOperationsBucket)
	if bucket == nil {
		return out, nil
	}
	prefix := []byte(session + "/")
	cursor := bucket.Cursor()
	for key, raw := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, raw = cursor.Next() {
		o, err := decodeVideoOperation(raw, key)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt == out[j].CreatedAt {
			return out[i].RootID < out[j].RootID
		}
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out, nil
}

func (s *Store) videoAdmission(tx *bolt.Tx, session, thread, turn string) (Record, error) {
	record, err := s.loadRecord(tx, session)
	if err != nil {
		return record, err
	}
	if record.Purpose == purposeDraft || record.CodexThreadID != thread || record.ActiveTurnID != turn || record.State != StateActive || record.CleanupOperationID != "" {
		return record, ErrSessionNotUsable
	}
	binding, err := readExpertBinding(tx, session)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return record, err
	}
	if binding.Expert != nil || binding.PendingOperation != "" {
		return record, errVideoExpert
	}
	return record, nil
}
func (s *Store) videoCurrent(session, thread, turn string) (Record, error) {
	var record Record
	err := s.db.View(func(tx *bolt.Tx) error {
		var err error
		record, err = s.videoAdmission(tx, session, thread, turn)
		return err
	})
	return record, err
}
func (s *Store) videoOperation(session, id string) (videoOperation, error) {
	var out videoOperation
	err := s.db.View(func(tx *bolt.Tx) error {
		if _, err := s.loadRecord(tx, session); err != nil {
			return err
		}
		var err error
		out, err = readVideoOperation(tx, session, id)
		return err
	})
	return out, err
}

// Admission and the per-conversation unresolved check share the reservation TX.
func (s *Store) reserveVideo(session, thread, root, digest string, mode wire.ImageMode) (out videoOperation, created bool, err error) {
	err = s.db.Update(func(tx *bolt.Tx) error {
		if _, e := s.videoAdmission(tx, session, thread, root); e != nil {
			return e
		}
		existing, e := readVideoOperation(tx, session, root)
		if e == nil {
			out = existing
			if existing.Digest != digest {
				return ErrTurnOperationConflict
			}
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		operations, e := videoOperations(tx, session)
		if e != nil {
			return e
		}
		for _, o := range operations {
			if o.unresolved() {
				out = o
				return nil
			}
		}
		out = videoOperation{Version: 1, SessionID: session, ThreadID: thread, RootID: root, Digest: digest, ImageMode: mode, CreatedAt: time.Now().UTC().Unix(), Phase: videoReserved}
		if e = writeVideoOperation(tx, out); e != nil {
			return e
		}
		created = true
		return nil
	})
	return
}

func (s *Store) resolveVideo(session, thread, root string, id *string) (selected *videoOperation, candidates []videoOperation, err error) {
	err = s.db.View(func(tx *bolt.Tx) error {
		if _, e := s.videoAdmission(tx, session, thread, root); e != nil {
			return e
		}
		target := root
		if id != nil {
			target = *id
		}
		if o, e := readVideoOperation(tx, session, target); e == nil {
			selected = &o
			return nil
		} else if !errors.Is(e, ErrNotFound) {
			return e
		}
		if id != nil {
			return ErrNotFound
		}
		all, e := videoOperations(tx, session)
		if e != nil {
			return e
		}
		var pending []videoOperation
		for _, o := range all {
			if o.unresolved() {
				pending = append(pending, o)
			}
		}
		if len(pending) == 1 {
			selected = &pending[0]
			return nil
		}
		if len(all) == 1 {
			selected = &all[0]
			return nil
		}
		if len(all) == 0 {
			return ErrNotFound
		}
		candidates = all
		return nil
	})
	return
}

// A late response can update an existing owned record, but never recreate a
// deleted session/record. No current expert permission is inferred from this.
func (s *Store) updateVideo(original videoOperation, change func(*videoOperation) error) (out videoOperation, err error) {
	err = s.db.Update(func(tx *bolt.Tx) error {
		record, e := s.loadRecord(tx, original.SessionID)
		if e != nil {
			return e
		}
		if record.CleanupOperationID != "" {
			return ErrSessionNotUsable
		}
		out, e = readVideoOperation(tx, original.SessionID, original.RootID)
		if e != nil {
			return e
		}
		if out.Digest != original.Digest || out.ThreadID != original.ThreadID {
			return errVideoState
		}
		if e = change(&out); e != nil {
			return e
		}
		return writeVideoOperation(tx, out)
	})
	return
}

func (s *Store) prepareVideoArtifact(original videoOperation, thread, turn, newID string) (out videoOperation, err error) {
	err = s.db.Update(func(tx *bolt.Tx) error {
		if _, e := s.videoAdmission(tx, original.SessionID, thread, turn); e != nil {
			return e
		}
		var e error
		out, e = readVideoOperation(tx, original.SessionID, original.RootID)
		if e != nil {
			return e
		}
		if out.Handoff || out.ProviderStatus != wire.ProviderStatusSucceeded || out.TaskID == "" {
			return errVideoState
		}
		if out.ArtifactID == "" {
			out.ArtifactID = newID
			return writeVideoOperation(tx, out)
		}
		return nil
	})
	return
}
func (s *Store) markVideoHandoff(original videoOperation, thread, turn string) (out videoOperation, err error) {
	err = s.db.Update(func(tx *bolt.Tx) error {
		if _, e := s.videoAdmission(tx, original.SessionID, thread, turn); e != nil {
			return e
		}
		var e error
		out, e = readVideoOperation(tx, original.SessionID, original.RootID)
		if e != nil {
			return e
		}
		if out.Handoff || out.ArtifactID == "" || out.ArtifactID != original.ArtifactID || out.ProviderStatus != wire.ProviderStatusSucceeded {
			return errVideoState
		}
		out.Handoff = true
		out.DeliveryTurnID = turn
		return writeVideoOperation(tx, out)
	})
	return
}
func deleteVideoOperationsForSession(tx *bolt.Tx, session string) error {
	bucket := tx.Bucket(videoOperationsBucket)
	if bucket == nil {
		return nil
	}
	prefix := []byte(session + "/")
	cursor := bucket.Cursor()
	for key, _ := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, _ = cursor.Next() {
		if err := cursor.Delete(); err != nil {
			return err
		}
	}
	return nil
}
