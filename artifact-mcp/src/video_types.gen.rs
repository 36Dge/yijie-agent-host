// Generated from video-generation/v1. DO NOT EDIT.
use serde::{Deserialize, Serialize};

fn optional_non_null<'de, D, T>(d: D) -> Result<Option<T>, D::Error>
where
    D: serde::Deserializer<'de>,
    T: Deserialize<'de>,
{
    T::deserialize(d).map(Some)
}
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum ImageMode {
    #[serde(rename = "none")]
    None,
    #[serde(rename = "first_frame")]
    FirstFrame,
    #[serde(rename = "first_last_frames")]
    FirstLastFrames,
    #[serde(rename = "reference_images")]
    ReferenceImages,
}
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum VideoStatus {
    #[serde(rename = "pending")]
    Pending,
    #[serde(rename = "staged")]
    Staged,
    #[serde(rename = "committed")]
    Committed,
    #[serde(rename = "blocked")]
    Blocked,
    #[serde(rename = "failed")]
    Failed,
    #[serde(rename = "unknown")]
    Unknown,
}
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum VideoCode {
    #[serde(rename = "ok")]
    Ok,
    #[serde(rename = "provider_pending")]
    ProviderPending,
    #[serde(rename = "existing_operation")]
    ExistingOperation,
    #[serde(rename = "selection_required")]
    SelectionRequired,
    #[serde(rename = "operation_not_found")]
    OperationNotFound,
    #[serde(rename = "expert_required")]
    ExpertRequired,
    #[serde(rename = "input_unsupported")]
    InputUnsupported,
    #[serde(rename = "operation_conflict")]
    OperationConflict,
    #[serde(rename = "service_unavailable")]
    ServiceUnavailable,
    #[serde(rename = "outcome_unknown")]
    OutcomeUnknown,
    #[serde(rename = "provider_failed")]
    ProviderFailed,
    #[serde(rename = "provider_expired")]
    ProviderExpired,
    #[serde(rename = "delivery_rejected")]
    DeliveryRejected,
    #[serde(rename = "delivery_unconfirmed")]
    DeliveryUnconfirmed,
    #[serde(rename = "local_cancelled")]
    LocalCancelled,
}
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum NextAction {
    #[serde(rename = "get_video_result")]
    GetVideoResult,
    #[serde(rename = "select_operation")]
    SelectOperation,
    #[serde(rename = "check_artifact")]
    CheckArtifact,
    #[serde(rename = "report_status")]
    ReportStatus,
    #[serde(rename = "use_expert")]
    UseExpert,
}
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum ProviderStatus {
    #[serde(rename = "queued")]
    Queued,
    #[serde(rename = "running")]
    Running,
    #[serde(rename = "succeeded")]
    Succeeded,
    #[serde(rename = "failed")]
    Failed,
    #[serde(rename = "cancelled")]
    Cancelled,
    #[serde(rename = "expired")]
    Expired,
}
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum OperationPhase {
    #[serde(rename = "submitting")]
    Submitting,
    #[serde(rename = "pending")]
    Pending,
    #[serde(rename = "succeeded")]
    Succeeded,
    #[serde(rename = "handed_off")]
    HandedOff,
    #[serde(rename = "failed")]
    Failed,
    #[serde(rename = "unknown")]
    Unknown,
}
#[derive(Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", try_from = "GenerateInputWire")]
pub struct GenerateInput {
    pub prompt: String,
    #[serde(rename = "image_mode")]
    pub image_mode: ImageMode,
    #[serde(rename = "image_indices")]
    pub image_indices: Vec<i64>,
}
#[derive(Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct GenerateInputWire {
    pub prompt: String,
    #[serde(rename = "image_mode")]
    pub image_mode: ImageMode,
    #[serde(rename = "image_indices")]
    pub image_indices: Vec<i64>,
}
impl TryFrom<GenerateInputWire> for GenerateInput {
    type Error = &'static str;
    fn try_from(w: GenerateInputWire) -> Result<Self, Self::Error> {
        let v = Self {
            prompt: w.prompt,
            image_mode: w.image_mode,
            image_indices: w.image_indices,
        };
        v.validate()?;
        Ok(v)
    }
}
impl GenerateInput {
    pub fn validate(&self) -> Result<(), &'static str> {
        {
            let value_prompt = &self.prompt;
            if value_prompt.chars().count() < 1 {
                return Err("invalid broker text");
            }
            if value_prompt.chars().count() > 4000 {
                return Err("invalid broker text");
            }
            if value_prompt.trim().is_empty() {
                return Err("empty video prompt");
            }
        }
        {
            let value_image_indices = &self.image_indices;
            if value_image_indices.len() > 10 {
                return Err("invalid broker collection");
            }
            for item in value_image_indices.iter() {
                if *item < 1 {
                    return Err("invalid broker number");
                }
                if *item > 10 {
                    return Err("invalid broker number");
                }
            }
        }
        if self.prompt.contains(char::from(0)) || forbidden_prompt_parameter(&self.prompt) {
            return Err("invalid video prompt");
        }
        match self.image_mode {
            ImageMode::None if !self.image_indices.is_empty() => return Err("unexpected images"),
            ImageMode::FirstFrame if self.image_indices.len() != 1 => {
                return Err("one first frame required")
            }
            ImageMode::FirstLastFrames if self.image_indices.len() != 2 => {
                return Err("two frame roles required")
            }
            ImageMode::ReferenceImages => {
                if self.image_indices.is_empty() {
                    return Err("reference images required");
                }
                for (i, n) in self.image_indices.iter().enumerate() {
                    if self.image_indices[..i].contains(n) {
                        return Err("duplicate reference index");
                    }
                }
            }
            _ => {}
        }
        Ok(())
    }
}
impl std::fmt::Debug for GenerateInput {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("GenerateInput([redacted])")
    }
}
#[derive(Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", try_from = "GetInputWire")]
pub struct GetInput {
    #[serde(rename = "video_operation_id")]
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        deserialize_with = "optional_non_null"
    )]
    pub video_operation_id: Option<String>,
}
#[derive(Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct GetInputWire {
    #[serde(rename = "video_operation_id")]
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        deserialize_with = "optional_non_null"
    )]
    pub video_operation_id: Option<String>,
}
impl TryFrom<GetInputWire> for GetInput {
    type Error = &'static str;
    fn try_from(w: GetInputWire) -> Result<Self, Self::Error> {
        let v = Self {
            video_operation_id: w.video_operation_id,
        };
        v.validate()?;
        Ok(v)
    }
}
impl GetInput {
    pub fn validate(&self) -> Result<(), &'static str> {
        self.video_operation_id
            .as_ref()
            .map(|value_video_operation_id| -> Result<(), &'static str> {
                if value_video_operation_id.chars().count() < 36 {
                    return Err("invalid broker text");
                }
                if value_video_operation_id.chars().count() > 36 {
                    return Err("invalid broker text");
                }
                if !valid_uuid(value_video_operation_id) {
                    return Err("invalid video identity");
                }
                if value_video_operation_id == "00000000-0000-0000-0000-000000000000" {
                    return Err("invalid broker identity");
                }
                Ok(())
            })
            .transpose()?;
        Ok(())
    }
}
impl std::fmt::Debug for GetInput {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("GetInput([redacted])")
    }
}
#[derive(Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", try_from = "GenerateRequestWire")]
pub struct GenerateRequest {
    #[serde(rename = "thread_id")]
    pub thread_id: String,
    #[serde(rename = "turn_id")]
    pub turn_id: String,
    pub input: GenerateInput,
}
#[derive(Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct GenerateRequestWire {
    #[serde(rename = "thread_id")]
    pub thread_id: String,
    #[serde(rename = "turn_id")]
    pub turn_id: String,
    pub input: GenerateInput,
}
impl TryFrom<GenerateRequestWire> for GenerateRequest {
    type Error = &'static str;
    fn try_from(w: GenerateRequestWire) -> Result<Self, Self::Error> {
        let v = Self {
            thread_id: w.thread_id,
            turn_id: w.turn_id,
            input: w.input,
        };
        v.validate()?;
        Ok(v)
    }
}
impl GenerateRequest {
    pub fn validate(&self) -> Result<(), &'static str> {
        {
            let value_thread_id = &self.thread_id;
            if value_thread_id.chars().count() < 36 {
                return Err("invalid broker text");
            }
            if value_thread_id.chars().count() > 36 {
                return Err("invalid broker text");
            }
            if !valid_uuid(value_thread_id) {
                return Err("invalid video identity");
            }
            if value_thread_id == "00000000-0000-0000-0000-000000000000" {
                return Err("invalid broker identity");
            }
        }
        {
            let value_turn_id = &self.turn_id;
            if value_turn_id.chars().count() < 36 {
                return Err("invalid broker text");
            }
            if value_turn_id.chars().count() > 36 {
                return Err("invalid broker text");
            }
            if !valid_uuid(value_turn_id) {
                return Err("invalid video identity");
            }
            if value_turn_id == "00000000-0000-0000-0000-000000000000" {
                return Err("invalid broker identity");
            }
        }
        {
            let value_input = &self.input;
            value_input.validate()?;
        }
        Ok(())
    }
}
impl std::fmt::Debug for GenerateRequest {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("GenerateRequest([redacted])")
    }
}
#[derive(Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", try_from = "GetRequestWire")]
pub struct GetRequest {
    #[serde(rename = "thread_id")]
    pub thread_id: String,
    #[serde(rename = "turn_id")]
    pub turn_id: String,
    pub input: GetInput,
}
#[derive(Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct GetRequestWire {
    #[serde(rename = "thread_id")]
    pub thread_id: String,
    #[serde(rename = "turn_id")]
    pub turn_id: String,
    pub input: GetInput,
}
impl TryFrom<GetRequestWire> for GetRequest {
    type Error = &'static str;
    fn try_from(w: GetRequestWire) -> Result<Self, Self::Error> {
        let v = Self {
            thread_id: w.thread_id,
            turn_id: w.turn_id,
            input: w.input,
        };
        v.validate()?;
        Ok(v)
    }
}
impl GetRequest {
    pub fn validate(&self) -> Result<(), &'static str> {
        {
            let value_thread_id = &self.thread_id;
            if value_thread_id.chars().count() < 36 {
                return Err("invalid broker text");
            }
            if value_thread_id.chars().count() > 36 {
                return Err("invalid broker text");
            }
            if !valid_uuid(value_thread_id) {
                return Err("invalid video identity");
            }
            if value_thread_id == "00000000-0000-0000-0000-000000000000" {
                return Err("invalid broker identity");
            }
        }
        {
            let value_turn_id = &self.turn_id;
            if value_turn_id.chars().count() < 36 {
                return Err("invalid broker text");
            }
            if value_turn_id.chars().count() > 36 {
                return Err("invalid broker text");
            }
            if !valid_uuid(value_turn_id) {
                return Err("invalid video identity");
            }
            if value_turn_id == "00000000-0000-0000-0000-000000000000" {
                return Err("invalid broker identity");
            }
        }
        {
            let value_input = &self.input;
            value_input.validate()?;
        }
        Ok(())
    }
}
impl std::fmt::Debug for GetRequest {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("GetRequest([redacted])")
    }
}
#[derive(Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", try_from = "OperationCandidateWire")]
pub struct OperationCandidate {
    #[serde(rename = "video_operation_id")]
    pub video_operation_id: String,
    #[serde(rename = "created_at")]
    pub created_at: i64,
    #[serde(rename = "image_mode")]
    pub image_mode: ImageMode,
    pub phase: OperationPhase,
}
#[derive(Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct OperationCandidateWire {
    #[serde(rename = "video_operation_id")]
    pub video_operation_id: String,
    #[serde(rename = "created_at")]
    pub created_at: i64,
    #[serde(rename = "image_mode")]
    pub image_mode: ImageMode,
    pub phase: OperationPhase,
}
impl TryFrom<OperationCandidateWire> for OperationCandidate {
    type Error = &'static str;
    fn try_from(w: OperationCandidateWire) -> Result<Self, Self::Error> {
        let v = Self {
            video_operation_id: w.video_operation_id,
            created_at: w.created_at,
            image_mode: w.image_mode,
            phase: w.phase,
        };
        v.validate()?;
        Ok(v)
    }
}
impl OperationCandidate {
    pub fn validate(&self) -> Result<(), &'static str> {
        {
            let value_video_operation_id = &self.video_operation_id;
            if value_video_operation_id.chars().count() < 36 {
                return Err("invalid broker text");
            }
            if value_video_operation_id.chars().count() > 36 {
                return Err("invalid broker text");
            }
            if !valid_uuid(value_video_operation_id) {
                return Err("invalid video identity");
            }
            if value_video_operation_id == "00000000-0000-0000-0000-000000000000" {
                return Err("invalid broker identity");
            }
        }
        {
            let value_created_at = &self.created_at;
            if *value_created_at < 0 {
                return Err("invalid broker number");
            }
        }
        Ok(())
    }
}
impl std::fmt::Debug for OperationCandidate {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("OperationCandidate([redacted])")
    }
}
#[derive(Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", try_from = "VideoResultWire")]
pub struct VideoResult {
    pub status: VideoStatus,
    pub code: VideoCode,
    #[serde(rename = "next_action")]
    pub next_action: NextAction,
    pub source: String,
    #[serde(rename = "video_operation_id")]
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        deserialize_with = "optional_non_null"
    )]
    pub video_operation_id: Option<String>,
    #[serde(rename = "provider_status")]
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        deserialize_with = "optional_non_null"
    )]
    pub provider_status: Option<ProviderStatus>,
    #[serde(rename = "provider_observed_at")]
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        deserialize_with = "optional_non_null"
    )]
    pub provider_observed_at: Option<i64>,
    #[serde(rename = "artifact_id")]
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        deserialize_with = "optional_non_null"
    )]
    pub artifact_id: Option<String>,
    #[serde(rename = "delivery_turn_id")]
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        deserialize_with = "optional_non_null"
    )]
    pub delivery_turn_id: Option<String>,
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        deserialize_with = "optional_non_null"
    )]
    pub candidates: Option<Vec<OperationCandidate>>,
    #[serde(rename = "candidates_complete")]
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        deserialize_with = "optional_non_null"
    )]
    pub candidates_complete: Option<bool>,
}
#[derive(Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct VideoResultWire {
    pub status: VideoStatus,
    pub code: VideoCode,
    #[serde(rename = "next_action")]
    pub next_action: NextAction,
    pub source: String,
    #[serde(rename = "video_operation_id")]
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        deserialize_with = "optional_non_null"
    )]
    pub video_operation_id: Option<String>,
    #[serde(rename = "provider_status")]
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        deserialize_with = "optional_non_null"
    )]
    pub provider_status: Option<ProviderStatus>,
    #[serde(rename = "provider_observed_at")]
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        deserialize_with = "optional_non_null"
    )]
    pub provider_observed_at: Option<i64>,
    #[serde(rename = "artifact_id")]
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        deserialize_with = "optional_non_null"
    )]
    pub artifact_id: Option<String>,
    #[serde(rename = "delivery_turn_id")]
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        deserialize_with = "optional_non_null"
    )]
    pub delivery_turn_id: Option<String>,
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        deserialize_with = "optional_non_null"
    )]
    pub candidates: Option<Vec<OperationCandidate>>,
    #[serde(rename = "candidates_complete")]
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        deserialize_with = "optional_non_null"
    )]
    pub candidates_complete: Option<bool>,
}
impl TryFrom<VideoResultWire> for VideoResult {
    type Error = &'static str;
    fn try_from(w: VideoResultWire) -> Result<Self, Self::Error> {
        let v = Self {
            status: w.status,
            code: w.code,
            next_action: w.next_action,
            source: w.source,
            video_operation_id: w.video_operation_id,
            provider_status: w.provider_status,
            provider_observed_at: w.provider_observed_at,
            artifact_id: w.artifact_id,
            delivery_turn_id: w.delivery_turn_id,
            candidates: w.candidates,
            candidates_complete: w.candidates_complete,
        };
        v.validate()?;
        Ok(v)
    }
}
impl VideoResult {
    pub fn validate(&self) -> Result<(), &'static str> {
        {
            let value_source = &self.source;
            if value_source != "builtin" {
                return Err("invalid broker constant");
            }
        }
        self.video_operation_id
            .as_ref()
            .map(|value_video_operation_id| -> Result<(), &'static str> {
                if value_video_operation_id.chars().count() < 36 {
                    return Err("invalid broker text");
                }
                if value_video_operation_id.chars().count() > 36 {
                    return Err("invalid broker text");
                }
                if !valid_uuid(value_video_operation_id) {
                    return Err("invalid video identity");
                }
                if value_video_operation_id == "00000000-0000-0000-0000-000000000000" {
                    return Err("invalid broker identity");
                }
                Ok(())
            })
            .transpose()?;
        self.provider_observed_at
            .as_ref()
            .map(|value_provider_observed_at| -> Result<(), &'static str> {
                if *value_provider_observed_at < 0 {
                    return Err("invalid broker number");
                }
                Ok(())
            })
            .transpose()?;
        self.artifact_id
            .as_ref()
            .map(|value_artifact_id| -> Result<(), &'static str> {
                if value_artifact_id.chars().count() < 36 {
                    return Err("invalid broker text");
                }
                if value_artifact_id.chars().count() > 36 {
                    return Err("invalid broker text");
                }
                if !valid_uuid(value_artifact_id) {
                    return Err("invalid video identity");
                }
                if value_artifact_id == "00000000-0000-0000-0000-000000000000" {
                    return Err("invalid broker identity");
                }
                Ok(())
            })
            .transpose()?;
        self.delivery_turn_id
            .as_ref()
            .map(|value_delivery_turn_id| -> Result<(), &'static str> {
                if value_delivery_turn_id.chars().count() < 36 {
                    return Err("invalid broker text");
                }
                if value_delivery_turn_id.chars().count() > 36 {
                    return Err("invalid broker text");
                }
                if !valid_uuid(value_delivery_turn_id) {
                    return Err("invalid video identity");
                }
                if value_delivery_turn_id == "00000000-0000-0000-0000-000000000000" {
                    return Err("invalid broker identity");
                }
                Ok(())
            })
            .transpose()?;
        self.candidates
            .as_ref()
            .map(|value_candidates| -> Result<(), &'static str> {
                if value_candidates.is_empty() {
                    return Err("invalid broker collection");
                }
                if value_candidates.len() > 5 {
                    return Err("invalid broker collection");
                }
                for item in value_candidates.iter() {
                    item.validate()?;
                }
                Ok(())
            })
            .transpose()?;
        let ready = matches!(self.status, VideoStatus::Staged | VideoStatus::Committed);
        if ready != (self.code == VideoCode::Ok)
            || self.artifact_id.is_some() != self.delivery_turn_id.is_some()
            || self.provider_status.is_some() != self.provider_observed_at.is_some()
            || self.candidates.is_some() != self.candidates_complete.is_some()
        {
            return Err("inconsistent video receipt");
        }
        if self.artifact_id.is_some()
            && (self.video_operation_id.is_none()
                || !matches!(self.code, VideoCode::Ok | VideoCode::DeliveryUnconfirmed))
        {
            return Err("invalid artifact reference");
        }
        if ready
            && (self.artifact_id.is_none()
                || !matches!(
                    self.next_action,
                    NextAction::CheckArtifact | NextAction::ReportStatus
                ))
        {
            return Err("invalid delivered receipt");
        }
        if self.status == VideoStatus::Staged && self.next_action != NextAction::CheckArtifact {
            return Err("staged receipt needs ACK check");
        }
        if self.code == VideoCode::DeliveryUnconfirmed
            && (self.artifact_id.is_none()
                || self.status != VideoStatus::Unknown
                || self.next_action != NextAction::ReportStatus)
        {
            return Err("invalid unconfirmed delivery");
        }
        if self.code == VideoCode::SelectionRequired {
            if self.candidates.is_none()
                || self.status != VideoStatus::Blocked
                || self.next_action != NextAction::SelectOperation
                || self.video_operation_id.is_some()
            {
                return Err("invalid selection receipt");
            }
        } else if self.candidates.is_some() {
            return Err("unexpected candidates");
        }
        if matches!(
            self.next_action,
            NextAction::GetVideoResult | NextAction::CheckArtifact
        ) && self.video_operation_id.is_none()
        {
            return Err("missing original operation");
        }
        if self.code == VideoCode::ExpertRequired
            && (self.status != VideoStatus::Blocked || self.next_action != NextAction::UseExpert)
        {
            return Err("invalid expert receipt");
        }
        Ok(())
    }
}
impl std::fmt::Debug for VideoResult {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("VideoResult([redacted])")
    }
}
pub const GENERATE_TOOL_NAME: &str = "generate_video";
pub const GET_TOOL_NAME: &str = "get_video_result";
pub const GENERATE_DESCRIPTION:&str="易界内置视频生成 / Seedance video generation. Use for an explicit new video request with no selected expert. One creation per root; an unresolved video in this conversation blocks another creation. Fixed model and provider defaults; never add --parameter suffixes. Only text-to-video and current-turn images are supported. Video/audio reference, editing or extending an existing clip are unavailable. Do not promise exact duration, resolution, aspect ratio or audio settings. A revision requires an explicitly requested new generation, not an in-place update. image_indices are 1-based current-turn images; first_last_frames allows the same image twice. Do not use for image/video analysis or script writing. A selected expert never falls back here. After acceptance, await get_video_result for the original operation using native Code Mode exec/wait; do not repeat creation, use a CLI, or ask for provider keys.";
pub const GET_DESCRIPTION:&str="查询并取回易界内置视频 / get the original Seedance video result. Never creates a provider task. Omit video_operation_id to resolve this root or the unique unresolved operation in the same conversation; ambiguity returns local candidates only. pending means continue the SAME operation, not failure. Stop cancels local waiting; the remote task may continue. After Artifact handoff, only report the existing Artifact state; never re-register with publish_artifact or download through a CLI. A selected expert blocks builtin recovery. staged is not Native commit; check the existing get_artifact_status.";
pub const GENERATE_PATH: &str = "/internal/media-generation/v1/generate-video";
pub const GET_PATH: &str = "/internal/media-generation/v1/get-video-result";
pub const MAX_REQUEST_BYTES: u64 = 16384;
pub const MAX_RESPONSE_BYTES: u64 = 16384;
pub const BINDING_WAIT_MILLISECONDS: u64 = 5000;
pub const GENERATE_TIMEOUT_SECONDS: u64 = 30;
pub const GET_TIMEOUT_SECONDS: u64 = 120;
pub const TOOL_TIMEOUT_SECONDS: u64 = 150;
pub const TOOL_APPROVAL_MODE: &str = "approve";
pub const MODEL: &str = "doubao-seedance-2-5-260628";
pub const ENDPOINT: &str = "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks";
pub const POLL_INTERVAL_SECONDS: u64 = 10;
pub const QUERY_TIMEOUT_SECONDS: u64 = 15;
pub const DOWNLOAD_TIMEOUT_SECONDS: u64 = 60;
pub const MAX_VIDEO_BYTES: u64 = 67108864;
pub const MAX_DOWNLOADS: u64 = 2;

pub const GENERATE_INPUT_SCHEMA:&str="{\"type\":\"object\",\"additionalProperties\":false,\"required\":[\"prompt\",\"image_mode\",\"image_indices\"],\"properties\":{\"prompt\":{\"type\":\"string\",\"minLength\":1,\"maxLength\":4000,\"pattern\":\"\\\\S\",\"not\":{\"pattern\":\"(^|\\\\s)--(resolution|rs|ratio|rt|ar|duration|dur|frames|fps|framespersecond|seed|camera_fixed|cf|watermark|wm|output_format|generate_audio|draft|return_last_frame|service_tier|execution_expires_after|priority|tools|callback_url)(\\\\s|=|$)\"}},\"image_mode\":{\"type\":\"string\",\"enum\":[\"none\",\"first_frame\",\"first_last_frames\",\"reference_images\"]},\"image_indices\":{\"type\":\"array\",\"maxItems\":10,\"items\":{\"type\":\"integer\",\"minimum\":1,\"maximum\":10}}},\"allOf\":[{\"if\":{\"properties\":{\"image_mode\":{\"const\":\"none\"}}},\"then\":{\"properties\":{\"image_indices\":{\"minItems\":0,\"maxItems\":0}}}},{\"if\":{\"properties\":{\"image_mode\":{\"const\":\"first_frame\"}}},\"then\":{\"properties\":{\"image_indices\":{\"minItems\":1,\"maxItems\":1}}}},{\"if\":{\"properties\":{\"image_mode\":{\"const\":\"first_last_frames\"}}},\"then\":{\"properties\":{\"image_indices\":{\"minItems\":2,\"maxItems\":2}}}},{\"if\":{\"properties\":{\"image_mode\":{\"const\":\"reference_images\"}}},\"then\":{\"properties\":{\"image_indices\":{\"minItems\":1,\"maxItems\":10,\"uniqueItems\":true}}}}]}";
pub const GET_INPUT_SCHEMA:&str="{\"type\":\"object\",\"additionalProperties\":false,\"required\":[],\"properties\":{\"video_operation_id\":{\"type\":\"string\",\"minLength\":36,\"maxLength\":36,\"pattern\":\"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$\",\"not\":{\"const\":\"00000000-0000-0000-0000-000000000000\"}}}}";
fn valid_uuid(value: &str) -> bool {
    value.len() == 36
        && value.bytes().enumerate().all(|(i, b)| {
            if [8, 13, 18, 23].contains(&i) {
                b == b'-'
            } else {
                b.is_ascii_digit() || (b'a'..=b'f').contains(&b)
            }
        })
}
fn forbidden_prompt_parameter(value: &str) -> bool {
    value.split_whitespace().any(|word| {
        word.strip_prefix("--").is_some_and(|rest| {
            [
                "resolution",
                "rs",
                "ratio",
                "rt",
                "ar",
                "duration",
                "dur",
                "frames",
                "fps",
                "framespersecond",
                "seed",
                "camera_fixed",
                "cf",
                "watermark",
                "wm",
                "output_format",
                "generate_audio",
                "draft",
                "return_last_frame",
                "service_tier",
                "execution_expires_after",
                "priority",
                "tools",
                "callback_url",
            ]
            .contains(&rest.split('=').next().unwrap_or(""))
        })
    })
}
impl VideoResult {
    pub fn is_error(&self) -> bool {
        !matches!(
            self.code,
            VideoCode::Ok
                | VideoCode::ProviderPending
                | VideoCode::ExistingOperation
                | VideoCode::SelectionRequired
        )
    }
}
