package session

import (
	"context"
	"errors"
	"time"

	expert "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
	"github.com/36Dge/yijie-agent-host/internal/skills"
)

type expertManagedResources interface {
	ExpertResources
	ExpertCatalogRevision() (string, error)
	DescribeExpert(expert.ExpertSelection) (skills.ExpertDescriptor, error)
	PrepareExpertResources(context.Context, string, expert.ExpertSelection) (skills.State, error)
	RemoveExpertResources(context.Context, string, expert.ExpertSelection) (skills.State, error)
	ReadExpertResourceOperation(string, string, string) (skills.CoreExpertOperation, error)
	CancelUnstartedExpertResource(string, string, string) (skills.CoreExpertOperation, error)
}

func (s *Service) managedExpertResources() (expertManagedResources, error) {
	resources, ok := s.expertResources.(expertManagedResources)
	if !ok || !s.store.expertWriter {
		return nil, expertFail(expert.ErrorCodeQualificationRequired)
	}
	return resources, nil
}
func expertResourceError(err error) expert.ErrorCode {
	switch skills.ErrorCodeOf(err) {
	case skills.CodeInvalidRequest:
		return expert.ErrorCodeInvalidRequest
	case skills.CodeNotFound:
		return expert.ErrorCodeVersionUnavailable
	case skills.CodeOperationConflict:
		return expert.ErrorCodeRequestConflict
	case skills.CodeBusy:
		return expert.ErrorCodeBusy
	default:
		return expert.ErrorCodeResourceUnavailable
	}
}
func (s *Service) ExpertStatus(ctx context.Context, input expert.PrivateStatusPayload) (expert.PrivateStatus, error) {
	if input.Validate() != nil {
		return expert.PrivateStatus{}, expertFail(expert.ErrorCodeInvalidRequest)
	}
	if e := s.ValidateMarketScope(input.HostInstanceId, input.Scope, false); e != nil {
		return expert.PrivateStatus{}, e
	}
	resources, e := s.managedExpertResources()
	if e != nil {
		return expert.PrivateStatus{}, e
	}
	revision, e := resources.ExpertCatalogRevision()
	if e != nil {
		return expert.PrivateStatus{}, expertFail(expertResourceError(e))
	}
	operations, e := s.store.latestExpertResourceOperations(s.marketScopeHash(input.Scope))
	if e != nil {
		return expert.PrivateStatus{}, expertFail(expert.ErrorCodeResourceUnavailable)
	}
	views := make([]expert.ResourceOperation, 0, len(operations))
	for _, operation := range operations {
		view := s.observeExpertResourceOperation(operation, resources)
		views = append(views, view)
	}
	available := s.ExpertsEnabled()
	if runtime, ok := s.runtime.(marketDeliveryRuntime); !ok {
		available = false
	} else if _, e := runtime.MarketProcess(); e != nil {
		available = false
	}
	if ctx.Err() != nil {
		return expert.PrivateStatus{}, expertFail(expert.ErrorCodeTemporarilyUnavailable)
	}
	return expert.PrivateStatus{CatalogRevision: expert.Digest(revision), ExecutionAvailable: available, ResourceOperations: views}, nil
}
func (s *Service) PrepareExpertResources(ctx context.Context, input expert.PrivateResourcePayload) (expert.ResourceOperation, error) {
	return s.mutateExpertResources(ctx, input, "prepare")
}
func (s *Service) RemoveExpertResources(ctx context.Context, input expert.PrivateResourcePayload) (expert.ResourceOperation, error) {
	return s.mutateExpertResources(ctx, input, "remove")
}
func (s *Service) ReadExpertResourceOperation(input expert.PrivateResourceReadPayload) (expert.ResourceOperation, error) {
	if input.Validate() != nil {
		return expert.ResourceOperation{}, expertFail(expert.ErrorCodeInvalidRequest)
	}
	if e := s.ValidateMarketScope(input.HostInstanceId, input.Scope, false); e != nil {
		return expert.ResourceOperation{}, e
	}
	operation, e := s.store.readExpertResourceOperation(string(input.OperationId))
	if e != nil {
		return expert.ResourceOperation{}, expertFail(expert.ErrorCodeNotFound)
	}
	if operation.ScopeHash != s.marketScopeHash(input.Scope) {
		return expert.ResourceOperation{}, expertFail(expert.ErrorCodeContextInvalid)
	}
	resources, _ := s.managedExpertResources()
	return s.observeExpertResourceOperation(operation, resources), nil
}

// Recovery is explicit and freshly authorized. It may observe an existing core
// result or reject an original operation that never reached the installer; it
// must never restart installation or deletion from a read/replay request.
func (s *Service) RecoverExpertResources(ctx context.Context, recovery expert.PrivateResourceRecoverPayload) (expert.ResourceOperation, error) {
	if recovery.Validate() != nil {
		return expert.ResourceOperation{}, expertFail(expert.ErrorCodeInvalidRequest)
	}
	if e := s.lockMarketModelOperation(ctx); e != nil {
		return expert.ResourceOperation{}, expertFail(expert.ErrorCodeBusy)
	}
	defer s.modelOperationMu.Unlock()
	if e := s.ValidateMarketScope(recovery.HostInstanceId, recovery.Scope, true); e != nil {
		return expert.ResourceOperation{}, e
	}
	input, action := recovery.Original, recovery.Action
	owner := s.marketScopeHash(recovery.Scope)
	if owner != s.marketScopeHash(input.Scope) {
		return expert.ResourceOperation{}, expertFail(expert.ErrorCodeContextInvalid)
	}
	digest, e := s.marketHMAC("yijie.expert-resources/v1", []any{action, input})
	if e != nil {
		return expert.ResourceOperation{}, e
	}
	resources, e := s.managedExpertResources()
	if e != nil {
		return expert.ResourceOperation{}, e
	}
	operation, e := s.store.readExpertResourceOperation(string(input.OperationId))
	if errors.Is(e, ErrNotFound) {
		descriptor, describeErr := resources.DescribeExpert(input.Expert)
		if describeErr != nil {
			return expert.ResourceOperation{}, expertFail(expertResourceError(describeErr))
		}
		operation = expertResourceOperation{Version: 1, ScopeHash: owner, IntentDigest: digest, InstallationID: descriptor.InstallationID, Reference: descriptor.Reference, Receipt: expert.ResourceOperation{OperationId: input.OperationId, Expert: input.Expert, Action: action, State: expert.OperationStatePending}}
		if e = s.store.beginExpertResourceOperation(&operation); e != nil {
			return expert.ResourceOperation{}, expertFail(expert.ErrorCodeOperationPending)
		}
	} else if e != nil {
		return expert.ResourceOperation{}, expertFail(expert.ErrorCodeResourceUnavailable)
	} else if operation.ScopeHash != owner || operation.IntentDigest != digest {
		return expert.ResourceOperation{}, expertFail(expert.ErrorCodeRequestConflict)
	}
	if operation.Receipt.State == expert.OperationStateSucceeded || operation.Receipt.State == expert.OperationStateRejected {
		return operation.Receipt, nil
	}
	if ctx.Err() != nil {
		return operation.Receipt, nil
	}
	if _, e = resources.CancelUnstartedExpertResource(string(input.OperationId), operation.InstallationID, action); e != nil {
		return expert.ResourceOperation{}, expertFail(expertResourceError(e))
	}
	return s.observeExpertResourceOperation(operation, resources), nil
}
func (s *Service) observeExpertResourceOperation(operation expertResourceOperation, resources expertManagedResources) expert.ResourceOperation {
	if operation.Receipt.State == expert.OperationStateSucceeded || operation.Receipt.State == expert.OperationStateRejected {
		return operation.Receipt
	}
	// A current installation is not evidence that this original mutation succeeded.
	// Only the existing durable Skill journal can settle an adapter receipt.
	operation.Receipt.State = expert.OperationStateUnknown
	code := expert.ErrorCodeOutcomeUnknown
	operation.Receipt.ErrorCode = &code
	if resources != nil {
		observed, e := resources.ReadExpertResourceOperation(string(operation.Receipt.OperationId), operation.InstallationID, operation.Receipt.Action)
		if e == nil && observed.Found {
			if observed.Complete {
				if observed.ErrorCode != "" {
					operation.Receipt.State = expert.OperationStateRejected
					code = expertResourceError(&skills.ServiceError{Code: observed.ErrorCode})
				} else if (operation.Receipt.Action == "remove" && !observed.Installed) || (operation.Receipt.Action == "prepare" && observed.Installed && observed.Enabled && observed.Version == string(operation.Reference.Version)) {
					operation.Receipt.State = expert.OperationStateSucceeded
					operation.Receipt.ErrorCode = nil
				} else {
					// The original core operation completed, but did not make
					// the requested resource usable. This is a known result.
					operation.Receipt.State = expert.OperationStateRejected
					code = expert.ErrorCodeResourceUnavailable
				}
			} else if observed.InProgress {
				operation.Receipt.State = expert.OperationStatePending
				operation.Receipt.ErrorCode = nil
			}
		}
	}
	_ = s.store.finishExpertResourceOperation(operation)
	return operation.Receipt
}
func (s *Service) mutateExpertResources(ctx context.Context, input expert.PrivateResourcePayload, action string) (expert.ResourceOperation, error) {
	// Leave five seconds for the managed core's normal rollback/reconciliation
	// inside the source-defined 25-second private method budget.
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if input.Validate() != nil {
		return expert.ResourceOperation{}, expertFail(expert.ErrorCodeInvalidRequest)
	}
	if e := s.ValidateMarketScope(input.HostInstanceId, input.Scope, false); e != nil {
		return expert.ResourceOperation{}, e
	}
	if e := s.lockMarketModelOperation(ctx); e != nil {
		return expert.ResourceOperation{}, expertFail(expert.ErrorCodeBusy)
	}
	defer s.modelOperationMu.Unlock()
	digest, e := s.marketHMAC("yijie.expert-resources/v1", []any{action, input})
	if e != nil {
		return expert.ResourceOperation{}, e
	}
	owner := s.marketScopeHash(input.Scope)
	if prior, e := s.store.readExpertResourceOperation(string(input.OperationId)); e == nil {
		if prior.ScopeHash != owner || prior.IntentDigest != digest {
			return expert.ResourceOperation{}, expertFail(expert.ErrorCodeRequestConflict)
		}
		resources, _ := s.managedExpertResources()
		return s.observeExpertResourceOperation(prior, resources), nil
	} else if !errors.Is(e, ErrNotFound) {
		return expert.ResourceOperation{}, expertFail(expert.ErrorCodeResourceUnavailable)
	}
	if e := s.ValidateMarketScope(input.HostInstanceId, input.Scope, true); e != nil {
		return expert.ResourceOperation{}, e
	}
	resources, e := s.managedExpertResources()
	if e != nil {
		return expert.ResourceOperation{}, e
	}
	descriptor, e := resources.DescribeExpert(input.Expert)
	if e != nil {
		return expert.ResourceOperation{}, expertFail(expertResourceError(e))
	}
	if s.store.noPendingExpertResource(string(input.Expert.ExpertId)) != nil {
		return expert.ResourceOperation{}, expertFail(expert.ErrorCodeOperationPending)
	}
	// Exact already-installed prepare is a no-op through the original journal.
	// Replacement/removal require both Native scoped retention checks and these
	// independent live Host references, while the same operation mutex is held.
	_, ready := resources.ResolveExpert(descriptor.Reference)
	if action == "remove" || ready != nil {
		referenced, e := s.store.expertResourceReferenced(string(input.Expert.ExpertId))
		if e != nil {
			return expert.ResourceOperation{}, expertFail(expert.ErrorCodeOperationPending)
		}
		if referenced || s.expertGrantReferences(string(input.Expert.ExpertId)) {
			return expert.ResourceOperation{}, expertFail(expert.ErrorCodeResourceInUse)
		}
	}
	operation := expertResourceOperation{Version: 1, ScopeHash: owner, IntentDigest: digest, InstallationID: descriptor.InstallationID, Reference: descriptor.Reference, Receipt: expert.ResourceOperation{OperationId: input.OperationId, Expert: input.Expert, Action: action, State: expert.OperationStatePending}}
	if e = s.store.beginExpertResourceOperation(&operation); e != nil {
		return expert.ResourceOperation{}, expertFail(expert.ErrorCodeRequestConflict)
	}
	if action == "prepare" {
		_, e = resources.PrepareExpertResources(ctx, string(input.OperationId), input.Expert)
	} else {
		_, e = resources.RemoveExpertResources(ctx, string(input.OperationId), input.Expert)
	}
	if e != nil && skills.ErrorCodeOf(e) == skills.CodeBusy {
		operation.Receipt.State = expert.OperationStateRejected
		code := expert.ErrorCodeBusy
		operation.Receipt.ErrorCode = &code
		if e = s.store.finishExpertResourceOperation(operation); e == nil {
			return operation.Receipt, nil
		}
	}
	return s.observeExpertResourceOperation(operation, resources), nil
}
func (s *Service) expertGrantReferences(expertID string) bool {
	if s.market == nil {
		return false
	}
	s.market.mu.Lock()
	defer s.market.mu.Unlock()
	s.market.sweep()
	for _, grant := range s.market.grants {
		if grant.expertSnapshot != nil && grant.expertSnapshot.Expert != nil && string(grant.expertSnapshot.Expert.ExpertId) == expertID && !grant.revoked && (grant.inFlight || (grant.body != nil && time.Now().Before(grant.deadline))) {
			return true
		}
	}
	return false
}
