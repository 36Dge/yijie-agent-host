# FEAT-157 Host 产品接线本地候选

2026-10-07，local_worktree_candidate，release=false。本次接线使用 Contracts 的 `market-host/1`、`market-provider/1` 及未发布 Broker 0.2.0 候选源；既有 51 项管理、selection、Sorftime、模型 profile、已固定 Runtime 制品与发布 pin 没有重定义。

## 实现

Native 独占 Host 原始 stdin/stdout。首个 hello 固定 Host instance、Native epoch、owner、tenant；只表示私管道可用，不表示模型或连接器可执行。日志改走 stderr。Native 在自身权限/SQL31 冻结流程后通过管道提交完整不可变内容；Host HTTP 只接受 grantRef 与原 turnOperationId。所有新 GET 要求 canonical request ID header 并回显；响应 application/json、no-store。HTTP bearer/Trace 不生成 grant、不决定 market 审批。

同一个 Host-owned Rust worker 负责 OAuth/Keyring 与 Gateway。provider 操作在没有 Runtime 的阶段可用；真实 Runtime 初始化成功后才绑定其 generation，复用同一子进程。v2 canonical manifest 明确 `externalCallsEnabled:true`、`providerProfile:tushare-oauth-v1`、`qualificationOnly:false`；外部网络装配不等于已授权或合格。固定私有 CODEX_HOME 为 `<HostHome>/market-worker/library-home`，只用 Keyring，平台 token 不进入 Host DTO、日志、bbolt。产品 v2 owner 缺少此私有 home 时拒绝启动。

完整模式要求现有 local/demo_fast、真实 PPID、instance nonce、模型与权限门禁，以及 `YIJIE_MARKET_CONNECTORS_ENABLED=true`、assembly-owned `YIJIE_MARKET_WORKER_MANIFEST`。缺模型时 Native 可显式传 `YIJIE_MARKET_PROVIDER_ONLY=true`，该模式不传/消费 Runtime 与模型配置，不启动 Runtime，不伪造 generation，readyz 保持未就绪而 liveness/hello 可用。后续模型配置完成需按正常 EOF 关闭并重启完整模式。

完整冻结意图用现有 bbolt receipt key 做 HMAC，不只比较 selection digest。accepted 原回执在任何 probe/prepare/config/turn 前返回；pending/uncertain 不自动派发。只持久化安全关联/摘要/原回执，正文、附件、grantRef 与秘密不进 ledger。正文上限、最多 8 个 grant 与 64MiB 合计内存、单调 TTL 和 1024 个控制回执均有界，消费中的正文直到操作返回才释放预算。

在既有 modelOperationMu 内串行完成模型与选集装配。新会话 prepare(null) → 完整 Gateway 配置 thread/start → 实际 turn/start → 一次绑定真实 thread/turn；已有会话先比对 task/session/localSession/cwd 与模型 revision，允许普通非 draft 会话显式转为 market purpose，仅接受全新 operation，再一次 idle/cold resume 合并配置。Store8 保存 market ownership，旧读者模式保留版本而不降级。新会话 expected revision 0 对应回执 actual revision 1；续轮保持实际 revision。

空选集显式关闭所有继承 MCP 与 Gateway，仍保留 ask/auto/full；非空只允许 ask，不能静默改模式。managed 模型切换在原流程里先撤旧租约、关闭所有 MCP，再正常 SwitchThread，不授予新工具。旧 turn/resume 不能写 managed 会话。原有 shell/file 等非 market 审批仍使用其既有路径，旧 decide 明确拒绝 market callback。

Gateway 元数据只用于按 callRef 查询 worker 的真实 PendingCall。审批显示其安全 review 与完整身份关联；私管道决定必须核对当前 Native scope、callback、revision、callRef、真实 thread/turn，再写 worker decision，最后正常回复 native elicitation。采用现有 Runtime callback 生命周期；超时/terminal/EOF 关闭准入。审批通过不是工具执行成功。工具读取只投影实际 Runtime MCP Items 的受限脱敏文本和状态；当前 native elicitation 没有 item ID，因此不猜测 service/callRef 关联。

正常 EOF 先关闭 Native grant 与 Broker admission，再 HTTP drain/Runtime EOF；不等 HTTP handler 完成才撤市场能力。启动和关闭使用现有生命周期锁，避免 provider-only 子进程在关闭之后才生成。所有 shutdown、取消与容量边界采用正常 stdin EOF/等待。超时保留 owner 与 STOP_PENDING，不强杀、不伪造外部操作完成或回滚。Broker receipt 满导致 revoke/shutdown capacity_exceeded 时保留原 typed 错误并 EOF 退役的既有规则继续保留。

## 验证及未证明的范围

`make market-product-test` 是正常 in-process 生命周期与 DTO/存储测试，不产生模型或平台调用。覆盖新会话、显式 adoption、续轮、原回执重读、模型切换先清 Gateway、撤销与正常 EOF、Store8 兼容、正文不持久化、实际 item 投影、不猜关联及旧审批不决定 market。

真实 canonical worker owner 测试显式指定 manifest；5 个顶层用例共 6 个子进程，涵盖空选择生命周期、未 qualified 非空拒绝、新 thread 双 ID、1024 回执正常容量边界的 revoke/shutdown 两子例、provider preinitialize 到同一 child 的真实 initialize。最新版 f52d2f51109b6ce7779866b034cd0dbb8567f8683700453e98ca1b7164752e5e 已全部通过，正常 exit0。前一 v2 978f... 制品也有 6 个正常退出，保留历史；最初测试临时目录不满足私有 storage guard，在 spawn 前被拒绝，改为正常新建 0700 Host home 后通过，没有改变已有权限。前一阶段 18 个实际子进程记录保留，最后增加产品 v2 必须具备私有 home 的前置检查后，再实跑同一 child 的 provider→initialize 正常用例，exit0；至此累计 31 个 owner 测试子进程正常退出。

本轮这些 Host 测试没有真实 OAuth 登录、真实 Tushare 工具调用或付费模型调用。当前 Tushare provider 支持授权与 metadata probe，但真实执行白名单仍须根据授权后的实际 metadata 完成资格；其他 50 个服务不因此被标记可执行。原 Runtime/Gateway run06 资格是旧的固定本地合成验证，不能代替本次 Host/Native 完整端到端结果。产品启动与 UI 验收由元仓 evidence 另行记录。

没有执行进程强杀、恶意 fixture、权限破坏、可执行文件替换或故障注入全套验收。不能由上述正常流程测试声称真实供应商异常/取消已经得到故障注入验证。摘要与实际命令结果见 `market-product-verification.json`。
