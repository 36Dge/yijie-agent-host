# Sidecar Mode

Desktop sidecar 面向 macOS Apple Silicon 本地运行。进程所有权固定为：

```text
Yijie Desktop → yijie-agent-host → codex app-server
```

Desktop 不直接访问 Runtime。Agent Host 持有 Runtime stdin/stdout，负责产物校验、初始化、状态转换、超时和关闭。

Runtime Baseline 2 已完成 Host → Runtime 的受管子进程链路、本机认证、ID 映射和 SSE，但 Desktop 还没有完成以下接入：

- 打包并向 Host 传入绝对 binary/manifest/Runtime Home/Host Home 路径；
- 安全创建或导入 MiniMax Key，并把 Key 文件路径交给 Host；
- 以当前用户权限读取 Host Home 中的 API token，不把它放入 URL、日志或前端持久存储；
- 观察 `/readyz` 后再开放任务入口；
- 消费 SSE 的 `stream_id:sequence` 游标，并处理 Host 重启后的 `409 event_stream_changed`；
- Desktop 退出时先向 Host 发送正常终止信号，并设置强制退出上限；
- 定义 Host 或 Runtime 崩溃后的 UI 状态和用户操作；
- 签名、公证、产物升级与回滚。

FEAT-128 Host S3 已提供默认关闭的 v3 Artifact route、encrypted staging、content/poster/ACK/range
和 strict-local synthetic producer。Desktop 仍需完成 SQLCipher authority、native transfer/ACK、private
IPC/history 与 renderer；因此 Host 能力不能被描述为端到端 Desktop Artifact 已完成。真实
MiniMax/video/file/report producer 也仍未启用。

在这些策略确认前，Host 不自动重启 Runtime，也不声明 Desktop sidecar 已完成。
