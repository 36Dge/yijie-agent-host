# yijie-agent-host

易界 AI 对 Codex Runtime 的薄宿主适配层。它不是自研 Agent 平台。

## 仓库职责

- 启动或连接 Codex app-server；
- 维护 `yijie_task_id` 与 `codex_thread_id` 映射；
- 加载易界 skills / plugins；
- 生成 MCP 工具配置；
- 把 Codex 事件转成易界任务事件；
- 执行租户权限校验、工具调用审批策略和日志脱敏。

## 不负责什么

- 不自研 planner；
- 不自研任务编排系统；
- 不直接实现平台 API；
- 不直接持有平台 token；
- 不承载业务数据库主状态。

## 本地开发

```bash
make dev
curl http://localhost:18080/healthz
```
