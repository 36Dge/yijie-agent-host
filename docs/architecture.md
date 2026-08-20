# Architecture

`yijie-agent-host` 是易界业务控制面和 Codex Runtime 之间的薄宿主。Runtime Baseline 2 落地本地只读 thread/turn 垂直切片，不增加第二套 Agent 平台。

```text
Yijie Desktop / yijie-api
            |
            | task/session HTTP + SSE
            v
    yijie-agent-host
      ├─ internal/app       本机 HTTP、认证、SSE、health/readiness
      ├─ internal/session   ID 映射、bbolt、事件转换与有界重放
      ├─ internal/artifact  每进程密钥 encrypted spool、limits、TTL 与 ACK tombstone
      ├─ internal/security  Host API token
      └─ internal/codex     产物校验、provider、stdio 与稳定 API
            |
            | managed child process
            v
 codex app-server 0.144.6
```

## 已实现

- `internal/codex` 独占易变的 app-server wire protocol；上层只观察脱敏状态；
- Runtime 由 Host 拉起和关闭，stdio 不暴露为网络端点；
- 固定产物验证先于进程创建；
- health 与 readiness 分离；
- 所有未实现的反向请求默认拒绝；
- Host bbolt 只保存恢复所需映射和状态，不保存 prompt、delta 或完成消息；
- Runtime 专用目录和 Host 状态目录彼此独立；
- EventHub 只提供当前 Host 进程的有界重放，重启后通过新 `stream_id` 明确切断旧游标。
- FEAT-128 v3 Artifact surface 使用独立 EventHub；bytes 不进入 event/bbolt/log，由 owner-only
  encrypted spool 暂存，并在 ACK、24h TTL 或 Host restart 时清理。

## 仍未实现

- MCP、Skills/Plugins 装载；
- 审批、attestation、平台身份与多租户服务认证；
- Desktop 打包、签名、升级和故障恢复；
- cloud runner。

这些能力必须继续遵循 `yijie-contracts`、`yijie-api`、`yijie-connectors` 和 `yijie-codex` 的仓库边界。
