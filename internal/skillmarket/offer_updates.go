package skillmarket

import (
	"context"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/skillmarketv2"
	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

// An App release changes offers, not native installation truth. Only installed
// objects receive an explicit native refresh intent; missing installations stay
// uninstalled. Personal imports never participate in built-in release updates.
func (s *Service) PrepareOfferUpdates(ctx context.Context) error {
	if s.compatible.Load() {
		return nil
	}
	s.serial.Lock()
	defer s.serial.Unlock()
	entries, err := s.entries()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Origin != wire.OriginBuiltin || e.Authority != "native" || e.ActiveOperation != "" {
			continue
		}
		state, err := s.readState(ctx, e)
		if err != nil {
			return err
		}
		if absent(state) {
			if e.Offer == nil {
				e.Listed = false
				e.Published = false
			} else {
				e.Selected = *e.Offer
				e.Listed = true
				e.Published = true
			}
			if err = s.saveEntry(e); err != nil {
				return err
			}
			continue
		}
		if e.Offer == nil || e.Offer.SourceRevision == e.Selected.SourceRevision || state.CacheState != "valid" || !state.UserConfigPresent {
			continue
		}
		id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("yijie-skill-release/"+e.ID+"/"+e.Offer.SourceRevision+"/"+time.Now().UTC().Format(time.RFC3339Nano))).String()
		request := wire.Mutation{OperationId: id, SkillId: e.ID, Kind: wire.MutationKindRefresh, ExpectedEpoch: e.Epoch}
		target, previous := *e.Offer, e.Selected
		e.Epoch++
		e.ActiveOperation = id
		op := operation{Result: wire.Operation{OperationId: id, SkillId: e.ID, Epoch: e.Epoch, State: wire.OperationStatePending}, Fingerprint: operationFingerprint(request), Kind: "refresh", Phase: "received", Target: &target, Previous: &previous, CreatedAt: time.Now().UTC()}
		if err = s.db.Update(func(tx *bolt.Tx) error {
			if err := encodePut(tx, "entries", e.ID, e); err != nil {
				return err
			}
			return encodePut(tx, "operations", id, op)
		}); err != nil {
			return err
		}
	}
	return s.publishCatalog("yijie-builtin")
}
