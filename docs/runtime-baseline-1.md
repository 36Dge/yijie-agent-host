# Agent Host Runtime Baseline 1

## 目标

Baseline 1 证明 `yijie-agent-host` 能在不使用模型凭据、不修改 Runtime、不执行真实任务的条件下，安全、可重复地管理 Runtime Baseline 0 产物并完成 stable app-server 初始化。

## 完成结果

- 消费 `yijie-codex` 生成的 schemaVersion 1 runtime manifest；
- 固定上游 `rust-v0.144.6` / `5d1fbf26c43abc65a203928b2e31561cb039e06d`；
- 固定 `codex-cli 0.144.6`、`aarch64-apple-darwin`、stdio、`experimentalApi=false` 和零 patch；
- 将 binary SHA-256 `1ef4f1d…df1fe`、大小、Schema tree 和 build-lock 摘要编入兼容策略，再执行文件名、实际哈希和 `--version` 校验；
- 用空、已存在的 `CODEX_HOME` 拉起受管 app-server；
- 完成双向 JSONL transport 和 `initialize → initialized`；
- Runtime ready、failed、stopping、stopped 状态驱动 HTTP readiness；
- 实现有界消息、有界写队列、有界 stderr tail、请求超时、启动取消和关闭超时；
- 对未知反向请求 fail closed；
- 覆盖版本不兼容、产物篡改、超时、取消、畸形/超大消息、目录不一致、断线和异常退出；
- 使用真实 Baseline 0 binary 完成无凭据集成测试。

## 验收命令

```bash
make lint
make test
make runtime-test
```

`make test` 使用受控 fake app-server 测试故障路径；`make runtime-test` 使用相邻 `yijie-codex` 中未提交的固定构建产物测试真实握手。CI 运行前两项；真实产物未进入代码仓库，因此跨仓集成门禁目前在本地或具备受信 Runtime artifact 的发布流水线运行。

## 明确不在 Baseline 1

- API Key、ChatGPT 登录或真实模型 turn；
- task/session/thread 映射与持久化；
- MCP 工具、Skills/Plugins、审批或 attestation；
- 事件重放、自动重连和任务恢复；
- Desktop sidecar 打包、签名、公证、升级和回滚；
- cloud runner。

下一基线开始前，应先固定 Host 与 `yijie-contracts` 的任务/事件最小契约，以及 Desktop 的进程所有权、退出和恢复策略。
