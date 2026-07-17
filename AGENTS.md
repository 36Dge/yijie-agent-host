# AGENTS.md

## 适用范围

本文件适用于 `yijie-agent-host` 整个仓库。目录中若出现更具体的 `AGENTS.md`，修改对应目录时以更具体的规则为准。

## 仓库职责与当前状态

`yijie-agent-host` 是易界业务系统与 Codex Runtime 之间的薄宿主和安全适配层。它负责 Runtime 进程或连接管理、任务与 thread 映射、事件转换、Skills/Plugins 装载、MCP 工具配置、策略检查、审批承接和日志脱敏，但不是自研 Agent 平台。

当前仓库仍是最小 HTTP 骨架：

- 只有 `cmd/desktop-host` 和 `internal/app` 有实际实现；
- `/healthz`、`/readyz` 和 `/v1/status` 返回静态骨架状态；
- `YIJIE_CODEX_APP_SERVER_URL` 只被读取和展示，尚未建立 Codex app-server 连接；
- `api/`、`internal/codex`、`internal/events`、`internal/policy`、`internal/session` 等目录仍是占位；
- cloud runner、Runtime healthcheck、真实事件映射、审批和工具执行链尚未实现。

当前 `/readyz` 成功只说明 HTTP 进程可响应，不代表 Runtime 已连接或任务链路可用。Codex 不得把占位状态描述为已完成集成。

## 仓库边界

- 不自研 planner、模型推理框架或完整任务编排平台；
- 不承载租户、任务、审批或审计等业务主状态，主状态属于 `yijie-api`；
- 不实现 Amazon、Temu、Shopee、TikTok Shop 等平台 API，工具执行属于 `yijie-connectors`；
- 不直接获取、刷新、记录或转发原始平台 token；
- 不维护 Codex Runtime 源码，Runtime 修改属于 `yijie-codex`；
- 不编写跨境电商业务 prompt，业务内容属于 `yijie-skills`；
- 不让 Desktop 直接绕过 Agent Host 访问 Runtime 或高风险工具。

宿主允许保存完成运行适配所必需的短期运行状态和 ID 映射，但不得让它演变为第二套业务数据库。是否持久化 session/thread 映射及其存储方案必须先确认。

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

- Agent Host 服务、任务事件和公共 schema 以相邻 `yijie-contracts` 为源；
- 修改事件或服务协议时，先更新并检查 `protobuf/yijie/services/agent_host/v1/`、`protobuf/yijie/events/v1/` 或对应 JSON Schema，再生成代码；
- 不手写与生成契约重复的 DTO，也不直接编辑生成文件；
- Codex app-server transport、端点、协议版本、能力探测和兼容范围必须结合 `yijie-codex` 的固定版本确认；
- 未知事件和新增字段应按兼容策略处理，不因单个未知事件终止整个 session；
- 事件必须保留 `trace_id`、`task_id`、`agent_session_id` 和 `codex_thread_id` 的关联，并定义顺序、重复、断线重放和终态语义；
- 超时、取消和用户中止必须沿 Desktop、Agent Host、Runtime 和工具调用链传播。

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

## 必须先确认的决策

以下事项不得猜测，信息不足时停止并询问用户：

- Codex app-server 的 transport、地址、固定版本、认证方式和能力协商；
- Desktop sidecar 的打包、拉起、升级、退出和故障恢复模型；
- cloud runner 的部署、隔离、认证、并发和任务恢复模型；
- task/session/thread 映射是否持久化以及数据库或本地存储方案；
- 审批来源、审批凭证、风险等级、有效期和工具参数绑定规则；
- 新依赖、协议变化、跨仓库发布顺序和任何 Runtime 核心修改。

## 开发与验证

```bash
make lint     # gofmt 检查和 go vet
make test     # race 单元测试和覆盖率
make generate # 当前为占位，不能视为契约生成完成
make dev      # 启动 desktop-host 骨架
```

- 纯映射和策略逻辑需要表驱动单元测试，覆盖未知事件、重复事件、拒绝和终态；
- transport 改动需要覆盖超时、取消、断线重连、版本不兼容和 Runtime 异常退出；
- 审批改动需要覆盖缺失、过期、参数不匹配、跨租户和重复执行；
- 真实 Runtime 集成必须增加兼容性或集成测试，不能只依赖静态 `/healthz`；
- 测试不得连接生产 Runtime、真实平台或未知外部端点。

## 完成标准

- 实现保持薄宿主定位，没有复制 planner、连接器或业务主状态；
- 协议先在 `yijie-contracts` 更新并完成兼容性检查；
- Runtime 版本、事件语义、取消和失败恢复均有明确测试；
- 高风险工具在缺少有效授权时默认拒绝，且审计链可追踪；
- `make lint` 和 `make test` 通过，相关集成测试也已执行；
- 尚未完成的 Runtime、cloud runner、持久化或真实审批验证被如实说明。
