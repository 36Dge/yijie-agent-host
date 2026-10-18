//! Host-owned transport adapter. Codex owns MCP execution; Host owns ingestion.
#[allow(dead_code)] // Generated policy also serves Desktop.
mod wire {
    include!("types.gen.rs");
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
use serde::Deserialize;
use std::{sync::Arc, time::Duration};
use subtle::ConstantTimeEq;
use tokio::io::{AsyncBufReadExt, BufReader};
use tokio_util::sync::CancellationToken;

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Startup {
    host_origin: String,
    host_token: String,
    capability: String,
}
#[derive(Clone)]
struct Gateway {
    config: Arc<Startup>,
    client: reqwest::Client,
}
fn unavailable() -> ErrorData {
    ErrorData::internal_error("产物登记不可用", None)
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
        let source: serde_json::Value = serde_json::from_str(include_str!(
            "../../api/schemas/artifact-media-v1.schema.json"
        ))
        .map_err(|_| unavailable())?;
        let tool=Tool::new("publish_artifact","将本轮工作区中已生成的 HTML、SVG、MP4 或文件登记为易界聊天产物，以便内置预览和保存。只接受工作区相对路径；不能用于读取任意磁盘文件。先生成文件，再登记；返回 staged 表示宿主接收，界面持久化独立完成。",Arc::new(source["definitions"]["PublishInput"].as_object().ok_or_else(unavailable)?.clone())).with_annotations(ToolAnnotations::new().read_only(false).destructive(false).idempotent(true).open_world(false));
        Ok(ListToolsResult {
            tools: vec![tool],
            ..Default::default()
        })
    }
    async fn call_tool(
        &self,
        request: CallToolRequestParams,
        context: RequestContext<RoleServer>,
    ) -> Result<CallToolResult, ErrorData> {
        if request.name != "publish_artifact" {
            return Err(unavailable());
        }
        let input: wire::PublishInput = serde_json::from_value(serde_json::Value::Object(
            request.arguments.ok_or_else(unavailable)?,
        ))
        .map_err(|_| unavailable())?;
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
        let response = tokio::select! {_=context.ct.cancelled()=>return Err(unavailable()),r=send=>r.map_err(|_|unavailable())?};
        if !response.status().is_success() {
            return Ok(CallToolResult::error(vec![Content::text(
                "文件未登记。请确认文件已在当前工作区生成、类型受支持且未超出大小限制。",
            )]));
        }
        if response.content_length().is_some_and(|n| n > 4096) {
            return Err(unavailable());
        }
        let receipt: wire::PublishReceipt = response.json().await.map_err(|_| unavailable())?;
        Ok(CallToolResult::success(vec![Content::text(
            serde_json::to_string(&receipt).map_err(|_| unavailable())?,
        )]))
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
    let capability = Arc::new(config.capability.clone());
    let gateway = Gateway {
        config: Arc::new(config),
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
    let router = Router::new()
        .route_service("/mcp", service)
        .layer(middleware::from_fn_with_state(capability, authorize));
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
