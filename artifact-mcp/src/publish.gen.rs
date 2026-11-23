// Code generated from workspace-publish-v1; DO NOT EDIT.
#[derive(serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct WorkspacePublishInput {
    pub relative_path: String,
    #[serde(default, deserialize_with = "present_string")]
    pub display_name: Option<String>,
}
fn present_string<'de, D: serde::Deserializer<'de>>(d: D) -> Result<Option<String>, D::Error> {
    <String as serde::Deserialize>::deserialize(d).map(Some)
}
pub const PUBLISH_REASON_HEADER: &str = "X-Yijie-Artifact-Reason";
pub const PRIVATE_NAME_BYTES: usize = 255;
pub const PUBLISH_BODY_BYTES: usize = 8192;
pub fn reason_message(reason: &str) -> &'static str {
    match reason {
        "invalid_request" => "登记参数无效。请提供当前工作区的相对文件路径。",
        "scope_unavailable" => "当前轮次无法登记文件。请确认当前会话仍在执行。",
        "workspace_file_unavailable" => "未能读取当前工作区中的文件，请核对相对路径。",
        "unsupported_type" => "此文件类型暂不支持登记。",
        "invalid_content" => "文件内容为空或未通过对应格式的校验，请检查原文件。",
        "file_too_large" => "文件超过该类型的单文件大小限制。",
        "reader_unavailable" => "当前聊天暂时无法接收此类文档，工作区原文件已保留。",
        "registration_conflict" => "此路径已登记，内容或有效文件名与已有产物不同。",
        "registration_limit" => "产物数量或暂存空间已达到限制。",
        "unavailable" => "产物登记结果未确认。若已有产物 ID，请查询同一 ID 的状态。",
        _ => "产物登记结果未确认。若已有产物 ID，请查询同一 ID 的状态。",
    }
}
