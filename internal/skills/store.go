package skills

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"time"
)

const (
	stateDirectoryName     = ".yijie-state"
	operationsFileName     = "operations.json"
	maxOperations          = 4096
	maxOperationsFileBytes = 64 << 20
)

type operationRecord struct {
	ID              string
	Fingerprint     string
	Kind            string
	SkillID         string
	Status          string
	Phase           string
	HadPrevious     bool
	PreviousEnabled bool
	State           *State
	Snapshot        *Snapshot
	ErrorCode       ErrorCode
	CompletedAt     time.Time
	done            chan struct{}
}

type operationStore struct {
	SchemaVersion int                  `json:"schema_version"`
	Operations    []persistedOperation `json:"operations"`
}

type persistedOperation struct {
	ID              string               `json:"id"`
	Fingerprint     string               `json:"fingerprint"`
	Kind            string               `json:"kind"`
	SkillID         string               `json:"skill_id"`
	Status          string               `json:"status"`
	Phase           string               `json:"phase"`
	HadPrevious     bool                 `json:"had_previous"`
	PreviousEnabled bool                 `json:"previous_enabled"`
	State           *persistedStateV1    `json:"state,omitempty"`
	Snapshot        *persistedSnapshotV1 `json:"snapshot,omitempty"`
	ErrorCode       ErrorCode            `json:"error_code"`
	CompletedAt     *time.Time           `json:"completed_at,omitempty"`
}

// persistedStateV1 intentionally mirrors the pre-Manifest-v2 journal shape.
// CatalogBlockedReason is accepted only to migrate journals written by 0.5.1
// development builds; it is never emitted. Keeping schema_version 1 readable by
// the previous Host makes an application rollback safe after any v2 operation.
type persistedStateV1 struct {
	ID                   string
	RuntimeName          string
	Version              string
	CatalogStatus        string
	CatalogBlockedReason *string `json:",omitempty"`
	MaintenanceStatus    string
	CapabilityReadiness  string
	InstallationStatus   string
	Enabled              bool
	RuntimeVisible       bool
	FailureCode          string
}

type persistedSnapshotV1 struct {
	CatalogRevision string
	ScannedAt       time.Time
	Skills          []persistedStateV1
}

func initializeStateRoot(managed *os.Root) (*os.Root, error) {
	info, err := managed.Lstat(stateDirectoryName)
	if errors.Is(err, os.ErrNotExist) {
		if err := managed.Mkdir(stateDirectoryName, 0o700); err != nil {
			return nil, err
		}
		info, err = managed.Lstat(stateDirectoryName)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("Skill state directory is not owner-only")
	}
	return managed.OpenRoot(stateDirectoryName)
}

func (s *Service) loadOperations() error {
	raw, err := s.state.ReadFile(operationsFileName)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || len(raw) == 0 || len(raw) > maxOperationsFileBytes {
		return errors.New("Skill operation journal is invalid")
	}
	info, err := s.state.Lstat(operationsFileName)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return errors.New("Skill operation journal is not owner-only")
	}
	var stored operationStore
	if err := strictJSON(raw, &stored); err != nil || stored.SchemaVersion != 1 || len(stored.Operations) > maxOperations {
		return errors.New("Skill operation journal is invalid")
	}
	for _, persisted := range stored.Operations {
		if err := validatePersistedOperation(persisted); err != nil {
			return err
		}
		if _, duplicate := s.operations[persisted.ID]; duplicate {
			return errors.New("Skill operation journal contains duplicate ids")
		}
		if persistedContainsBlockedReason(persisted) {
			s.operationStoreNeedsV1Rewrite = true
		}
		record := &operationRecord{
			ID:              persisted.ID,
			Fingerprint:     persisted.Fingerprint,
			Kind:            persisted.Kind,
			SkillID:         persisted.SkillID,
			Status:          persisted.Status,
			Phase:           persisted.Phase,
			HadPrevious:     persisted.HadPrevious,
			PreviousEnabled: persisted.PreviousEnabled,
			State:           stateFromPersistedV1(persisted.State),
			Snapshot:        snapshotFromPersistedV1(persisted.Snapshot),
			ErrorCode:       persisted.ErrorCode,
			CompletedAt:     dereferenceTime(persisted.CompletedAt),
			done:            make(chan struct{}),
		}
		// No operation from a previous process is still executing. Completed
		// records replay immediately; in-progress records are claimed and safely
		// resumed by the first matching request.
		close(record.done)
		s.operations[record.ID] = record
	}
	return nil
}

// Contracts 0.5.0 operation journals could contain successful blocked catalog
// projections without a reason. Contracts 0.5.1 requires one on every HTTP
// projection, so upgrade the durable replay value once during startup instead
// of accepting the journal and later turning an idempotent replay into HTTP 500.
func (s *Service) migrateLegacyBlockedReasons(current catalog) error {
	for _, record := range s.operations {
		backfillLegacyBlockedReason(record.State, current)
		if record.Snapshot == nil {
			continue
		}
		for index := range record.Snapshot.Skills {
			backfillLegacyBlockedReason(&record.Snapshot.Skills[index], current)
		}
	}
	if !s.operationStoreNeedsV1Rewrite {
		return nil
	}
	return s.persistOperationsLocked()
}

func backfillLegacyBlockedReason(state *State, current catalog) bool {
	if state == nil || state.CatalogStatus != "blocked" || state.CatalogBlockedReason != "" {
		return false
	}
	reason := "maintenance_ended"
	if skill, exists := current.byID[state.ID]; exists {
		reason = catalogBlockedReason(skill)
		if reason == "" {
			reason = "security_review_pending"
		}
	}
	state.CatalogBlockedReason = reason
	return true
}

func validatePersistedOperation(operation persistedOperation) error {
	if validateOperationID(operation.ID) != nil || !isLowerSHA256(operation.Fingerprint) {
		return errors.New("Skill operation journal identity is invalid")
	}
	switch operation.Kind {
	case "scan":
		if operation.SkillID != "" {
			return errors.New("Skill scan operation journal is invalid")
		}
	case "install", "enabled", "uninstall":
		if !validSkillID(operation.SkillID) {
			return errors.New("Skill mutation operation journal is invalid")
		}
	default:
		return errors.New("Skill operation journal kind is invalid")
	}
	if operation.Status != "in_progress" && operation.Status != "complete" {
		return errors.New("Skill operation journal status is invalid")
	}
	if !validOperationPhase(operation.Status, operation.Phase) {
		return errors.New("Skill operation journal phase is invalid")
	}
	if operation.Status == "in_progress" && operation.CompletedAt != nil {
		return errors.New("Skill in-progress operation has a completion time")
	}
	// The previous Host recovery resets every in-progress phase to reserved but
	// preserves unknown fields such as the v2 ambiguous-result ErrorCode. Accept
	// that downgrade-safe transitional shape; current recovery immediately
	// finalizes it as a replayable completed error.
	if operation.Status == "in_progress" && operation.ErrorCode != "" && !validErrorCode(operation.ErrorCode) {
		return errors.New("Skill in-progress operation error is invalid")
	}
	if operation.Status == "complete" {
		if operation.CompletedAt == nil || operation.CompletedAt.IsZero() {
			return errors.New("Skill operation journal completion time is invalid")
		}
		if operation.ErrorCode != "" && !validErrorCode(operation.ErrorCode) {
			return errors.New("Skill operation journal error code is invalid")
		}
		if operation.ErrorCode == "" && operation.Kind == "scan" && operation.Snapshot == nil {
			return errors.New("Skill scan operation journal result is missing")
		}
		if operation.ErrorCode == "" && operation.Kind != "scan" && operation.State == nil {
			return errors.New("Skill mutation operation journal result is missing")
		}
		if operation.Snapshot != nil && !validSnapshot(*snapshotFromPersistedV1(operation.Snapshot)) {
			return errors.New("Skill scan operation journal result is invalid")
		}
		if operation.State != nil && !validState(*stateFromPersistedV1(operation.State)) {
			return errors.New("Skill mutation operation journal result is invalid")
		}
	}
	return nil
}

func validOperationPhase(status, phase string) bool {
	if status == "complete" {
		return phase == "complete"
	}
	switch phase {
	case "reserved", "committing", "filesystem_committed", "runtime_confirmed":
		return true
	default:
		return false
	}
}

func validErrorCode(code ErrorCode) bool {
	switch code {
	case CodeInvalidRequest, CodeNotFound, CodeNotInstallable, CodeOperationConflict, CodeBusy,
		CodeBundleMissing, CodeManifestInvalid, CodeArchiveChecksum, CodeArchiveUnsafe, CodeArchiveTooLarge,
		CodeInstallFailed, CodeUninstallFailed, CodeScanFailed, CodeRuntimeUnavailable, CodeRuntimeSyncFailed,
		CodeInstallReceiptInvalid, CodeInstalledFilesMissing, CodeInstalledFilesCorrupt, CodeCapabilityUnavailable:
		return true
	default:
		return false
	}
}

func validSnapshot(snapshot Snapshot) bool {
	if !isLowerSHA256(snapshot.CatalogRevision) || snapshot.ScannedAt.IsZero() || len(snapshot.Skills) > 256 {
		return false
	}
	seen := make(map[string]struct{}, len(snapshot.Skills))
	for _, state := range snapshot.Skills {
		if !validState(state) {
			return false
		}
		if _, duplicate := seen[state.ID]; duplicate {
			return false
		}
		seen[state.ID] = struct{}{}
	}
	return true
}

func validState(state State) bool {
	if !validSkillID(state.ID) || !validRuntimeName(state.RuntimeName) || !semanticVersionPattern.MatchString(state.Version) {
		return false
	}
	if state.CatalogStatus != "installable" && state.CatalogStatus != "blocked" {
		return false
	}
	// Empty is accepted only for operation journals written before Contracts
	// 0.5.1. Fresh v2 catalog projections always populate blocked reasons, and
	// the HTTP adapter rejects a blocked response without one.
	if state.CatalogStatus == "installable" && state.CatalogBlockedReason != "" {
		return false
	}
	if state.CatalogBlockedReason != "" && !validCatalogBlockedReason(state.CatalogBlockedReason) {
		return false
	}
	if state.MaintenanceStatus != "maintained" && state.MaintenanceStatus != "unmaintained" {
		return false
	}
	if state.CapabilityReadiness != "ready" && state.CapabilityReadiness != "degraded" && state.CapabilityReadiness != "blocked" {
		return false
	}
	switch state.InstallationStatus {
	case "not_installed", "installing", "installed", "uninstalling", "error":
	default:
		return false
	}
	if state.FailureCode != "" && !validStateFailureCode(state.FailureCode) {
		return false
	}
	if state.RuntimeVisible && (!state.Enabled || state.InstallationStatus != "installed") {
		return false
	}
	if state.InstallationStatus == "not_installed" && (state.Enabled || state.RuntimeVisible) {
		return false
	}
	return true
}

func validCatalogBlockedReason(reason string) bool {
	switch reason {
	case "source_unverified", "license_unverified", "distribution_not_authorized",
		"security_review_pending", "capability_unavailable", "maintenance_ended":
		return true
	default:
		return false
	}
}

func validStateFailureCode(code string) bool {
	switch ErrorCode(code) {
	case CodeBundleMissing, CodeManifestInvalid, CodeArchiveChecksum, CodeArchiveUnsafe, CodeArchiveTooLarge,
		CodeInstallReceiptInvalid, CodeInstalledFilesMissing, CodeInstalledFilesCorrupt, CodeCapabilityUnavailable,
		CodeRuntimeUnavailable, CodeRuntimeSyncFailed, CodeInstallFailed, CodeUninstallFailed, CodeScanFailed:
		return true
	default:
		return false
	}
}

func (s *Service) persistOperationsLocked() error {
	stored := operationStore{SchemaVersion: 1, Operations: make([]persistedOperation, 0, len(s.operations))}
	ids := make([]string, 0, len(s.operations))
	for id := range s.operations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		record := s.operations[id]
		stored.Operations = append(stored.Operations, persistedOperation{
			ID:              record.ID,
			Fingerprint:     record.Fingerprint,
			Kind:            record.Kind,
			SkillID:         record.SkillID,
			Status:          record.Status,
			Phase:           record.Phase,
			HadPrevious:     record.HadPrevious,
			PreviousEnabled: record.PreviousEnabled,
			State:           stateToPersistedV1(record.State),
			Snapshot:        snapshotToPersistedV1(record.Snapshot),
			ErrorCode:       record.ErrorCode,
			CompletedAt:     timePointer(record.CompletedAt),
		})
	}
	if s.persistOperationsFault != nil {
		if err := s.persistOperationsFault(stored); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	if len(raw)+1 > maxOperationsFileBytes {
		return errors.New("Skill operation journal is full")
	}
	raw = append(raw, '\n')
	temporary := operationsFileName + ".tmp"
	_ = s.state.Remove(temporary)
	if err := writeOwnerOnlyFile(s.state, temporary, raw); err != nil {
		return err
	}
	if err := s.state.Rename(temporary, operationsFileName); err != nil {
		_ = s.state.Remove(temporary)
		return err
	}
	if err := syncRoot(s.state); err != nil {
		return err
	}
	s.operationStoreNeedsV1Rewrite = false
	return nil
}

func persistedContainsBlockedReason(operation persistedOperation) bool {
	if operation.State != nil && operation.State.CatalogBlockedReason != nil {
		return true
	}
	if operation.Snapshot != nil {
		for index := range operation.Snapshot.Skills {
			if operation.Snapshot.Skills[index].CatalogBlockedReason != nil {
				return true
			}
		}
	}
	return false
}

func stateFromPersistedV1(state *persistedStateV1) *State {
	if state == nil {
		return nil
	}
	return &State{
		ID: state.ID, RuntimeName: state.RuntimeName, Version: state.Version,
		CatalogStatus: state.CatalogStatus, MaintenanceStatus: state.MaintenanceStatus,
		CapabilityReadiness: state.CapabilityReadiness, InstallationStatus: state.InstallationStatus,
		Enabled: state.Enabled, RuntimeVisible: state.RuntimeVisible, FailureCode: state.FailureCode,
	}
}

func snapshotFromPersistedV1(snapshot *persistedSnapshotV1) *Snapshot {
	if snapshot == nil {
		return nil
	}
	converted := &Snapshot{CatalogRevision: snapshot.CatalogRevision, ScannedAt: snapshot.ScannedAt, Skills: make([]State, len(snapshot.Skills))}
	for index := range snapshot.Skills {
		converted.Skills[index] = *stateFromPersistedV1(&snapshot.Skills[index])
	}
	return converted
}

func stateToPersistedV1(state *State) *persistedStateV1 {
	if state == nil {
		return nil
	}
	return &persistedStateV1{
		ID: state.ID, RuntimeName: state.RuntimeName, Version: state.Version,
		CatalogStatus: state.CatalogStatus, MaintenanceStatus: state.MaintenanceStatus,
		CapabilityReadiness: state.CapabilityReadiness, InstallationStatus: state.InstallationStatus,
		Enabled: state.Enabled, RuntimeVisible: state.RuntimeVisible, FailureCode: state.FailureCode,
	}
}

func snapshotToPersistedV1(snapshot *Snapshot) *persistedSnapshotV1 {
	if snapshot == nil {
		return nil
	}
	converted := &persistedSnapshotV1{CatalogRevision: snapshot.CatalogRevision, ScannedAt: snapshot.ScannedAt, Skills: make([]persistedStateV1, len(snapshot.Skills))}
	for index := range snapshot.Skills {
		converted.Skills[index] = *stateToPersistedV1(&snapshot.Skills[index])
	}
	return converted
}

func (s *Service) reserveOperation(id, fingerprint, kind, skillID string) (*operationRecord, bool, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if existing := s.operations[id]; existing != nil {
		if existing.Fingerprint != fingerprint {
			return nil, false, errorWithCode(CodeOperationConflict, errors.New("operation id was reused with different input"))
		}
		if existing.Status == "in_progress" && channelClosed(existing.done) {
			// An ambiguous failed generation is immutable until recovery converts it
			// to a replayable completed error. This keeps existing joiners and an
			// immediate same-ID caller on the same result without racing field reset.
			if existing.ErrorCode != "" || existing.Phase != "reserved" {
				return existing, false, nil
			}
			existing.done = make(chan struct{})
			existing.State = nil
			existing.Snapshot = nil
			existing.ErrorCode = ""
			existing.CompletedAt = time.Time{}
			return existing, true, nil
		}
		return existing, false, nil
	}
	if len(s.operations) >= maxOperations {
		// Never evict an idempotency key: Contracts 0.5.1 defines no expiry window. New
		// operations fail closed at the bounded storage limit while every old ID
		// continues to replay or conflict deterministically.
		return nil, false, errorWithCode(CodeBusy, errors.New("Skill operation journal is full"))
	}
	record := &operationRecord{
		ID:          id,
		Fingerprint: fingerprint,
		Kind:        kind,
		SkillID:     skillID,
		Status:      "in_progress",
		Phase:       "reserved",
		done:        make(chan struct{}),
	}
	s.operations[id] = record
	if err := s.persistOperationsLocked(); err != nil {
		delete(s.operations, id)
		return nil, false, errorWithCode(CodeScanFailed, err)
	}
	return record, true, nil
}

func (s *Service) updateOperationPhase(record *operationRecord, phase string, hadPrevious, previousEnabled bool) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	oldPhase := record.Phase
	oldHadPrevious := record.HadPrevious
	oldPreviousEnabled := record.PreviousEnabled
	record.Phase = phase
	record.HadPrevious = hadPrevious
	record.PreviousEnabled = previousEnabled
	if err := s.persistOperationsLocked(); err != nil {
		record.Phase = oldPhase
		record.HadPrevious = oldHadPrevious
		record.PreviousEnabled = oldPreviousEnabled
		return errorWithCode(CodeScanFailed, err)
	}
	return nil
}

// commitStateOperation makes the successful result and commit decision one
// durable journal write. Callers keep rollback material until it succeeds, so
// a journal failure can still restore filesystem and Runtime state before an
// error response is returned.
func (s *Service) commitStateOperation(record *operationRecord, state State) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	oldStatus := record.Status
	oldPhase := record.Phase
	oldState := record.State
	oldErrorCode := record.ErrorCode
	oldCompletedAt := record.CompletedAt
	copyState := state
	record.Status = "complete"
	record.Phase = "complete"
	record.State = &copyState
	record.ErrorCode = ""
	record.CompletedAt = s.now().UTC()
	if err := s.persistOperationsLocked(); err != nil {
		record.Status = oldStatus
		record.Phase = oldPhase
		record.State = oldState
		record.ErrorCode = oldErrorCode
		record.CompletedAt = oldCompletedAt
		return errorWithCode(CodeScanFailed, err)
	}
	return nil
}

func (s *Service) finishStateOperation(record *operationRecord, state State, operationErr error, keep bool) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if !keep {
		delete(s.operations, record.ID)
		persistErr := s.persistOperationsLocked()
		closeOnce(record.done)
		if persistErr != nil {
			return errorWithCode(CodeScanFailed, persistErr)
		}
		return nil
	}
	if record.Status == "complete" && operationErr == nil {
		closeOnce(record.done)
		return nil
	}
	// A failed mutation whose durable phase is past reserved still has an
	// ambiguous filesystem or Runtime commit. Preserve its in-progress phase so
	// recovery can restore the previous state; marking it complete would make a
	// leftover backup/trash look like committed garbage on restart.
	if operationErr != nil && record.Status == "in_progress" && record.Phase != "reserved" {
		record.ErrorCode = ErrorCodeOf(operationErr)
		if err := s.persistOperationsLocked(); err != nil {
			record.ErrorCode = CodeScanFailed
			closeOnce(record.done)
			return errorWithCode(CodeScanFailed, err)
		}
		closeOnce(record.done)
		return nil
	}
	oldStatus := record.Status
	oldPhase := record.Phase
	oldCompletedAt := record.CompletedAt
	record.Status = "complete"
	record.Phase = "complete"
	record.CompletedAt = s.now().UTC()
	if operationErr == nil {
		copyState := state
		record.State = &copyState
	} else {
		record.ErrorCode = ErrorCodeOf(operationErr)
	}
	persistErr := s.persistOperationsLocked()
	if persistErr != nil {
		record.Status = oldStatus
		record.Phase = oldPhase
		record.CompletedAt = oldCompletedAt
		record.State = nil
		record.ErrorCode = CodeScanFailed
		closeOnce(record.done)
		return errorWithCode(CodeScanFailed, persistErr)
	}
	closeOnce(record.done)
	return nil
}

func (s *Service) finishSnapshotOperation(record *operationRecord, snapshot Snapshot, operationErr error, keep bool) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if !keep {
		delete(s.operations, record.ID)
		persistErr := s.persistOperationsLocked()
		closeOnce(record.done)
		if persistErr != nil {
			return errorWithCode(CodeScanFailed, persistErr)
		}
		return nil
	}
	oldStatus := record.Status
	oldPhase := record.Phase
	oldCompletedAt := record.CompletedAt
	record.Status = "complete"
	record.Phase = "complete"
	record.CompletedAt = s.now().UTC()
	if operationErr == nil {
		copySnapshot := cloneSnapshot(snapshot)
		record.Snapshot = &copySnapshot
	} else {
		record.ErrorCode = ErrorCodeOf(operationErr)
	}
	persistErr := s.persistOperationsLocked()
	if persistErr != nil {
		record.Status = oldStatus
		record.Phase = oldPhase
		record.CompletedAt = oldCompletedAt
		record.Snapshot = nil
		record.ErrorCode = CodeScanFailed
		closeOnce(record.done)
		return errorWithCode(CodeScanFailed, persistErr)
	}
	closeOnce(record.done)
	return nil
}

func waitStateOperation(ctx context.Context, record *operationRecord) (State, error) {
	select {
	case <-ctx.Done():
		return State{}, ctx.Err()
	case <-record.done:
		if record.ErrorCode != "" {
			return State{}, errorWithCode(record.ErrorCode, errors.New("replayed Skill operation failed"))
		}
		if record.State == nil {
			return State{}, errorWithCode(CodeScanFailed, errors.New("Skill operation result is missing"))
		}
		return *record.State, nil
	}
}

func waitSnapshotOperation(ctx context.Context, record *operationRecord) (Snapshot, error) {
	select {
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	case <-record.done:
		if record.ErrorCode != "" {
			return Snapshot{}, errorWithCode(record.ErrorCode, errors.New("replayed Skill operation failed"))
		}
		if record.Snapshot == nil {
			return Snapshot{}, errorWithCode(CodeScanFailed, errors.New("Skill operation result is missing"))
		}
		return cloneSnapshot(*record.Snapshot), nil
	}
}

func channelClosed(channel <-chan struct{}) bool {
	select {
	case <-channel:
		return true
	default:
		return false
	}
}

func closeOnce(channel chan struct{}) {
	if !channelClosed(channel) {
		close(channel)
	}
}

func cloneStatePointer(state *State) *State {
	if state == nil {
		return nil
	}
	copyState := *state
	return &copyState
}

func cloneSnapshotPointer(snapshot *Snapshot) *Snapshot {
	if snapshot == nil {
		return nil
	}
	copySnapshot := cloneSnapshot(*snapshot)
	return &copySnapshot
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copyValue := value
	return &copyValue
}

func dereferenceTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func operationRetryDelay(attempt int) time.Duration {
	delay := time.Duration(1<<min(attempt, 5)) * 100 * time.Millisecond
	return min(delay, 3*time.Second)
}
