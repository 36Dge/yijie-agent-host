# Codex App Server Integration

## 固定兼容边界

Agent Host Runtime Baseline 2 继承 Baseline 1 的固定兼容边界，只兼容以下 Runtime：

| 项目 | 固定值 |
| --- | --- |
| 上游 tag | `rust-v0.144.6` |
| 上游 commit | `5d1fbf26c43abc65a203928b2e31561cb039e06d` |
| Runtime 版本 | `codex-cli 0.144.6` |
| 发布目标 | `aarch64-apple-darwin` |
| binary size | `355765224` bytes |
| binary SHA-256 | `98910475280a2a8abc10a1c104d4072358121aee33de5fee3dda75418ccf84c1` |
| Schema tree SHA-256 | `82ee9de771cf1d41bac16d87380f1121e7794107aa3aa526ad702d5d1bf7afe1` |
| transport | JSONL over stdio |
| API surface | stable，`experimentalApi=false` |
| Yijie Runtime patch | `0001-feat-126-filter-persistent-diagnostics.patch` / `6b337a02caf064c6819fab5c7367a485004c85cce0d42acb06fa6d5003e599a0` |

Host 不接受“同版本号但不同哈希”的二进制，也不把随附 manifest 当作可自行声明的新信任根。启动前依次把 manifest 中的 binary、Schema tree 和 build-lock 摘要与编译时固定值比较，再校验实际二进制文件名、大小、SHA-256，以及 `codex --version` 的精确输出。

## 进程与握手

Host 拉起：

```text
codex app-server --listen stdio:// --strict-config
```

`0.144.6` 顶层 `codex app-server` 命令没有公开 `--session-source` 参数，并在内部固定使用 `SessionSource::VSCode`。Host 不伪造参数，也不为此修改 Runtime；升级时必须重新评估该行为。

Runtime 启动环境继承普通进程环境，但强制覆盖 `CODEX_HOME`，移除 OpenAI/Codex 凭据以及父进程中所有 MiniMax/Yijie Key 变量。仅当 Host 显式加载通过校验的 MiniMax Key 时，才以 `MINIMAX_API_KEY` 注入子进程。Key 不写入 `config.toml`、manifest、状态、日志或 bbolt。

连接建立后 Host 发送：

```json
{
  "method": "initialize",
  "id": 0,
  "params": {
    "clientInfo": {
      "name": "yijie_agent_host",
      "title": "Yijie Agent Host",
      "version": "0.1.0"
    },
    "capabilities": {
      "experimentalApi": false
    }
  }
}
```

Host 验证响应包含 `userAgent`、`codexHome`、`platformFamily` 和 `platformOs`，并要求返回的 `codexHome` 与配置目录一致，然后发送：

```json
{"method":"initialized","params":{}}
```

只有以上步骤全部完成，Runtime 状态才进入 `ready`。

## Transport 规则

- stdout 只承载一行一个 JSON message；stderr 不进入协议解析器；
- 单条消息默认上限 16 MiB，Host 写队列默认 64 条；两者均可显式配置；
- Host 请求 ID 使用 int64，并接受 Runtime 返回的 int64/string ID；
- 响应按 ID 路由到等待请求，通知按 method 接收；未知通知不会导致连接退出；
- Runtime 发起的反向请求必须有明确处理器。Baseline 1 尚未接审批、attestation 或动态工具，因此统一返回 JSON-RPC `-32601`，禁止悬挂或默认放行；
- malformed JSON、超大消息、stdout EOF、写失败或子进程异常退出都会使 readiness 立即失效；
- Host 关闭时先停止 readiness，再关闭 Runtime stdin，等待进程退出，超时后终止子进程。

Baseline 2 检测断线但不自动重启。映射可持久化不等于请求可自动重放；自动重启仍需先定义活动 turn、幂等与审批恢复语义。

## MiniMax 与稳定 thread/turn API

Host 管理 Runtime 专用 `CODEX_HOME/config.toml` 和固定模型目录，配置如下语义：

- provider `minimax`；模型 `MiniMax-M3`；
- 中国站 base URL `https://api.minimaxi.com/v1`；
- `wire_api="responses"`，不使用 WebSocket；
- `env_key="MINIMAX_API_KEY"`，不把 Key 内联到配置；
- 模型目录只允许 `none` 和 `high` reasoning effort；
- `thread/start` 固定 `approvalPolicy="never"`、`sandbox="read-only"`、`ephemeral=false`；
- 只调用 stable `thread/start`、`thread/resume`、`turn/start`、`turn/interrupt`，不使用 experimental 字段。

受管配置文件以固定 marker 标识。Host 不覆盖用户创建的 `config.toml` 或模型目录，避免吞掉外部配置；开发期应给 Host 独立空目录。

Runtime 通知只映射 `thread/started`、`turn/started`、`item/started`、`item/agentMessage/delta`、`item/completed`、`turn/completed`、thread 级 `error` 和 thread 级 `warning`。未知通知被忽略；不能关联到 session 的全局 warning 不会广播给任意任务。

## 状态和失败分类

Runtime 状态依次为：

```text
not_configured → verifying → starting → initializing → ready
                                                    ↘ failed
ready → stopping → stopped
```

`/v1/status` 只返回固定版本身份、transport、状态和稳定 failure code，不返回 binary、manifest、`CODEX_HOME` 或原始 stderr。`/healthz` 只表示 Host HTTP 进程存活；`/readyz` 才表示 Runtime 已完成握手。
