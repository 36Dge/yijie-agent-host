// Generated from artifact/media-v1; DO NOT EDIT.
#[derive(Clone, serde::Serialize, serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct PublishInput {
    pub relative_path: String,
    pub display_name: String,
}
#[derive(Clone, serde::Serialize, serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct PublishRequest {
    pub thread_id: String,
    pub turn_id: String,
    pub input: PublishInput,
}
#[derive(Clone, serde::Serialize, serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct PublishReceipt {
    pub artifact_id: String,
    pub status: String,
}
pub fn media_type_allowed(kind: &str, mime: &str) -> bool {
    match kind {
        "image" => matches!(
            mime,
            "image/png" | "image/jpeg" | "image/webp" | "image/svg+xml"
        ),
        "video" => matches!(mime, "video/mp4"),
        "file" => matches!(
            mime,
            "text/plain"
                | "text/csv"
                | "application/json"
                | "application/pdf"
                | "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
                | "text/html"
        ),
        "report" => matches!(mime, "application/vnd.yijie.report+json;version=1"),
        _ => false,
    }
}
