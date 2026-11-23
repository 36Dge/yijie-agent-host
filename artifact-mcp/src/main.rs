//! Host-owned transport adapter. Codex owns MCP execution; Host owns ingestion.
#[allow(dead_code)] // Generated policy also serves Desktop.
mod wire {
    include!("types.gen.rs");
}
mod publish_wire {
    include!("publish.gen.rs");
}
use axum::{
    Router,
    extract::{Request, State},
    http::StatusCode,
    middleware::{self, Next},
    response::Response,
};
use rmcp::{
    ErrorData, RoleServer, ServerHandler,
    model::*,
    service::RequestContext,
    transport::streamable_http_server::{
        StreamableHttpServerConfig, StreamableHttpService, session::local::LocalSessionManager,
    },
};
use std::{sync::Arc, time::Duration};
use subtle::ConstantTimeEq;
use tokio::io::{AsyncBufReadExt, BufReader};
use tokio_util::sync::CancellationToken;

#[allow(dead_code)]
mod document_wire {
    include!("document_types.gen.rs");
}
type Startup = document_wire::AdapterStartup;
mod media;
mod video;
#[derive(Clone)]
struct Gateway {
    config: Arc<Startup>,
    client: reqwest::Client,
}
fn unavailable() -> ErrorData {
    ErrorData::internal_error("产物登记不可用", None)
}

fn publish_error(reason: &str) -> CallToolResult {
    CallToolResult::error(vec![Content::text(publish_wire::reason_message(reason))])
}

fn publish_input(args: JsonObject) -> Option<wire::PublishInput> {
    if args.len() > 2
        || args.values().any(|v| {
            v.as_str()
                .is_none_or(|s| s.len() > publish_wire::PUBLISH_BODY_BYTES)
        })
        || serde_json::to_vec(&args).ok()?.len() > publish_wire::PUBLISH_BODY_BYTES
    {
        return None;
    }
    let input: publish_wire::WorkspacePublishInput =
        serde_json::from_value(serde_json::Value::Object(args)).ok()?;
    let path = input.relative_path;
    if path.is_empty() || path.len() > 1024 {
        return None;
    }
    let basename = path.rsplit('/').next().unwrap_or_default();
    let display_name = [input.display_name.as_deref().unwrap_or_default(), basename]
        .into_iter()
        .find(|name| !name.is_empty() && name.len() <= publish_wire::PRIVATE_NAME_BYTES)
        .map(str::to_owned)
        .unwrap_or_else(|| match basename.rsplit_once('.').map(|(_, ext)| ext) {
            Some(ext)
                if !ext.is_empty()
                    && ext.len() + 9 <= publish_wire::PRIVATE_NAME_BYTES
                    && ext.bytes().all(|b| b.is_ascii_alphanumeric()) =>
            {
                format!("artifact.{ext}")
            }
            _ => "artifact".into(),
        });
    Some(wire::PublishInput {
        relative_path: path,
        display_name,
    })
}

#[cfg(test)]
mod workspace_publish_tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn model_arguments_fit_the_existing_private_request() {
        for (args, expected) in [
            (
                json!({"relative_path":"exports/报告.docx"}),
                Some("报告.docx"),
            ),
            (
                json!({"relative_path":"q.png", "display_name":"百度（https://example.com）"}),
                Some("百度（https://example.com）"),
            ),
            (
                json!({"relative_path":"q.png", "display_name":"图".repeat(86)}),
                Some("q.png"),
            ),
            (
                json!({"relative_path":"报告.pptx", "display_name":""}),
                Some("报告.pptx"),
            ),
            (json!({"relative_path":"q.png", "display_name":null}), None),
        ] {
            let args = args.as_object().unwrap().clone();
            let path = args["relative_path"].as_str().unwrap().to_owned();
            let input = publish_input(args);
            assert_eq!(input.as_ref().map(|v| v.display_name.as_str()), expected);
            if let Some(value) = input {
                assert_eq!(value.relative_path, path);
            }
        }
        assert_eq!(
            publish_wire::reason_message("future_reason"),
            publish_wire::reason_message("unavailable")
        );
    }
}

impl ServerHandler for Gateway {
    fn get_info(&self) -> ServerInfo {
        ServerInfo::new(ServerCapabilities::builder().enable_tools().build())
            .with_server_info(Implementation::new("yijie-artifacts", "0.1.0"))
    }
    async fn list_tools(
        &self,
        _: Option<PaginatedRequestParams>,
        _: RequestContext<RoleServer>,
    ) -> Result<ListToolsResult, ErrorData> {
        let mut schema: JsonObject = serde_json::from_str(include_str!(
            "../../api/schemas/workspace-publish-v1.schema.json"
        ))
        .map_err(|_| unavailable())?;
        let description = schema
            .get("description")
            .and_then(|v| v.as_str())
            .ok_or_else(unavailable)?
            .to_owned();
        schema.retain(|key, _| {
            matches!(
                key.as_str(),
                "type" | "properties" | "required" | "additionalProperties" | "examples"
            )
        });
        let tool = Tool::new("publish_artifact", description, Arc::new(schema)).with_annotations(
            ToolAnnotations::new()
                .read_only(false)
                .destructive(false)
                .idempotent(true)
                .open_world(false),
        );
        let mut tools = vec![tool];
        {
            let schema: serde_json::Value = serde_json::from_str(include_str!(
                "../../api/schemas/document-runtime-v1.schema.json"
            ))
            .map_err(|_| unavailable())?;
            for (name, definition, description) in [
                (
                    "load_document_environment",
                    "EnvironmentInput",
                    "查询应用内置九格式文档环境和对应指南。生成或解析文档前先调用，使用返回的专用Python/helper路径；不使用系统环境或安装市场技能。此工具只读，不执行代码。",
                ),
                (
                    "get_artifact_status",
                    "StatusInput",
                    "查询当前会话产物在本Host代际的交付事实。只有committed表示已收到Native提交ACK；staged仅暂存，unknown不能推断失败或重放生成。",
                ),
            ] {
                if name == "load_document_environment"
                    && !self.config.document_runtime_enabled.unwrap_or(false)
                {
                    continue;
                }
                tools.push(
                    Tool::new(
                        name,
                        description,
                        Arc::new(
                            schema["definitions"][definition]
                                .as_object()
                                .ok_or_else(unavailable)?
                                .clone(),
                        ),
                    )
                    .with_annotations(
                        ToolAnnotations::new()
                            .read_only(true)
                            .destructive(false)
                            .idempotent(true)
                            .open_world(false),
                    ),
                );
            }
        }
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
        if matches!(
            request.name.as_ref(),
            "load_document_environment" | "get_artifact_status"
        ) {
            return self.document_tool(request, context).await;
        }
        if request.name != "publish_artifact" {
            return Err(unavailable());
        }
        let Some(input) = request.arguments.and_then(publish_input) else {
            return Ok(publish_error("invalid_request"));
        };
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
        let input = wire::PublishRequest {
            thread_id: thread.into(),
            turn_id: turn.into(),
            input,
        };
        let send = self
            .client
            .post(format!(
                "{}/internal/artifact-media/v1/publish",
                self.config.host_origin
            ))
            .bearer_auth(&self.config.host_token)
            .json(&input)
            .send();
        let mut response = tokio::select! {
            _ = context.ct.cancelled() => return Err(unavailable()),
            result = send => match result {
                Ok(response) => response,
                Err(_) => return Ok(publish_error("unavailable")),
            }
        };
        if !response.status().is_success() {
            let reason = response
                .headers()
                .get(publish_wire::PUBLISH_REASON_HEADER)
                .and_then(|value| value.to_str().ok())
                .unwrap_or_default();
            return Ok(publish_error(reason));
        }
        if response.content_length().is_some_and(|n| n > 4096) {
            return Ok(publish_error("unavailable"));
        }
        let mut bytes = Vec::new();
        loop {
            match response.chunk().await {
                Ok(Some(chunk)) if bytes.len() + chunk.len() <= 4096 => {
                    bytes.extend_from_slice(&chunk)
                }
                Ok(None) => break,
                _ => return Ok(publish_error("unavailable")),
            }
        }
        let Ok(receipt) = serde_json::from_slice::<wire::PublishReceipt>(&bytes) else {
            return Ok(publish_error("unavailable"));
        };
        if receipt.status != "staged" || receipt.artifact_id.is_empty() {
            return Ok(publish_error("unavailable"));
        }
        Ok(CallToolResult::success(vec![Content::text(
            serde_json::to_string(&receipt).map_err(|_| unavailable())?,
        )]))
    }
}
impl Gateway {
    async fn document_tool(
        &self,
        request: CallToolRequestParams,
        context: RequestContext<RoleServer>,
    ) -> Result<CallToolResult, ErrorData> {
        if request.name == "load_document_environment"
            && !self.config.document_runtime_enabled.unwrap_or(false)
        {
            return Err(unavailable());
        }
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
        let args = serde_json::Value::Object(request.arguments.unwrap_or_default());
        let (path, body) = if request.name == "load_document_environment" {
            let input: document_wire::EnvironmentInput =
                serde_json::from_value(args).map_err(|_| unavailable())?;
            (
                "environment",
                serde_json::to_value(document_wire::EnvironmentRequest {
                    thread_id: thread.into(),
                    turn_id: turn.into(),
                    input,
                })
                .map_err(|_| unavailable())?,
            )
        } else {
            let input: document_wire::StatusInput =
                serde_json::from_value(args).map_err(|_| unavailable())?;
            (
                "status",
                serde_json::to_value(document_wire::StatusRequest {
                    thread_id: thread.into(),
                    turn_id: turn.into(),
                    input,
                })
                .map_err(|_| unavailable())?,
            )
        };
        let send = self
            .client
            .post(format!(
                "{}/internal/document-runtime/v1/{path}",
                self.config.host_origin
            ))
            .bearer_auth(&self.config.host_token)
            .timeout(Duration::from_secs(10))
            .json(&body)
            .send();
        let mut response = tokio::select! {_=context.ct.cancelled()=>return Err(unavailable()),r=send=>r.map_err(|_|unavailable())?};
        if !response.status().is_success() {
            return Ok(CallToolResult::error(vec![Content::text(
                "内置文档能力当前不可用；未执行生成或安装，也未回落系统环境。",
            )]));
        }
        let mut bytes = Vec::new();
        loop {
            let chunk = tokio::select! {_=context.ct.cancelled()=>return Err(unavailable()),r=response.chunk()=>r.map_err(|_|unavailable())?};
            let Some(chunk) = chunk else { break };
            if bytes.len() + chunk.len() > 16 * 1024 {
                return Err(unavailable());
            };
            bytes.extend_from_slice(&chunk);
        }
        let normalized = if path == "environment" {
            let value: document_wire::EnvironmentResponse =
                serde_json::from_slice(&bytes).map_err(|_| unavailable())?;
            serde_json::to_string(&value)
        } else {
            let value: document_wire::StatusResponse =
                serde_json::from_slice(&bytes).map_err(|_| unavailable())?;
            serde_json::to_string(&value)
        }
        .map_err(|_| unavailable())?;
        Ok(CallToolResult::success(vec![Content::text(normalized)]))
    }
}

async fn authorize(
    State(capability): State<Arc<String>>,
    request: Request,
    next: Next,
) -> Result<Response, StatusCode> {
    if request.headers().contains_key("origin") {
        return Err(StatusCode::FORBIDDEN);
    }
    let expected = format!("Bearer {}", capability);
    let supplied = request
        .headers()
        .get("authorization")
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    if !bool::from(expected.as_bytes().ct_eq(supplied.as_bytes())) {
        return Err(StatusCode::UNAUTHORIZED);
    }
    Ok(next.run(request).await)
}
#[tokio::main]
async fn main() {
    if run().await.is_err() {
        eprintln!("artifact_adapter_unavailable");
        std::process::exit(1);
    }
}
async fn run() -> Result<(), Box<dyn std::error::Error>> {
    let mut input = BufReader::new(tokio::io::stdin());
    let mut line = String::new();
    input.read_line(&mut line).await?;
    if line.len() > 8192 {
        return Err("startup limit".into());
    }
    let config: Startup = serde_json::from_str(&line)?;
    line.clear();
    let url = reqwest::Url::parse(&config.host_origin)?;
    if url.scheme() != "http"
        || url.host_str() != Some("127.0.0.1")
        || url.path() != "/"
        || url.port().is_none()
        || url.query().is_some()
        || config.capability.len() != 64
    {
        return Err("local authority required".into());
    }
    let media_capability = match &config.media {
        None => None,
        Some(media)
            if media.capability.len() == 64
                && media.host_token.len() == 64
                && media.capability != media.host_token
                && media.capability != config.capability
                && media.host_token != config.host_token =>
        {
            Some(Arc::new(media.capability.clone()))
        }
        _ => return Err("separate media authority required".into()),
    };
    let capability = Arc::new(config.capability.clone());
    let config = Arc::new(config);
    let media_gateway = media::MediaGateway::new(config.clone())?;
    let gateway = Gateway {
        config,
        client: reqwest::Client::builder()
            .no_proxy()
            .redirect(reqwest::redirect::Policy::none())
            .timeout(Duration::from_secs(30))
            .build()?,
    };
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await?;
    let addr = listener.local_addr()?;
    let shutdown = CancellationToken::new();
    let service = StreamableHttpService::new(
        move || Ok(gateway.clone()),
        Arc::new(LocalSessionManager::default()),
        StreamableHttpServerConfig::default()
            .with_cancellation_token(shutdown.clone())
            .with_allowed_hosts([addr.to_string()]),
    );
    let mut router = Router::new()
        .route_service("/mcp", service)
        .layer(middleware::from_fn_with_state(capability, authorize));
    if let Some(capability) = media_capability {
        let service = StreamableHttpService::new(
            move || Ok(media_gateway.clone()),
            Arc::new(LocalSessionManager::default()),
            StreamableHttpServerConfig::default()
                .with_cancellation_token(shutdown.clone())
                .with_allowed_hosts([addr.to_string()]),
        );
        router = router.merge(
            Router::new()
                .route_service("/media/mcp", service)
                .layer(middleware::from_fn_with_state(capability, authorize)),
        );
    }
    println!("http://{addr}/mcp");
    let stop = shutdown.clone();
    let server = tokio::spawn(async move {
        axum::serve(listener, router)
            .with_graceful_shutdown(stop.cancelled_owned())
            .await
    });
    // EOF is the only process-control operation. Never kill a child or abandon a join.
    while input.read_line(&mut line).await? != 0 {
        line.clear();
    }
    shutdown.cancel();
    server.await??;
    Ok(())
}
