package session

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

// Only an authenticated, live native turn can obtain this namespace. The
// durable installation seed avoids retaining query text or per-call records.
func (s *Store) deepSearchScope(binding, root, rootTurn, caller, turn string) (uuid.UUID, error) {
	var seed []byte
	key := []byte("web_search_scope_seed_v2")
	err := s.db.View(func(tx *bolt.Tx) error { seed = append(seed, tx.Bucket(metadataBucket).Get(key)...); return nil })
	if err == nil && len(seed) == 0 {
		err = s.db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(metadataBucket)
			seed = append([]byte(nil), b.Get(key)...)
			if len(seed) > 0 {
				return nil
			}
			seed = make([]byte, 32)
			if _, e := rand.Read(seed); e != nil {
				return e
			}
			return b.Put(key, seed)
		})
	}
	if err != nil || len(seed) != 32 {
		return uuid.Nil, errors.New("search scope unavailable")
	}
	mac := hmac.New(sha256.New, seed)
	payload, _ := json.Marshal([5]string{binding, root, rootTurn, caller, turn})
	_, _ = mac.Write(payload)
	var id uuid.UUID
	copy(id[:], mac.Sum(nil))
	id[6] = (id[6] & 15) | 0x80
	id[8] = (id[8] & 63) | 0x80
	return id, nil
}
func (s *Service) AdmitBuiltinWebV2(ctx context.Context, q wire.WebAdmissionRequest) (wire.WebAdmissionReceiptV2, error) {
	rec, err := s.AdmitBuiltinWeb(ctx, q)
	if err != nil {
		return wire.WebAdmissionReceiptV2{}, err
	}
	scope, err := s.store.deepSearchScope(q.BindingEpoch.String(), rec.RootThreadId, rec.RootTurnId, q.ThreadId, q.TurnId)
	if err != nil {
		return wire.WebAdmissionReceiptV2{}, err
	}
	return wire.WebAdmissionReceiptV2{ScopeId: scope, RootThreadId: rec.RootThreadId, RootTurnId: rec.RootTurnId, OperationId: rec.OperationId, DeadlineUnixMs: rec.DeadlineUnixMs}, nil
}

func webOperationFact(raw json.RawMessage, tool string) *native.DeepSearchReceipt {
	var envelope struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if len(raw) > maxRetainedEventDataBytes || json.Unmarshal(raw, &envelope) != nil {
		return nil
	}
	key := "ai.yijie/deep-search-receipt"
	if tool == "kimi_web_fetch" {
		key = "ai.yijie/fetch-receipt"
	}
	f := envelope.Meta[key]
	if len(f) == 0 || len(f) > 512 || wire.Validate("DeepSearchReceipt", f) != nil {
		return nil
	}
	var v native.DeepSearchReceipt
	if json.Unmarshal(f, &v) != nil {
		return nil
	}
	return &v
}
func stripSearchFact(item *native.NativeItem) {
	if item.Mcp != nil && item.Mcp.SearchReceipt != nil {
		m := *item.Mcp
		m.SearchReceipt = nil
		item.Mcp = &m
	}
}
func stripNativeEvent(event Event, strip func(*native.NativeItem)) Event {
	n := event.Payload.Native
	if n == nil {
		return event
	}
	cp := *n
	if cp.Item != nil {
		it := *cp.Item
		strip(&it)
		cp.Item = &it
	}
	if cp.Turn != nil {
		t := *cp.Turn
		t.Items = append([]native.NativeItem{}, t.Items...)
		for i := range t.Items {
			strip(&t.Items[i])
		}
		cp.Turn = &t
	}
	event.Payload.Native = &cp
	return event
}
func stripNativeSnapshot(v *native.NativeThreadSnapshot, strip func(*native.NativeItem)) {
	for i := range v.Turns {
		for j := range v.Turns[i].Items {
			strip(&v.Turns[i].Items[j])
		}
	}
}

func StripSearchReceipt(e Event) Event                   { return stripNativeEvent(e, stripSearchFact) }
func StripSearchSnapshot(v *native.NativeThreadSnapshot) { stripNativeSnapshot(v, stripSearchFact) }

func stripPublication(item *native.NativeItem) {
	item.Publication = nil
	if item.Type == native.NativeItemTypeUnpublishedText {
		*item = native.NativeItem{Id: item.Id, Type: native.NativeItemTypeUnknown, Availability: native.NativeItemAvailabilityUnavailable}
	}
}
func StripNativePublication(e Event) Event { return stripNativeEvent(e, stripPublication) }
func StripNativePublicationSnapshot(v *native.NativeThreadSnapshot) {
	stripNativeSnapshot(v, stripPublication)
}
