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
- FEAT-128 提供默认关闭的 v3 Artifact SSE、encrypted staging、content/poster/ACK/range 与 strict-local synthetic producer；另有默认关闭的 MiniMax `image-01` 中国区文生图/单张人物参考生图 provider projection。
- FEAT-129 提供精确 `local + demo_fast` 下的本地 Skill 查询、扫描、原子安装、启停和卸载，并只将当前 catalog 中 receipt 有效的具体 Skill 目录通过固定 Runtime Skills API 重放；HTTP 边界仍校验 owner-only bearer 与 `plugin.read`/`plugin.manage`。

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

FEAT-126 S10BF1 的测试专用 fake readiness 由 Host 独占权威：
`cmd/feat126-fake-readiness`只在S10 exact-true profile下接受canonical run ID，自行选择冻结的
fixture case并校验loopback fake server。输出分别命名`dataset_id`与`fixture_case_id`；调用方不能
传入或覆盖二者。该探针不属于Host公共API，不改变默认provider、业务wire或Runtime pin。

受保护 API 的 bearer token 位于 `$YIJIE_AGENT_HOST_HOME/api-token`，仅供同一用户的 Desktop/本地开发客户端读取。路由包括：

```text
POST /v1/tasks/{task_id}/agent-sessions
POST /v1/agent-sessions/{agent_session_id}/resume
GET  /v1/agent-sessions/{agent_session_id}
POST /v1/agent-sessions/{agent_session_id}/turns
POST /v1/agent-sessions/{agent_session_id}/turns/{turn_id}/interrupt
GET  /v1/agent-sessions/{agent_session_id}/events
GET  /v3/agent-sessions/{agent_session_id}/events?event_schema_version=3
GET|HEAD /v3/agent-sessions/{agent_session_id}/artifacts/{artifact_id}/content
GET|HEAD /v3/agent-sessions/{agent_session_id}/artifacts/{artifact_id}/poster
POST /v3/agent-sessions/{agent_session_id}/artifacts/{artifact_id}/ack
GET  /v1/skills
POST /v1/skills/scan-operations
POST /v1/skills/{skill_id}/install-operations
PUT  /v1/skills/{skill_id}/enabled
POST /v1/skills/{skill_id}/uninstall-operations
```

Skill 管理只有在 Desktop 显式提供下列精确启动配置时启用：

```bash
export YIJIE_ENV=local
export YIJIE_LOCAL_PROFILE=demo_fast
export YIJIE_SKILL_BUNDLE_ROOT=/absolute/path/to/read-only/skill-packages
export YIJIE_SKILL_INSTALL_ROOT=/absolute/path/to/private/app-data/skills
```

两个根目录必须同时存在、为规范绝对路径且互不重叠；`YIJIE_AGENT_HOST_HOME` 也必须为规范绝对路径。正常 Desktop 流程透明读取并携带 Host bearer，用户不需要登录或手动授权，但直接调用 Host 不能绕过 bearer/capability 校验。内置包的 `desktop-distribution` 许可由 `yijie-skills` 发布治理负责，不因本地技术校验通过而自动获得。

Runtime 不会注册整个 App Data 根；清单外目录、损坏安装和包含额外嵌套 `SKILL.md` 的包均 fail closed。operation ID 不做静默过期或裁剪；当前本地 journal 最多保留 4096 条，达到上限后新 operation 返回 `skill_busy`，旧 ID 仍保持重放/冲突语义。

Desktop 管理的本地进程还会设置 `YIJIE_AGENT_HOST_PARENT_PID`。Host 只在该值与启动瞬间的真实 PPID
精确一致时启用 parent watchdog；Desktop 正常退出、崩溃或被强制终止后，Host 都沿既有 Runtime/HTTP
优雅关闭链自退。独立运行 Host 时不设置该变量，行为保持不变。该 PID 只用于私有进程生命周期，
不会进入探针、状态响应或公共 wire。

v3 route 必须显式设置 `YIJIE_AGENT_HOST_V3_ARTIFACTS_ENABLED=true`。零费用 synthetic profile 还要求
`YIJIE_FEAT128_SYNTHETIC_ENABLED=true` 与固定
`YIJIE_FEAT128_SYNTHETIC_MANIFEST=feat128-artifact-v1`；它只允许 `YIJIE_ENV=local`，且不能与
MiniMax 或其它 fake profile 共启。默认配置不会注册 v3 route，也不会生成 Artifact。

真实图片生成还必须显式设置以下 exact-local conjunction：

```bash
export YIJIE_ENV=local
export YIJIE_MODEL_PROVIDER=minimax
export YIJIE_AGENT_HOST_V2_MULTIMODAL_TURNS_ENABLED=true
export YIJIE_AGENT_HOST_V3_ARTIFACTS_ENABLED=true
export YIJIE_FEAT128_IMAGE_GENERATION_ENABLED=true
```

启用后，Host 才以 `experimentalApi=true` 向固定 Runtime 注册单一 `generate_image` dynamic tool。它固定调用
`https://api.minimaxi.com/v1/image_generation`、模型 `image-01`、base64、`n=1`，仅支持文生图或当前轮单张
PNG/JPEG 人物参考生图；普通看图/分析不会调用。Key 继续使用上述内存或 owner-only Key 文件配置。

未配置 Runtime 时 Host 仍可启动用于诊断：`/healthz` 返回 `200`，`/readyz` 返回 `503`，会话路由不注册。

## 验证

```bash
YIJIE_CONTRACTS_REF=d6dff903e0c12b6a5e69599df1e33ef46d8bea6b make sync-contracts # 从不可变 0.5.0 local candidate 同步并重新生成 DTO
make contract-check # 校验契约版本、快照哈希、相邻源和生成物，无网络依赖
make lint           # gofmt、go vet 和 shell 语法
make test           # 契约同步检查、race 单测和故障测试；不依赖真实 Runtime
make runtime-test  # 固定产物握手、thread 与 Skills 生命周期；不请求模型
make runtime-turn-test # 显式真实测试，最多 2 次短 MiniMax 请求，不在 CI
```

`api/contracts.lock` 固定当前消费的 `0.5.0` local candidate 完整 commit（尚无 tag、未发布）、
`oapi-codegen` identity/version，以及 OpenAPI、Runtime 兼容清单、Agent session
event v1/v2/v3、ReportDocumentV1 与 Skill Bundle Manifest v1 JSON Schema 的 SHA-256，并精确快照 Skill 正常包、摘要损坏包和 Zip Slip 包。同步脚本拒绝 dirty source，并从已锁定 commit 的 Git
对象读取源文件；不得手改 `api/` 快照或 `internal/contracts/agenthost.gen.go`。测试会
验证 ref provenance、generator、digest 与生成漂移。V1/v2 producer conformance 保持原有测试；
v3 S3 已实现但默认关闭，并由 schema、auth/range/integrity/ACK/TTL/restart/synthetic conformance tests 约束。
这不代表真实 provider 的付费验证或端到端 Desktop UI 已完成。

`make runtime-turn-test` 默认从 `.local/secrets/minimax-api-key` 读取 Key，也可使用上述环境变量；脚本和测试不会输出 Key。它验证一次正常完成、Runtime 重启后的 `thread/resume`，以及一次 `turn/interrupt`。
