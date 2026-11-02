# 回答完成与资源展示修复验收

2026-11-01，用户授权深入修复只有过程说明就结束、资源查询结果被隐藏、结束后的空思考入口三项问题。`contract-impact = semantic`；本机固定开发客户端候选，不推送、不发布。公共 JSON 形状不变；Runtime/Host/Native 数据身份及权限保持。

## 原因与实现

- Runtime 原结束分支只判断是否还有工具/输入，没有判断最后输出是否只是过程说明。复用同轮采样循环与 turn ExtensionData，Host 的 `[yijie-answer-completion-v1]` 只为普通聊天显式启用；有结构化输出 schema 的计划/标题走原路径。补答至多两次且不暴露工具，分发器也先拒绝额外工具，仍无结果发出已有 failed 终态。无另建任务或已执行操作重放。
- Host 原 MCP 展示仅支持特定工具；原生资源查询成功的空数组被误当成没有展示适配。现在只为 `list_mcp_resources` 和 `list_mcp_resource_templates` 增加封闭结果投影，显示真实数量及经脱敏的名称/标题，URI/cursor/meta 保持隐藏，未知结构不伪造空列表。复用既有文本、诊断和容量规则。
- Desktop 按实际非空文本判断思考内容，结束后不显示空标题；保留原始记录。已确认工具生命周期与内容可用性分开展示，partial 不能抹去完成进度，也不能推导成功。

## 权威与来源

- Runtime source `04c11b66e4b4994419a8bc3fe5539421838cf057`，canonical `answer-completion-v1`，完整保留此前检索执行约束与 r5 补丁；269 份 app-server schema 与前一产物摘要相同。
- Contracts source `b8854c159375a1abd2fef47f7e6ee182b84a4b09`，运行时来源与 NativeMcpDisplay 展示语义先行，消费者经原同步脚本精确锁定。旧 v3/v4 仍可解码新 text 输出，结构比较无变化。
- Host 源码通过 Desktop 原 `kimi-k31-build-candidate.mjs --freeze` 记录为 local_worktree_candidate，不冒称已发布版本。worker 仅增加已验证的 Runtime wrapper commit，codex-rs 库子树逐字节不变。

## 已执行验证

- Runtime 5 项规则单测、核心 Clippy、canonical release 编译与补丁重放/生成 schema 校验通过。
- 真实 Runtime、本机正常 Responses：7 轮覆盖未标阶段过程说明、空输出、两次仍失败、补答期间额外工具执行前拒绝、简短答案、只要求计划、明确 commentary；同一轮续答、失败后新轮与正常 Shutdown 通过。
- 原 13 步检索硬拦截、7 次真实 worker 检索选择/切模型、6 次标题/计划/图片/专家/本地文件调用面回归均通过；所有请求仅本机，无供应商调用。
- Host 的资源展示、内容部分可用、闭合 schema、原有 MCP 展示和固定诊断映射使用 race 检查通过，go vet 通过。
- Desktop 83 项针对性回归通过，lint/TypeScript、前端 build、设计文档 build 通过。实际组件亮/暗主题与 1180×760 验收：空思考隐藏、真实空结果、对应进度、失败显示“未完成”，无横向溢出。旧测试对 Markdown 及空思考的过时断言按已获批行为更新。

## 边界

未标阶段自然语言的识别是保守兜底，不等同事实质量或所有任务语义的通用判定。旧轮次不自动重跑；当时已省略的历史工具结果不猜测补造。旧的已保存失败回答不会凭更新变成已完成正文。未执行真实供应商任务和用户历史重放；未执行包含强杀、可执行替换、权限破坏或攻击 fixture 的全仓套件。

首次编译期间收紧了“不要执行”与“只要计划”的区分，原构建输入校验按设计拒绝了变更中的候选；未安装该次输出。最终规范构建全部通过，原受审 Runtime 保持原样。

激活步骤：通过应用正常退出→固定入口增量重启→ready/source/process 只读核验。激活结果另记录于 Desktop `.local/development/answer-completion-verification.json`，未完成激活前不称客户端已使用新规则。


## 本机激活

用户确认已正常退出后，记录为 CLI exit 0、signal=null，并观察到 Native 正常清理及专用容器 STOPPED。通过原 `ai.yijie.development-entry` 固定入口重新启动，worker 复用构建耗时 0.61 秒，Desktop 原生增量构建 8.91 秒；未生成安装包或全量备份。运行中 Runtime 路径为 answer-completion-v1，SHA-256 `aeb9977fcaa266271ba2f009d9cd11f7063b8e6db7bef0815be56050abecd863` 与 canonical manifest 一致；Host/前端 ready 200，原 authenticated workflow ready。原 Runtime 的 31 个 thread 条目仍在，本次审计的原 rollout 校验值不变；未发起用户对话或历史重放。最终本机回执 `.local/development/answer-completion-verification.json` 为 ACTIVE_SCOPED_LOCAL_DEVELOPMENT。
