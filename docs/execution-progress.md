# 执行进展修复的 Host 接入

2026-11-03，Owner 复审后要求实施且必须复用 `yijie-codex` 既有机制。`contract-impact = semantic`，不新增公共 HTTP/RPC/DTO、数据库或第二个执行循环。

Host 只做配置与指令投影：

- 普通受管聊天配置原生 `features.rollout_budget`，默认 limit=250000，prefill/sampling 权重均为 1，剩余 125000/50000/25000 时由 Runtime 提醒。实际累计、共享子代理与 `SessionBudgetExceeded` 全由 Runtime 控制；Host 不累计 token、不按调用次数重试或去重。
- 预算是一次加载的根会话树预算，口径为未缓存输入加输出。它不包含缓存读取费用，不是单轮预算、跨重载永久额度或人民币封顶；响应结算前的一次在途请求可能超过阈值。显式 Goal 预算、上下文压缩与上下文提醒保持各自的原生语义。
- 专家长任务和 input-only 草案不强加普通聊天默认；调用方已传入的显式 native budget override 保持。普通聊天默认通过既有 thread config override 投影，优先级与其它 Host 受管 profile 设置一致。`RolloutBudgetLimitTokens=0` 表示此适配不覆盖原生设置。
- 普通聊天的通用业务指令只保留在 developer instructions，base 为短中性指导。专家保持原来的完整系统基线与 role/index 组合，避免改变专家指令优先级；不降低原生权限或审批。
- Kimi hosted-only 路由明确没有可调用的 query/open/find 接口；不让计划或空命令冒充检索。已有选定的 Kimi Search Pro/Fetch 继续走原 Broker/Worker、单次审批与原生搜索关闭配置，不自动修改选集。
- 重复计划检测与收尾完全位于新的 canonical Runtime 候选；复用已有两次无工具补答、拒绝工具和真实失败终态。Host 只消费原事件。

实现及验证状态以元仓本次记录为准。源码变更不等于已激活；Runtime → Contracts → Host/Desktop 的不可变来源与 schema 门禁必须先完成。真实供应商验证需要本任务的明确有限额度，不借历史台账。

## 2026-11-03 本地验证

新 canonical Runtime 与 Contracts 精确引用下，`TestExecutionProgressRepeatedPlanRuntime` 通过：同一计划在 explanation/搜索事件 ID 改变时仍于三次更新后进入原有收尾，第四次工具列表为空，有答案共四次完成，无答案共五次失败。`TestExecutionProgressNativeBudgetRuntime` 通过：原生预算按未缓存输入+输出计算，跨两个用户轮共享并生成原生失败；通用指令实际仅一份。原有七个补答/短答/仅计划/补答时拒绝工具的正常协议场景通过。上述均为真实固定 Runtime 加本机回环普通响应，供应商请求为零，不是 Kimi 效果评测。
