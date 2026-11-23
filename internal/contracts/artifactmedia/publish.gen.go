// Code generated from workspace-publish-v1; DO NOT EDIT.
package artifactmedia

const PublishReasonHeader = "X-Yijie-Artifact-Reason"

type PublishReason string

func (reason PublishReason) Error() string { return string(reason) }

const (
	ReasonInvalidRequest           PublishReason = "invalid_request"
	ReasonScopeUnavailable         PublishReason = "scope_unavailable"
	ReasonWorkspaceFileUnavailable PublishReason = "workspace_file_unavailable"
	ReasonUnsupportedType          PublishReason = "unsupported_type"
	ReasonInvalidContent           PublishReason = "invalid_content"
	ReasonFileTooLarge             PublishReason = "file_too_large"
	ReasonReaderUnavailable        PublishReason = "reader_unavailable"
	ReasonRegistrationConflict     PublishReason = "registration_conflict"
	ReasonRegistrationLimit        PublishReason = "registration_limit"
	ReasonUnavailable              PublishReason = "unavailable"
)

func (reason PublishReason) Known() bool {
	switch reason {
	case ReasonInvalidRequest, ReasonScopeUnavailable, ReasonWorkspaceFileUnavailable, ReasonUnsupportedType, ReasonInvalidContent, ReasonFileTooLarge, ReasonReaderUnavailable, ReasonRegistrationConflict, ReasonRegistrationLimit, ReasonUnavailable:
		return true
	}
	return false
}
