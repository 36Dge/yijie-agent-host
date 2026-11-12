//! Application-owned media MCP; the Host retains admission, provider and Artifact ownership.
use crate::Startup;
use rmcp::{ErrorData, RoleServer, ServerHandler, model::*, service::RequestContext};
use std::{sync::Arc, time::Duration};

#[allow(dead_code)]
mod wire {
    include!("media_types.gen.rs");
}

#[derive(Clone)]
pub(crate) struct MediaGateway {
    config: Arc<Startup>,
    client: reqwest::Client,
}

fn unavailable() -> ErrorData {
    ErrorData::internal_error("易界内置生图暂不可用", None)
}

impl MediaGateway {
    pub(crate) fn new(config: Arc<Startup>) -> Result<Self, reqwest::Error> {
        Ok(Self {
            config,
            client: reqwest::Client::builder()
                .no_proxy()
                .redirect(reqwest::redirect::Policy::none())
                .timeout(Duration::from_secs(145))
                .build()?,
        })
    }
}

impl ServerHandler for MediaGateway {
    fn get_info(&self) -> ServerInfo {
        ServerInfo::new(ServerCapabilities::builder().enable_tools().build())
            .with_server_info(Implementation::new("yijie-media", "0.1.0"))
    }

    async fn list_tools(
        &self,
        _: Option<PaginatedRequestParams>,
        _: RequestContext<RoleServer>,
    ) -> Result<ListToolsResult, ErrorData> {
        let schema: serde_json::Value = serde_json::from_str(include_str!(
            "../../api/schemas/media-generation-v1.schema.json"
        ))
        .map_err(|_| unavailable())?;
        let tool = Tool::new(
            wire::TOOL_NAME,
            wire::TOOL_DESCRIPTION,
            Arc::new(
                schema["$defs"]["GenerateInput"]
                    .as_object()
                    .ok_or_else(unavailable)?
                    .clone(),
            ),
        )
        .with_annotations(
            ToolAnnotations::new()
                .read_only(false)
                .destructive(false)
                .idempotent(false)
                .open_world(true),
        );
        let mut tools = vec![tool];
        tools.extend(crate::video::tools()?);
        Ok(ListToolsResult {
            tools,
            ..Default::default()
        })
    }

    async fn call_tool(
        &self,
        request: CallToolRequestParams,
        context: RequestContext<RoleServer>,
    ) -> Result<CallToolResult, ErrorData> {
        if crate::video::is_tool(&request.name) {
            return crate::video::call(&self.config, &self.client, request, context).await;
        }
        if request.name != wire::TOOL_NAME {
            return Err(unavailable());
        }
        let input: wire::GenerateInput = serde_json::from_value(serde_json::Value::Object(
            request.arguments.ok_or_else(unavailable)?,
        ))
        .map_err(|_| ErrorData::invalid_params("需要图片描述和当前参考图选择", None))?;
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
        let body = wire::GenerateRequest {
            thread_id: thread.into(),
            turn_id: turn.into(),
            input,
        };
        body.validate().map_err(|_| unavailable())?;
        let capability = self.config.media.as_ref().ok_or_else(unavailable)?;
        let send = self
            .client
            .post(format!("{}{}", self.config.host_origin, wire::PRIVATE_PATH))
            .bearer_auth(&capability.host_token)
            .json(&body)
            .send();
        let mut response = tokio::select! {
            _ = context.ct.cancelled() => return Err(unavailable()),
            result = send => result.map_err(|_| unavailable())?,
        };
        if !response.status().is_success() {
            return Ok(CallToolResult::error(vec![Content::text(
                "生图请求未获准。请报告状态；不要换用脚本、索取Key或重复生成。",
            )]));
        }
        let mut bytes = Vec::new();
        loop {
            let chunk = tokio::select! {
                _ = context.ct.cancelled() => return Err(unavailable()),
                result = response.chunk() => result.map_err(|_| unavailable())?,
            };
            let Some(chunk) = chunk else { break };
            if bytes.len() + chunk.len() > 16 << 10 {
                return Err(unavailable());
            }
            bytes.extend_from_slice(&chunk);
        }
        let receipt: wire::GenerateResult =
            serde_json::from_slice(&bytes).map_err(|_| unavailable())?;
        let success = receipt.code == wire::GenerateCode::Ok;
        let instruction = match receipt.next_action {
            wire::NextAction::CheckArtifact => {
                "图片已生成。staged仅表示暂存；使用现有get_artifact_status核对提交，不要重新生成。"
            }
            wire::NextAction::UseExpert => {
                "本次仅可使用已选专家的MCP工具；不可用时报告原因，不切换内置生图。"
            }
            wire::NextAction::ReportFailure => {
                "如实报告返回状态。未知结果、取消或失败不授权重新提交；不要使用CLI或索取供应商Key。"
            }
        };
        let mut result =
            CallToolResult::structured(serde_json::to_value(&receipt).map_err(|_| unavailable())?);
        result.content.push(Content::text(instruction));
        result.is_error = Some(!success);
        Ok(result)
    }
}
