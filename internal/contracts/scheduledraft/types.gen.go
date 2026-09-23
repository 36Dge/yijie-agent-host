// Code generated from draft-execution source. DO NOT EDIT.
package scheduledraft

import "encoding/json"

type Identity = string
type Version = int64
type Text = string
type CreateRequest struct {
	SchemaVersion int64    `json:"schema_version"`
	PolicyVersion int64    `json:"policy_version"`
	TaskId        Identity `json:"task_id"`
	WorkspaceId   Identity `json:"workspace_id"`
}
type ResumeRequest struct {
	SchemaVersion int64 `json:"schema_version"`
	PolicyVersion int64 `json:"policy_version"`
}
type TurnRequest struct {
	SchemaVersion int64    `json:"schema_version"`
	PolicyVersion int64    `json:"policy_version"`
	OperationId   Identity `json:"operation_id"`
	Text          Text     `json:"text"`
}
type SessionReceipt struct {
	SchemaVersion  int64    `json:"schema_version"`
	PolicyVersion  int64    `json:"policy_version"`
	Purpose        string   `json:"purpose"`
	TaskId         Identity `json:"task_id"`
	AgentSessionId Identity `json:"agent_session_id"`
	WorkspaceId    Identity `json:"workspace_id"`
}
type TurnReceipt struct {
	SchemaVersion  int64    `json:"schema_version"`
	PolicyVersion  int64    `json:"policy_version"`
	AgentSessionId Identity `json:"agent_session_id"`
	OperationId    Identity `json:"operation_id"`
	TurnId         Identity `json:"turn_id"`
}
type Capability struct {
	SchemaVersion int64            `json:"schema_version"`
	Available     bool             `json:"available"`
	Reason        CapabilityReason `json:"reason"`
}
type ErrorCode string
type Error struct {
	SchemaVersion int64     `json:"schema_version"`
	Code          ErrorCode `json:"code"`
}
type Clarification struct {
	SchemaVersion int64                            `json:"schema_version"`
	Kind          string                           `json:"kind"`
	MissingFields []ClarificationMissingFieldsItem `json:"missing_fields"`
	Question      string                           `json:"question"`
}
type Candidate struct {
	SchemaVersion int64             `json:"schema_version"`
	Kind          string            `json:"kind"`
	Name          string            `json:"name"`
	Content       string            `json:"content"`
	Schedule      CandidateSchedule `json:"schedule"`
	Target        CandidateTarget   `json:"target"`
}
type Output = json.RawMessage
type RecoveryMapping struct {
	SchemaVersion            int64                       `json:"schema_version"`
	PolicyVersion            int64                       `json:"policy_version"`
	Purpose                  string                      `json:"purpose"`
	TaskId                   Identity                    `json:"task_id"`
	AgentSessionId           Identity                    `json:"agent_session_id"`
	WorkspaceId              Identity                    `json:"workspace_id"`
	MappingState             RecoveryMappingMappingState `json:"mapping_state"`
	CodexThreadId            *Identity                   `json:"codex_thread_id,omitempty"`
	RespondingHostInstanceId *Identity                   `json:"responding_host_instance_id,omitempty"`
}
type CapabilityReason string
type ClarificationMissingFieldsItem string
type CandidateSchedule struct {
	Frequency CandidateScheduleFrequency `json:"frequency"`
	TimeZone  string                     `json:"time_zone"`
	LocalTime string                     `json:"local_time"`
	LocalDate *string                    `json:"local_date,omitempty"`
	Weekdays  *[]int64                   `json:"weekdays,omitempty"`
}
type CandidateTarget struct {
	Mode              CandidateTargetMode `json:"mode"`
	ExistingChatLabel *string             `json:"existing_chat_label,omitempty"`
}
type RecoveryMappingMappingState string
type CandidateScheduleFrequency string
type CandidateTargetMode string
