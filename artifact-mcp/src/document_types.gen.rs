// Generated from document-runtime/v1; DO NOT EDIT.
#[derive(Clone, serde::Serialize, serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct EnvironmentInput {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub format: Option<String>,
}
#[derive(Clone, serde::Serialize, serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct StatusInput {
    pub artifact_id: String,
}
#[derive(Clone, serde::Serialize, serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct EnvironmentRequest {
    pub thread_id: String,
    pub turn_id: String,
    pub input: EnvironmentInput,
}
#[derive(Clone, serde::Serialize, serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct StatusRequest {
    pub thread_id: String,
    pub turn_id: String,
    pub input: StatusInput,
}
#[derive(Clone, serde::Serialize, serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Guide {
    pub format: String,
    pub path: String,
}
#[derive(Clone, serde::Serialize, serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Limits {
    pub generated_file_bytes: u64,
    pub text_source_bytes: u64,
    pub max_pages: u64,
    pub max_sheets: u64,
    pub max_cells: u64,
    pub max_visual_tiles: u64,
    pub max_artifacts: u64,
    pub step_soft_timeout_seconds: u64,
    pub job_soft_timeout_seconds: u64,
}
#[derive(Clone, serde::Serialize, serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct EnvironmentResponse {
    pub schema_version: u64,
    pub status: String,
    pub code: String,
    pub bundle_id: String,
    pub platform: String,
    pub python_path: String,
    pub helper_path: String,
    pub renderer_path: String,
    pub guides: Vec<Guide>,
    pub limits: Limits,
}
#[derive(Clone, serde::Serialize, serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct StatusResponse {
    pub schema_version: u64,
    pub artifact_id: String,
    pub status: String,
}
#[derive(Clone, serde::Serialize, serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct BundleFile {
    pub path: String,
    pub kind: String,
    pub sha256: String,
    pub size_bytes: u64,
    pub link_target: String,
}
#[derive(Clone, serde::Serialize, serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct BundleManifest {
    pub schema_version: u64,
    pub bundle_id: String,
    pub platform: String,
    pub minimum_macos: String,
    pub python_version: String,
    pub libreoffice_version: String,
    pub python: String,
    pub helper: String,
    pub renderer: String,
    pub libreoffice: String,
    pub fonts: Vec<String>,
    pub guides: Vec<Guide>,
    pub limits: Limits,
    pub files: Vec<BundleFile>,
}
#[derive(Clone, serde::Serialize, serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct AdapterStartup {
    pub host_origin: String,
    pub host_token: String,
    pub capability: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub document_runtime_enabled: Option<bool>,
}
#[derive(Clone, serde::Serialize, serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct InputReceipt {
    pub schema_version: u64,
    pub operation_id: String,
    pub attachment_id: String,
    pub sha256: String,
    pub size_bytes: u64,
    pub format: String,
    pub expires_at: u64,
}
