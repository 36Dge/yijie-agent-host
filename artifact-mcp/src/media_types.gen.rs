// Generated from media-generation/v1; DO NOT EDIT.
use serde::{Deserialize, Serialize};

fn optional_non_null<'de, D, T>(d: D) -> Result<Option<T>, D::Error>
where
    D: serde::Deserializer<'de>,
    T: Deserialize<'de>,
{
    T::deserialize(d).map(Some)
}
#[derive(Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", try_from = "GenerateInputWire")]
pub struct GenerateInput {
    pub prompt: String,
    #[serde(rename = "use_current_reference")]
    pub use_current_reference: bool,
}
#[derive(Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct GenerateInputWire {
    pub prompt: String,
    #[serde(rename = "use_current_reference")]
    pub use_current_reference: bool,
}
impl TryFrom<GenerateInputWire> for GenerateInput {
    type Error = &'static str;
    fn try_from(w: GenerateInputWire) -> Result<Self, Self::Error> {
        let v = Self {
            prompt: w.prompt,
            use_current_reference: w.use_current_reference,
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
            if value_prompt.chars().count() > 1500 {
                return Err("invalid broker text");
            }
        }
        if self.prompt.trim().is_empty() || self.prompt.contains(char::from(0)) {
            return Err("invalid image prompt");
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
        }
        {
            let value_turn_id = &self.turn_id;
            if value_turn_id.chars().count() < 36 {
                return Err("invalid broker text");
            }
            if value_turn_id.chars().count() > 36 {
                return Err("invalid broker text");
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
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum GenerateStatus {
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
pub enum GenerateCode {
    #[serde(rename = "ok")]
    Ok,
    #[serde(rename = "expert_required")]
    ExpertRequired,
    #[serde(rename = "state_pending")]
    StatePending,
    #[serde(rename = "outcome_unknown")]
    OutcomeUnknown,
    #[serde(rename = "cancelled")]
    Cancelled,
    #[serde(rename = "service_unavailable")]
    ServiceUnavailable,
    #[serde(rename = "generation_failed")]
    GenerationFailed,
    #[serde(rename = "input_unsupported")]
    InputUnsupported,
    #[serde(rename = "operation_conflict")]
    OperationConflict,
    #[serde(rename = "content_rejected")]
    ContentRejected,
}
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum NextAction {
    #[serde(rename = "check_artifact")]
    CheckArtifact,
    #[serde(rename = "use_expert")]
    UseExpert,
    #[serde(rename = "report_failure")]
    ReportFailure,
}
#[derive(Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", try_from = "GenerateResultWire")]
pub struct GenerateResult {
    pub status: GenerateStatus,
    pub code: GenerateCode,
    #[serde(rename = "next_action")]
    pub next_action: NextAction,
    pub source: String,
    #[serde(rename = "artifact_id")]
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        deserialize_with = "optional_non_null"
    )]
    pub artifact_id: Option<String>,
}
#[derive(Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct GenerateResultWire {
    pub status: GenerateStatus,
    pub code: GenerateCode,
    #[serde(rename = "next_action")]
    pub next_action: NextAction,
    pub source: String,
    #[serde(rename = "artifact_id")]
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        deserialize_with = "optional_non_null"
    )]
    pub artifact_id: Option<String>,
}
impl TryFrom<GenerateResultWire> for GenerateResult {
    type Error = &'static str;
    fn try_from(w: GenerateResultWire) -> Result<Self, Self::Error> {
        let v = Self {
            status: w.status,
            code: w.code,
            next_action: w.next_action,
            source: w.source,
            artifact_id: w.artifact_id,
        };
        v.validate()?;
        Ok(v)
    }
}
impl GenerateResult {
    pub fn validate(&self) -> Result<(), &'static str> {
        {
            let value_source = &self.source;
            if value_source != "builtin" {
                return Err("invalid broker constant");
            }
        }
        self.artifact_id
            .as_ref()
            .map(|value_artifact_id| -> Result<(), &'static str> {
                if value_artifact_id.chars().count() < 36 {
                    return Err("invalid broker text");
                }
                if value_artifact_id.chars().count() > 36 {
                    return Err("invalid broker text");
                }
                Ok(())
            })
            .transpose()?;
        let ready = matches!(
            self.status,
            GenerateStatus::Staged | GenerateStatus::Committed
        );
        if ready != (self.code == GenerateCode::Ok)
            || ready != self.artifact_id.is_some()
            || (ready && self.next_action != NextAction::CheckArtifact)
        {
            return Err("inconsistent image result");
        }
        Ok(())
    }
}
impl std::fmt::Debug for GenerateResult {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("GenerateResult([redacted])")
    }
}

pub const TOOL_NAME: &str = "generate_image";
pub const TOOL_DESCRIPTION:&str="易界内置图片生成 / Seedream image generation. For conversations with no selected expert, generate one image explicitly requested by the user, optionally using their single current-turn reference. A selected expert never falls back to this tool, including unavailable, failed, unsupported or unknown outcomes. Do not generate for image analysis. A blocked/unknown result does not authorize retry or a different provider. Images use the desktop Artifact viewer; staged is not yet committed. Discover this tool through native tool search; do not use an image CLI or ask for provider keys.";
pub const PRIVATE_PATH: &str = "/internal/media-generation/v1/generate-image";
pub const TOOL_TIMEOUT_SECONDS: u64 = 150;
pub const IMAGE_EXPERT_SERVICES: &[(&str, &str, &str, &str)] = &[(
    "linkfox-expert-aigc-imagegen-mannequin-to-model",
    "0.1.0",
    "042a9acb59429f68adccd0a925fa9d83ea8ba1890b2a1b3f23ecd9049703b80c",
    "linkfox",
)];
