package skillmarket

import (
	"context"
	"path/filepath"
	"testing"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/runtimeskillmarket"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/skillmarketv2"
	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

// Pure decision coverage for a normal completed native uninstall observed
// after cancellation. No process fault or filesystem fault is constructed.
type completedOrphanFacts struct{ Runtime }

func (*completedOrphanFacts) WithSkillMaintenance(ctx context.Context, action func(context.Context) error) error {
	return action(ctx)
}
func (*completedOrphanFacts) ReadSkillPluginStates(_ context.Context, ids, markets []string, cwd string) ([]native.PluginState, error) {
	return []native.PluginState{{PluginId: ids[0], CacheState: "absent", Skills: []native.SkillSummary{}, Errors: []string{}}}, nil
}
func TestFEAT174LateOrphanCancellationConfirmsCompletedOutcome(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "state.db"), 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range buckets {
			if _, err := tx.CreateBucket(name); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := &Service{db: db, config: Config{Runtime: &completedOrphanFacts{}}}
	if !s.NativeRoute("ordinary-unmapped") {
		t.Fatal("missing routing data granted legacy authority")
	}
	id := uuid.NewString()
	plugin := "ordinary@yijie-user"
	op := operation{NativePluginID: plugin, Kind: "uninstall", Phase: "native_pending", Cancel: true, Result: wire.Operation{OperationId: id, SkillId: orphanID(plugin), State: wire.OperationStatePending}}
	if err = s.saveOperation(&op); err != nil {
		t.Fatal(err)
	}
	if err = s.executeOrphan(context.Background(), &op); err != nil {
		t.Fatal(err)
	}
	if op.Result.State != wire.OperationStateComplete || op.Result.ErrorCode != "cancel_too_late" {
		t.Fatal("confirmed native result remained unresolved", op.Result)
	}
	_ = db.View(func(tx *bolt.Tx) error {
		if tx.Bucket([]byte("pending")).Stats().KeyN != 0 {
			t.Fatal("completed intent still blocks admission")
		}
		return nil
	})
}
