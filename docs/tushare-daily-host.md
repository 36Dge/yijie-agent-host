# FEAT-157 Tushare daily Host 接线

2026-10-08，local_worktree_candidate，release=false。本次在现有单一 Host-owned worker 与可信 Native grant 上收敛接线，没有新增凭据进程、Wire DTO、HTTP 授权入口或真实模型/金融调用。

Host canonical manifest reader 精确接受外部网络开启的 schemaVersion 2 `tushare-daily-v1`。既有 `tushare-oauth-v1` 仍可用于 OAuth/metadata 管理，不能从其部署标记推断工具可用；真正工具资格由 worker 决定。普通文件、SHA、大小、content-addressed 路径、固定 Codex 来源及非 qualification 制品校验保持。

本轮 Contracts 语义位于 `market-provider/transport.json` 的 qualification/generation/admission/tool_profile 和 `market-broker-control/control.json` 的 qualification：仅经当前 `daily` schema 审核的 `tushare_daily`，输入为同源归一化单个有效 A 股代码与单个合法交易查询日期。首个验收样本不是产品硬编码参数。其他工具与服务不被此 profile 授予执行资格。

Host 不再在每轮发送时创建 provider_probe operation、重新连接远端或消耗 64 项 provider operation ledger。Native 的显式 Enable 与其当前管理 probe/资格路径仍是独立前提；Host 的 prepare 仅让 worker 按当前 owner/tenant、Host/Native epoch、权限 revision 和 exact installation/revision/generation 查唯一 live backend，其 credentialRef 由该 binding 冻结。OAuth 成功、历史 SQL 与旧 provider receipt 不能在 submit 中重建 backend。同代 credentialRef 替换必须推进 generation 并撤旧绑定。Host 仍验证完整 Submission.services 并纳入 full-intent HMAC。

accepted 回执继续先于任何重新准入或 Runtime 工作返回。新发送保留非空 ask 门禁、空选集关闭全部继承 MCP、首轮 prepare(null) 后完整配置 thread/start、续轮一次 idle/cold resume、真实 thread/turn 双 ID 冻结及终态/取消撤销。Gateway server key 保持 `yijie_market`；请求批准与结果读取继续使用已装配的 Native/Host 私有协议与实际 Runtime Items，不猜 callRef/item 关联。

时限不向上放宽：Native HTTP 请求原本为 30 秒；Host 准入与 native RPC 共用最多 20 秒，失败时沿用最多 5 秒的撤租清理，总计最多 25 秒并留响应余量。已有更短父 context 保持更短，排队仍复用既有 modelOperationMu，但可在本次 context 到期时退出，不留下后台派发；准入配置/prepare 最多 10 秒且不超过 Runtime 原配置时限。后续 native RPC 共用剩余 active 预算，不再各自延长整次提交。明确在准入前取消的请求不发起 native mutation；进入 native mutation 后仍保留原 pending/uncertain 回执，并只恢复原事实、不重发。

验证：`make market-daily-host-test` 的 9 个定向普通用例（含 profile 纯数据子例）已通过 race 检查；新增 deadline 用例以普通 in-process Runtime adapter 核对阶段共用更短调用者 deadline，不使用睡眠超时/故障注入。Broker/Provider/Host 三族 consumer `--check`、`make lint`、`git diff --check` 通过。没有重启正在运行的 App/Host/worker，没有真实模型或 Tushare tools/call，没有新增真实子进程测试计数。新版 canonical daily 制品及真实批准调用结果由后续产品验收另行记录，不能把本次单元通过当成真实金融查询通过。
