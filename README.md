# yijie-agent-host

易界业务系统与 Codex Runtime 之间的薄宿主和安全适配层。它不实现 planner、业务数据库或平台连接器。

## 当前状态

Agent Host Runtime Baseline 2 已完成真实 thread/turn 的只读最小垂直切片：

- 只接受 `yijie-codex` Runtime Baseline 0 固定的 `codex-cli 0.144.6`；
- Host 管理 Runtime 与专用 `CODEX_HOME`，只向子进程注入显式配置的 MiniMax Key；
- MiniMax 中国站 `https://api.minimaxi.com/v1`、Responses API、`MiniMax-M3`；
- 支持 `thread/start`、`thread/resume`、`turn/start` 和 `turn/interrupt`；
- 将要求的 8 类 Runtime 通知映射为稳定、脱敏、可关联的 SSE 事件；
- HTTP/SSE 接口以 `yijie-contracts` 的 Agent Host OpenAPI 为权威源，Host 固定契约快照并直接使用生成 DTO；
- 使用 bbolt 持久化 `task_id → agent_session_id → codex_thread_id → turn_id`，不持久化消息正文；
- 每进程事件流有独立 `stream_id` 和单调 `sequence`，支持有界进程内重放；
- 会话 HTTP API 使用 Host 自动生成的本机 bearer token；Runtime 固定 `read-only`、`approvalPolicy=never`。

完整范围和证据见 [`docs/runtime-baseline-2.md`](docs/runtime-baseline-2.md)。Baseline 1 的产物与生命周期基线仍见 [`docs/runtime-baseline-1.md`](docs/runtime-baseline-1.md)。

## 本地运行

仓库已忽略 `.local/`。首次使用时创建开发目录，并把 MiniMax Key 写入 owner-only 文件；以下命令中的绝对路径按本机仓库位置替换：

```bash
export YIJIE_CODEX_BINARY=/absolute/path/to/yijie-codex/.yijie/build/macos/aarch64-apple-darwin/codex
export YIJIE_CODEX_MANIFEST=/absolute/path/to/yijie-codex/.yijie/build/macos/aarch64-apple-darwin/runtime-manifest.json
export YIJIE_CODEX_HOME=/absolute/path/to/yijie-agent-host/.local/codex-home
export YIJIE_AGENT_HOST_HOME=/absolute/path/to/yijie-agent-host/.local/host-home
export YIJIE_MODEL_PROVIDER=minimax
export YIJIE_MINIMAX_API_KEY_FILE=/absolute/path/to/yijie-agent-host/.local/secrets/minimax-api-key
make dev
```

Key 文件必须是普通文件且权限不宽于 `0600`。Host 只监听 `127.0.0.1:18080`。探针无需认证：

```bash
curl http://127.0.0.1:18080/healthz
curl http://127.0.0.1:18080/readyz
curl http://127.0.0.1:18080/v1/status
```

受保护 API 的 bearer token 位于 `$YIJIE_AGENT_HOST_HOME/api-token`，仅供同一用户的 Desktop/本地开发客户端读取。路由包括：

```text
POST /v1/tasks/{task_id}/agent-sessions
POST /v1/agent-sessions/{agent_session_id}/resume
GET  /v1/agent-sessions/{agent_session_id}
POST /v1/agent-sessions/{agent_session_id}/turns
POST /v1/agent-sessions/{agent_session_id}/turns/{turn_id}/interrupt
GET  /v1/agent-sessions/{agent_session_id}/events
```

未配置 Runtime 时 Host 仍可启动用于诊断：`/healthz` 返回 `200`，`/readyz` 返回 `503`，会话路由不注册。

## 验证

```bash
YIJIE_CONTRACTS_REF=contracts-v0.2.0 make sync-contracts # 从不可移动 tag 同步并重新生成 DTO
make contract-check # 校验契约版本、快照哈希、相邻源和生成物，无网络依赖
make lint           # gofmt、go vet 和 shell 语法
make test           # 契约同步检查、race 单测和故障测试；不依赖真实 Runtime
make runtime-test  # 固定产物握手 + MiniMax 配置/thread 启动；不请求模型
make runtime-turn-test # 显式真实测试，最多 2 次短 MiniMax 请求，不在 CI
```

`api/contracts.lock` 固定当前消费的 `contracts-v0.2.0` tag、完整 commit、
`oapi-codegen` identity/version，以及 OpenAPI、Runtime 兼容清单和 Agent session
事件 JSON Schema 的 SHA-256。同步脚本拒绝 dirty source，并从已锁定 commit 的 Git
对象读取源文件；不得手改 `api/` 快照或 `internal/contracts/agenthost.gen.go`。测试会
验证 tag provenance、generator、digest、生成漂移，并用快照 Schema 校验 Host 实际
序列化的 8 类事件。

`make runtime-turn-test` 默认从 `.local/secrets/minimax-api-key` 读取 Key，也可使用上述环境变量；脚本和测试不会输出 Key。它验证一次正常完成、Runtime 重启后的 `thread/resume`，以及一次 `turn/interrupt`。
