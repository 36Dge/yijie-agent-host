# 执行进展修复的 Host 接入

2026-11-03，Owner 复审后要求实施且必须复用 `yijie-codex` 既有机制。`contract-impact = semantic`，不新增公共 HTTP/RPC/DTO、数据库或第二个执行循环。

Host 只做配置与指令投影：

- 普通受管聊天配置原生 `features.rollout_budget`，默认 limit=250000，prefill/sampling 权重均为 1，剩余 125000/50000/25000 时由 Runtime 提醒。实际累计、共享子代理与 `SessionBudgetExceeded` 全由 Runtime 控制；Host 不累计 token、不按调用次数重试或去重。
- 预算是一次加载的根会话树预算，口径为未缓存输入加输出。它不包含缓存读取费用，不是单轮预算、跨重载永久额度或人民币封顶；响应结算前的一次在途请求可能超过阈值。显式 Goal 预算、上下文压缩与上下文提醒保持各自的原生语义。
- 专家长任务和 input-only 草案不强加普通聊天默认；调用方已传入的显式 native budget override 保持。普通聊天默认通过既有 thread config override 投影，优先级与其它 Host 受管 profile 设置一致。`RolloutBudgetLimitTokens=0` 表示此适配不覆盖原生设置。
- 普通聊天和专家的通用业务指令只保留在 developer instructions，base 为短指导；专家额外保留 role/index。2026-11-04 已消除专家路径重复的完整业务基线；不降低原生权限或审批。
- Kimi hosted-only 路由明确没有可调用的 query/open/find 接口；不让计划或空命令冒充检索。已有选定的 Kimi Search Pro/Fetch 继续走原 Broker/Worker、单次审批与原生搜索关闭配置，不自动修改选集。
- 重复计划检测与收尾完全位于新的 canonical Runtime 候选；复用已有两次无工具补答、拒绝工具和真实失败终态。Host 只消费原事件。

实现及验证状态以元仓本次记录为准。源码变更不等于已激活；Runtime → Contracts → Host/Desktop 的不可变来源与 schema 门禁必须先完成。真实供应商验证需要本任务的明确有限额度，不借历史台账。

## 2026-11-03 本地验证

新 canonical Runtime 与 Contracts 精确引用下，`TestExecutionProgressRepeatedPlanRuntime` 通过：同一计划在 explanation/搜索事件 ID 改变时仍于三次更新后进入原有收尾，第四次工具列表为空，有答案共四次完成，无答案共五次失败。`TestExecutionProgressNativeBudgetRuntime` 通过：原生预算按未缓存输入+输出计算，跨两个用户轮共享并生成原生失败；通用指令实际仅一份。原有七个补答/短答/仅计划/补答时拒绝工具的正常协议场景通过。上述均为真实固定 Runtime 加本机回环普通响应，供应商请求为零，不是 Kimi 效果评测。

## 2026-11-04 提示词 P1 整改

本轮整体 `contract-impact = breaking`：HTTP text 新增可选技能身份为 additive，
提示词装配改变为 semantic，最高影响来自 Desktop 私有 SQL 43 的前向 reader guard。
Owner 已要求实施并选择仅显式加载技能。Runtime 源码和二进制不改，仍为
`yijie-codex@55b4e51049d6f4fc0af4b35f12832357f5e4ea06`。

- Host 原有配置白名单只接受 projects，误拒原生 `skills/config/write` 写出的配置。
  现保留 projects/skills 的宿主管理边界，结构完全用固定 Runtime 的 config schema
  校验；不重写原生用户配置、不复制配置 DTO、不放开 provider/security 配置。
- Desktop 仅发送 Contracts 定义的 `{id, version}`。Host 复用受管目录、安装回执、
  `skills/list` 核验已启用资源，转为原生 `UserInput::Skill{name,path}`。
  正文读取、同名消歧与同轮去重均由 Runtime 完成。已接受的操作先按原持久索引
  重放，不因后来卸载技能而再次分派；新操作才重新校验资源。
- 启停后调用原生 `config/batchWrite`，参数 `edits=[]`、`reloadUserConfig=true`。
  Runtime 更新已加载线程配置，不重建线程、不重置原生预算、不新增 Host 配置缓存。
- 当前受管聊天设置原生 `skills.include_instructions=false`。用户仍可显式选择技能
  或用原生命名语法点名；没有常驻技能目录，也不根据任务相似性自动启用技能。
  历史 rollout 不改写，旧目录仍可能存在于旧历史，由原生上下文管理处理。
- 专家 role/index 仅在 base；通用业务政策仅在 developer。取消专家保留短的当前
  角色清除说明，不删除历史，不让旧模型切换消息重新指定专家。

API 来源为 Contracts `358a4642df9c8be15aea55b08589045669ffefc4`；原生 schema 与
模型/市场/专家输入来源为 `633aa9eee300a3123c1574306234682059f557ec`。
Owner/producer 为 Agent Runtime Team，consumers 为 Desktop/Host；跨仓实施由本次
Owner 指令授权。生产发布/tag 为 N/A，本次仅原固定本地开发入口。

固定 Runtime 的本机回环证明：停用→正常 EOF→重开保留原生配置；同名 A/B 显式
选择只载入 A；启用后原线程热重载，三轮共享同一原生预算并到额失败。普通、专家、
冷恢复、切换模型与显式技能的 13 个请求场景保留原生行为。普通新对话文本字符
20136→12802，专家 26124→13031；这是字符测量，不是供应商 token 或费用比例。
工具 schema 保持原能力；未开展付费模型测试、攻击 fixture、权限破坏或强杀测试。

部署顺序：Contracts→Host→Native SQL 43→前端显式技能开关。旧请求省略 skills 时
维持原语义。回退可关闭该前端开关，但不得用旧 reader 打开已迁移数据库；须保留
SQL 43 读取能力。无需回退 Runtime，也不清空历史或重放任务。

同轮继续完成两项 P2 收敛：检索说明不再按 provider 或临时工具选择拼接，固定为
供应商无关规则，由当前工具定义声明能力；短 base 明确当前工具定义优先于旧历史
能力描述。Kimi→MiniMax→Kimi 的实际请求中，该规则始终只有一份。原生权限更新
块和旧历史仍保留，没有实现 Host 消息去重或摘要。

草案文本 fallback 使用标准 `json.Compact` 机械压紧同一 canonical OutputSchema，
保持原生 outputSchema 和 input-only 证明；回环解析严格相同，工具仍为空，草案
提示文本 6491→3557 字符，减少 2934。标题入口继续禁用，错误原因改为尚未接入和
验证原生 input-only；不再错误声称 Runtime 本身没有该能力。

多代理/Goal 工具范围与原生压缩阈值保持：轻量用途尚无已确认的产品选择入口，
不能按任务关键词自动裁剪；长历史压缩仍用 Runtime 原配置，未新增模型调用。
通用业务指令中的既有检索、安全和输出约束未擅自删减；具体业务内容瘦身需在其
既有资产所有权下另行验证，不把原生机制复用等同于承诺所有文字都可删除。
