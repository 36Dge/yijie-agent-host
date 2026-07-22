# Security Policy

`yijie-agent-host` 不直接持有电商平台 token。所有高风险工具调用必须经过策略检查、用户审批和审计日志。

Runtime Baseline 2 可在内存中持有用户显式配置的 MiniMax 模型 API Key，并只通过 `MINIMAX_API_KEY` 传给受管 Runtime 子进程。推荐来源是 owner-only 原始 Key 文件；Host 拒绝符号链接、组/其他用户可读文件、过大内容和换行控制字符。Key 不写入 Runtime 配置、Host bbolt、事件、状态或日志。

Host 会在独立状态目录生成 256-bit 本机 API token，文件权限为 `0600`。会话和 SSE API 要求 bearer token；health/readiness/status 只返回脱敏诊断信息。Host 只监听 loopback，但 loopback 不是认证替代品。

Baseline 2 固定 Runtime 为 `read-only`、`approvalPolicy=never`，不接 MCP、命令审批或动态工具。未知 Runtime 反向请求默认拒绝。事件仅转发 agent message 文本，不转发命令输出或任意 Runtime JSON；provider 错误会截断并脱敏 bearer 值。
