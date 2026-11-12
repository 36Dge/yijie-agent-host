# FEAT-173 本地内置视频

在既有 native media profile 中默认启用 Seedance：普通 MCP `yijie_media.generate_video` 创建，`get_video_result` 查询与交付。固定 `doubao-seedance-2-5-260628`，只发 model/content，API 可选参数省略。无新 Runtime 补丁、MCP 进程、任务 worker 或用户授权入口。专家绑定/待选状态在每个入口、GET、下载和发布前检查；选定专家后不可调用内置视频，失败也不清空绑定。

入口沿用原图片 root 绑定、当前附件与取消 context。私有 `video_operations_v1` 懒创建，以 session/root 为键，version=1，不变更既有顶层 Store 版本；只记录幂等摘要、任务/产物身份、真实最后观察及查询节流时间。发送前预占，任何不确定结果都不自动 POST。一个聊天有未决操作时，新 root 只能查原任务；独立并行需求使用现有新建任务入口。正常清理会话级联删除该 bucket 记录，迟到响应不能重建已删除记录。

创建总预算 30 秒；查询总预算 120 秒包含入口绑定、轮询、下载槽位等待和下载，adapter 145 秒、MCP 150 秒。查询间隔至少 10 秒，429 Retry-After 跨工具调用仍有效；该节流时间不冒充供应商状态。单 GET 最大15秒，下载最大60秒，均受整体剩余期限限制，下载并发2。下载使用仍可用的预算，上限60秒；整体期限已耗尽时才返回pending等待后续观察。远端 running 不支持取消；本机停止只停止当前等待/发布，不调用 DELETE。正常重开在交付前仍可查询有 task ID 的原任务；未获 ID 的不确定创建保留 unknown，不根据时间猜失败。

复用原 encrypted Artifact spool、media v9 事件、Native SQLCipher 提交和 ACK。首次 started 前持久化 handoff_attempted；同锁内发布 started/completed，均绑定当前观察 root，先于其终态。视频 kind=video、provenance=provider、video/mp4、64MiB；复用现有接收、预览、Range、保存和原生 ACK。handoff 后禁止重下载/重发/新建 Artifact ID 或 ACK 镜像。Host 重开后若原 spool 已失效，只报告 delivery_unconfirmed；Native 已提交的视频仍可在本地播放保存。

凭据仍是 `imagegen.ArkCredential(HostHome)`，沿用 Seedream 的服务名、账户作用域和非交互读取。用户无需安装 MCP 或提供 Key；运营方必须对最终受管可执行文件完成原条目的访问预置，并核验相同 Key 的 Seedance 模型资格。只读凭据可用不等于模型资格或费用验收。

开发回退：正常退出 App 后以 `YIJIE_FEAT173_VIDEO_MCP=false pnpm client:dev` 重开，同 Home/Runtime/图片能力保留；不删除视频记录。公开发布、Git 提交/推送不在本次授权范围。正常启动和真实生成的最终证据以元仓 FEAT-173 实施记录为准。

局部验证覆盖同源契约、真实固定 Runtime/adapter 的两主聊 namespace、完整 Host admission→media 事件→资源→ACK、重复创建/停止/迟到响应/节流/恢复/清理/专家阻断。普通 MP4 fixture 不代表 Seedance 输出；真实生成、编码播放和保存必须在独立费用授权后完成。

2026-11-12实际验收：独立获准的文生、首帧、同图首尾帧、双图参考共4次创建均成功，Kimi/MiniMax主聊共32次。已确认原生提交、播放、保存、真实停止后原任务恢复和正常重开。普通日常入口已恢复，Host同SHA凭据只读ready；详情与保留限制见元仓FEAT-173的12-real-acceptance.md。

实现复审：保留到期时刚收到的远端终态，区分单次预算与用户停止，查询/下载Retry-After跨取消保留；响应体读取超时仍可读取重试。首期只支持文本/当前图片，不承诺原片编辑、延长或精确输出参数；专家自身能力不受这组内置限制。证据见元仓13-implementation-reaudit.md。
