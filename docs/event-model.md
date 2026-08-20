# Event Model

Agent Host 把 Codex events 转换为易界 agent task events，并携带 `trace_id`、`task_id`、
`agent_session_id` 和 `codex_thread_id`。

FEAT-128 新增显式协商的 v3 stream。v1/v2 事件集合与 wire 不变；只有
`GET /v3/agent-sessions/{id}/events?event_schema_version=3` 会接收
`item.artifact.started|progress|completed|failed`。游标继续使用 `after`、`stream_id` 与
`Last-Event-ID`，`after_sequence` 被拒绝。

Artifact bytes 不进入 SSE、bbolt 或日志。Host 将 verified bytes 放入 owner-only、每进程密钥加密的
短期 spool；Desktop native 通过同 session 的 content/poster route 读取并校验，完成本地事务后发送
幂等 ACK。ACK、`staged_at + 24h` 或 Host restart 会清理 staging。当前真实 provider projection
保持关闭；严格 local profile `feat128-artifact-v1` 可生成四类 deterministic synthetic lifecycle。
