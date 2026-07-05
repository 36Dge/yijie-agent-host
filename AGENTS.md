# AGENTS.md

## 仓库职责

`yijie-agent-host` 是业务系统与 Codex Runtime 之间的薄适配层。

## 禁止事项

- 禁止自研 Agent planner；
- 禁止重建完整任务编排平台；
- 禁止直接处理平台 token；
- 禁止直接调用 Amazon、Temu、Shopee、TikTok Shop API；
- 禁止承载业务主状态。

## 技术栈

Go。当前骨架只提供最小 HTTP 服务，Codex app-server 连接为占位。

## 开发命令

```bash
make dev
make test
```
