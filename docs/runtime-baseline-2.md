# Agent Host Runtime Baseline 2

## 结果

Baseline 2 在 Runtime Baseline 1 上完成 MiniMax 中国站的真实、只读 thread/turn 最小垂直切片。边界仍是本地 Desktop sidecar；没有引入工具执行、审批、MCP、Skills/Plugins 或 cloud runner。

固定决策：

| 项目 | 结果 |
| --- | --- |
| Runtime | `codex-cli 0.144.6`，`rust-v0.144.6` / `5d1fbf26c43abc65a203928b2e31561cb039e06d` |
| transport/API | JSONL stdio、stable、`experimentalApi=false` |
| 模型 provider | MiniMax 中国站，按量付费 API Key |
| endpoint | `https://api.minimaxi.com/v1`，Responses API |
| model | `MiniMax-M3` |
| Runtime 权限 | `read-only`、`approvalPolicy=never` |
| Host 状态 | 独立目录中的 bbolt，只保存映射/状态 |
| 内容事件 | 当前 Host 进程内有界缓存，不持久化 |
| HTTP 认证 | Host Home 中自动生成的 owner-only bearer token |

## 垂直切片

Host 实现稳定 Runtime 方法：

- `thread/start`：新建持久 thread，返回并保存 Runtime session/thread 身份；
- `thread/resume`：按已保存 `codex_thread_id` 恢复历史；
- `turn/start`：文本输入，reasoning effort 仅 `none`/`high`；
- `turn/interrupt`：只允许中止该 session 当前活动 turn。

Host 映射事件：

- `thread/started` → `thread.started`；
- `turn/started` → `turn.started`；
- `item/started` → `item.started`；
- `item/agentMessage/delta` → `item.agent_message.delta`；
- `item/completed` → `item.completed`；
- `turn/completed` → `turn.completed`；
- `error` 和 `warning` → 同名稳定事件。

公共语义由相邻 `yijie-contracts` 的 Agent Host HTTP/SSE OpenAPI、Agent Host Protobuf、`AgentSessionEvent` Protobuf、AsyncAPI 和 JSON Schema 定义。Host 用版本和 SHA-256 锁定精确快照，HTTP handler 直接消费生成 DTO；不再维护平行的手写请求/响应结构。

## ID 与持久化

```text
task_id
  └─ agent_session_id
       └─ codex_thread_id
            └─ active/last turn_id
```

`task_id` 来自控制面；`agent_session_id` 由 Host 生成；thread/turn ID 由 Runtime 生成。bbolt 对 task 和 thread 建唯一索引，并保存 Runtime session ID、cwd、provider/model、trace、活动/最近 turn 和稳定失败码。

Host 不持久化输入、delta、完成消息、provider 原始响应、API Key 或 Host API token 内容。Runtime 自己在专用 `CODEX_HOME` 保存 thread rollout，Host bbolt 只保存恢复索引。

## 事件与恢复

每个 `agent_session_id` 在一个 Host 进程内对应一个随机 `stream_id`，`sequence` 从 1 严格递增，`event_id` 用于至少一次交付去重。SSE 的 `id` 为 `stream_id:sequence`，客户端可用 `Last-Event-ID` 或 `stream_id`/`after` 查询参数重连；`after>0` 时必须同时提供对应 `stream_id`。

- 缓存最多 512 个事件；游标早于缓存返回 `409 event_replay_unavailable`；
- Host 重启后映射仍在，但新进程使用新 stream；旧游标返回 `409 event_stream_changed`；
- 不跨进程重放文本 delta；客户端应通过 session 状态和 `thread/resume` 恢复；
- `turn.completed` 是唯一 turn 终态，`error`/`warning` 本身不是终态；
- 慢订阅者被断开，防止拖垮 Runtime 读取链路。

## 安全边界

- Host 清理父进程中的 OpenAI/Codex/MiniMax Key 变量，只注入本次显式加载的 `MINIMAX_API_KEY`；
- managed `config.toml` 只保存 `env_key`，不保存 Key；
- Host 拒绝覆盖非受管 Runtime 配置；
- 会话 API 必须携带 Host bearer token；服务只监听 `127.0.0.1`；
- 输入最大 1 MiB；JSON 拒绝未知字段和多余值；
- 原始 Runtime 错误不返回 HTTP 客户端，事件错误做长度限制和 bearer 脱敏；
- 没有实现的 Runtime 反向请求返回 JSON-RPC method-not-found。

## 验证门禁

```bash
make contract-check
make lint
make test
make runtime-test
make runtime-turn-test
```

前三项是无计费常规门禁。`runtime-test` 使用固定真实二进制验证产物、握手、受管 provider 配置和真实 `thread/start`，但不调用模型。

`runtime-turn-test` 必须人工显式执行，不进入 CI，最多发起 2 次短请求：第一轮验证正常 turn 和 agent message 事件；重启 Runtime 后验证 `thread/resume`；第二轮立即验证 `turn/interrupt` 和最终 `interrupted`。测试不会打印 Key。

## Baseline 2 之后

尚未完成且不属于本基线：Desktop 正式打包/Key 导入 UX、Host 自动重启与活动 turn 恢复、跨进程事件持久化、平台身份与多租户认证、MCP/工具策略/审批、Skills/Plugins、cloud runner。进入 Baseline 3 前应先确定工具和审批信任边界。
