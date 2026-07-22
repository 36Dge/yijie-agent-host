# Contributing

涉及 Codex app-server 协议、事件模型或工具审批策略时，必须同步更新文档和 `yijie-contracts`。

Runtime transport、provider 或 thread/turn 变更至少执行 `make lint && make test && make runtime-test`。不得用浮动 Codex binary、未校验 manifest、experimental API 或真实生产凭据替代固定 Baseline 测试。

`make runtime-turn-test` 是人工显式门禁，最多发起 2 次短 MiniMax 请求，不进入 CI。Key 只能通过环境或被 `.gitignore` 覆盖的 owner-only 文件提供；测试、日志、fixture 和失败快照不得包含 Key。
