# Event Model

Agent Host 后续负责把 Codex events 转换为易界 agent task events，并携带 `trace_id`、`task_id`、`agent_session_id` 和 `codex_thread_id`。
