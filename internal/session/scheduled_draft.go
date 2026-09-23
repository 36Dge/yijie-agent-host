package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/scheduledraft"
	bolt "go.etcd.io/bbolt"
)

const purposeOrdinary = "ordinary"
const purposeDraft = "scheduled_plan_draft"

var ErrDraftPurpose = errors.New("session purpose conflicts")
var ErrDraftStorageDisabled = errors.New("draft candidate storage disabled")
var ErrDraftPolicy = errors.New("effective draft policy unqualified")

type draftIdentity struct{ workspace string }

// Native constructor only; the ordinary launcher never selects this writer.
func WithScheduledDraftStorage() StoreOption {
	return func(s *Store) error { s.draftWriter = true; return nil }
}
func (s *Store) prepareDraftFormat(tx *bolt.Tx) error {
	v := string(tx.Bucket(metadataBucket).Get(storeSchemaVersionKey))
	if v == "6" {
		return tx.Bucket(sessionsBucket).ForEach(func(_, b []byte) error {
			var r Record
			if json.Unmarshal(b, &r) != nil {
				return ErrDraftPurpose
			}
			return validateStoredPurpose(tx, &r)
		})
	}
	if !s.draftWriter {
		return nil
	}
	if v != "5" || s.feat126ProjectDirectory != "" {
		return ErrDraftStorageDisabled
	}
	b := tx.Bucket(sessionsBucket)
	var records []Record
	if e := b.ForEach(func(_, data []byte) error {
		var r Record
		if json.Unmarshal(data, &r) != nil {
			return ErrDraftPurpose
		}
		if r.Purpose != "" && r.Purpose != purposeOrdinary {
			return ErrDraftPurpose
		}
		r.Purpose = purposeOrdinary
		records = append(records, r)
		return nil
	}); e != nil {
		return e
	}
	for _, r := range records {
		raw, e := json.Marshal(r)
		if e != nil {
			return e
		}
		if e = b.Put([]byte(r.AgentSessionID), raw); e != nil {
			return e
		}
	}
	return tx.Bucket(metadataBucket).Put(storeSchemaVersionKey, []byte("6"))
}
func validateStoredPurpose(tx *bolt.Tx, r *Record) error {
	v := string(tx.Bucket(metadataBucket).Get(storeSchemaVersionKey))
	if v != "6" {
		if (r.Purpose != "" && r.Purpose != purposeOrdinary) || r.DraftWorkspaceID != "" || r.DraftSchemaVersion != 0 || r.DraftPolicyVersion != 0 {
			return ErrDraftPurpose
		}
		r.Purpose = purposeOrdinary
		return nil
	}
	switch r.Purpose {
	case purposeOrdinary:
		if r.DraftWorkspaceID != "" || r.DraftPolicyVersion != 0 || r.DraftSchemaVersion != 0 {
			return ErrDraftPurpose
		}
	case purposeDraft:
		if !isCanonicalNonZeroUUID(r.DraftWorkspaceID) || r.DraftPolicyVersion != 1 || r.DraftSchemaVersion != 1 {
			return ErrDraftPurpose
		}
	default:
		return ErrDraftPurpose
	}
	return nil
}
func validatePurposeWrite(tx *bolt.Tx, r Record) error {
	if r.Purpose == purposeDraft && string(tx.Bucket(metadataBucket).Get(storeSchemaVersionKey)) != "6" {
		return ErrDraftStorageDisabled
	}
	if e := validateStoredPurpose(tx, &r); e != nil {
		return e
	}
	if old := tx.Bucket(sessionsBucket).Get([]byte(r.AgentSessionID)); old != nil {
		var p Record
		if json.Unmarshal(old, &p) != nil {
			return ErrDraftPurpose
		}
		if e := validateStoredPurpose(tx, &p); e != nil {
			return e
		}
		if p.Purpose != r.Purpose || p.DraftWorkspaceID != r.DraftWorkspaceID || p.DraftSchemaVersion != r.DraftSchemaVersion || p.DraftPolicyVersion != r.DraftPolicyVersion {
			return ErrDraftPurpose
		}
	}
	return nil
}
func (s *Service) requireOrdinaryPurpose(id string) error {
	r, e := s.store.Get(id)
	if e != nil {
		return e
	}
	if r.Purpose != purposeOrdinary {
		return ErrDraftPurpose
	}
	return nil
}

// Resolve is owned by native startup. No HTTP absolute directory is accepted.
func WithScheduledDraftDirectory(resolve func(string) (string, error)) ServiceOption {
	return func(s *Service) { s.draftDirectory = resolve }
}

// scopedRoot is supplied by native startup after scope/storage qualification.
// It is the Desktop-owned scheduled-workspaces/<tenant>/<owner> directory,
// never a request parameter; this reader neither creates nor modifies it.
func WithScheduledDraftWorkspaceRoot(scopedRoot string) ServiceOption {
	return WithScheduledDraftDirectory(func(id string) (string, error) {
		if !isCanonicalNonZeroUUID(id) {
			return "", ErrDraftPolicy
		}
		root, e := canonicalDirectory(scopedRoot)
		if e != nil || root != filepath.Clean(scopedRoot) {
			return "", ErrDraftPolicy
		}
		path := filepath.Join(root, id)
		resolved, e := canonicalDirectory(path)
		if e != nil || resolved != path {
			return "", ErrDraftPolicy
		}
		return resolved, nil
	})
}
func (s *Service) resolveDraftCwd(id string) (string, error) {
	if s.draftDirectory == nil {
		return "", ErrDraftPolicy
	}
	cwd, e := s.draftDirectory(id)
	if e != nil {
		return "", ErrDraftPolicy
	}
	canonical, e := canonicalDirectory(cwd)
	if e != nil || canonical != cwd {
		return "", ErrDraftPolicy
	}
	entries, e := os.ReadDir(canonical)
	if e != nil || len(entries) != 0 {
		return "", ErrDraftPolicy
	}
	return canonical, nil
}

type draftRuntime interface{ ScheduledDraftReady() bool }

func (s *Service) ScheduledDraftCapability() wire.Capability {
	reason := "storage_disabled"
	ready := false
	if s.store.draftWriter && s.draftDirectory != nil {
		reason = "policy_unqualified"
		if r, ok := s.runtime.(draftRuntime); ok && r.ScheduledDraftReady() {
			ready = true
			reason = "ready"
		}
	}
	return wire.Capability{SchemaVersion: 1, Available: ready, Reason: wire.CapabilityReason(reason)}
}
func (s *Service) requireDraftReady() error {
	c := s.ScheduledDraftCapability()
	if !c.Available {
		if c.Reason == "storage_disabled" {
			return ErrDraftStorageDisabled
		}
		return ErrDraftPolicy
	}
	return nil
}
func draftReceipt(r Record) wire.SessionReceipt {
	return wire.SessionReceipt{SchemaVersion: 1, PolicyVersion: 1, Purpose: purposeDraft, TaskId: r.TaskID, AgentSessionId: r.AgentSessionID, WorkspaceId: r.DraftWorkspaceID}
}
func (s *Service) CreateScheduledDraft(ctx context.Context, input wire.CreateRequest) (wire.SessionReceipt, error) {
	if wire.Validate("CreateRequest", input) != nil {
		return wire.SessionReceipt{}, ErrInvalidArgument
	}
	// Existing identity is a read receipt, never a second thread/start.
	if r, e := s.LookupSessionMapping(input.TaskId); e == nil {
		if r.Purpose != purposeDraft || r.DraftWorkspaceID != input.WorkspaceId || r.DraftSchemaVersion != input.SchemaVersion || r.DraftPolicyVersion != input.PolicyVersion {
			return wire.SessionReceipt{}, ErrTurnOperationConflict
		}
		if r.CodexThreadID == "" {
			return wire.SessionReceipt{}, ErrTurnOperationPending
		}
		return draftReceipt(r), nil
	} else if !errors.Is(e, ErrNotFound) {
		return wire.SessionReceipt{}, e
	}
	if e := s.requireDraftReady(); e != nil {
		return wire.SessionReceipt{}, e
	}
	cwd, e := s.resolveDraftCwd(input.WorkspaceId)
	if e != nil {
		return wire.SessionReceipt{}, ErrDraftPolicy
	}
	r, e := s.startSession(ctx, StartSessionInput{TaskID: input.TaskId, Cwd: cwd}, &draftIdentity{input.WorkspaceId})
	if e != nil {
		return wire.SessionReceipt{}, e
	}
	return draftReceipt(r), nil
}
func (s *Service) ResumeScheduledDraft(ctx context.Context, id string, input wire.ResumeRequest) (wire.SessionReceipt, error) {
	s.draftOperationMu.Lock()
	defer s.draftOperationMu.Unlock()
	if wire.Validate("ResumeRequest", input) != nil || !isCanonicalNonZeroUUID(id) {
		return wire.SessionReceipt{}, ErrInvalidArgument
	}
	if e := s.requireDraftReady(); e != nil {
		return wire.SessionReceipt{}, e
	}
	current, e := s.store.Get(id)
	if e != nil {
		return wire.SessionReceipt{}, e
	}
	if current.Purpose != purposeDraft {
		return wire.SessionReceipt{}, ErrDraftPurpose
	}
	if current.State != StateIdle || current.ActiveTurnID != "" {
		return wire.SessionReceipt{}, ErrTurnActive
	}
	r, e := s.resumeSession(ctx, id, TraceContext{}, true)
	if e != nil {
		return wire.SessionReceipt{}, e
	}
	return draftReceipt(r), nil
}
func (s *Service) StartScheduledDraftTurn(ctx context.Context, id string, input wire.TurnRequest) (wire.TurnReceipt, error) {
	s.draftOperationMu.Lock()
	defer s.draftOperationMu.Unlock()
	if wire.Validate("TurnRequest", input) != nil || strings.TrimSpace(input.Text) == "" || !isCanonicalNonZeroUUID(id) {
		return wire.TurnReceipt{}, ErrInvalidArgument
	}
	if e := s.requireDraftReady(); e != nil {
		return wire.TurnReceipt{}, e
	}
	turn, e := s.startTurnV2(ctx, StartTurnV2Input{AgentSessionID: id, OperationID: input.OperationId, ContentBlocks: []TurnContentBlock{{Type: ContentBlockText, Text: input.Text}}}, true)
	if e != nil {
		return wire.TurnReceipt{}, e
	}
	return wire.TurnReceipt{SchemaVersion: 1, PolicyVersion: 1, AgentSessionId: id, OperationId: input.OperationId, TurnId: turn.ID}, nil
}
func (s *Service) draftCwdMatches(r Record) error {
	if s.draftDirectory == nil {
		return ErrDraftPolicy
	}
	cwd, e := s.resolveDraftCwd(r.DraftWorkspaceID)
	if e != nil || cwd != r.Cwd {
		return ErrDraftPolicy
	}
	return nil
}

// ReadScheduledDraftMapping reads immutable purpose metadata without readiness,
// directory resolution, resume or any Runtime call.
func (s *Service) ReadScheduledDraftMapping(taskID string) (wire.RecoveryMapping, error) {
	r, err := s.LookupSessionMapping(taskID)
	if err != nil {
		return wire.RecoveryMapping{}, err
	}
	if r.Purpose != purposeDraft {
		return wire.RecoveryMapping{}, ErrDraftPurpose
	}
	result := wire.RecoveryMapping{SchemaVersion: r.DraftSchemaVersion, PolicyVersion: r.DraftPolicyVersion,
		Purpose: r.Purpose, TaskId: r.TaskID, AgentSessionId: r.AgentSessionID,
		WorkspaceId: r.DraftWorkspaceID, MappingState: "reserved"}
	if r.CodexThreadID != "" {
		result.MappingState, result.CodexThreadId = "bound", &r.CodexThreadID
	}
	if wire.Validate("RecoveryMapping", result) != nil {
		return wire.RecoveryMapping{}, ErrSessionNotUsable
	}
	return result, nil
}
