package skills

import (
	"context"
	"errors"
	"io/fs"
	"time"

	expert "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
)

// ExpertDescriptor contains only trusted catalog identity. It is process-local,
// never accepted from a renderer or copied into the public Skill HTTP protocol.
type ExpertDescriptor struct {
	InstallationID  string
	CatalogRevision string
	Reference       expert.ExpertRef
}
type expertResourcePermitKey struct{}
type expertResourcePermit struct{ skillID, action string }

func expertResourceRollbackTimeout(ctx context.Context) time.Duration {
	if _, ok := ctx.Value(expertResourcePermitKey{}).(expertResourcePermit); ok {
		return 5 * time.Second
	}
	return rollbackRuntimeTimeout
}
func expertResourceMutationAllowed(ctx context.Context, skillID, action string) bool {
	p, ok := ctx.Value(expertResourcePermitKey{}).(expertResourcePermit)
	return ok && p.skillID == skillID && p.action == action
}

func (s *Service) ExpertCatalogRevision() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", errorWithCode(CodeRuntimeUnavailable, errors.New("managed resources are closed"))
	}
	current, e := s.loadCurrentCatalog()
	if e != nil {
		return "", e
	}
	if !isLowerSHA256(s.expertCatalogDigest) {
		return "", errorWithCode(CodeManifestInvalid, errors.New("expert authority unavailable"))
	}
	return current.revision, nil
}
func (s *Service) DescribeExpert(selection expert.ExpertSelection) (ExpertDescriptor, error) {
	if selection.Validate() != nil {
		return ExpertDescriptor{}, errorWithCode(CodeInvalidRequest, errors.New("expert selection unavailable"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ExpertDescriptor{}, errorWithCode(CodeRuntimeUnavailable, errors.New("managed resources are closed"))
	}
	current, e := s.loadCurrentCatalog()
	if e != nil {
		return ExpertDescriptor{}, e
	}
	for _, entry := range current.manifest.Skills {
		if !isExpertResource(entry) || entry.Expert == nil || entry.Expert.ExpertId != string(selection.ExpertId) {
			continue
		}
		if entry.Version != string(selection.Version) {
			return ExpertDescriptor{}, errorWithCode(CodeNotFound, errors.New("expert version unavailable"))
		}
		knowledge, e := expert.KnowledgeManifestDigest(entry.Expert.KnowledgeResources)
		if e != nil {
			return ExpertDescriptor{}, errorWithCode(CodeManifestInvalid, e)
		}
		reference := expert.ExpertRef{ExpertId: selection.ExpertId, Version: selection.Version, ArchiveSha256: expert.Digest(entry.Archive.SHA256), RoleSha256: expert.Digest(entry.Expert.RoleResource.Sha256), KnowledgeManifestSha256: expert.Digest(knowledge)}
		if reference.Validate() != nil {
			return ExpertDescriptor{}, errorWithCode(CodeManifestInvalid, errors.New("expert identity unavailable"))
		}
		return ExpertDescriptor{InstallationID: entry.ID, CatalogRevision: current.revision, Reference: reference}, nil
	}
	return ExpertDescriptor{}, errorWithCode(CodeNotFound, errors.New("expert selection unavailable"))
}

// Called only after the session adapter holds the shared operation mutex and
// verifies Native ownership plus all Host references. Public HTTP keeps using
// Install/Uninstall without this private, exact-resource authorization context.
func (s *Service) PrepareExpertResources(ctx context.Context, operationID string, selection expert.ExpertSelection) (State, error) {
	descriptor, e := s.DescribeExpert(selection)
	if e != nil {
		return State{}, e
	}
	ctx = context.WithValue(ctx, expertResourcePermitKey{}, expertResourcePermit{descriptor.InstallationID, "prepare"})
	return s.Install(ctx, InstallInput{OperationID: operationID, SkillID: descriptor.InstallationID, ExpectedVersion: string(descriptor.Reference.Version), ExpectedArchiveSHA256: string(descriptor.Reference.ArchiveSha256), CatalogRevision: descriptor.CatalogRevision})
}
func (s *Service) RemoveExpertResources(ctx context.Context, operationID string, selection expert.ExpertSelection) (State, error) {
	descriptor, e := s.DescribeExpert(selection)
	if e != nil {
		return State{}, e
	}
	ctx = context.WithValue(ctx, expertResourcePermitKey{}, expertResourcePermit{descriptor.InstallationID, "remove"})
	return s.Uninstall(ctx, UninstallInput{OperationID: operationID, SkillID: descriptor.InstallationID})
}

// CoreExpertOperation is a read-only observation of the existing install
// journal. It never reclaims, resumes, prepares or deletes an operation.
type CoreExpertOperation struct {
	Found, Complete, InProgress bool
	Version                     string
	ErrorCode                   ErrorCode
	Installed, Enabled          bool
}

func (s *Service) ReadExpertResourceOperation(operationID, installationID, action string) (CoreExpertOperation, error) {
	if validateOperationID(operationID) != nil || !validSkillID(installationID) || (action != "prepare" && action != "remove") {
		return CoreExpertOperation{}, errorWithCode(CodeInvalidRequest, errors.New("expert operation identity unavailable"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return CoreExpertOperation{}, errorWithCode(CodeRuntimeUnavailable, errors.New("managed resources are closed"))
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.readExpertResourceOperationLocked(operationID, installationID, action)
}

// Caller holds both mu and opMu. This observes the original journal only.
func (s *Service) readExpertResourceOperationLocked(operationID, installationID, action string) (CoreExpertOperation, error) {
	if s.operationDB != nil {
		s.releaseCompletedOperationCache()
		if e := s.cachePersistedOperation(operationID); e != nil {
			return CoreExpertOperation{}, errorWithCode(CodeScanFailed, e)
		}
	}
	record := s.operations[operationID]
	if record == nil {
		return CoreExpertOperation{}, nil
	}
	kind := "install"
	if action == "remove" {
		kind = "uninstall"
	}
	if record.SkillID != installationID || record.Kind != kind {
		return CoreExpertOperation{}, errorWithCode(CodeOperationConflict, errors.New("expert operation differs"))
	}
	out := CoreExpertOperation{Found: true, Complete: record.Status == "complete", InProgress: !channelClosed(record.done), ErrorCode: record.ErrorCode}
	if record.State != nil {
		out.Version = record.State.Version
		out.Installed = record.State.InstallationStatus == "installed"
		out.Enabled = record.State.Enabled
	}
	return out, nil
}

// CancelUnstartedExpertResource is used only by explicit recovery under the
// session adapter's operation mutex. It acquires the existing mutation lane and
// writes a completed rejection for an absent original ID. It never installs,
// removes, enables, or projects a resource. Existing core evidence is observed.
func (s *Service) CancelUnstartedExpertResource(operationID, installationID, action string) (CoreExpertOperation, error) {
	if validateOperationID(operationID) != nil || !validSkillID(installationID) || (action != "prepare" && action != "remove") {
		return CoreExpertOperation{}, errorWithCode(CodeInvalidRequest, errors.New("expert recovery identity unavailable"))
	}
	if !s.tryStartMutation() {
		return CoreExpertOperation{}, errorWithCode(CodeBusy, errors.New("managed resource mutation is active"))
	}
	defer s.finishMutation()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return CoreExpertOperation{}, errorWithCode(CodeRuntimeUnavailable, errors.New("managed resources are closed"))
	}
	// This private fingerprint deliberately cannot match a future install or
	// uninstall request. The original ID is permanently consumed as rejected.
	fingerprint := operationFingerprint("expert-unstarted-rejection", operationID, installationID, action)
	s.opMu.Lock()
	original, e := s.readExpertResourceOperationLocked(operationID, installationID, action)
	record := s.operations[operationID]
	// NewService returns only after canonical transaction recovery has durably
	// restored unfinished mutations. A closed owner in reserved phase with no
	// transaction files is therefore safe to end without another mutation.
	finishingRejection := e == nil && original.Found && !original.Complete && !original.InProgress && record.Phase == "reserved" && (record.ErrorCode == "" || record.Fingerprint == fingerprint)
	if finishingRejection {
		var quiescent bool
		quiescent, e = s.expertTransactionQuiescentLocked(operationID)
		finishingRejection = e == nil && quiescent
	}
	if finishingRejection {
		// Keep reservation and this terminal write under the same opMu. A late
		// old caller cannot reclaim the closed owner between these two checks.
		e = s.finishStateOperationLocked(record, State{}, errorWithCode(CodeOperationConflict, errors.New("explicit recovery ended a quiescent expert operation")), true)
		if e == nil {
			original, e = s.readExpertResourceOperationLocked(operationID, installationID, action)
		}
		s.opMu.Unlock()
		return original, e
	}
	s.opMu.Unlock()
	if e != nil || original.Found {
		return original, e
	}
	kind := "install"
	if action == "remove" {
		kind = "uninstall"
	}
	record, owner, e := s.reserveOperation(operationID, fingerprint, kind, installationID)
	if e != nil {
		return CoreExpertOperation{}, e
	}
	if owner {
		if e = s.finishStateOperation(record, State{}, errorWithCode(CodeOperationConflict, errors.New("explicit recovery rejected an unstarted expert operation")), true); e != nil {
			return CoreExpertOperation{}, e
		}
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.readExpertResourceOperationLocked(operationID, installationID, action)
}

func (s *Service) expertTransactionQuiescentLocked(operationID string) (bool, error) {
	entries, e := fs.ReadDir(s.managed.FS(), ".")
	if e != nil {
		return false, errorWithCode(CodeScanFailed, e)
	}
	stage := transactionName(".yijie-staging", "", operationID)
	for _, entry := range entries {
		if entry.Name() == stage {
			return false, nil
		}
		if _, id, ok := parseSkillTransactionName(entry.Name()); ok && id == operationID {
			return false, nil
		}
	}
	return true, nil
}
