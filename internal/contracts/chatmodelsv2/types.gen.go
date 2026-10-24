// Code generated from model-selection-v2.schema.json; DO NOT EDIT.
package chatmodelsv2

type ProfileId string

const (
	ProfileIdKimiK3MaxV1     ProfileId = "kimi-k3-max-v1"
	ProfileIdKimiK31ProMaxV1 ProfileId = "kimi-k3-1-pro-max-v1"
	ProfileIdMinimaxM3HighV1 ProfileId = "minimax-m3-high-v1"
)

type CanonicalId = string
type Revision = int64
type SelectionState string

const (
	SelectionStateReady     SelectionState = "ready"
	SelectionStateSwitching SelectionState = "switching"
	SelectionStateUnknown   SelectionState = "unknown"
)

type ModelDefinition struct {
	ProfileId     ActiveProfileId `json:"profile_id"`
	Label         string          `json:"label"`
	Provider      string          `json:"provider"`
	Model         string          `json:"model"`
	Effort        string          `json:"effort"`
	ContextWindow int64           `json:"context_window"`
}
type ModelAvailability struct {
	Profile   ModelDefinition         `json:"profile"`
	Available bool                    `json:"available"`
	Reason    ModelAvailabilityReason `json:"reason"`
}
type Catalog struct {
	SchemaVersion  int64               `json:"schema_version"`
	DefaultProfile ActiveProfileId     `json:"default_profile"`
	Models         []ModelAvailability `json:"models"`
}
type Selection struct {
	SchemaVersion  int64                 `json:"schema_version"`
	AgentSessionId CanonicalId           `json:"agent_session_id"`
	State          SelectionState        `json:"state"`
	Revision       Revision              `json:"revision"`
	ProfileId      *ProfileId            `json:"profile_id,omitempty"`
	OperationId    *CanonicalId          `json:"operation_id,omitempty"`
	Availability   SelectionAvailability `json:"availability"`
}
type SelectRequest struct {
	SchemaVersion    int64           `json:"schema_version"`
	OperationId      CanonicalId     `json:"operation_id"`
	ExpectedRevision Revision        `json:"expected_revision"`
	ProfileId        ActiveProfileId `json:"profile_id"`
}
type Error struct {
	SchemaVersion int64     `json:"schema_version"`
	Code          ErrorCode `json:"code"`
}
type ActiveProfileId string

const (
	ActiveProfileIdKimiK31ProMaxV1 ActiveProfileId = "kimi-k3-1-pro-max-v1"
	ActiveProfileIdMinimaxM3HighV1 ActiveProfileId = "minimax-m3-high-v1"
)

type SelectionAvailability string

const (
	SelectionAvailabilityReady              SelectionAvailability = "ready"
	SelectionAvailabilityRetired            SelectionAvailability = "retired"
	SelectionAvailabilityNotConfigured      SelectionAvailability = "not_configured"
	SelectionAvailabilityRuntimeUnavailable SelectionAvailability = "runtime_unavailable"
	SelectionAvailabilityActivationRequired SelectionAvailability = "activation_required"
	SelectionAvailabilitySelectionUnknown   SelectionAvailability = "selection_unknown"
)

type OperationRequestSnapshot struct {
	SchemaVersion    int64       `json:"schema_version"`
	OperationId      CanonicalId `json:"operation_id"`
	ExpectedRevision Revision    `json:"expected_revision"`
	ProfileId        ProfileId   `json:"profile_id"`
}
type ModelOperationState string

const (
	ModelOperationStatePrepared  ModelOperationState = "prepared"
	ModelOperationStateApplying  ModelOperationState = "applying"
	ModelOperationStateUnknown   ModelOperationState = "unknown"
	ModelOperationStateCommitted ModelOperationState = "committed"
	ModelOperationStateCancelled ModelOperationState = "cancelled"
)

type ModelOperationReceipt struct {
	SchemaVersion  int64                    `json:"schema_version"`
	AgentSessionId CanonicalId              `json:"agent_session_id"`
	OperationId    CanonicalId              `json:"operation_id"`
	State          ModelOperationState      `json:"state"`
	Request        OperationRequestSnapshot `json:"request"`
	Result         *SelectionResultSnapshot `json:"result,omitempty"`
}
type RecoverRequest struct {
	SchemaVersion    int64           `json:"schema_version"`
	OperationId      CanonicalId     `json:"operation_id"`
	ExpectedRevision Revision        `json:"expected_revision"`
	ProfileId        ActiveProfileId `json:"profile_id"`
}
type SelectionResultSnapshot struct {
	SchemaVersion  int64                  `json:"schema_version"`
	AgentSessionId CanonicalId            `json:"agent_session_id"`
	State          SelectionState         `json:"state"`
	Revision       Revision               `json:"revision"`
	ProfileId      *ProfileId             `json:"profile_id,omitempty"`
	OperationId    *CanonicalId           `json:"operation_id,omitempty"`
	Availability   *SelectionAvailability `json:"availability,omitempty"`
}
type ModelAvailabilityReason string

const (
	ModelAvailabilityReasonReady              ModelAvailabilityReason = "ready"
	ModelAvailabilityReasonNotConfigured      ModelAvailabilityReason = "not_configured"
	ModelAvailabilityReasonRuntimeUnavailable ModelAvailabilityReason = "runtime_unavailable"
	ModelAvailabilityReasonActivationRequired ModelAvailabilityReason = "activation_required"
)

type ErrorCode string

const (
	ErrorCodeInvalidRequest     ErrorCode = "invalid_request"
	ErrorCodeUnauthorized       ErrorCode = "unauthorized"
	ErrorCodeNotFound           ErrorCode = "not_found"
	ErrorCodeBusy               ErrorCode = "busy"
	ErrorCodeRevisionConflict   ErrorCode = "revision_conflict"
	ErrorCodeRequestConflict    ErrorCode = "request_conflict"
	ErrorCodeModelUnavailable   ErrorCode = "model_unavailable"
	ErrorCodeSelectionUnknown   ErrorCode = "selection_unknown"
	ErrorCodeRuntimeUnavailable ErrorCode = "runtime_unavailable"
	ErrorCodeModelRetired       ErrorCode = "model_retired"
	ErrorCodeActivationRequired ErrorCode = "activation_required"
)

func Definitions() []ModelDefinition {
	return []ModelDefinition{
		{ProfileId: "kimi-k3-1-pro-max-v1", Label: "Kimi K3.1 Pro", Provider: "kimi", Model: "kimi-k3.1-pro", Effort: "max", ContextWindow: 1048576},
		{ProfileId: "minimax-m3-high-v1", Label: "MiniMax M3", Provider: "minimax", Model: "MiniMax-M3", Effort: "high", ContextWindow: 1000000},
	}
}
