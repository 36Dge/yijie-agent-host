# 原生轻量配置与 P2 提示词复审

2026-11-04。Owner 再次要求复用既有 Runtime，确认完整模式仍是默认；仅新建聊天
可以显式选择轻量问答。本批整体 `contract-impact = breaking`，来自 Host 私有
schema 12 / Desktop SQL 44 前向 reader；公共新请求字段为 additive，提示文本为
semantic。没有 Runtime 补丁、Agent 循环、工具筛选引擎、预算账本或历史摘要器。

`ToolProfile` 是产品选择元数据，唯一 wire 源为 Agent Host OpenAPI，固定 Contracts
`ccf89302398309fbc8d500ec95531e3e68b579f9`。Host 对 full 保留原配置，对 light 只向
原生 thread/start、thread/resume 投影 `features.multi_agent=false` 与
`features.goals=false`；实际工具注册、技能加载、权限和预算仍由 Runtime 负责。
不能使用已被忽略的 `multiAgentMode` 参数或假设当前模型支持 tool_search。

选择在原 Native 提交事务与 Host 会话 Record 中固定。旧记录省略字段，完整模式
仍省略字段以保持旧字节；schema 12 在原模型包络上扩展，旧 reader 必须拒绝，
不能忽略 light 后恢复成 full。普通恢复、模型切换、当前连接器提交均恢复原模式。
已有会话不能中途切模式；专家、计划草案保持各自原配置，轻量会话不得被静默改成专家。
已接受重放继续读原回执，不能因新选择产生第二次执行。

真实固定 Runtime 的普通回环验证 full→light→正常 EOF/恢复→切换模型；技能
config/batchWrite 热刷新也没有把 light 改回 full。工具 JSON 由 20064 bytes 降到
5530 bytes，exec、计划等其它工具保留。它是工具定义大小，不是供应商计费 token。

长历史复审没有发现需要重造压缩器的缺口。固定 Runtime 直接使用 ModelInfo 的
auto_compact_token_limit、context_window_token_status、原生 compacted item 和
替换历史。当前模型的受管阈值保持；普通回环用明确标为模拟的大缓存用量触发一次
原生压缩，下一次采样确实使用原生摘要。没有降低生产阈值或压缩用户真实历史。
用户界面已对同模型选择短路；实际模型/专家变化和当前市场每轮 capability 更新仍
需要原生生命周期，不按文本相等跳过授权配置切换。旧权限更新和旧长指令不删除。

共享提示由 6161 字符精简至 4150。按结果/输出范围、公开进展、连接器优先级、
引用依据、数字/研究口径、禁止脚本检索绕行、首次失败说明、有条件重试、工具用途
等 13 类规则逐项核对保留。这里只缩短稳定文字，不新增按任务猜测的提示词分类器。
已存在于历史中的旧规则由原生上下文机制处理，不能声称重开就自动删除旧成本。

Owner/consumer 为本次个人本机 Desktop/Agent Runtime 维护范围。激活顺序为
Contracts→Host→Native→renderer 开关；只本地提交，不发布 tag。回退可关闭新建
入口，但必须保留 Host 12 / SQL 44 reader 与 light 配置恢复；不能降 schema、
清历史或用旧程序直接打开新数据。权限、批准、连接器绑定、原生预算参数均不变。

安全验证只使用普通回环、正常 EOF、临时正常数据库与元数据；没有付费模型调用、
强杀、权限破坏、恶意归档或攻击 fixture。完整日志在工作区
`.local/diagnostics/2026-11-04-prompt-chain-p2/`，激活事实以后态记录为准。
