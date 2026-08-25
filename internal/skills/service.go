package skills

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/google/uuid"
)

const transactionSeparator = "--"

const rollbackRuntimeTimeout = 10 * time.Second

var validScanReasons = map[string]struct{}{
	"startup":           {},
	"page_open":         {},
	"app_upgrade":       {},
	"window_resume":     {},
	"directory_changed": {},
	"user_retry":        {},
}

type Service struct {
	mu                           sync.Mutex
	opMu                         sync.Mutex
	bundleRoot                   string
	managedRoot                  string
	bundle                       *os.Root
	managed                      *os.Root
	state                        *os.Root
	runtime                      Runtime
	now                          func() time.Time
	changed                      chan struct{}
	mutation                     chan struct{}
	operations                   map[string]*operationRecord
	operationStoreNeedsV1Rewrite bool
	persistOperationsFault       func(operationStore) error
	registeredRoots              []string
	rootsRegistered              bool
	syncManaged                  func(*os.Root) error
	syncSkill                    func(*os.Root) error
	closed                       bool
}

func NewService(config Config) (*Service, error) {
	if config.Runtime == nil {
		return nil, errorWithCode(CodeRuntimeUnavailable, errors.New("Runtime Skills API is unavailable"))
	}
	bundleRoot, err := canonicalDirectory(config.BundleRoot, false)
	if err != nil {
		return nil, errorWithCode(CodeBundleMissing, err)
	}
	managedRoot, err := canonicalDirectory(config.ManagedRoot, true)
	if err != nil {
		return nil, errorWithCode(CodeScanFailed, err)
	}
	if pathsOverlap(bundleRoot, managedRoot) {
		return nil, errorWithCode(CodeInvalidRequest, errors.New("bundle and managed roots must not overlap"))
	}
	bundleInfo, err := os.Stat(bundleRoot)
	if err != nil || bundleInfo.Mode().Perm()&0o022 != 0 {
		return nil, errorWithCode(CodeBundleMissing, errors.New("bundle root is writable by group or others"))
	}
	if err := validateDirectoryAuthority(bundleRoot, false); err != nil {
		return nil, errorWithCode(CodeBundleMissing, err)
	}
	if err := os.Chmod(managedRoot, 0o700); err != nil {
		return nil, errorWithCode(CodeScanFailed, err)
	}
	managedInfo, err := os.Stat(managedRoot)
	if err != nil || managedInfo.Mode().Perm()&0o077 != 0 {
		return nil, errorWithCode(CodeScanFailed, errors.New("managed root is not owner-only"))
	}
	if err := validateDirectoryAuthority(managedRoot, true); err != nil {
		return nil, errorWithCode(CodeScanFailed, err)
	}
	bundle, err := os.OpenRoot(bundleRoot)
	if err != nil {
		return nil, errorWithCode(CodeBundleMissing, err)
	}
	managed, err := os.OpenRoot(managedRoot)
	if err != nil {
		bundle.Close()
		return nil, errorWithCode(CodeScanFailed, err)
	}
	initialCatalog, err := loadCatalog(bundleRoot)
	if err != nil {
		bundle.Close()
		managed.Close()
		return nil, err
	}
	state, err := initializeStateRoot(managed)
	if err != nil {
		bundle.Close()
		managed.Close()
		return nil, errorWithCode(CodeScanFailed, err)
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	service := &Service{
		bundleRoot:  bundleRoot,
		managedRoot: managedRoot,
		bundle:      bundle,
		managed:     managed,
		state:       state,
		runtime:     config.Runtime,
		now:         now,
		changed:     make(chan struct{}, 1),
		mutation:    make(chan struct{}, 1),
		operations:  make(map[string]*operationRecord),
		syncManaged: syncRoot,
		syncSkill:   syncRoot,
	}
	if err := service.loadOperations(); err != nil {
		service.Close()
		return nil, errorWithCode(CodeScanFailed, err)
	}
	if err := service.migrateLegacyBlockedReasons(initialCatalog); err != nil {
		service.Close()
		return nil, errorWithCode(CodeScanFailed, err)
	}
	service.mu.Lock()
	err = service.recoverTransactionsLocked()
	service.mu.Unlock()
	if err != nil {
		service.Close()
		return nil, err
	}
	return service, nil
}

func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	bundleErr := s.bundle.Close()
	managedErr := s.managed.Close()
	stateErr := s.state.Close()
	return errors.Join(bundleErr, managedErr, stateErr)
}

func (s *Service) List(ctx context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reconcileLocked(ctx)
}

func (s *Service) Scan(ctx context.Context, input ScanInput) (Snapshot, error) {
	if err := validateOperationID(input.OperationID); err != nil {
		return Snapshot{}, err
	}
	if _, ok := validScanReasons[input.Reason]; !ok {
		return Snapshot{}, errorWithCode(CodeInvalidRequest, errors.New("scan reason is invalid"))
	}
	fingerprint := operationFingerprint("scan", input.OperationID, input.Reason)
	record, owner, err := s.reserveOperation(input.OperationID, fingerprint, "scan", "")
	if err != nil {
		return Snapshot{}, err
	}
	if !owner {
		return waitSnapshotOperation(ctx, record)
	}
	select {
	case s.mutation <- struct{}{}:
	case <-ctx.Done():
		_ = s.finishSnapshotOperation(record, Snapshot{}, ctx.Err(), false)
		return Snapshot{}, ctx.Err()
	default:
		err := errorWithCode(CodeBusy, errors.New("another Skill mutation is active"))
		if finishErr := s.finishSnapshotOperation(record, Snapshot{}, err, false); finishErr != nil {
			return Snapshot{}, finishErr
		}
		return Snapshot{}, err
	}
	defer func() { <-s.mutation }()
	s.mu.Lock()
	snapshot, scanErr := s.reconcileLocked(ctx)
	s.mu.Unlock()
	if finishErr := s.finishSnapshotOperation(record, snapshot, scanErr, true); finishErr != nil {
		// The journal result is the replay contract. If finalization fails, expose
		// that failure first so the owner and immediate same-ID callers observe
		// the same ErrorCode.
		scanErr = errors.Join(finishErr, scanErr)
	}
	return snapshot, scanErr
}

func (s *Service) Install(ctx context.Context, input InstallInput) (State, error) {
	if err := validateOperationID(input.OperationID); err != nil {
		return State{}, err
	}
	if !validSkillID(input.SkillID) || !semanticVersionPattern.MatchString(input.ExpectedVersion) ||
		!isLowerSHA256(input.ExpectedArchiveSHA256) || !isLowerSHA256(input.CatalogRevision) {
		return State{}, errorWithCode(CodeInvalidRequest, errors.New("install input is invalid"))
	}
	fingerprint := operationFingerprint(
		"install", input.OperationID, input.SkillID, input.ExpectedVersion,
		input.ExpectedArchiveSHA256, input.CatalogRevision,
	)
	record, owner, err := s.reserveOperation(input.OperationID, fingerprint, "install", input.SkillID)
	if err != nil {
		return State{}, err
	}
	if !owner {
		return waitStateOperation(ctx, record)
	}
	if !s.tryStartMutation() {
		err := errorWithCode(CodeBusy, errors.New("another Skill mutation is active"))
		if finishErr := s.finishStateOperation(record, State{}, err, false); finishErr != nil {
			return State{}, finishErr
		}
		return State{}, err
	}
	defer s.finishMutation()
	s.mu.Lock()
	state, installErr := s.installLocked(ctx, input, record)
	s.mu.Unlock()
	if finishErr := s.finishStateOperation(record, state, installErr, true); finishErr != nil {
		installErr = errors.Join(finishErr, installErr)
	}
	return state, installErr
}

func (s *Service) installLocked(ctx context.Context, input InstallInput, record *operationRecord) (State, error) {
	if err := s.recoverTransactionsLocked(); err != nil {
		return State{}, err
	}
	catalog, err := loadCatalog(s.bundleRoot)
	if err != nil {
		return State{}, err
	}
	skill, err := manifestSkill(catalog, input.SkillID)
	if err != nil {
		return State{}, err
	}
	if !installableSkill(skill) {
		return State{}, errorWithCode(CodeNotInstallable, errors.New("Skill is not installable"))
	}
	if input.ExpectedVersion != skill.Version ||
		subtle.ConstantTimeCompare([]byte(input.ExpectedArchiveSHA256), []byte(skill.Archive.SHA256)) != 1 ||
		subtle.ConstantTimeCompare([]byte(input.CatalogRevision), []byte(catalog.revision)) != 1 {
		return State{}, errorWithCode(CodeInvalidRequest, errors.New("install precondition does not match catalog"))
	}
	if _, err := s.reconcileCatalogLocked(ctx, catalog); err != nil {
		return State{}, err
	}

	previous := inspectInstallation(s.managed, skill)
	if previous.valid && previous.receipt.Version == skill.Version && previous.receipt.ArchiveSHA256 == skill.Archive.SHA256 {
		snapshot, err := s.reconcileCatalogLocked(ctx, catalog)
		if err != nil {
			return State{}, err
		}
		state := stateByID(snapshot, skill.ID)
		if err := s.commitStateOperation(record, state); err != nil {
			return State{}, err
		}
		return state, nil
	}

	stageName := transactionName(".yijie-staging", "", input.OperationID)
	backupName := transactionName(".yijie-backup", skill.ID, input.OperationID)
	failedName := transactionName(".yijie-failed", skill.ID, input.OperationID)
	_ = s.managed.RemoveAll(stageName)
	_ = s.managed.RemoveAll(backupName)
	_ = s.managed.RemoveAll(failedName)
	if _, err := extractArchive(s.bundle, s.managed, stageName, skill, catalog.revision); err != nil {
		return State{}, err
	}
	preserveEnabled := !previous.exists || previous.enabled
	if !preserveEnabled {
		stage, openErr := s.managed.OpenRoot(stageName)
		if openErr != nil {
			_ = s.managed.RemoveAll(stageName)
			return State{}, errorWithCode(CodeInstallFailed, openErr)
		}
		markerErr := writeOwnerOnlyFile(stage, disabledFileName, nil)
		if markerErr == nil {
			markerErr = syncRoot(stage)
		}
		stage.Close()
		if markerErr != nil {
			_ = s.managed.RemoveAll(stageName)
			return State{}, errorWithCode(CodeInstallFailed, markerErr)
		}
	}

	hadPrevious := previous.exists
	if err := s.updateOperationPhase(record, "committing", hadPrevious, previous.enabled); err != nil {
		_ = s.managed.RemoveAll(stageName)
		return State{}, err
	}
	if hadPrevious {
		if err := s.managed.Rename(skill.ID, backupName); err != nil {
			_ = s.managed.RemoveAll(stageName)
			return State{}, s.finishRollbackLocked(record, errorWithCode(CodeInstallFailed, err))
		}
	}
	if err := s.managed.Rename(stageName, skill.ID); err != nil {
		var rollbackErr error
		if hadPrevious {
			rollbackErr = s.managed.Rename(backupName, skill.ID)
		}
		_ = s.managed.RemoveAll(stageName)
		if rollbackErr != nil {
			return State{}, errorWithCode(CodeInstallFailed, errors.New("installation rollback failed"))
		}
		if rollbackErr = s.syncManagedRoot(); rollbackErr != nil {
			return State{}, errorWithCode(CodeInstallFailed, errors.New("installation rollback durability failed"))
		}
		return State{}, s.finishRollbackLocked(record, errorWithCode(CodeInstallFailed, err))
	}
	if err := s.syncManagedRoot(); err != nil {
		rollbackErr := s.rollbackInstallLocked(skill.ID, failedName, backupName, hadPrevious)
		if rollbackErr != nil {
			return State{}, errorWithCode(CodeInstallFailed, errors.New("installation rollback failed"))
		}
		return State{}, s.finishRollbackLocked(record, errorWithCode(CodeInstallFailed, err))
	}
	if err := s.updateOperationPhase(record, "filesystem_committed", hadPrevious, previous.enabled); err != nil {
		rollbackErr := s.rollbackInstallLocked(skill.ID, failedName, backupName, hadPrevious)
		if rollbackErr != nil {
			return State{}, errors.Join(err, rollbackErr)
		}
		return State{}, s.finishRollbackLocked(record, err)
	}

	snapshot, syncErr := s.reconcileCatalogLocked(ctx, catalog)
	if syncErr != nil {
		rollbackErr := s.rollbackInstallLocked(skill.ID, failedName, backupName, hadPrevious)
		if rollbackErr == nil {
			rollbackErr = s.reconcileRollbackLocked(catalog)
		}
		if rollbackErr != nil {
			return State{}, errorWithCode(CodeInstallFailed, errors.New("installation rollback failed"))
		}
		return State{}, s.finishRollbackLocked(record, syncErr)
	}
	state := stateByID(snapshot, skill.ID)
	if err := s.commitStateOperation(record, state); err != nil {
		rollbackErr := s.rollbackInstallLocked(skill.ID, failedName, backupName, hadPrevious)
		if rollbackErr == nil {
			rollbackErr = s.reconcileRollbackLocked(catalog)
		}
		if rollbackErr != nil {
			return State{}, errorWithCode(CodeInstallFailed, errors.New("installation rollback failed"))
		}
		return State{}, s.finishRollbackLocked(record, err)
	}
	if hadPrevious {
		// The completed success result is the durable commit point. Backup removal is
		// post-commit garbage collection: a failure must not turn an installed,
		// Runtime-visible version into a permanently replayed operation error.
		// recoverTransactionsLocked retries any confirmed backup left behind.
		if err := s.managed.RemoveAll(backupName); err == nil {
			_ = s.syncManagedRoot()
		}
	}
	return state, nil
}

func (s *Service) SetEnabled(ctx context.Context, input EnabledInput) (State, error) {
	if err := validateOperationID(input.OperationID); err != nil {
		return State{}, err
	}
	if !validSkillID(input.SkillID) {
		return State{}, errorWithCode(CodeInvalidRequest, errors.New("Skill id is invalid"))
	}
	fingerprint := operationFingerprint("enabled", input.OperationID, input.SkillID, fmt.Sprint(input.Enabled))
	record, owner, err := s.reserveOperation(input.OperationID, fingerprint, "enabled", input.SkillID)
	if err != nil {
		return State{}, err
	}
	if !owner {
		return waitStateOperation(ctx, record)
	}
	if !s.tryStartMutation() {
		err := errorWithCode(CodeBusy, errors.New("another Skill mutation is active"))
		if finishErr := s.finishStateOperation(record, State{}, err, false); finishErr != nil {
			return State{}, finishErr
		}
		return State{}, err
	}
	defer s.finishMutation()
	s.mu.Lock()
	state, enabledErr := s.setEnabledLocked(ctx, input, record)
	s.mu.Unlock()
	if finishErr := s.finishStateOperation(record, state, enabledErr, true); finishErr != nil {
		enabledErr = errors.Join(finishErr, enabledErr)
	}
	return state, enabledErr
}

func (s *Service) setEnabledLocked(ctx context.Context, input EnabledInput, record *operationRecord) (State, error) {
	if err := s.recoverTransactionsLocked(); err != nil {
		return State{}, err
	}
	catalog, err := loadCatalog(s.bundleRoot)
	if err != nil {
		return State{}, err
	}
	skill, err := manifestSkill(catalog, input.SkillID)
	if err != nil {
		return State{}, err
	}
	if _, err := s.reconcileCatalogLocked(ctx, catalog); err != nil {
		return State{}, err
	}
	installed := inspectInstallation(s.managed, skill)
	if !installed.exists {
		return State{}, errorWithCode(CodeInvalidRequest, errors.New("Skill is not installed"))
	}
	if !installed.valid {
		return State{}, errorWithCode(CodeInvalidRequest, errors.New("installed Skill cannot be enabled"))
	}
	if input.Enabled && !installableSkill(skill) {
		return State{}, errorWithCode(CodeNotInstallable, errors.New("Skill is not installable"))
	}
	if installed.enabled != input.Enabled {
		if err := s.updateOperationPhase(record, "committing", true, installed.enabled); err != nil {
			return State{}, err
		}
		if err := s.setDisabledMarkerLocked(skill.ID, !input.Enabled); err != nil {
			if rollbackErr := s.restoreEnabledStateLocked(catalog, skill.ID, installed.enabled, record); rollbackErr != nil {
				return State{}, errorWithCode(CodeInstallFailed, errors.Join(err, errors.New("enabled state rollback failed"), rollbackErr))
			}
			return State{}, errorWithCode(CodeInstallFailed, err)
		}
	}
	snapshot, syncErr := s.reconcileCatalogLocked(ctx, catalog)
	if syncErr != nil {
		if installed.enabled != input.Enabled {
			if rollbackErr := s.restoreEnabledStateLocked(catalog, skill.ID, installed.enabled, record); rollbackErr != nil {
				return State{}, errorWithCode(CodeInstallFailed, errors.New("enabled state rollback failed"))
			}
		}
		return State{}, syncErr
	}
	state := stateByID(snapshot, skill.ID)
	if err := s.commitStateOperation(record, state); err != nil {
		if installed.enabled != input.Enabled {
			if rollbackErr := s.restoreEnabledStateLocked(catalog, skill.ID, installed.enabled, record); rollbackErr != nil {
				return State{}, errorWithCode(CodeInstallFailed, errors.New("enabled state rollback failed"))
			}
		}
		return State{}, err
	}
	return state, nil
}

func (s *Service) Uninstall(ctx context.Context, input UninstallInput) (State, error) {
	if err := validateOperationID(input.OperationID); err != nil {
		return State{}, err
	}
	if !validSkillID(input.SkillID) {
		return State{}, errorWithCode(CodeInvalidRequest, errors.New("Skill id is invalid"))
	}
	fingerprint := operationFingerprint("uninstall", input.OperationID, input.SkillID)
	record, owner, err := s.reserveOperation(input.OperationID, fingerprint, "uninstall", input.SkillID)
	if err != nil {
		return State{}, err
	}
	if !owner {
		return waitStateOperation(ctx, record)
	}
	if !s.tryStartMutation() {
		err := errorWithCode(CodeBusy, errors.New("another Skill mutation is active"))
		if finishErr := s.finishStateOperation(record, State{}, err, false); finishErr != nil {
			return State{}, finishErr
		}
		return State{}, err
	}
	defer s.finishMutation()
	s.mu.Lock()
	state, uninstallErr := s.uninstallLocked(ctx, input, record)
	s.mu.Unlock()
	if finishErr := s.finishStateOperation(record, state, uninstallErr, true); finishErr != nil {
		uninstallErr = errors.Join(finishErr, uninstallErr)
	}
	return state, uninstallErr
}

func (s *Service) uninstallLocked(ctx context.Context, input UninstallInput, record *operationRecord) (State, error) {
	if err := s.recoverTransactionsLocked(); err != nil {
		return State{}, err
	}
	catalog, err := loadCatalog(s.bundleRoot)
	if err != nil {
		return State{}, err
	}
	skill, err := manifestSkill(catalog, input.SkillID)
	if err != nil {
		return State{}, err
	}
	if _, err := s.reconcileCatalogLocked(ctx, catalog); err != nil {
		return State{}, err
	}
	installed := inspectInstallation(s.managed, skill)
	if !installed.exists {
		snapshot, err := s.reconcileCatalogLocked(ctx, catalog)
		if err != nil {
			return State{}, err
		}
		state := stateByID(snapshot, skill.ID)
		if err := s.commitStateOperation(record, state); err != nil {
			return State{}, err
		}
		return state, nil
	}
	if err := s.updateOperationPhase(record, "committing", true, installed.enabled); err != nil {
		return State{}, err
	}

	// A valid Skill is disabled and confirmed absent from the model-visible
	// projection before its directory is moved. Unsafe/corrupt paths are never
	// sent to Runtime and are removed from the root before the forced reload.
	if installed.valid {
		if _, ok := safeEntrypointPath(s.managedRoot, s.managed, skill); ok {
			entrypoint := filepath.Join(s.managedRoot, skill.ID, skill.Entrypoint)
			_, installations := inspectCatalog(s.managed, catalog)
			roots, rootsErr := s.runtimeRootsLocked(catalog, installations)
			if rootsErr != nil {
				return State{}, s.rollbackUninstallLocked(catalog, record, "", skill.ID, false, rootsErr)
			}
			if err := s.setRuntimeRootsLocked(ctx, roots); err != nil {
				return State{}, s.rollbackUninstallLocked(catalog, record, "", skill.ID, false, err)
			}
			if _, err := s.runtime.WriteSkillConfig(ctx, entrypoint, false); err != nil {
				cause := errorWithCode(CodeRuntimeSyncFailed, err)
				return State{}, s.rollbackUninstallLocked(catalog, record, "", skill.ID, false, cause)
			}
			if err := s.confirmRuntimeDisabled(ctx, entrypoint, skill.RuntimeName); err != nil {
				return State{}, s.rollbackUninstallLocked(catalog, record, "", skill.ID, false, err)
			}
		}
	}
	trashName := transactionName(".yijie-trash", skill.ID, input.OperationID)
	_ = s.managed.RemoveAll(trashName)
	if err := s.managed.Rename(skill.ID, trashName); err != nil {
		cause := errorWithCode(CodeUninstallFailed, err)
		return State{}, s.rollbackUninstallLocked(catalog, record, trashName, skill.ID, false, cause)
	}
	if err := s.syncManagedRoot(); err != nil {
		cause := errorWithCode(CodeUninstallFailed, err)
		return State{}, s.rollbackUninstallLocked(catalog, record, trashName, skill.ID, true, cause)
	}
	if err := s.updateOperationPhase(record, "filesystem_committed", true, installed.enabled); err != nil {
		return State{}, s.rollbackUninstallLocked(catalog, record, trashName, skill.ID, true, err)
	}
	snapshot, syncErr := s.reconcileCatalogLocked(ctx, catalog)
	if syncErr != nil {
		return State{}, s.rollbackUninstallLocked(catalog, record, trashName, skill.ID, true, syncErr)
	}
	state := stateByID(snapshot, skill.ID)
	if err := s.commitStateOperation(record, state); err != nil {
		return State{}, s.rollbackUninstallLocked(catalog, record, trashName, skill.ID, true, err)
	}
	// Runtime confirmation is also the uninstall commit point. Trash cleanup is
	// retryable garbage collection; reporting failure after the target is gone
	// would make the idempotency journal disagree with filesystem/Runtime truth.
	if err := s.managed.RemoveAll(trashName); err == nil {
		_ = s.syncManagedRoot()
	}
	return state, nil
}

func (s *Service) Run(ctx context.Context) {
	attempt := 0
	pending := true
	for {
		if pending {
			if _, err := s.List(ctx); err == nil {
				attempt = 0
				pending = false
			} else {
				delay := operationRetryDelay(attempt)
				attempt++
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-s.changed:
					if !timer.Stop() {
						<-timer.C
					}
				case <-timer.C:
				}
				continue
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-s.changed:
			pending = true
		}
	}
}

func (s *Service) SignalRuntimeChanged() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

func (s *Service) reconcileLocked(ctx context.Context) (Snapshot, error) {
	if s.closed {
		return Snapshot{}, errorWithCode(CodeRuntimeUnavailable, errors.New("Skill service is closed"))
	}
	if err := s.recoverTransactionsLocked(); err != nil {
		return Snapshot{}, err
	}
	catalog, err := loadCatalog(s.bundleRoot)
	if err != nil {
		return Snapshot{}, err
	}
	return s.reconcileCatalogLocked(ctx, catalog)
}

func (s *Service) reconcileCatalogLocked(ctx context.Context, catalog catalog) (Snapshot, error) {
	states, installations := inspectCatalog(s.managed, catalog)
	if err := s.syncRuntimeLocked(ctx, catalog, states, installations); err != nil {
		for index := range states {
			states[index].RuntimeVisible = false
			if states[index].InstallationStatus == "installed" && states[index].FailureCode == "" {
				states[index].FailureCode = string(ErrorCodeOf(err))
			}
		}
		return Snapshot{}, err
	}
	return Snapshot{CatalogRevision: catalog.revision, ScannedAt: s.now().UTC(), Skills: states}, nil
}

func inspectCatalog(managed *os.Root, catalog catalog) ([]State, map[string]installation) {
	states := make([]State, 0, len(catalog.manifest.Skills))
	installations := make(map[string]installation, len(catalog.manifest.Skills))
	for _, skill := range catalog.manifest.Skills {
		installed := inspectInstallation(managed, skill)
		installations[skill.ID] = installed
		state := State{
			ID:                   skill.ID,
			RuntimeName:          skill.RuntimeName,
			Version:              skill.Version,
			CatalogStatus:        skill.Release.CatalogStatus,
			CatalogBlockedReason: catalogBlockedReason(skill),
			MaintenanceStatus:    skill.Release.MaintenanceStatus,
			CapabilityReadiness:  capabilityReadiness(skill),
			InstallationStatus:   "not_installed",
		}
		if installed.exists {
			state.InstallationStatus = "error"
			state.FailureCode = string(installed.failure)
			if installed.valid {
				state.InstallationStatus = "installed"
				state.FailureCode = ""
				state.Enabled = installed.enabled
				state.Version = installed.receipt.Version
			}
		}
		states = append(states, state)
	}
	return states, installations
}

func (s *Service) syncRuntimeLocked(
	ctx context.Context,
	catalog catalog,
	states []State,
	installations map[string]installation,
) error {
	if s.runtime == nil {
		return errorWithCode(CodeRuntimeUnavailable, errors.New("Runtime Skills API is unavailable"))
	}
	roots, err := s.runtimeRootsLocked(catalog, installations)
	if err != nil {
		return err
	}
	if err := s.setRuntimeRootsLocked(ctx, roots); err != nil {
		return err
	}
	desired := make(map[string]bool, len(catalog.manifest.Skills))
	expectedPaths := make(map[string]ManifestSkill, len(catalog.manifest.Skills))
	expectedPathByID := make(map[string]string, len(catalog.manifest.Skills))
	for _, skill := range catalog.manifest.Skills {
		installed := installations[skill.ID]
		enabled := installed.valid && installed.enabled && installableSkill(skill)
		desired[skill.ID] = enabled
		entrypoint := filepath.Join(s.managedRoot, skill.ID, skill.Entrypoint)
		if !installed.valid {
			continue
		}
		expectedPaths[entrypoint] = skill
		expectedPathByID[skill.ID] = entrypoint
		if _, safe := safeEntrypointPath(s.managedRoot, s.managed, skill); !safe {
			return errorWithCode(CodeRuntimeSyncFailed, errors.New("validated Skill entrypoint became unsafe"))
		}
		if _, err := s.runtime.WriteSkillConfig(ctx, entrypoint, enabled); err != nil {
			return errorWithCode(CodeRuntimeSyncFailed, err)
		}
	}
	entries, err := s.runtime.ListSkills(ctx, []string{s.managedRoot}, true)
	if err != nil {
		return errorWithCode(CodeRuntimeSyncFailed, err)
	}
	visible := make(map[string]bool, len(expectedPaths))
	seenExpected := make(map[string]bool, len(expectedPaths))
	unexpectedEnabled := false
	for _, entry := range entries {
		for _, runtimeErr := range entry.Errors {
			if pathInside(s.managedRoot, runtimeErr.Path) {
				return errorWithCode(CodeRuntimeSyncFailed, errors.New("Runtime reported a managed Skill load error"))
			}
		}
		for _, runtimeSkill := range entry.Skills {
			skill, expected := expectedPaths[runtimeSkill.Path]
			if !expected {
				if pathInside(s.managedRoot, runtimeSkill.Path) && runtimeSkill.Enabled {
					if _, writeErr := s.runtime.WriteSkillConfig(ctx, runtimeSkill.Path, false); writeErr != nil {
						return errorWithCode(CodeRuntimeSyncFailed, writeErr)
					}
					unexpectedEnabled = true
				}
				continue
			}
			if seenExpected[runtimeSkill.Path] || runtimeSkill.Name != skill.RuntimeName ||
				runtimeSkill.Enabled != desired[skill.ID] {
				return errorWithCode(CodeRuntimeSyncFailed, errors.New("Runtime managed Skill projection is inconsistent"))
			}
			seenExpected[runtimeSkill.Path] = true
			visible[skill.ID] = runtimeSkill.Enabled
		}
	}
	if unexpectedEnabled {
		return errorWithCode(CodeRuntimeSyncFailed, errors.New("Runtime exposed an unmanaged Skill"))
	}
	for index := range states {
		if desired[states[index].ID] && !seenExpected[expectedPathByID[states[index].ID]] {
			return errorWithCode(CodeRuntimeSyncFailed, errors.New("Runtime did not project an enabled managed Skill"))
		}
		states[index].RuntimeVisible = desired[states[index].ID] && visible[states[index].ID]
	}
	return nil
}

func (s *Service) runtimeRootsLocked(catalog catalog, installations map[string]installation) ([]string, error) {
	roots := make([]string, 0, len(catalog.manifest.Skills))
	for _, skill := range catalog.manifest.Skills {
		if !installations[skill.ID].valid {
			continue
		}
		entrypoint, safe := safeEntrypointPath(s.managedRoot, s.managed, skill)
		expected := filepath.Join(s.managedRoot, skill.ID, skill.Entrypoint)
		if !safe || entrypoint != expected {
			return nil, errorWithCode(CodeRuntimeSyncFailed, errors.New("validated Skill entrypoint became unsafe"))
		}
		roots = append(roots, filepath.Join(s.managedRoot, skill.ID))
	}
	sort.Strings(roots)
	return roots, nil
}

func (s *Service) setRuntimeRootsLocked(ctx context.Context, roots []string) error {
	if s.rootsRegistered && slices.Equal(s.registeredRoots, roots) {
		return nil
	}
	if err := s.runtime.SetSkillsExtraRoots(ctx, roots); err != nil {
		// The Runtime may have applied the request before a timeout/transport
		// failure. Invalidate the cache so compensation and retries always resend
		// the desired roots instead of trusting an ambiguous local snapshot.
		s.registeredRoots = nil
		s.rootsRegistered = false
		return errorWithCode(CodeRuntimeSyncFailed, err)
	}
	s.registeredRoots = append(s.registeredRoots[:0], roots...)
	s.rootsRegistered = true
	return nil
}

func (s *Service) syncManagedRoot() error {
	return s.syncManaged(s.managed)
}

func (s *Service) rollbackInstallLocked(skillID, failedName, backupName string, hadPrevious bool) error {
	if err := s.managed.Rename(skillID, failedName); err != nil {
		return err
	}
	if hadPrevious {
		if err := s.managed.Rename(backupName, skillID); err != nil {
			return err
		}
	}
	if err := s.managed.RemoveAll(failedName); err != nil {
		return err
	}
	return s.syncManagedRoot()
}

func (s *Service) restoreTrashLocked(trashName, skillID string) error {
	if err := s.managed.Rename(trashName, skillID); err != nil {
		return err
	}
	return s.syncManagedRoot()
}

func (s *Service) finishRollbackLocked(record *operationRecord, cause error) error {
	if err := s.updateOperationPhase(record, "reserved", false, false); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (s *Service) rollbackUninstallLocked(
	current catalog,
	record *operationRecord,
	trashName string,
	skillID string,
	movedToTrash bool,
	cause error,
) error {
	if movedToTrash {
		if err := s.restoreTrashLocked(trashName, skillID); err != nil {
			return errorWithCode(CodeUninstallFailed, errors.Join(errors.New("uninstall rollback failed"), cause, err))
		}
	}
	if err := s.reconcileRollbackLocked(current); err != nil {
		return errorWithCode(CodeUninstallFailed, errors.Join(errors.New("uninstall Runtime rollback failed"), cause, err))
	}
	return s.finishRollbackLocked(record, cause)
}

func (s *Service) reconcileRollbackLocked(current catalog) error {
	rollbackContext, cancel := context.WithTimeout(context.Background(), rollbackRuntimeTimeout)
	defer cancel()
	_, err := s.reconcileCatalogLocked(rollbackContext, current)
	return err
}

func (s *Service) confirmRuntimeDisabled(ctx context.Context, entrypoint, runtimeName string) error {
	entries, err := s.runtime.ListSkills(ctx, []string{s.managedRoot}, true)
	if err != nil {
		return errorWithCode(CodeRuntimeSyncFailed, err)
	}
	for _, entry := range entries {
		for _, runtimeErr := range entry.Errors {
			if pathInside(s.managedRoot, runtimeErr.Path) {
				return errorWithCode(CodeRuntimeSyncFailed, errors.New("Runtime reported a managed Skill load error"))
			}
		}
		for _, skill := range entry.Skills {
			if skill.Path == entrypoint && (skill.Name != runtimeName || skill.Enabled) {
				return errorWithCode(CodeRuntimeSyncFailed, errors.New("Runtime still exposes the Skill being uninstalled"))
			}
		}
	}
	return nil
}

func (s *Service) tryStartMutation() bool {
	select {
	case s.mutation <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Service) finishMutation() {
	<-s.mutation
}

func (s *Service) setDisabledMarkerLocked(skillID string, disabled bool) error {
	skillRoot, err := s.managed.OpenRoot(skillID)
	if err != nil {
		return err
	}
	defer skillRoot.Close()
	if disabled {
		if info, err := skillRoot.Lstat(disabledFileName); err == nil {
			if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Size() != 0 {
				return errors.New("disabled marker is invalid")
			}
			// A previous create may have succeeded while its directory fsync
			// failed. Retry the durability boundary before recovery clears the
			// journal phase.
			return s.syncSkill(skillRoot)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := writeOwnerOnlyFile(skillRoot, disabledFileName, nil); err != nil {
			return err
		}
		return s.syncSkill(skillRoot)
	}
	if err := skillRoot.Remove(disabledFileName); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return s.syncSkill(skillRoot)
}

func (s *Service) restoreEnabledStateLocked(
	current catalog,
	skillID string,
	enabled bool,
	record *operationRecord,
) error {
	if err := s.setDisabledMarkerLocked(skillID, !enabled); err != nil {
		return err
	}
	if err := s.reconcileRollbackLocked(current); err != nil {
		return err
	}
	return s.updateOperationPhase(record, "reserved", false, false)
}

func (s *Service) recoverTransactionsLocked() error {
	s.opMu.Lock()
	records := make(map[string]operationRecord, len(s.operations))
	for id, record := range s.operations {
		records[id] = *record
	}
	s.opMu.Unlock()

	managedChanged := false
	managedNeedsSync := false
	entries, err := fs.ReadDir(s.managed.FS(), ".")
	if err != nil {
		return errorWithCode(CodeScanFailed, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case validStageTransactionName(name):
			if err := s.managed.RemoveAll(name); err != nil {
				return errorWithCode(CodeScanFailed, err)
			}
			managedChanged = true
		case strings.HasPrefix(name, ".yijie-failed"+transactionSeparator):
			if _, _, ok := parseSkillTransactionName(name); ok {
				if err := s.managed.RemoveAll(name); err != nil {
					return errorWithCode(CodeScanFailed, err)
				}
				managedChanged = true
			}
		case strings.HasPrefix(name, ".yijie-trash"+transactionSeparator):
			skillID, operationID, ok := parseSkillTransactionName(name)
			if !ok {
				continue
			}
			record, exists := records[operationID]
			confirmed := exists && transactionConfirmed(&record)
			if confirmed {
				if err := s.managed.RemoveAll(name); err != nil {
					return errorWithCode(CodeScanFailed, err)
				}
				managedChanged = true
			} else if _, targetErr := s.managed.Lstat(skillID); errors.Is(targetErr, os.ErrNotExist) {
				if err := s.managed.Rename(name, skillID); err != nil {
					return errorWithCode(CodeScanFailed, err)
				}
				managedChanged = true
			} else if targetErr == nil {
				if err := s.managed.RemoveAll(name); err != nil {
					return errorWithCode(CodeScanFailed, err)
				}
				managedChanged = true
			} else {
				return errorWithCode(CodeScanFailed, targetErr)
			}
		case strings.HasPrefix(name, ".yijie-backup"+transactionSeparator):
			skillID, operationID, ok := parseSkillTransactionName(name)
			if !ok {
				continue
			}
			record, exists := records[operationID]
			confirmed := exists && transactionConfirmed(&record)
			if confirmed {
				if err := s.managed.RemoveAll(name); err != nil {
					return errorWithCode(CodeScanFailed, err)
				}
				managedChanged = true
			} else if _, targetErr := s.managed.Lstat(skillID); errors.Is(targetErr, os.ErrNotExist) {
				if err := s.managed.Rename(name, skillID); err != nil {
					return errorWithCode(CodeScanFailed, err)
				}
				managedChanged = true
			} else if targetErr == nil {
				if err := s.managed.RemoveAll(skillID); err != nil {
					return errorWithCode(CodeScanFailed, err)
				}
				if err := s.managed.Rename(name, skillID); err != nil {
					return errorWithCode(CodeScanFailed, err)
				}
				managedChanged = true
			} else {
				return errorWithCode(CodeScanFailed, targetErr)
			}
		}
	}

	// If a first install reached its filesystem commit without a previous
	// version, no backup name exists. A restart before Runtime confirmation
	// conservatively removes that unconfirmed copy. Enabled marker changes are
	// likewise restored to their previous desired state.
	for _, record := range records {
		if record.Status != "in_progress" || record.Phase == "reserved" || record.Phase == "runtime_confirmed" {
			continue
		}
		switch record.Kind {
		case "install":
			// A previous rollback may already have restored the target namespace
			// before its managed-root fsync failed. Even if no transaction name is
			// left to mutate on this pass, retry that durability boundary before
			// clearing the journal phase.
			managedNeedsSync = true
			if !record.HadPrevious {
				if err := s.managed.RemoveAll(record.SkillID); err != nil {
					return errorWithCode(CodeScanFailed, err)
				}
				managedChanged = true
			}
		case "uninstall":
			// The inverse rename may likewise have completed before its directory
			// fsync failed, leaving no trash name for this recovery pass to see.
			managedNeedsSync = true
		case "enabled":
			if err := s.setDisabledMarkerLocked(record.SkillID, !record.PreviousEnabled); err != nil &&
				!errors.Is(err, os.ErrNotExist) {
				return errorWithCode(CodeScanFailed, err)
			}
		}
	}

	// Make every filesystem recovery durable before clearing its journal phase.
	// If this fsync fails, the original phase remains available for the next
	// List/restart and every recovery action above is idempotent.
	if managedChanged || managedNeedsSync {
		if err := s.syncManagedRoot(); err != nil {
			return errorWithCode(CodeScanFailed, err)
		}
	}

	s.opMu.Lock()
	for _, record := range s.operations {
		if record.Status == "in_progress" {
			if record.ErrorCode != "" {
				record.Status = "complete"
				record.Phase = "complete"
				record.CompletedAt = s.now().UTC()
			} else {
				record.Phase = "reserved"
			}
			record.HadPrevious = false
			record.PreviousEnabled = false
		}
	}
	persistErr := s.persistOperationsLocked()
	s.opMu.Unlock()
	if persistErr != nil {
		return errorWithCode(CodeScanFailed, persistErr)
	}
	return nil
}

func transactionConfirmed(record *operationRecord) bool {
	return record != nil &&
		((record.Status == "complete" && record.ErrorCode == "") || record.Phase == "runtime_confirmed")
}

func operationFingerprint(parts ...string) string {
	digest := sha256.New()
	for _, part := range parts {
		fmt.Fprintf(digest, "%d:%s;", len(part), part)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func validateOperationID(value string) error {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed == uuid.Nil || parsed.String() != value {
		return errorWithCode(CodeInvalidRequest, errors.New("operation id is invalid"))
	}
	return nil
}

func capabilityReadiness(skill ManifestSkill) string {
	if skill.Capabilities.ExecutionMode == "model-only" && skill.Capabilities.Network == "none" &&
		skill.Capabilities.Filesystem == "none" && len(skill.Capabilities.RequiredTools) == 0 {
		return "ready"
	}
	if installableSkill(skill) {
		return "degraded"
	}
	return "blocked"
}

func installableSkill(skill ManifestSkill) bool {
	return skill.Release.CatalogStatus == "installable" && catalogEntryMode(skill) == "bundled" &&
		skill.Entrypoint == "SKILL.md" && skill.Archive.Path != "" &&
		skill.Provenance.ReviewStatus == "verified" && skill.License.RedistributionStatus == "verified" &&
		(skill.License.AuthorizationScope == "local-development" || skill.License.AuthorizationScope == "desktop-distribution")
}

func catalogBlockedReason(skill ManifestSkill) string {
	if skill.Release.CatalogStatus == "installable" {
		return ""
	}
	if skill.Release.BlockedReason != "" {
		return skill.Release.BlockedReason
	}
	if skill.Provenance.ReviewStatus != "verified" {
		return "source_unverified"
	}
	if skill.License.RedistributionStatus != "verified" {
		return "license_unverified"
	}
	if skill.License.AuthorizationScope == "none" {
		return "distribution_not_authorized"
	}
	if skill.Release.MaintenanceStatus == "unmaintained" {
		return "maintenance_ended"
	}
	if capabilityReadiness(skill) == "blocked" {
		return "capability_unavailable"
	}
	return "security_review_pending"
}

func stateByID(snapshot Snapshot, id string) State {
	for _, state := range snapshot.Skills {
		if state.ID == id {
			return state
		}
	}
	return State{ID: id, InstallationStatus: "not_installed"}
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Skills = append([]State(nil), snapshot.Skills...)
	return snapshot
}

func pathsOverlap(first, second string) bool {
	return pathInside(first, second) || pathInside(second, first)
}

func pathInside(root, candidate string) bool {
	if root == "" || candidate == "" || !filepath.IsAbs(candidate) {
		return false
	}
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func transactionName(prefix, skillID, operationID string) string {
	if skillID == "" {
		return prefix + transactionSeparator + operationID
	}
	return prefix + transactionSeparator + skillID + transactionSeparator + operationID
}

func validStageTransactionName(name string) bool {
	prefix := ".yijie-staging" + transactionSeparator
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	operationID := strings.TrimPrefix(name, prefix)
	parsed, err := uuid.Parse(operationID)
	return err == nil && parsed != uuid.Nil && parsed.String() == strings.ToLower(operationID)
}

func parseSkillTransactionName(name string) (string, string, bool) {
	parts := strings.Split(name, transactionSeparator)
	if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	parsed, err := uuid.Parse(parts[2])
	if err != nil || parsed == uuid.Nil || parsed.String() != strings.ToLower(parts[2]) {
		return "", "", false
	}
	if !validSkillID(parts[1]) {
		return "", "", false
	}
	return parts[1], parts[2], true
}

func validSkillID(value string) bool {
	if len(value) < 3 || len(value) > 128 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	lastPunctuation := false
	for _, character := range value {
		letterOrDigit := character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
		punctuation := character == '.' || character == '-'
		if !letterOrDigit && !punctuation || punctuation && lastPunctuation {
			return false
		}
		lastPunctuation = punctuation
	}
	return !lastPunctuation
}

func validRuntimeName(value string) bool {
	if len(value) < 1 || len(value) > 64 ||
		!((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= '0' && value[0] <= '9')) {
		return false
	}
	lastHyphen := false
	for _, character := range value {
		letterOrDigit := character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
		hyphen := character == '-'
		if !letterOrDigit && !hyphen || hyphen && lastHyphen {
			return false
		}
		lastHyphen = hyphen
	}
	return !lastHyphen
}

func sortedStates(states []State) []State {
	copyStates := append([]State(nil), states...)
	sort.SliceStable(copyStates, func(i, j int) bool { return copyStates[i].ID < copyStates[j].ID })
	return copyStates
}

var _ Runtime = (*codex.Manager)(nil)
