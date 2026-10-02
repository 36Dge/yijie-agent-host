// Code generated from model-selection-v1.schema.json; DO NOT EDIT.
package chatmodels

type ProfileId string

const (
	ProfileIdKimiK3MaxV1     ProfileId = "kimi-k3-max-v1"
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
	ProfileId     ProfileId `json:"profile_id"`
	Label         string    `json:"label"`
	Provider      string    `json:"provider"`
	Model         string    `json:"model"`
	Effort        string    `json:"effort"`
	ContextWindow int64     `json:"context_window"`
}
type ModelAvailability struct {
	Profile   ModelDefinition         `json:"profile"`
	Available bool                    `json:"available"`
	Reason    ModelAvailabilityReason `json:"reason"`
}
type Catalog struct {
	SchemaVersion  int64               `json:"schema_version"`
	DefaultProfile ProfileId           `json:"default_profile"`
	Models         []ModelAvailability `json:"models"`
}
type Selection struct {
	SchemaVersion  int64          `json:"schema_version"`
	AgentSessionId CanonicalId    `json:"agent_session_id"`
	State          SelectionState `json:"state"`
	Revision       Revision       `json:"revision"`
	ProfileId      *ProfileId     `json:"profile_id,omitempty"`
	OperationId    *CanonicalId   `json:"operation_id,omitempty"`
}
type SelectRequest struct {
	SchemaVersion    int64       `json:"schema_version"`
	OperationId      CanonicalId `json:"operation_id"`
	ExpectedRevision Revision    `json:"expected_revision"`
	ProfileId        ProfileId   `json:"profile_id"`
}
type Error struct {
	SchemaVersion int64     `json:"schema_version"`
	Code          ErrorCode `json:"code"`
}
type ModelAvailabilityReason string

const (
	ModelAvailabilityReasonReady              ModelAvailabilityReason = "ready"
	ModelAvailabilityReasonNotConfigured      ModelAvailabilityReason = "not_configured"
	ModelAvailabilityReasonRuntimeUnavailable ModelAvailabilityReason = "runtime_unavailable"
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
)

func Definitions() []ModelDefinition {
	return []ModelDefinition{
		{ProfileId: "kimi-k3-max-v1", Label: "Kimi K3", Provider: "kimi", Model: "kimi-k3", Effort: "max", ContextWindow: 1048576},
		{ProfileId: "minimax-m3-high-v1", Label: "MiniMax M3", Provider: "minimax", Model: "MiniMax-M3", Effort: "high", ContextWindow: 1000000},
	}
}
