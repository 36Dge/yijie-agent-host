package skillmarket

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/runtimeskillmarket"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/skillmarketv2"
	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

func (s *Service) signal() {
	s.config.Runtime.SetSkillSubmissionReady(false)
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		}
		if s.compatible.Load() {
			s.updateSubmissionReadiness()
			continue
		}
		if !s.ready.Load() {
			continue
		}
		operations, err := s.pending()
		if err != nil {
			continue
		}
		for _, op := range operations {
			if ctx.Err() != nil {
				return
			}
			s.serial.Lock()
			_ = s.execute(ctx, op.Result.OperationId)
			s.serial.Unlock()
			s.updateSubmissionReadiness()
		}
		s.updateSubmissionReadiness()
	}
}
func (s *Service) Mutate(ctx context.Context, request wire.Mutation) (wire.Operation, error) {
	if request.Validate() != nil || request.SkillId == "" {
		return wire.Operation{}, fail("invalid_request")
	}
	fp := operationFingerprint(request)
	if result, ok, err := s.operationReplayed(request.OperationId, fp); ok || err != nil {
		return result, err
	}
	if s.compatible.Load() {
		return wire.Operation{}, fail("compatibility_mode")
	}
	if !s.ready.Load() {
		return wire.Operation{}, fail("skill_migration_pending")
	}
	s.serial.Lock()
	defer s.serial.Unlock()
	if result, ok, err := s.operationReplayed(request.OperationId, fp); ok || err != nil {
		return result, err
	}
	var e entry
	// Recheck after waiting for maintenance to release this same writer lock.
	if s.compatible.Load() {
		return wire.Operation{}, fail("compatibility_mode")
	}
	if !s.ready.Load() {
		return wire.Operation{}, fail("skill_migration_pending")
	}
	if err := s.get("entries", request.SkillId, &e); err != nil {
		if ErrorCode(err) == "skill_not_found" && strings.HasPrefix(request.SkillId, "unresolved-") {
			return s.mutateOrphan(ctx, request, fp)
		}
		return wire.Operation{}, err
	}
	if request.ExpectedEpoch != e.Epoch {
		return wire.Operation{}, fail("skill_epoch_conflict")
	}
	if e.ActiveOperation != "" {
		return wire.Operation{}, fail("skill_busy")
	}
	if e.Authority != "native" {
		return wire.Operation{}, fail("skill_migration_pending")
	}
	op := operation{Result: wire.Operation{OperationId: request.OperationId, SkillId: e.ID, Epoch: e.Epoch + 1, State: wire.OperationStatePending}, Fingerprint: fp, Kind: string(request.Kind), Phase: "received", CreatedAt: time.Now().UTC()}
	switch request.Kind {
	case wire.MutationKindInstall, wire.MutationKindRefresh:
		if e.Offer == nil && request.Kind == wire.MutationKindInstall {
			return wire.Operation{}, fail("skill_not_offered")
		}
		if e.Offer != nil {
			v := *e.Offer
			op.Target = &v
		} else {
			v := e.Selected
			op.Target = &v
		}
	case wire.MutationKindRollback:
		if e.Previous == nil {
			return wire.Operation{}, fail("rollback_source_unavailable")
		}
		v := *e.Previous
		op.Target = &v
	}
	if request.Kind == wire.MutationKindInstall || request.Kind == wire.MutationKindRefresh || request.Kind == wire.MutationKindRollback {
		if err := s.requireInstallationSlot(ctx, e.pluginID()); err != nil {
			return wire.Operation{}, err
		}
	}
	previous := e.Selected
	op.Previous = &previous
	e.Epoch++
	e.ActiveOperation = request.OperationId
	err := s.db.Update(func(tx *bolt.Tx) error {
		if err := encodePut(tx, "entries", e.ID, e); err != nil {
			return err
		}
		return encodePut(tx, "operations", request.OperationId, op)
	})
	if err == nil {
		s.signal()
	}
	return op.Result, err
}

func (s *Service) Import(ctx context.Context, request wire.ImportRequest, data []byte) (wire.Operation, error) {
	if request.Validate() != nil || int64(len(data)) != request.Bytes || digest(data) != request.Sha256 {
		return wire.Operation{}, fail("invalid_request")
	}
	if (strings.EqualFold(filepath.Ext(request.FileName), ".zip")) != (request.Format == "zip") {
		return wire.Operation{}, fail("invalid_request")
	}
	fp := operationFingerprint(request)
	if result, ok, err := s.operationReplayed(request.OperationId, fp); err != nil || (ok && result.State != wire.OperationStatePending) {
		return result, err
	}
	if s.compatible.Load() {
		return wire.Operation{}, fail("compatibility_mode")
	}
	if !s.ready.Load() {
		return wire.Operation{}, fail("skill_migration_pending")
	}
	files, err := ParseImport(request.FileName, data)
	if err != nil {
		return wire.Operation{}, err
	}
	content := contentDigest(files)
	s.serial.Lock()
	defer s.serial.Unlock()
	var existing operation
	replayErr := s.get("operations", request.OperationId, &existing)
	if replayErr == nil {
		if existing.Fingerprint != fp {
			return wire.Operation{}, fail("operation_conflict")
		}
		if existing.Phase != "received" || existing.Target == nil {
			return existing.Result, nil
		}
		if err = materializeSource(s.root("yijie-user"), *existing.Target, files); err != nil {
			return existing.Result, err
		}
		existing.Phase = "source_prepared"
		err = s.saveOperation(&existing)
		s.signal()
		return existing.Result, err
	} else if ErrorCode(replayErr) != "skill_not_found" {
		return wire.Operation{}, replayErr
	}
	if s.compatible.Load() {
		return wire.Operation{}, fail("compatibility_mode")
	}
	if !s.ready.Load() {
		return wire.Operation{}, fail("skill_migration_pending")
	}
	entries, err := s.entries()
	if err != nil {
		return wire.Operation{}, err
	}
	var e entry
	found := false
	for _, candidate := range entries {
		if candidate.Origin == wire.OriginUser && candidate.Selected.ContentSha256 == content {
			e = candidate
			found = true
			break
		}
	}
	if !found {
		id := uuid.NewString()
		source := populateSource(wire.CatalogSource{Id: id, PluginName: "yj-user-" + strings.ReplaceAll(id, "-", ""), ContentVersion: "1.0.0", DisplayName: strings.TrimSuffix(request.FileName, filepath.Ext(request.FileName)), Description: "用户导入的技能", Category: "personal", IconKey: "skill", RequiredTools: []string{}}, files)
		e = entry{ID: id, Origin: wire.OriginUser, Selected: source, Authority: "native"}
	}
	if e.ActiveOperation != "" {
		return wire.Operation{}, fail("skill_busy")
	}
	before, err := s.readState(ctx, e)
	if err != nil {
		return wire.Operation{}, err
	}
	if found && before.CacheState == "valid" && before.UserConfigPresent && before.CacheVersion != nil && *before.CacheVersion == e.Selected.NativePackageVersion {
		op := operation{Result: wire.Operation{OperationId: request.OperationId, SkillId: e.ID, Epoch: e.Epoch, State: wire.OperationStateComplete}, Fingerprint: fp, Kind: "duplicate_import", Phase: "complete", CreatedAt: time.Now().UTC()}
		err = s.saveOperation(&op)
		return op.Result, err
	}
	if !absent(before) {
		return wire.Operation{}, fail("reconciliation_required")
	}
	if err = s.requireInstallationSlot(ctx, e.pluginID()); err != nil {
		return wire.Operation{}, err
	}
	e.Epoch++
	e.ActiveOperation = request.OperationId
	target := e.Selected
	op := operation{Result: wire.Operation{OperationId: request.OperationId, SkillId: e.ID, Epoch: e.Epoch, State: wire.OperationStatePending}, Fingerprint: fp, Kind: "import", Phase: "received", Target: &target, Before: &before, DesiredEnabled: true, OwnedInstall: true, InputDigest: request.Sha256, CreatedAt: time.Now().UTC()}
	err = s.db.Update(func(tx *bolt.Tx) error {
		if err := encodePut(tx, "entries", e.ID, e); err != nil {
			return err
		}
		return encodePut(tx, "operations", request.OperationId, op)
	})
	if err != nil {
		return wire.Operation{}, err
	}
	if err = materializeSource(s.root(e.market()), target, files); err != nil {
		return op.Result, err
	}
	op.Phase = "source_prepared"
	err = s.saveOperation(&op)
	s.signal()
	return op.Result, err
}

func (s *Service) Cancel(ctx context.Context, id string) (wire.Operation, error) {
	var op operation
	err := s.db.Update(func(tx *bolt.Tx) error {
		raw := tx.Bucket([]byte("operations")).Get([]byte(id))
		if raw == nil {
			return fail("skill_not_found")
		}
		if err := json.Unmarshal(raw, &op); err != nil {
			return err
		}
		if op.Result.State == wire.OperationStateComplete || op.Result.State == wire.OperationStateCancelled {
			return nil
		}
		op.Cancel = true
		op.Result.State = wire.OperationStatePending
		return encodePut(tx, "operations", id, op)
	})
	if err == nil {
		s.signal()
	}
	return op.Result, err
}
func absent(state native.PluginState) bool {
	return state.CacheState == "absent" && !state.UserConfigPresent
}
func (s *Service) readState(ctx context.Context, e entry) (native.PluginState, error) {
	states, err := s.config.Runtime.ReadSkillPluginStates(ctx, []string{e.pluginID()}, nil, "")
	if err != nil {
		return native.PluginState{}, err
	}
	if len(states) != 1 || states[0].PluginId != e.pluginID() {
		return native.PluginState{}, fail("native_state_unknown")
	}
	return states[0], nil
}
func (s *Service) saveEntry(e entry) error {
	return s.db.Update(func(tx *bolt.Tx) error { return encodePut(tx, "entries", e.ID, e) })
}
func (s *Service) execute(ctx context.Context, id string) error {
	var op operation
	if err := s.get("operations", id, &op); err != nil {
		return err
	}
	if op.Result.State != wire.OperationStatePending {
		return nil
	}
	if op.NativePluginID != "" {
		return s.executeOrphan(ctx, &op)
	}
	var e entry
	if err := s.get("entries", op.Result.SkillId, &e); err != nil {
		return err
	}
	if e.Epoch != op.Result.Epoch || e.ActiveOperation != id {
		return s.attention(&op, "skill_epoch_conflict")
	}
	err := s.config.Runtime.WithSkillMaintenance(ctx, func(maintenance context.Context) error {
		if err := s.get("operations", id, &op); err != nil {
			return err
		}
		if op.Cancel {
			return s.cancelOwned(maintenance, &op, &e)
		}
		before, err := s.readState(maintenance, e)
		if err != nil {
			return err
		}
		if op.Before == nil {
			op.Before = &before
			op.DesiredEnabled = before.EffectiveEnabled
			if before.UserEnabled != nil {
				op.DesiredEnabled = *before.UserEnabled
			}
			op.OwnedInstall = absent(before) && (op.Kind == "install" || op.Kind == "import")
			if op.OwnedInstall {
				op.DesiredEnabled = true
			}
			if err = s.saveOperation(&op); err != nil {
				return err
			}
		}
		if (op.Kind == "refresh" || op.Kind == "rollback") && op.Phase != "native_pending" && op.Phase != "verified" {
			if err := s.prepareRefreshRules(maintenance, &op); err != nil {
				return err
			}
		}
		if op.Target != nil && (op.Phase == "received" || op.Phase == "source_prepared") {
			files, err := readVerifiedFiles(filepath.Join(s.root(e.market()), "sources", op.Target.PluginName, op.Target.SourceRevision, "skills/main"), *op.Target)
			if err != nil {
				return fail("source_unavailable")
			}
			if err = materializeSource(s.root(e.market()), *op.Target, files); err != nil {
				return err
			}
			e.Selected = *op.Target
			e.Listed = true
			if err = s.saveEntry(e); err != nil {
				return err
			}
			if err = s.publishCatalog(e.market()); err != nil {
				return err
			}
			op.Phase = "catalog_prepared"
			if err = s.saveOperation(&op); err != nil {
				return err
			}
		}
		if op.Target != nil && op.Phase == "catalog_prepared" {
			detail, err := s.config.Runtime.ReadSkillPlugin(maintenance, s.root(e.market()), e.Selected.PluginName)
			if err != nil {
				var rejected *codex.RPCError
				if errors.As(err, &rejected) && (rejected.Code == -32600 || rejected.Code == -32602) {
					return fail("native_preflight_failed")
				}
				return err
			}
			if detail.Summary.Id != e.pluginID() || len(detail.Skills) != 1 || len(detail.McpServers) != 0 || len(detail.Hooks) != 0 || len(detail.Apps) != 0 || len(detail.AppTemplates) != 0 {
				return fail("native_preflight_failed")
			}
			if e.Origin == wire.OriginUser {
				_, e.DisplayName, _ = strings.Cut(detail.Skills[0].Name, ":")
				if e.DisplayName == "" {
					e.DisplayName = detail.Skills[0].Name
				}
				if metadata := detail.Skills[0].Interface; metadata != nil && metadata.DisplayName != nil {
					e.DisplayName = *metadata.DisplayName
				}
				e.Description = detail.Skills[0].Description
				if err = s.saveEntry(e); err != nil {
					return err
				}
			}
		}
		if op.Cancel {
			return s.cancelOwned(maintenance, &op, &e)
		}
		op.Phase = "native_pending"
		if err = s.saveOperation(&op); err != nil {
			return err
		}
		if op.Cancel {
			return s.cancelOwned(maintenance, &op, &e)
		}
		state, err := s.readState(maintenance, e)
		if err != nil {
			return err
		}
		switch op.Kind {
		case "import", "install":
			if absent(state) {
				err = s.config.Runtime.InstallSkillPlugin(maintenance, s.root(e.market()), e.Selected.PluginName)
			} else if state.CacheState == "valid" && state.CacheVersion != nil && *state.CacheVersion == e.Selected.NativePackageVersion {
				if !state.UserConfigPresent {
					if !op.OwnedInstall {
						return fail("reconciliation_required")
					}
					err = s.config.Runtime.SetSkillPluginEnabled(maintenance, e.pluginID(), op.DesiredEnabled)
				}
			} else {
				return fail("reconciliation_required")
			}
		case "enable", "disable":
			if state.CacheState != "valid" || !state.UserConfigPresent {
				return fail("reconciliation_required")
			}
			op.DesiredEnabled = op.Kind == "enable"
			if err = s.saveOperation(&op); err != nil {
				return err
			}
			err = s.config.Runtime.SetSkillPluginEnabled(maintenance, e.pluginID(), op.DesiredEnabled)
		case "uninstall":
			if !absent(state) {
				err = s.config.Runtime.UninstallSkillPlugin(maintenance, e.pluginID())
			}
		case "refresh", "rollback":
			if !state.UserConfigPresent {
				return fail("reconciliation_required")
			}
			if state.CacheVersion == nil || *state.CacheVersion != e.Selected.NativePackageVersion || !state.ManifestValid {
				err = s.config.Runtime.RefreshSkillPlugin(maintenance, e.pluginID(), s.root(e.market()), op.Kind == "rollback" || state.CacheState == "invalid")
			}
		default:
			return fail("operation_invalid")
		}
		if err != nil {
			return err
		}
		if err = s.config.Runtime.ReloadSkillPluginConfig(maintenance); err != nil {
			return err
		}
		state, err = s.readState(maintenance, e)
		if err != nil {
			return err
		}
		if op.Kind == "refresh" || op.Kind == "rollback" {
			if err = s.applyRefreshRules(maintenance, &op, state, false); err != nil {
				return err
			}
			state, err = s.readState(maintenance, e)
			if err != nil {
				return err
			}
			if op.Before != nil && len(op.Before.Skills) == 1 && len(state.Skills) == 1 && state.Skills[0].Enabled != op.Before.Skills[0].Enabled {
				return fail("refresh_effective_state_changed")
			}
		}
		if op.Kind == "uninstall" {
			if !absent(state) {
				return fail("reconciliation_required")
			}
			if e.Origin == wire.OriginUser || e.Offer == nil {
				e.Listed = false
				e.Published = false
			} else {
				e.Selected = *e.Offer
			}
			if err = s.saveEntry(e); err != nil {
				return err
			}
			if err = s.publishCatalog(e.market()); err != nil {
				return err
			}
		} else {
			if state.CacheState != "valid" || !state.ManifestValid || !state.UserConfigPresent || state.CacheVersion == nil || *state.CacheVersion != e.Selected.NativePackageVersion || state.UserEnabled == nil || *state.UserEnabled != op.DesiredEnabled || len(state.Skills) != 1 || len(state.Errors) != 0 {
				return fail("native_verification_pending")
			}
			if err = s.verifyVisibility(maintenance, &e, state); err != nil {
				return err
			}
		}
		op.Phase = "verified"
		if err = s.saveOperation(&op); err != nil {
			return err
		}
		if op.Cancel {
			return s.cancelOwned(maintenance, &op, &e)
		}
		if op.Target != nil && op.Previous != nil && op.Previous.SourceRevision != op.Target.SourceRevision {
			v := *op.Previous
			e.Previous = &v
		}
		return s.complete(&op, &e, wire.OperationStateComplete)
	})
	if err != nil {
		// The native outcome is unknown until the next inventory read. Keep the
		// durable intent and source; never compensate from an RPC error alone.
		code := ErrorCode(err)
		if code == "source_unavailable" && op.Kind == "import" && op.Phase == "received" {
			op.Result.ErrorCode = "source_reselection_required"
			return s.saveOperation(&op)
		}
		if code != "skill_runtime_unavailable" && code != "native_verification_pending" && code != "native_state_unknown" && code != "cancel_pending" {
			return s.attention(&op, code)
		}
		op.Result.ErrorCode = "reconciliation_pending"
		_ = s.saveOperation(&op)
	}
	return err
}
func (s *Service) verifyVisibility(ctx context.Context, e *entry, state native.PluginState) error {
	entries, err := s.config.Runtime.ListSkills(ctx, []string{s.config.DataRoot}, true)
	if err != nil {
		return err
	}
	if len(state.Skills) != 1 || state.Skills[0].Path == nil {
		return fail("native_verification_pending")
	}
	skill := state.Skills[0]
	count := 0
	for _, entry := range entries {
		for _, actual := range entry.Skills {
			if actual.Path == string(*skill.Path) && actual.Name == skill.Name {
				if actual.Enabled {
					count++
				}
				e.DeclaredTools = []string{}
				e.MCPDependencies = []string{}
				if actual.Dependencies != nil {
					for _, tool := range actual.Dependencies.Tools {
						e.DeclaredTools = append(e.DeclaredTools, tool.Value)
						if tool.Type == "mcp" {
							e.MCPDependencies = append(e.MCPDependencies, tool.Value)
						}
					}
				}
			}
		}
	}
	if state.EffectiveEnabled && skill.Enabled && count != 1 || (!state.EffectiveEnabled || !skill.Enabled) && count != 0 {
		return fail("native_verification_pending")
	}
	return nil
}
func (s *Service) attention(op *operation, code string) error {
	op.Result.State = wire.OperationStateNeedsAttention
	op.Result.ErrorCode = code
	return s.saveOperation(op)
}
func (s *Service) complete(op *operation, e *entry, state wire.OperationState) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		var current operation
		if err := json.Unmarshal(tx.Bucket([]byte("operations")).Get([]byte(op.Result.OperationId)), &current); err != nil {
			return err
		}
		if current.Cancel && state != wire.OperationStateCancelled && !op.CancelResolved {
			return fail("cancel_pending")
		}
		var currentEntry entry
		if err := json.Unmarshal(tx.Bucket([]byte("entries")).Get([]byte(e.ID)), &currentEntry); err != nil {
			return err
		}
		if currentEntry.Epoch != op.Result.Epoch || currentEntry.ActiveOperation != op.Result.OperationId {
			return fail("skill_epoch_conflict")
		}
		e.ActiveOperation = ""
		if state == wire.OperationStateComplete && op.Kind != "uninstall" {
			e.Published = true
		}
		op.Phase = "complete"
		op.Result.State = state
		op.Result.ErrorCode = ""
		if err := encodePut(tx, "entries", e.ID, e); err != nil {
			return err
		}
		return encodePut(tx, "operations", op.Result.OperationId, op)
	})
}
func (s *Service) cancelOwned(ctx context.Context, op *operation, e *entry) error {
	if e.Epoch != op.Result.Epoch {
		return fail("skill_epoch_conflict")
	}
	current, err := s.readState(ctx, *e)
	if err != nil {
		return err
	}
	if op.OwnedInstall && op.Before != nil && absent(*op.Before) {
		if !absent(current) {
			if current.CacheVersion != nil && *current.CacheVersion != e.Selected.NativePackageVersion {
				return fail("reconciliation_required")
			}
			if err = s.config.Runtime.UninstallSkillPlugin(ctx, e.pluginID()); err != nil {
				return err
			}
			current, err = s.readState(ctx, *e)
			if err != nil {
				return err
			}
		}
		if !absent(current) {
			return fail("reconciliation_required")
		}
		if e.Origin == wire.OriginUser || e.Offer == nil {
			e.Published = false
			e.Listed = false
		}
	} else if op.Phase == "native_pending" || op.Phase == "verified" {
		// Cancellation of changes to existing installations is an explicit restore,
		// never an uninstall. Content rollback uses the native refresh algorithm.
		if op.Kind == "uninstall" {
			if !absent(current) {
				return fail("reconciliation_required")
			}
			if e.Origin == wire.OriginUser || e.Offer == nil {
				e.Listed = false
				e.Published = false
			} else {
				e.Selected = *e.Offer
			}
			if err = s.saveEntry(*e); err != nil {
				return err
			}
			if err = s.publishCatalog(e.market()); err != nil {
				return err
			}
			op.CancelResolved = true
			return s.complete(op, e, wire.OperationStateComplete)
		}
		if op.Previous != nil && op.Previous.SourceRevision != e.Selected.SourceRevision {
			e.Selected = *op.Previous
			if err = s.saveEntry(*e); err != nil {
				return err
			}
			if err = s.publishCatalog(e.market()); err != nil {
				return err
			}
			if err = s.config.Runtime.RefreshSkillPlugin(ctx, e.pluginID(), s.root(e.market()), true); err != nil {
				return err
			}
		}
		if op.Before != nil && op.Before.UserEnabled != nil {
			if err = s.config.Runtime.SetSkillPluginEnabled(ctx, e.pluginID(), *op.Before.UserEnabled); err != nil {
				return err
			}
		}
		current, err = s.readState(ctx, *e)
		if err != nil {
			return err
		}
		if op.Rules != nil {
			if err = s.applyRefreshRules(ctx, op, current, true); err != nil {
				return err
			}
			current, err = s.readState(ctx, *e)
		}
		if err != nil {
			return err
		}
		if op.Before == nil || current.UserEnabled == nil || op.Before.UserEnabled == nil || *current.UserEnabled != *op.Before.UserEnabled || current.CacheVersion == nil || *current.CacheVersion != e.Selected.NativePackageVersion {
			return fail("reconciliation_required")
		}
	} else if op.Previous != nil {
		e.Selected = *op.Previous
	}
	if err = s.saveEntry(*e); err != nil {
		return err
	}
	if err = s.publishCatalog(e.market()); err != nil {
		return err
	}
	if err = s.config.Runtime.ReloadSkillPluginConfig(ctx); err != nil {
		return err
	}
	return s.complete(op, e, wire.OperationStateCancelled)
}

func (s *Service) updateSubmissionReadiness() {
	available := s.ready.Load()
	err := s.db.View(func(tx *bolt.Tx) error {
		available = available && tx.Bucket([]byte("pending")).Stats().KeyN == 0
		return nil
	})
	s.config.Runtime.SetSkillSubmissionReady(available && err == nil)
}

func (s *Service) mutateOrphan(ctx context.Context, request wire.Mutation, fp string) (wire.Operation, error) {
	if request.ExpectedEpoch != 0 || (request.Kind != wire.MutationKindDisable && request.Kind != wire.MutationKindUninstall) {
		return wire.Operation{}, fail("source_unavailable")
	}
	if err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("pending")).ForEach(func(key, _ []byte) error {
			var op operation
			if err := json.Unmarshal(tx.Bucket([]byte("operations")).Get(key), &op); err != nil {
				return err
			}
			if op.Result.SkillId == request.SkillId {
				return fail("skill_busy")
			}
			return nil
		})
	}); err != nil {
		return wire.Operation{}, err
	}
	inventory, err := s.config.Runtime.ReadSkillPluginStates(ctx, nil, []string{"yijie-builtin", "yijie-user"}, "")
	if err != nil {
		return wire.Operation{}, err
	}
	for _, state := range inventory {
		if orphanID(state.PluginId) != request.SkillId {
			continue
		}
		op := operation{Result: wire.Operation{OperationId: request.OperationId, SkillId: request.SkillId, State: wire.OperationStatePending}, Fingerprint: fp, Kind: string(request.Kind), Phase: "received", NativePluginID: state.PluginId, Before: &state, CreatedAt: time.Now().UTC()}
		if err = s.saveOperation(&op); err != nil {
			return wire.Operation{}, err
		}
		s.signal()
		return op.Result, nil
	}
	return wire.Operation{}, fail("skill_not_found")
}
func (s *Service) executeOrphan(ctx context.Context, op *operation) error {
	err := s.config.Runtime.WithSkillMaintenance(ctx, func(ctx context.Context) error {
		if err := s.get("operations", op.Result.OperationId, op); err != nil {
			return err
		}
		if op.Cancel && op.Phase == "received" {
			op.Result.State = wire.OperationStateCancelled
			return s.saveOperation(op)
		}
		if op.Cancel {
			states, err := s.config.Runtime.ReadSkillPluginStates(ctx, []string{op.NativePluginID}, nil, "")
			if err != nil {
				return err
			}
			if len(states) != 1 {
				return fail("native_state_unknown")
			}
			completed := op.Kind == "uninstall" && absent(states[0]) || op.Kind == "disable" && states[0].UserEnabled != nil && !*states[0].UserEnabled && !states[0].EffectiveEnabled
			if !completed {
				return fail("reconciliation_required")
			}
			op.Phase = "complete"
			op.Result.State = wire.OperationStateComplete
			op.Result.ErrorCode = "cancel_too_late"
			return s.saveOperation(op)
		}
		op.Phase = "native_pending"
		if err := s.saveOperation(op); err != nil {
			return err
		}
		var err error
		if op.Kind == "uninstall" {
			err = s.config.Runtime.UninstallSkillPlugin(ctx, op.NativePluginID)
		} else {
			err = s.config.Runtime.SetSkillPluginEnabled(ctx, op.NativePluginID, false)
		}
		if err != nil {
			return err
		}
		states, err := s.config.Runtime.ReadSkillPluginStates(ctx, []string{op.NativePluginID}, nil, "")
		if err != nil {
			return err
		}
		if len(states) != 1 {
			return fail("native_state_unknown")
		}
		if op.Kind == "uninstall" && !absent(states[0]) || op.Kind == "disable" && (states[0].EffectiveEnabled || states[0].UserEnabled == nil || *states[0].UserEnabled) {
			return fail("native_verification_pending")
		}
		op.Phase = "complete"
		op.Result.State = wire.OperationStateComplete
		op.Result.ErrorCode = ""
		return s.saveOperation(op)
	})
	if err != nil {
		code := ErrorCode(err)
		if code != "skill_runtime_unavailable" && code != "native_verification_pending" && code != "native_state_unknown" {
			return s.attention(op, code)
		}
	}
	return err
}
