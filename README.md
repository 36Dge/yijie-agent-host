# yijie-agent-host

FEAT-137 已由 Owner 永久终止，未完成验收。生产入口拒绝旧审批/D4 producer 开关；现行 Runtime 使用保留的 FEAT-136 双补丁制品，维持 `read-only/never`。退役来源：Contracts `4d3f967938dde1c86ca34003a0a5628717f96262` 的 `docs/retirements/FEAT-137.json`。旧 v6 实现与四补丁测试材料仅作历史保留。

易界业务系统与 Codex Runtime 之间的薄宿主和安全适配层。它不实现 planner、业务数据库或平台连接器。

## 当前状态

Agent Host Runtime Baseline 2 已完成真实 thread/turn 的只读最小垂直切片：

- 只接受 `yijie-codex` Runtime Baseline 0 固定的 `codex-cli 0.144.6`；
- Host 管理 Runtime 与专用 `CODEX_HOME`，只向子进程注入显式配置的 MiniMax Key；
- MiniMax 中国站 `https://api.minimaxi.com/v1`、Responses API、`MiniMax-M3`；
- 支持 `thread/start`、`thread/resume`、`turn/start` 和 `turn/interrupt`；
- 将受支持的 Runtime 通知映射为稳定、脱敏、可关联的 SSE 事件；
- HTTP/SSE 接口以 `yijie-contracts` 的 Agent Host OpenAPI 为权威源，Host 固定契约快照并直接使用生成 DTO；
- 使用 bbolt 持久化 `task_id → agent_session_id → codex_thread_id → turn_id`，不持久化消息正文；
- 每进程事件流有独立 `stream_id` 和单调 `sequence`，支持有界进程内重放；
- 会话 HTTP API 使用 Host 自动生成的本机 bearer token；Runtime 固定 `read-only`、`approvalPolicy=never`。
- FEAT-128 提供默认关闭的 v3 Artifact SSE、encrypted staging、content/poster/ACK/range 与 strict-local synthetic producer；另有默认关闭的 MiniMax `image-01` 中国区文生图/单张人物参考生图 provider projection。
- FEAT-129 提供精确 `local + demo_fast` 下的本地 Skill 查询、扫描、原子安装、启停和卸载，继续精确消费 Manifest v2 与 `yijie-skills@0.3.0` 的 38 项目录，并只将当前 catalog 中 receipt 有效的具体 Skill 目录通过固定 Runtime Skills API 重放；HTTP 边界仍校验 owner-only bearer 与 `plugin.read`/`plugin.manage`。
- FEAT-134/136 提供默认关闭、显式协商的 v4/v5 SSE。v5 在 v4 稳定投影之上增加有界 Command 与 Tool Item：Command 只发布分类摘要、结构化工作区 cwd 和脱敏输出；Tool 只做通用稳定投影，当前没有新增 producer。

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
GET  /v4/agent-sessions/{agent_session_id}/events?event_schema_version=4
GET  /v5/agent-sessions/{agent_session_id}/events?event_schema_version=5
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

两个根目录必须同时存在、为规范绝对路径且互不重叠；`YIJIE_AGENT_HOST_HOME` 也必须为规范绝对路径。正常 Desktop 流程透明读取并携带 Host bearer，用户不需要登录或手动授权，但直接调用 Host 不能绕过 bearer/capability 校验。当前锁定的 `yijie-skills@0.3.0` 清单中 38 项均已声明并验证 `desktop-distribution`，正式产品目录不存在 blocked 条目。

Runtime 不会注册整个 App Data 根；清单外目录、损坏安装和包含额外嵌套 `SKILL.md` 的包均 fail closed。operation ID 不做静默过期或裁剪。旧 JSON journal 达到 4096 条时自动完整迁移到本地 bbolt journal，保留原文件的精确备份及所有旧 ID 的重放/冲突语义；4096 只继续限制同时未完成的操作，不再限制历史操作总数。迁移后旧 Host 会拒绝 v2 标记，回滚必须保留 v2 reader，详见 [`docs/skills-journal-v2.md`](docs/skills-journal-v2.md)。

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

v5 Command/Tool 投影只在下列精确、相互依赖的本地配置下注册；缺少任一项时保持关闭：

```bash
export YIJIE_ENV=local
export YIJIE_LOCAL_PROFILE=demo_fast
export YIJIE_FEAT134_STREAMING_ENABLED=true
export YIJIE_FEAT136_COMMAND_TOOL_ITEMS_ENABLED=true
```

该门禁本身不动态改变 Runtime pin；Host 仅接受已审查的 FEAT-126 → FEAT-136 → FEAT-137
四 patch Runtime artifact。FEAT-137 只在 exact local/demo_fast 与 FEAT-134/136/137 gates
全开时使用 `approvalPolicy=on-request`，仍固定 `sandbox=read-only` 且不开启 dynamic tools 或
experimental API；其它配置保持 `approvalPolicy=never`。客户端仍须在 v5 route 上显式协商
`event_schema_version=5`；关闭 FEAT-136 后 v1-v4 行为不变。

第四个 Runtime patch 的确定性 approval producer 默认关闭，普通 stable 入口行为不变。它只允许
Owner-run D4 在上述全部审批门禁已通过后，再显式设置
`YIJIE_FEAT137_D4_DETERMINISTIC_PRODUCER_ENABLED=true`。Host 无条件从父进程环境剥离 Runtime
私有 producer 变量；只有该 D4 gate 为 exact `true` 时，才向受管 Runtime 子进程注入一次 exact
私有值。错误值、gate-off、production、Fake provider 或 dynamic-tools 配置均 fail closed；该开关不
提升权限、不改变审批决定，也不扩张 public v6。

exact FEAT-137 command-approval authority 同时使用独立的受管 MiniMax profile，在
`[features]` 中逐项固定 `hooks=false`、`plugins=false`、`apps=false`、`tool_suggest=false` 和
`shell_snapshot=false`；受管配置不声明 MCP server，也不启用 dynamic tools。普通 gate-off
MiniMax profile 保持既有字节和语义。approval profile 还在受管 MiniMax provider 中固定
`request_max_retries=0` 与 `stream_max_retries=0`，decision POST 仍由 Host 保持不自动重试。D4 producer
在五项关闭或两项 Provider retry authority 缺失、漂移时
必须在 Runtime 进程启动前失败；所有 MiniMax profile 也会在同一边界重验受管 model catalog 的固定
字节，避免 prepare 与 process spawn 之间的目录漂移。

## 验证

```bash
make feat136-contract-check # 校验 v0.7.0 exact pin、普通快照与生成物
go test -race ./internal/session -run '^TestFEAT136' -count=1
go test -race ./internal/app -run '^TestFEAT136' -count=1
make lint                   # gofmt、go vet 和 shell 语法
```

`api/contracts.lock` 固定当前消费的 Contracts `0.7.0` 完整 commit
`aeccf5d561bd4259389cdb325bae84ce3e0dea86`（该 commit 当前没有 v0.7.0 tag，不能描述为已发布）、
`oapi-codegen` identity/version，以及 OpenAPI、Runtime v1/approval v1-v3 兼容清单、Agent session
event v1-v6、ReportDocumentV1 与 Skill Bundle Manifest v1/v2 JSON Schema 的 SHA-256。FEAT-137
的 scoped checker 只读取普通 OpenAPI/schema/v4-v6 JSON fixture；既有 archive、checksum、
Zip Slip 和 archive-error fixture 不进入本次证据，只比较不可变 Git tree object ID 并保留既有
reviewed digest。`api/skills.lock` 另行固定 `yijie-skills@0.3.0` commit、源码树、双渠道 Manifest
与 38 个归档清单摘要。不得手改 `api/` 快照或 `internal/contracts/agenthost.gen.go`。Manifest v1 只保留兼容与既有回归，产品主路径是 v2；
v3 S3 已实现但默认关闭，并由 schema、auth/range/integrity/ACK/TTL/restart/synthetic conformance tests 约束。
这不代表真实 provider 的付费验证或端到端 Desktop UI 已完成。

历史复合门禁 `make sync-contracts`、`make contract-check`、`make test` 与 Runtime 集成脚本会覆盖
归档、权限、进程故障或真实模型路径，不属于 FEAT-136 的安全定向证据。本批不运行这些命令，也不把
未执行项记录为通过。真实只读 Command D4 必须在 Host→Desktop conformance 完成并获得单独付费调用
授权后执行；Tool D4 继续等待真实 producer 与 Owner 决策。
