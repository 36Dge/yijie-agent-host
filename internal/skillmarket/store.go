package skillmarket

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/runtimeskillmarket"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/skillmarketv2"
	legacy "github.com/36Dge/yijie-agent-host/internal/skills"
	bolt "go.etcd.io/bbolt"
)

type Runtime interface {
	ReadSkillMCPInventory(context.Context) ([]native.McpServerStatus, error)
	SetSkillSubmissionReady(bool)
	RegisterSkillMarketplace(context.Context, string) error
	ReadSkillPlugin(context.Context, string, string) (native.PluginDetail, error)
	InstallSkillPlugin(context.Context, string, string) error
	UninstallSkillPlugin(context.Context, string) error
	ReadSkillPluginStates(context.Context, []string, []string, string) ([]native.PluginState, error)
	RefreshSkillPlugin(context.Context, string, string, bool) error
	SetSkillPluginEnabled(context.Context, string, bool) error
	ReadSkillPluginConfig(context.Context, string) (map[string]any, error)
	WriteSkillPluginConfig(context.Context, []any, string) error
	ReloadSkillPluginConfig(context.Context) error
	ListSkills(context.Context, []string, bool) ([]codex.SkillsListEntry, error)
	WithSkillMaintenance(context.Context, func(context.Context) error) error
}

type Config struct {
	BundleRoot, BundleSHA256, DataRoot, HostHome string
	Runtime                                      Runtime
}
type Service struct {
	compatible atomic.Bool
	legacy     *legacy.Service
	config     Config
	db         *bolt.DB
	serial     sync.Mutex
	wake       chan struct{}
	ready      atomic.Bool
	startupErr atomic.Value
}
type entry struct {
	MCPDependencies []string            `json:"mcp_dependencies,omitempty"`
	DisplayName     string              `json:"native_display_name,omitempty"`
	Description     string              `json:"native_description,omitempty"`
	DeclaredTools   []string            `json:"declared_tools,omitempty"`
	ID              string              `json:"id"`
	Origin          wire.Origin         `json:"origin"`
	Selected        wire.CatalogSource  `json:"selected"`
	Previous        *wire.CatalogSource `json:"previous,omitempty"`
	Offer           *wire.CatalogSource `json:"offer,omitempty"`
	Epoch           int64               `json:"epoch"`
	Published       bool                `json:"published"`
	Listed          bool                `json:"listed"`
	Authority       string              `json:"authority"`
	ActiveOperation string              `json:"active_operation,omitempty"`
}

func (e entry) market() string {
	if e.Origin == wire.OriginBuiltin {
		return "yijie-builtin"
	}
	return "yijie-user"
}
func (e entry) pluginID() string { return e.Selected.PluginName + "@" + e.market() }

type operation struct {
	Rules          *ruleChange         `json:"rules,omitempty"`
	CancelResolved bool                `json:"cancel_resolved,omitempty"`
	NativePluginID string              `json:"native_plugin_id,omitempty"`
	Result         wire.Operation      `json:"result"`
	Fingerprint    string              `json:"fingerprint"`
	Kind           string              `json:"kind"`
	Phase          string              `json:"phase"`
	Cancel         bool                `json:"cancel"`
	Before         *native.PluginState `json:"before,omitempty"`
	Target         *wire.CatalogSource `json:"target,omitempty"`
	Previous       *wire.CatalogSource `json:"previous,omitempty"`
	DesiredEnabled bool                `json:"desired_enabled"`
	OwnedInstall   bool                `json:"owned_install"`
	InputDigest    string              `json:"input_digest,omitempty"`
	CreatedAt      time.Time           `json:"created_at"`
}
type catalogIntent struct {
	Before, After string
	Bytes         []byte
	Complete      bool
}

var buckets = [][]byte{[]byte("meta"), []byte("entries"), []byte("operations"), []byte("catalogs"), []byte("migrations"), []byte("pending"), []byte("maintenance")}

type Error struct{ Code string }

func (e *Error) Error() string { return "Skill marketplace: " + e.Code }
func fail(code string) error   { return &Error{Code: code} }
func ErrorCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "skill_runtime_unavailable"
}

func Open(config Config) (*Service, error) {
	if config.Runtime == nil || !filepath.IsAbs(config.DataRoot) || !filepath.IsAbs(config.HostHome) || !filepath.IsAbs(config.BundleRoot) {
		return nil, fail("market_configuration_invalid")
	}
	for _, dir := range []string{config.DataRoot, filepath.Join(config.DataRoot, "builtin"), filepath.Join(config.DataRoot, "user"), filepath.Join(config.DataRoot, "staging"), filepath.Join(config.HostHome, "skill-market-v2")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
		info, err := os.Lstat(dir)
		real, e := filepath.EvalSymlinks(dir)
		if err != nil || e != nil || !info.IsDir() || real != dir || info.Mode().Perm()&0077 != 0 {
			return nil, fail("market_directory_invalid")
		}
	}
	db, err := bolt.Open(filepath.Join(config.HostHome, "skill-market-v2/state.db"), 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	service := &Service{config: config, db: db, wake: make(chan struct{}, 1)}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range buckets {
			if _, e := tx.CreateBucketIfNotExists(name); e != nil {
				return e
			}
		}
		meta := tx.Bucket(buckets[0])
		if v := meta.Get([]byte("schema")); v != nil && string(v) != "1" && string(v) != "2" {
			return fail("market_schema_unsupported")
		}
		if meta.Get([]byte("pending-index")) == nil {
			if err := tx.Bucket([]byte("operations")).ForEach(func(key, raw []byte) error {
				var op operation
				if err := json.Unmarshal(raw, &op); err != nil {
					return err
				}
				if op.Result.State == wire.OperationStatePending || op.Result.State == wire.OperationStateNeedsAttention {
					return tx.Bucket([]byte("pending")).Put(key, []byte{1})
				}
				return nil
			}); err != nil {
				return err
			}
			if err := meta.Put([]byte("pending-index"), []byte("1")); err != nil {
				return err
			}
		}
		return meta.Put([]byte("schema"), []byte("2"))
	})
	if err == nil {
		_ = db.View(func(tx *bolt.Tx) error {
			service.compatible.Store(string(tx.Bucket([]byte("meta")).Get([]byte("mode"))) == "compatible_legacy")
			return nil
		})
		err = service.prepareBundle()
	}
	if err == nil {
		err = service.recoverCatalogs()
	}
	if err == nil {
		for _, market := range []string{"yijie-builtin", "yijie-user"} {
			if err = service.publishCatalog(market); err != nil {
				break
			}
		}
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return service, nil
}
func (s *Service) Close() error { return s.db.Close() }
func (s *Service) root(market string) string {
	if market == "yijie-builtin" {
		return filepath.Join(s.config.DataRoot, "builtin")
	}
	return filepath.Join(s.config.DataRoot, "user")
}
func encodePut(tx *bolt.Tx, bucket, key string, value any) error {
	data, e := json.Marshal(value)
	if e != nil {
		return e
	}
	if bucket == "operations" {
		var op operation
		if err := json.Unmarshal(data, &op); err != nil {
			return err
		}
		index := tx.Bucket([]byte("pending"))
		if op.Result.State == wire.OperationStatePending || op.Result.State == wire.OperationStateNeedsAttention {
			if index.Get([]byte(key)) == nil && index.Stats().KeyN >= 512 {
				return fail("skill_busy")
			}
			if err := index.Put([]byte(key), []byte{1}); err != nil {
				return err
			}
		} else if err := index.Delete([]byte(key)); err != nil {
			return err
		}
	}
	return tx.Bucket([]byte(bucket)).Put([]byte(key), data)
}
func (s *Service) get(bucket, key string, out any) error {
	return s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket([]byte(bucket)).Get([]byte(key))
		if raw == nil {
			return fail("skill_not_found")
		}
		return json.Unmarshal(raw, out)
	})
}
func (s *Service) entries() ([]entry, error) {
	out := []entry{}
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("entries")).ForEach(func(_, v []byte) error {
			var e entry
			if err := json.Unmarshal(v, &e); err != nil {
				return err
			}
			out = append(out, e)
			return nil
		})
	})
	return out, err
}
func (s *Service) pending() ([]operation, error) {
	out := []operation{}
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("pending")).ForEach(func(key, _ []byte) error {
			v := tx.Bucket([]byte("operations")).Get(key)
			var op operation
			if err := json.Unmarshal(v, &op); err != nil {
				return err
			}
			if op.Result.State == wire.OperationStatePending {
				out = append(out, op)
			}
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, err
}
func (s *Service) saveOperation(op *operation) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		raw := tx.Bucket([]byte("operations")).Get([]byte(op.Result.OperationId))
		if raw != nil {
			var current operation
			if err := json.Unmarshal(raw, &current); err != nil {
				return err
			}
			op.Cancel = op.Cancel || current.Cancel
		}
		return encodePut(tx, "operations", op.Result.OperationId, op)
	})
}

func (s *Service) prepareBundle() error {
	raw, err := os.ReadFile(filepath.Join(s.config.BundleRoot, "presentation.json"))
	if err != nil {
		return err
	}
	if len(raw) > 4<<20 || len(s.config.BundleSHA256) != 64 || digest(raw) != s.config.BundleSHA256 {
		return fail("bundle_authority_mismatch")
	}
	var bundle wire.Bundle
	if json.Unmarshal(raw, &bundle) != nil {
		return fail("bundle_invalid")
	}
	old, err := s.entries()
	if err != nil {
		return err
	}
	current := map[string]entry{}
	for _, e := range old {
		if e.Origin == wire.OriginBuiltin {
			e.Offer = nil
		}
		current[e.ID] = e
	}
	names := map[string]bool{}
	for _, source := range bundle.Skills {
		if source.PluginName != "yj-builtin-"+digest([]byte(source.Id))[:24] || names[source.PluginName] || source.SourcePath != "plugins/"+source.PluginName {
			return fail("bundle_identity_invalid")
		}
		names[source.PluginName] = true
		files, e := readVerifiedFiles(filepath.Join(s.config.BundleRoot, filepath.FromSlash(source.SourcePath), "skills/main"), source)
		if e != nil {
			return e
		}
		source.SourcePath = "sources/" + source.PluginName + "/" + source.SourceRevision
		if e = materializeSource(s.root("yijie-builtin"), source, files); e != nil {
			return e
		}
		existing, ok := current[source.Id]
		if !ok {
			existing = entry{ID: source.Id, Origin: wire.OriginBuiltin, Selected: source, Published: true, Listed: true, Authority: "native"}
		}
		if existing.Origin != wire.OriginBuiltin || existing.Selected.PluginName != source.PluginName {
			return fail("bundle_identity_conflict")
		}
		copy := source
		existing.Offer = &copy
		current[source.Id] = existing
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		for id, e := range current {
			if err := encodePut(tx, "entries", id, e); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) catalogBytes(market string) ([]byte, error) {
	entries, err := s.entries()
	if err != nil {
		return nil, err
	}
	plugins := []any{}
	for _, e := range entries {
		if e.market() != market || !e.Listed {
			continue
		}
		source := e.Selected
		plugins = append(plugins, map[string]any{"name": source.PluginName, "source": "./sources/" + source.PluginName + "/" + source.SourceRevision, "policy": map[string]any{"installation": "AVAILABLE", "authentication": "ON_INSTALL"}, "category": source.Category})
	}
	return canonical(map[string]any{"name": market, "plugins": plugins}), nil
}
func (s *Service) manifestPath(market string) string {
	return filepath.Join(s.root(market), ".agents/plugins/marketplace.json")
}
func (s *Service) recoverCatalogs() error {
	for _, market := range []string{"yijie-builtin", "yijie-user"} {
		var intent catalogIntent
		err := s.get("catalogs", market, &intent)
		if ErrorCode(err) == "skill_not_found" {
			continue
		}
		if err != nil {
			return err
		}
		if intent.Complete {
			continue
		}
		if err = s.applyCatalog(market, intent); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) publishCatalog(market string) error {
	raw, err := s.catalogBytes(market)
	if err != nil {
		return err
	}
	manifest := s.manifestPath(market)
	if err = os.MkdirAll(filepath.Dir(manifest), 0700); err != nil {
		return err
	}
	before, err := os.ReadFile(manifest)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if digest(before) == digest(raw) {
		return nil
	}
	var previous catalogIntent
	previousErr := s.get("catalogs", market, &previous)
	if previousErr == nil && (!previous.Complete || digest(before) != previous.After) {
		return fail("catalog_reconciliation_required")
	}
	if previousErr != nil && ErrorCode(previousErr) != "skill_not_found" {
		return previousErr
	}
	if ErrorCode(previousErr) == "skill_not_found" && len(before) != 0 {
		return fail("catalog_reconciliation_required")
	}
	intent := catalogIntent{Before: digest(before), After: digest(raw), Bytes: raw}
	if err = s.db.Update(func(tx *bolt.Tx) error { return encodePut(tx, "catalogs", market, intent) }); err != nil {
		return err
	}
	return s.applyCatalog(market, intent)
}
func (s *Service) applyCatalog(market string, intent catalogIntent) error {
	filename := s.manifestPath(market)
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return err
	}
	current, err := os.ReadFile(filename)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if digest(current) != intent.Before && digest(current) != intent.After {
		return fail("catalog_reconciliation_required")
	}
	history := filepath.Join(s.root(market), "catalog-generations")
	if err = os.MkdirAll(history, 0700); err != nil {
		return err
	}
	generation := filepath.Join(history, intent.After+".json")
	if stored, e := os.ReadFile(generation); e == nil {
		if digest(stored) != intent.After {
			return fail("catalog_reconciliation_required")
		}
	} else if errors.Is(e, os.ErrNotExist) {
		if err = writeDurable(generation, intent.Bytes); err != nil {
			return err
		}
	} else {
		return e
	}
	if err = syncDirectory(history); err != nil {
		return err
	}
	if digest(current) != intent.After {
		if err = replaceDurable(filename, intent.Bytes); err != nil {
			return err
		}
	}
	intent.Complete = true
	return s.db.Update(func(tx *bolt.Tx) error { return encodePut(tx, "catalogs", market, intent) })
}
