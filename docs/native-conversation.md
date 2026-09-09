# FEAT-132 原生对话 Host 边界

本地开发候选（2026-09-09，未发布）。公共源为兄弟 Contracts 的 native-conversation v1；本仓 api/native-conversation.lock.json 固定真实 Contracts commit 6f632f155eacdaf93df0e0b00b5dab9e369c5442 及源摘要。

`internal/codex/session_protocol.go::ReadThread` 调用现有 JSON-RPC client 的 thread/read(includeTurns=true)，原始 Runtime 历史在 `internal/session/native_conversation.go` 做安全字段投影。新 history 路由和 v7 SSE 经现有 bearer 校验。普通 Runtime 仍由现有 Start/Resume/StartTurn/Interrupt 入口管理，不新增 Runtime patch、实验 API 或 model/tool 能力。

Host 不保存正文，不按正文重建 Item，不维护 reasoning 累加，不合成 failed 或 recovered lifecycle。v4/v5 只保留单向兼容投影、安全预算及真实 terminal 的 Artifact 交付排序。本次新 EventHub 只做有界传输/replay。

bbolt schema 4 → 5：事务升级既有 schema 元数据，新增原生 revision/terminal 来源字段默认 absent；旧记录不被推定为原生事实。resume 的旧读取不能覆盖较新的 native revision；已观察的同 Turn 原生失败不会被冷历史 completed 改写。schema 5 不允许交给旧 Host 降级打开；回滚必须保持数据兼容，禁止删字段/重写终态。

安全验证：`go test -race ./internal/session ./internal/app ./internal/codex -run '^TestFEAT132Native'`。包括原生实际 RPC + 内存 JSONL 管道、producer OpenAPI conformance、鉴权/no-store、最终值、原生 error、summary 索引和 resume。测试不替换 executable，不启动模型，不强杀进程。

完整make test的旧binary/fault/attack fixture不能在本次运行，既有定向验证不等于完整Go测试集通过。2026-09-09真实local/demo_fast D4及后续日常入口验证已通过，实际累计19/25次文本、1/3次图片，日常复验未新增调用。Contracts来源已固定；Desktop的canonical FEAT-152整体Host pin已更新为`9e9d317f7e4ecff5f8aeec94fa467f9bede32139`及真实源摘要，并通过正常入口核验。完整证据与能力限制见元仓FEAT-132的02-verification.md和05-daily-entry-verification-2026-09-09.md；未发布或部署，不放宽来源和权限门禁。
