# FEAT-132 原生对话 Host 边界

## FEAT-144 独立原生线程状态读取（2026-09-10）

`GET /v2/agent-sessions/{agent_session_id}/native-thread-status` 使用现有 owner bearer、
`no-store` 和标准安全错误响应。它从 Host 已保存的精确 native thread 绑定调用
`thread/read(includeTurns=false)`，只返回该 thread 的原生 `status.type`：
`notLoaded`、`idle`、`systemError` 或 `active`，以及原生 ID 和 `runtime_read` 来源。
缺少绑定、身份不符、未知/缺失状态或读取失败均不提供可用状态。

读取不调用 resume/start，不初始化 MCP，不读取 Turn 正文，不写 Host 映射或任何执行记录。
`notLoaded` 只说明当前 Runtime 没有加载该线程，不能解释为历史 Turn 已完成或未执行。
新状态 DTO 和路径独立于已有 v1/v2 history DTO；不新增历史字段，不改变权限语义，
不把当前观察持久化为新的生命周期真相。旧 history/SSE 和 native idle permission gate 保留原行为。
安全定向测试为三个层级的 `TestFEAT144NativeThreadStatus*`，不启动进程或真实服务。

## FEAT-144 原生配置与项目 trust（2026-09-10）

现行 MiniMax + RuntimePermissions 路径将安全/Provider/MCP 模板精确保存为
`CODEX_HOME/yijie-runtime-v1.toml`，通过固定 Runtime 原生 `-c` 层读取。
`CODEX_HOME/config.toml` 仅保留 Codex 原生 projects/trust；Host 不决定项目根或信任等级。
旧布局仅在完整模板前缀逐字节匹配、尾部通过固定 ProjectConfig 闭合结构检查时迁移；尾部原字节保留。
先准备新受管文件，再由既有 authority/身份复核/原子替换设施迁移根配置。
未知字段或漂移继续拒绝，两个文件均在 Runtime spawn 前检查，模板不改为宽松语义比较。

该私有布局只能交给支持它的 Host；回滚不得使用此前只认识整个 config.toml 模板的旧 Host。
必须保留包含此兼容 reader 的实际提交。Desktop SQLCipher schema15 最低 reader 不变。
原生 MCP、Prompt、FEAT-152 三模式和 FEAT-137 退役门禁保持原语义。
`go-toml/v2@v2.4.3` 只解析/编码配置；错误内容不外传，不包含密钥值。
原生停用仍先证明线程 idle、正常 EOF/cleanup、以禁用 MCP 的配置重启并验证后才返回。
现行配置层的 Runtime 初始化使用中性根目录，避免把启动器仓库当作任务工作区；
CODEX_HOME 不变，实际 thread/start/resume 仍使用真实任务 cwd、读取原项目配置并执行原门禁。
定向测试见 `native_config_layers_test.go`，无调用的真实验证为
`TestPinnedRuntimeFEAT144NativeTrustSurvivesRestart`；不得用该测试冒充产品真实 D4。

## FEAT-132 历史边界

本地开发候选（2026-09-09，未发布）。公共源为兄弟 Contracts 的 native-conversation v1；本仓 api/native-conversation.lock.json 固定真实 Contracts commit 6f632f155eacdaf93df0e0b00b5dab9e369c5442 及源摘要。

`internal/codex/session_protocol.go::ReadThread` 调用现有 JSON-RPC client 的 thread/read(includeTurns=true)，原始 Runtime 历史在 `internal/session/native_conversation.go` 做安全字段投影。新 history 路由和 v7 SSE 经现有 bearer 校验。普通 Runtime 仍由现有 Start/Resume/StartTurn/Interrupt 入口管理，不新增 Runtime patch、实验 API 或 model/tool 能力。

Host 不保存正文，不按正文重建 Item，不维护 reasoning 累加，不合成 failed 或 recovered lifecycle。v4/v5 只保留单向兼容投影、安全预算及真实 terminal 的 Artifact 交付排序。本次新 EventHub 只做有界传输/replay。

bbolt schema 4 → 5：事务升级既有 schema 元数据，新增原生 revision/terminal 来源字段默认 absent；旧记录不被推定为原生事实。resume 的旧读取不能覆盖较新的 native revision；已观察的同 Turn 原生失败不会被冷历史 completed 改写。schema 5 不允许交给旧 Host 降级打开；回滚必须保持数据兼容，禁止删字段/重写终态。

安全验证：`go test -race ./internal/session ./internal/app ./internal/codex -run '^TestFEAT132Native'`。包括原生实际 RPC + 内存 JSONL 管道、producer OpenAPI conformance、鉴权/no-store、最终值、原生 error、summary 索引和 resume。测试不替换 executable，不启动模型，不强杀进程。

完整make test的旧binary/fault/attack fixture不能在本次运行，既有定向验证不等于完整Go测试集通过。2026-09-09真实local/demo_fast D4及后续日常入口验证已通过，实际累计19/25次文本、1/3次图片，日常复验未新增调用。Contracts来源已固定；Desktop的canonical FEAT-152整体Host pin已更新为`9e9d317f7e4ecff5f8aeec94fa467f9bede32139`及真实源摘要，并通过正常入口核验。完整证据与能力限制见元仓FEAT-132的02-verification.md和05-daily-entry-verification-2026-09-09.md；未发布或部署，不放宽来源和权限门禁。
