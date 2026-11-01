# 网页检索路由与失败处理

> 2026-11-01 后续硬拦截版本：用户进一步明确不能只靠模型指令。当前增加 Runtime 原工具分发器的执行前约束，见文末“执行前硬拦截”；下面原有指令层的实现和验证保留为上一阶段记录。

2026-11-01。用户要求审计内置搜索与 Kimi 检索连接器的选择方式，并停止本地脚本补抓网页和无条件重复失败调用。

`contract-impact = semantic`：调整 Host 提供给 Runtime 的模型指令，以及已选检索连接器时的既有 `web_search=disabled` 配置适用范围。Owner/消费者为本次用户授权的本机易界客户端、Host 和固定 Runtime。HTTP/IPC/MCP schema、审批、沙箱、历史格式及凭据不变；公共 Contracts 变更为 N/A。使用既有 `thread/start` / `thread/resume` 的 base/developer instructions 和 config，不新增 Runtime 补丁、依赖或执行拦截器。部署为固定开发入口正常退出后增量重启；回滚使用相同入口恢复 Host 源码及其来源记录，无数据迁移。

## 当前路由

- 普通新对话的连接器选集默认为空。全局安装、启用、启动重连仅使连接器可选，不会将其自动加入对话。当前对话的选集来自 Desktop composer，提交时冻结。
- 没有适用的已选检索连接器时，保留 Runtime 原有内置搜索配置；模型请求使用 `tool_choice=auto`。这表示搜索可用，由模型决定是否调用，不是每次对话强制搜索。
- 选中并通过资格校验的 `kimi-web` 会经 Broker/Gateway 向该轮暴露 `kimi_web_search`、`kimi_web_search_pro`、`kimi_web_fetch`。Host 在本轮关闭内置搜索，避免同时提供两条检索入口。此前这一配置仅针对 Kimi K3.1，修正后对当前可用模型一致。
- 非空连接器选集仍要求原有的“请求批准”模式；安装、启用、资格校验及单次调用批准相互独立。Search/Pro/Fetch 分别调用 Kimi `/v1/tools/search`、`/v1/tools/search_pro`、`/v1/tools/fetch`，目标网页由供应商读取，不通过本地 shell 抓取。
- Search/Pro/Fetch 的具体选择仍由模型遵循既有工具描述：普通问答、研究使用 Pro；纯结果列表使用 Basic；指定 URL 正文使用 Fetch。没有按用户问题关键词硬编码分类器。连接器调用沿用逐次审批，不因本次调整增加执行权限。
- 移除选集或切换模型时，既有 SwitchThread → unsubscribe/resume 流程从 cwd 配置重新装配，不将旧轮的关闭配置写回持久配置。选集无效会在准入阶段拒绝，不静默切换搜索路径。
- 专家、本地参考咨询和计划草拟各自已有的禁用工具规则保持；它们不属于“普通新对话默认内置搜索”的范围。

## 本次规则

1. 在线资料检索使用适用的已选连接器或内置检索能力。该能力已可用或已调用时，不得转用 exec_command / shell 中的 Python、curl、wget、Node 等方式补抓、验证或重复搜索网页，也不得以此为由请求扩大本地网络权限。
2. 需要正文时只使用当前检索路径提供的读取能力；未提供或读取失败，说明缺少的证据，使用已获取的资料回答。保留用户任务实际需要的本地计算、文件处理和软件开发命令。
3. 首次出现权限、网络、DNS、代理或认证错误，先说明已观察到的错误和影响。权限/网络/参数等相关前提未实际变化时，不重跑相同或等价请求；改解释器、命令包装或无关参数不算变化。不把 DNS 错误单独解释为已证明的权限拒绝。
4. 退出码 0 不足以证明抓取成功；输出包含错误且没有资料时按读取失败处理。证据足够即回答，证据不足说明限制，不制造进度调用。

这部分属于模型行为指令，不是任意脚本的执行前硬拦截。Host 的既有审批回调不能观察所有沙箱内命令；本次不声称通过 UI 隐藏、错误文案或指令文字实现了执行器级保证。

## 验证与边界

复用固定真实 Runtime 与 canonical worker，仅连接本机正常响应服务，验证新对话、恢复、移除选集、重新选择、模型切换后的工具声明和指令传播。使用普通数据，正常退出，不触发实际联网脚本、故障注入、攻击 fixture 或真实收费模型请求。模型的真实遵循程度与本机协议验证区分记录。

已完成：

- Go 1.26.5 离线工具链：`make lint`（gofmt、go vet、shell 语法）及 diff 检查通过。系统默认本地 Go 1.26.4 不满足 go.mod，改用已安装的项目精确工具链，无下载/升级。
- `TestFEAT162SelectedRetrievalConfig`：3 个模型身份 × 3 种选集，9 项配置断言通过。
- `TestFEAT162SelectedRetrievalRuntime`：固定 Runtime + canonical worker，7 次本机 Responses 请求，覆盖 start/resume/clear/reselect/带选集切模型/清选集切模型/切回 Kimi；三工具暴露、内置搜索互斥、base/developer 指令实际送达、正常命令能力保留均通过。0 次供应商请求，正常 Shutdown。
- `TestFEAT164CallSurfacesRuntime`：6 次本机 Responses 请求，普通输入/图片、标题、计划草拟及恢复、专家、正常本地文件读取及续答通过；0 次供应商请求，正常 Shutdown。
- 专家配置隔离、专家完整基础指令保留、原权限模式映射 3 项回归通过。两组 Runtime 集成及这 3 项回归使用 `-race`。
- Desktop canonical `kimi-k31-build-candidate.mjs --freeze` 校验全部现有 media/daily 依赖，并记录这次 Host 源码为 `local_worktree_candidate`。未伪称 immutable source、发布资格或生产验收；既有 Runtime、连接器制品和公共契约来源保持。
- 用户通过 ⌘Q 正常退出后，经固定 `pnpm client:dev` 增量重启成功：Native PID 48649、Host PID 48761；Host SHA-256 `e95bc52198d39037d68160da26931a085da7a6272d339df93be0813709bf1321` 包含新规则；Host readiness、前端及原 authenticated workflow 就绪。既有 worker 缓存命中（0.80s），Native 增量编译 10.07s；未生成安装包、全量备份或迁移历史数据。运行证据位于 Desktop `.local/development/web-retrieval-policy-verification.json`，是本机启动验证，不是收费模型行为验收。

未执行：

- 收费模型的自主选工具及真实失败重试验收：本轮没有调用供应商；本机固定响应仅证明工具面和指令传播，不能证明模型对任意提问都遵循规则。
- 全量 `make test`：该仓包含二进制篡改、故障及攻击 fixture，按用户长期条款跳过，采用上述正常、定向测试；未声称全仓测试通过。
- 本地联网失败制造、强杀进程、权限故障、可执行文件替换：按用户长期条款不执行，不用伪造结果替代验收。

## 代码审计入口

- Desktop `src/stores/chat-composer-drafts.store.ts`、`src/pages/chat/ChatPage.vue`：空选集、用户选择、全局启用不自动入选、提交继承。
- Host `internal/codex/market_turns.go`：校验选集、准备 lease、关闭内置搜索、配置 Gateway。
- Host `internal/codex/session_protocol.go`：start/resume 重新发送基础/开发者指令，合并本轮配置。
- Host `internal/codex/web_retrieval_instructions.go`：禁止本地补抓、失败说明及重试前提。
- Runtime `.yijie/patches/chat-models/0004-feat-156-upstream-tool-choice.patch`：保留 `tool_choice=auto`。
- Connectors `worker/src/kimi_web.rs` 与 Contracts 同源工具描述：Basic/Pro/Fetch 的参数、独立端点、审批和不自动重试。

## 执行前硬拦截（2026-11-01）

用户已授权此 Runtime 执行行为调整。`contract-impact = semantic`，沿用既有工具拒绝、Warning 和 NativeNotification 的自由字符串 code；无新 RPC、事件形状、权限授权或历史迁移。Runtime source/manifest 与同形 schema 经原 Contracts 生成/同步链固定，消费者为 Host 与 Desktop。使用固定开发入口正常退出后增量激活，不生成客户端安装包或数据备份。

- 检查位于原 `tool.handle` 之前，拒绝后不进入原命令/连接器执行器；复用原生命周期的 Blocked 结果。
- 搜索状态由真实内置搜索项及 Kimi Search/Pro/Fetch 调用记录建立；与当前轮的 ExtensionData 绑定，无额外业务数据库。识别常用 shell、Python、Node 网页读取、被调用的脚本和本轮交互式解释器输入；空轮询、向 cat 等程序传数据保留。
- 失败使用真实退出码、工具错误及明确的捕获后网络异常，不因界面截断而漏掉异常；成功搜索结果中讨论 DNS/报错的文章不会被误判为执行失败。
- 同一失败操作再次执行前比较规范化参数、实际工作目录、权限与输入快照；保持单轮边界。依赖安装、配置修复等已实际完成的操作会更新相应重试条件。对超出有界观察范围的输入不谎称“条件未变”，保留原执行器检查；此限制与网页补抓拒绝分开。
- 继续保留本地计算、文件处理、开发构建、localhost 请求、显式 API 写请求和已识别的软件包/数据文件下载。原 sandbox 与审批继续生效。
- 拦截原因由既有 Warning 通道输出，Host 映射固定安全类别，前端复用诊断白名单显示中文。原始命令、路径和供应商文本不直接显示。没有伪造执行、成功或失败终态。

此检查针对 Runtime 负责分发的调用，不声称可以控制供应商服务端内部不可见的搜索重试，也不是对任意动态程序的网络隔离器；公共网页读取识别与条件观察均有明确范围。原连接器自身的不自动重试、检索路径互斥和模型规则继续保留。

已执行验证：

- canonical Runtime：8 组规则/兼容单测、核心 Clippy、原补丁链重放与 release 编译通过；269 份 app-server schema 与 r5 完全同摘要。零轮正常 EOF 退出检查通过，无信号和供应商调用。
- `TestYijieRetrievalExecutionGuardRuntime`：真实 canonical 产物、13 次回环 Responses、`-race` 通过。网页补抓、交互式 Python 补抓、条件未变重试三次均没有进入命令执行器；7 个普通命令正常开始，解释器以 exit() 正常退出；补齐缺少输入后同命令重试成功；两种拦截 Warning 实际到达。没有请求 example.com 或真实模型服务。
- 此测试初次准备遗漏临时 Runtime 目录；随后发现非 TTY 的 Python 直接遇到 EOF，均在测试准备中修正后重跑。未修改产品判定以迎合测试，未把前两次失败记作通过。
- 原模型调用面 6 次、原检索选集/切模型路径 7 次回环请求在新产物上通过；最终成功的三组测试覆盖 26 个本机模型协议步骤，0 次供应商请求；另保留上述测试准备失败记录。
- Host 固定原因投影 8 个普通案例、前端 15 项领域回归、8 项开发入口/完整功能装配测试通过。Host lint、Desktop lint/类型检查与前端构建通过。
- Contracts 完整正常生成 23 个 family、生成一致性、lint、14 项定向测试及 10 个完整基线的结构兼容检查通过，详见同名 Contracts 说明。含攻击归档/可执行文件篡改的历史全量测试未执行，不能据此声称全仓测试通过。

固定来源：Runtime `9ebafd71de2dea4c2f016bb38307b0a19a722384`，Contracts `ffb0ef6ef478114464f4ae9e6974a3be3276d8ce`。Host/Desktop 通过原同步入口消费此来源，Host 使用原 canonical freeze 验证的 `local_worktree_candidate`，未宣称已发布。开发客户端已激活：Native PID 46947、Host PID 47054、worker PID 47059、Runtime PID 47061。实际进程使用 `web-retrieval-guard-v1`，Runtime SHA-256 `a59919e60fc813ae4c4ebafa76e39d58cd3bb44164b3905b7a8ddecfcdd14641`；Host SHA-256 `70b0b3736845c0b93243c9265c50837f16d73432c1408899094361bbbd6faa2b` 包含对应精确产物锁与安全原因映射。Host readiness、前端及新的诊断模块均 HTTP 200，原 authenticated workflow 就绪。Native 增量编译 9.69s，worker 0.51s 命中原二进制，没有新 App 安装包、全量数据备份或数据迁移。

启动前检查确认用户客户端与服务已退出，因此直接使用原 `pnpm client:dev`；没有再次要求用户退出。首次启动遇到既有 worker 源码锁拒绝后，服务按原流程正常清理。审查确认新 Runtime wrapper 的 `codex-rs` tree 与原固定库同为 `755f32c45a4c30e805e3c7fe1db89dcb4dcc6e41`，仅登记精确兼容 wrapper，未改校验器、库或依赖；第二次 canonical 启动成功。详见 Connectors `docs/feat164-library-source-compatibility.md`。

最终本机证据：Desktop `.local/development/web-retrieval-guard-verification.json`；真实 Runtime 测试日志：Host `.local/web-retrieval-guard-runtime-e2e.log`。原数据目录保持；没有收费模型自主行为、任意动态程序或供应商内部重试的验收声明。
