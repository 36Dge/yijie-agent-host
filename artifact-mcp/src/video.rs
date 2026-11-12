//! Two ordinary MCP tools, sharing the existing media endpoint and capability.
//! All operation state, provider access and Artifact handoff remain in the Host.
use crate::Startup;
use rmcp::{ErrorData, RoleServer, model::*, service::RequestContext};
use std::sync::Arc;

#[allow(dead_code)]
mod wire {
    include!("video_types.gen.rs");
}

fn unavailable() -> ErrorData {
    ErrorData::internal_error("视频状态暂未确认；请查询原任务，不要重新生成", None)
}

pub(crate) fn is_tool(name: &str) -> bool {
    matches!(name, wire::GENERATE_TOOL_NAME | wire::GET_TOOL_NAME)
}

pub(crate) fn tools() -> Result<Vec<Tool>, ErrorData> {
    [
        (
            wire::GENERATE_TOOL_NAME,
            wire::GENERATE_DESCRIPTION,
            wire::GENERATE_INPUT_SCHEMA,
            false,
        ),
        (
            wire::GET_TOOL_NAME,
            wire::GET_DESCRIPTION,
            wire::GET_INPUT_SCHEMA,
            true,
        ),
    ]
    .into_iter()
    .map(|(name, description, schema, idempotent)| {
        let input = serde_json::from_str::<serde_json::Value>(schema).map_err(|_| unavailable())?;
        Ok(Tool::new(
            name,
            description,
            Arc::new(input.as_object().ok_or_else(unavailable)?.clone()),
        )
        .with_annotations(
            ToolAnnotations::new()
                .read_only(false)
                .destructive(false)
                .idempotent(idempotent)
                .open_world(true),
        ))
    })
    .collect()
}

pub(crate) async fn call(
    config: &Startup,
    client: &reqwest::Client,
    request: CallToolRequestParams,
    context: RequestContext<RoleServer>,
) -> Result<CallToolResult, ErrorData> {
    let thread = context
        .meta
        .0
        .get("threadId")
        .and_then(|v| v.as_str())
        .ok_or_else(unavailable)?;
    let turn = context
        .meta
        .0
        .get("x-codex-turn-metadata")
        .and_then(|v| v.get("turn_id"))
        .and_then(|v| v.as_str())
        .ok_or_else(unavailable)?;
    let input = serde_json::Value::Object(request.arguments.unwrap_or_default());
    let (path, body) = match request.name.as_ref() {
        wire::GENERATE_TOOL_NAME => {
            let input = serde_json::from_value(input).map_err(|_| {
                ErrorData::invalid_params("需要视频描述、图片模式和当前图片编号", None)
            })?;
            let body = wire::GenerateRequest {
                thread_id: thread.into(),
                turn_id: turn.into(),
                input,
            };
            body.validate().map_err(|_| unavailable())?;
            (
                wire::GENERATE_PATH,
                serde_json::to_value(body).map_err(|_| unavailable())?,
            )
        }
        wire::GET_TOOL_NAME => {
            let input = serde_json::from_value(input).map_err(|_| {
                ErrorData::invalid_params(
                    "视频任务编号不受支持；可以省略编号查找当前聊天的任务",
                    None,
                )
            })?;
            let body = wire::GetRequest {
                thread_id: thread.into(),
                turn_id: turn.into(),
                input,
            };
            body.validate().map_err(|_| unavailable())?;
            (
                wire::GET_PATH,
                serde_json::to_value(body).map_err(|_| unavailable())?,
            )
        }
        _ => return Err(unavailable()),
    };
    let capability = config.media.as_ref().ok_or_else(unavailable)?;
    let send = client
        .post(format!("{}{}", config.host_origin, path))
        .bearer_auth(&capability.host_token)
        .json(&body)
        .send();
    let mut response = tokio::select! {
        _ = context.ct.cancelled() => return Err(unavailable()),
        response = send => response.map_err(|_| unavailable())?,
    };
    if !response.status().is_success() {
        return Ok(CallToolResult::error(vec![Content::text(
            "视频请求未获准。保留专家选择并报告状态；不要换用脚本、索取 Key 或重复创建任务。",
        )]));
    }
    let mut bytes = Vec::new();
    loop {
        let chunk = tokio::select! {
            _ = context.ct.cancelled() => return Err(unavailable()),
            result = response.chunk() => result.map_err(|_| unavailable())?,
        };
        let Some(chunk) = chunk else { break };
        if bytes.len() + chunk.len() > wire::MAX_RESPONSE_BYTES as usize {
            return Err(unavailable());
        }
        bytes.extend_from_slice(&chunk);
    }
    receipt_result(&bytes)
}

fn receipt_result(bytes: &[u8]) -> Result<CallToolResult, ErrorData> {
    let receipt: wire::VideoResult = serde_json::from_slice(bytes).map_err(|_| unavailable())?;
    let instruction = match receipt.next_action {
        wire::NextAction::GetVideoResult => {
            "继续用 get_video_result 查询同一个 video_operation_id。pending 不是失败；通过原生 exec/wait 等待，不重复创建。"
        }
        wire::NextAction::SelectOperation => {
            "当前聊天有多个视频任务，请让用户选择返回的任务编号；不猜测最新任务，不重新生成。"
        }
        wire::NextAction::CheckArtifact => {
            "视频已交接。staged 仅表示暂存；用原有 get_artifact_status 核对 Native 提交，不要再次下载、发布或生成。"
        }
        wire::NextAction::UseExpert => {
            "本次仅可使用已选专家的视频工具；不可用或失败时明确报告，保留选择，不调用内置视频。"
        }
        wire::NextAction::ReportStatus => {
            "如实报告状态。停止只结束本地等待，远端任务可能继续；恢复查询原任务，不以未知、失败或取消为由自动重新生成。"
        }
    };
    let mut result =
        CallToolResult::structured(serde_json::to_value(&receipt).map_err(|_| unavailable())?);
    result.content.push(Content::text(instruction));
    result.is_error = Some(receipt.is_error());
    Ok(result)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn shared_video_examples_and_receipt_semantics() {
        let examples: serde_json::Value = serde_json::from_str(include_str!(
            "../../api/fixtures/video-generation-v1.examples.json"
        ))
        .unwrap();
        for example in examples.as_array().unwrap() {
            let value = example["value"].clone();
            let valid = match example["type"].as_str().unwrap() {
                "GenerateInput" => serde_json::from_value::<wire::GenerateInput>(value).is_ok(),
                "GetInput" => serde_json::from_value::<wire::GetInput>(value).is_ok(),
                "VideoResult" => receipt_result(&serde_json::to_vec(&value).unwrap()).is_ok(),
                _ => panic!("unhandled example type"),
            };
            assert_eq!(
                valid,
                example["valid"].as_bool().unwrap(),
                "{}",
                example["name"]
            );
            if valid && example["type"] == "VideoResult" {
                let result =
                    receipt_result(&serde_json::to_vec(&example["value"]).unwrap()).unwrap();
                let normal = matches!(
                    example["value"]["code"].as_str().unwrap(),
                    "ok" | "provider_pending" | "existing_operation" | "selection_required"
                );
                assert_eq!(result.is_error, Some(!normal));
            }
        }
        let tools = tools().unwrap();
        assert_eq!(tools.len(), 2);
        for tool in tools {
            assert!(
                !serde_json::to_string(&tool.input_schema)
                    .unwrap()
                    .contains("$ref")
            );
        }
    }
}
