# AGENTS.md

## 适用范围

本文件适用于 `yijie-agent-host` 整个仓库。目录中若出现更具体的 `AGENTS.md`，修改对应目录时以更具体的规则为准。

## 仓库职责与当前状态

`yijie-agent-host` 是易界业务系统与 Codex Runtime 之间的薄宿主和安全适配层。它负责 Runtime 进程或连接管理、任务与 thread 映射、事件转换、Skills/Plugins 装载、MCP 工具配置、策略检查、审批承接和日志脱敏，但不是自研 Agent 平台。

当前已完成 Agent Host Runtime Baseline 2：

- `internal/codex` 校验 Runtime Baseline 0 manifest、binary SHA-256、大小和精确版本；
- desktop-host 以受管子进程启动固定 `codex app-server`，使用 JSONL over stdio；
- stable API、`experimentalApi=false` 和 `initialize → initialized` 已通过真实 Runtime 集成测试；
- transport 支持双向 request/response/notification 分类、有界消息与写队列、超时、取消、半关闭和异常退出检测；
- 未实现的 Runtime 反向请求统一 fail closed；
- `/healthz` 只表示 Host 存活，`/readyz` 只有 Runtime 完成握手时才成功，`/v1/status` 不暴露本地路径；
- MiniMax 中国站 Responses API 通过子进程专用 `MINIMAX_API_KEY` 接入，Key 不进入 Runtime 配置、状态、日志或 bbolt；
- stable `thread/start`、`thread/resume`、`turn/start`、`turn/interrupt` 和要求的 thread/turn/item/error/warning 事件已经适配；
- `task_id → agent_session_id → codex_thread_id → turn_id` 映射使用独立 Host Home 中的 bbolt 持久化；
- 会话内容只做当前进程内有界事件重放，不持久化，Host 重启产生新 `stream_id`；
- 本机会话 HTTP/SSE 使用 Host 自动生成的 bearer token，Runtime 权限固定为 read-only/never。

审批、MCP、Skills/Plugins、Desktop 打包、平台身份、多租户服务认证、自动故障恢复和 cloud runner 尚未实现，不得把 Runtime Baseline 2 描述为完整 Agent 链路。

## 仓库边界

- 不自研 planner、模型推理框架或完整任务编排平台；
- 不承载租户、任务、审批或审计等业务主状态，主状态属于 `yijie-api`；
- 不实现 Amazon、Temu、Shopee、TikTok Shop 等平台 API，工具执行属于 `yijie-connectors`；
- 不直接获取、刷新、记录或转发原始平台 token；
- 不维护 Codex Runtime 源码，Runtime 修改属于 `yijie-codex`；
- 不编写跨境电商业务 prompt，业务内容属于 `yijie-skills`；
- 不让 Desktop 直接绕过 Agent Host 访问 Runtime 或高风险工具。

宿主允许保存完成运行适配所必需的短期运行状态和 ID 映射，但不得让它演变为第二套业务数据库。Baseline 2 已确认使用 bbolt 仅持久化恢复索引和状态，禁止扩展为业务主状态。

## 代码组织

- `cmd/desktop-host/`：本地 sidecar 模式入口，保持轻量；
- `cmd/cloud-runner/`：云端 runner 占位，未确认运行模型前不实现；
- `cmd/runtime-healthcheck/`：独立 Runtime 探测占位；
- `internal/app/`：配置、依赖组装和 HTTP 生命周期；
- `internal/codex/`：Codex app-server transport、版本协商和客户端适配；
- `internal/events/`：Codex event 到易界事件的纯映射；
- `internal/policy/`：工具风险和审批策略适配，不成为业务权限真相源；
- `internal/session/`：task、session、thread 映射及生命周期；
- `internal/skills/`、`internal/plugins/`、`internal/tools/`：装载和配置适配，不复制资产或连接器实现；
- `internal/security/`：输入校验、脱敏和最小权限边界；
- `api/`：本仓库实现所需的局部接口材料，稳定跨仓库契约以 `yijie-contracts` 为源。

## Contract First 与 Runtime 兼容

- 每个任务先标记 `contract-impact = none | additive | semantic | breaking`；分类覆盖跨进程、跨仓、跨版本及持久化/重放边界，形状不变但 Runtime 映射、错误、顺序、重放或终态变化仍属于契约影响，`none` 必须说明理由；
- 按 `breaking > semantic > additive > none` 的最高风险唯一选择；任一受支持交互可能失效即 breaking，不确定时不能假定 additive/none；
- Agent Host 服务、任务事件和公共 schema 以相邻 `yijie-contracts` 为源；
- HTTP/SSE 权威契约位于 `yijie-contracts/openapi/agent-host/agent-host.yaml` 和 `jsonschema/agent/session-event.schema.json`；本仓只保存版本/哈希锁定的精确快照、生成 DTO及 producer 一致性测试；
- 修改事件或服务协议时，先更新并检查 `openapi/agent-host/`、`protobuf/yijie/services/agent_host/v1/`、`protobuf/yijie/events/v1/` 或对应 JSON Schema；契约形成不可变 tag/完整 commit 后，本仓固定 version、完整 commit、digest 和 generator 版本，再同步生成；
- 不手写与生成契约重复的 DTO，也不直接编辑生成文件；
- Codex app-server transport 以固定 `yijie-codex` Runtime canonical schema 为上游权威：先形成 Runtime 候选，再更新 contracts 兼容投影，最后同步 Host；不得机械倒置为先发明 Host schema；
- 未知事件和新增字段应按兼容策略处理，不因单个未知事件终止整个 session；
- 事件必须保留 `trace_id`、`task_id`、`agent_session_id` 和 `codex_thread_id` 的关联，并定义顺序、重复、断线重放和终态语义；
- 超时、取消和用户中止必须沿 Desktop、Agent Host、Runtime 和工具调用链传播。

当前 `api/contracts.lock` 已固定 FEAT-128 `0.4.0` local candidate 的完整 commit、OpenAPI、
Runtime compatibility、Agent session event v1/v2/v3 与 ReportDocumentV1 源 digest，以及
`oapi-codegen` identity/version；该 candidate 尚无 tag、未发布。`make sync-contracts` 只接受干净 Git
仓库中可解析的完整 candidate commit 或匹配版本的不可移动 tag，并从该 commit 的 Git
对象同步；`contract-check` 会验证 ref→commit、generator、digest、snapshot 与生成类型。
因此当前契约来源锁阻塞项已经关闭，后续版本不得退回 dirty/floating sibling
或只记录计划 tag 的做法。

`internal/session.Event/EventPayload` 当前是 JSON Schema 尚无 Go generator 时的显式
adapter 例外，由 Agent Runtime Team 负责，并由 schema conformance test 约束。移除
条件是在首个生产 Agent Host 发布前评估并接入生成类型；若仍保留，必须在发布记录中
重新批准例外、注明期限，不能无期限延续。

dirty/floating sibling 只能用于本地候选验证，不能作为发布来源；下游实现不得在契约
可消费和精确 pin 前合并或启用。兄弟元仓存在时同时遵循
`../yijie/docs/dev/contract-first.md`。

## 策略、审批与工具边界

高风险调用遵循以下流程：

```text
proposal -> policy check -> approval task -> user decision -> connector execution -> audit event
```

- Agent Host 负责拦截和编排，不代替 `yijie-api` 的身份、权限和审批主状态；
- 工具缺少风险等级、权限 scope、租户上下文或有效审批时默认拒绝；
- 审批必须绑定具体 task、tool、参数摘要、影响范围和有效期，禁止复用模糊或过期批准；
- 发送给 Runtime 的 MCP 配置只包含调用能力，不包含原始平台凭据；
- 日志和事件需脱敏，禁止输出 prompt 中的秘密、token、cookie、DSN、PII 或完整商家数据。

## 运行模式与可靠性

- desktop sidecar 和 cloud runner 共用协议及核心适配逻辑，但进程、认证、存储和网络边界必须分别设计；
- 当前只存在 desktop-host 骨架，不得提前声称 cloud runner 已实现；
- Runtime 客户端必须设置连接、请求、空闲和关闭超时，并处理重连、背压、半关闭和子进程异常退出；
- `/readyz` 在真实集成后必须反映关键依赖是否可服务，不能继续无条件返回 ready；
- 状态接口不得泄露内部 URL 中的凭据或其他敏感配置。

Baseline 2 对断线和退出的处理是撤销 readiness、失败所有等待请求并终止损坏连接，不自动重启。bbolt 映射允许显式 `thread/resume`，但自动重连/重启仍必须先定义活动 turn 幂等恢复和审批状态语义。

## 已固定的 Runtime Baseline 1/2 决策

- Runtime 来自相邻 `yijie-codex` Runtime Baseline 0：`rust-v0.144.6` / `5d1fbf26c43abc65a203928b2e31561cb039e06d`；
- 只支持 `codex-cli 0.144.6`、`aarch64-apple-darwin`、stdio、stable API 和 `experimentalApi=false`；
- Host 通过绝对 binary/manifest/`CODEX_HOME` 路径消费产物，不使用 URL 连接本地 Runtime；
- `codex app-server` 参数固定为 `--listen stdio:// --strict-config`；该版本顶层 CLI 不公开 `--session-source`；
- Runtime 只接受 manifest 固定的单一 FEAT-126 日志安全 patch；canonical `yijie-codex/codex-rs` 仍不得直接修改；
- 模型认证固定为 MiniMax 中国站按量付费 API Key，endpoint 为 `https://api.minimaxi.com/v1`，模型为 `MiniMax-M3`，wire API 为 Responses；
- Key 由 Host 显式从环境或 owner-only 文件读取，只以 `MINIMAX_API_KEY` 注入 Runtime；
- Runtime Home 与 Host Home 独立；Host Home 使用 bbolt 持久化映射，并保存本机 HTTP bearer token；
- Baseline 2 只支持 read-only/never，不接工具、审批、MCP 或 experimental API；
- 真实模型门禁最多 2 次短请求，人工显式执行，不进入 CI。

## 必须先确认的决策

以下事项不得猜测，信息不足时停止并询问用户：

- Desktop sidecar 的打包、拉起、升级、退出和故障恢复模型；
- cloud runner 的部署、隔离、认证、并发和任务恢复模型；
- 审批来源、审批凭证、风险等级、有效期和工具参数绑定规则；
- 新依赖、协议变化、跨仓库发布顺序和任何 Runtime 核心修改。

## 开发与验证

```bash
make lint          # gofmt 检查、go vet 和 shell 语法
make test          # race 单元测试、transport 和故障覆盖
make runtime-test  # 固定产物握手和 provider/thread 配置，不调用模型
make runtime-turn-test # 人工显式 MiniMax 垂直切片，最多 2 次短请求
make sync-contracts # 从相邻 yijie-contracts 同步固定快照并生成 DTO
make contract-check # 校验快照、版本、哈希及生成物；make test 会先执行
make generate      # 仅从已锁定的本地 OpenAPI 快照生成 Go DTO
make dev           # 启动 desktop-host；未配置 Runtime 时 readiness 为 false
```

- 纯映射和策略逻辑需要表驱动单元测试，覆盖未知事件、重复事件、拒绝和终态；
- transport 改动需要覆盖超时、取消、断线重连、版本不兼容和 Runtime 异常退出；
- 审批改动需要覆盖缺失、过期、参数不匹配、跨租户和重复执行；
- 真实 Runtime 集成必须增加兼容性或集成测试，不能只依赖静态 `/healthz`；
- 测试不得连接生产 Runtime、真实平台或未知外部端点。

## 完成标准

- 实现保持薄宿主定位，没有复制 planner、连接器或业务主状态；
- 公共 Host 协议先在 `yijie-contracts` 更新并完成兼容性检查；Runtime 与私有 bbolt 状态分别走上游投影和本仓 migration 权威源；
- 当 `contract-impact != none` 时按权威源路由：公共 Host wire 提供契约 tag/完整 commit、digest/generator pin 和 producer conformance；Runtime 提供上游/投影引用与双向兼容；bbolt 私有状态提供 migration/version、恢复和回滚验证；不适用的 contracts 字段写 `N/A + 理由`；所有路径都记录部署/回滚顺序；`none` 只需分类理由；
- Runtime 版本、事件语义、取消和失败恢复均有明确测试；
- 高风险工具在缺少有效授权时默认拒绝，且审计链可追踪；
- `make lint` 和 `make test` 通过，相关集成测试也已执行；
- 尚未完成的 Runtime、cloud runner、持久化或真实审批验证被如实说明。
