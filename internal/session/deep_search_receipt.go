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

func deepSearchFact(raw json.RawMessage) *native.DeepSearchReceipt {
	var envelope struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if len(raw) > maxRetainedEventDataBytes || json.Unmarshal(raw, &envelope) != nil {
		return nil
	}
	f := envelope.Meta["ai.yijie/deep-search-receipt"]
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
func StripSearchReceipt(event Event) Event {
	n := event.Payload.Native
	if n == nil {
		return event
	}
	cp := *n
	if cp.Item != nil {
		it := *cp.Item
		stripSearchFact(&it)
		cp.Item = &it
	}
	if cp.Turn != nil {
		t := *cp.Turn
		t.Items = append([]native.NativeItem(nil), t.Items...)
		for i := range t.Items {
			stripSearchFact(&t.Items[i])
		}
		cp.Turn = &t
	}
	event.Payload.Native = &cp
	return event
}
func StripSearchSnapshot(v *native.NativeThreadSnapshot) {
	for i := range v.Turns {
		for j := range v.Turns[i].Items {
			stripSearchFact(&v.Turns[i].Items[j])
		}
	}
}
