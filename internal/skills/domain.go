package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

type ErrorCode string

const (
	CodeInvalidRequest        ErrorCode = "invalid_request"
	CodeNotFound              ErrorCode = "skill_not_found"
	CodeNotInstallable        ErrorCode = "skill_not_installable"
	CodeOperationConflict     ErrorCode = "skill_operation_conflict"
	CodeBusy                  ErrorCode = "skill_busy"
	CodeBundleMissing         ErrorCode = "bundle_missing"
	CodeManifestInvalid       ErrorCode = "bundle_manifest_invalid"
	CodeArchiveChecksum       ErrorCode = "archive_checksum_mismatch"
	CodeArchiveUnsafe         ErrorCode = "archive_unsafe"
	CodeArchiveTooLarge       ErrorCode = "archive_too_large"
	CodeInstallFailed         ErrorCode = "install_failed"
	CodeUninstallFailed       ErrorCode = "uninstall_failed"
	CodeScanFailed            ErrorCode = "scan_failed"
	CodeRuntimeUnavailable    ErrorCode = "runtime_unavailable"
	CodeRuntimeSyncFailed     ErrorCode = "runtime_sync_failed"
	CodeInstallReceiptInvalid ErrorCode = "install_receipt_invalid"
	CodeInstalledFilesMissing ErrorCode = "installed_files_missing"
	CodeInstalledFilesCorrupt ErrorCode = "installed_files_corrupt"
	CodeCapabilityUnavailable ErrorCode = "capability_unavailable"
)

type ServiceError struct {
	Code ErrorCode
	Err  error
}

func (e *ServiceError) Error() string {
	if e == nil {
		return "Skill operation failed"
	}
	return fmt.Sprintf("Skill operation failed: %s", e.Code)
}

func (e *ServiceError) Unwrap() error { return e.Err }

func errorWithCode(code ErrorCode, err error) error {
	if err == nil {
		err = errors.New(string(code))
	}
	return &ServiceError{Code: code, Err: err}
}

func ErrorCodeOf(err error) ErrorCode {
	var serviceError *ServiceError
	if errors.As(err, &serviceError) {
		return serviceError.Code
	}
	return CodeScanFailed
}

type State struct {
	ID                   string
	RuntimeName          string
	Version              string
	CatalogStatus        string
	CatalogBlockedReason string
	MaintenanceStatus    string
	CapabilityReadiness  string
	InstallationStatus   string
	Enabled              bool
	RuntimeVisible       bool
	FailureCode          string
}

type Snapshot struct {
	CatalogRevision string
	ScannedAt       time.Time
	Skills          []State
}

type InstallInput struct {
	OperationID           string
	SkillID               string
	ExpectedVersion       string
	ExpectedArchiveSHA256 string
	CatalogRevision       string
}

type ScanInput struct {
	OperationID string
	Reason      string
}

type EnabledInput struct {
	OperationID string
	SkillID     string
	Enabled     bool
}

type UninstallInput struct {
	OperationID string
	SkillID     string
}

// Runtime is the exact stable Skills projection exposed by the pinned Codex
// Runtime adapter. Service never sends paths supplied by an HTTP caller.
type Runtime interface {
	ListSkills(context.Context, []string, bool) ([]codex.SkillsListEntry, error)
	SetSkillsExtraRoots(context.Context, []string) error
	WriteSkillConfig(context.Context, string, bool) (bool, error)
}

type Config struct {
	BundleRoot  string
	ManagedRoot string
	Runtime     Runtime
	Now         func() time.Time
}

// HandleRuntimeNotification validates and coalesces skills/changed without
// issuing a Runtime request on the Runtime reader goroutine.
func (s *Service) HandleRuntimeNotification(params json.RawMessage) error {
	if err := codex.ValidateSkillsChangedNotification(params); err != nil {
		return err
	}
	s.SignalRuntimeChanged()
	return nil
}
