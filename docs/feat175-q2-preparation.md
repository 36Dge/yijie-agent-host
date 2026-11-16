# FEAT-175 Q2 离线准备包

本包冻结12个自然语言任务、当前技术profile、工具定义及指令，提供原生事件离线评分。准备阶段的零调用状态保留在原包中；随后已执行一次获准的真实Q2并停批，见[执行说明](feat175-q2-execution.md)。本文的 `feat175-q2.mjs` 只做离线冻结／评分，`execute`等未支持命令直接拒绝；后续新增的独立受控入口见[原生执行说明](feat175-q2-execution.md)，当前没有可自动复用的付费授权。

## 固定内容

- `internal/codex/testdata/feat175-q2/cases.json`：六类提示×Kimi K3.1 Pro／MiniMax M3，各一次；提示不点名工具。明确URL、Pro站点／日期、Basic limit／include_content均有独立判据。
- `effective-profile.json`：通过实际Host纯装配函数生成的静态快照，包含模型目录、base/developer指令、原生权限、配置投影与rollout预算；不等于已经观察到真实供应商wire。临时MCP地址不被冻结，也不提供可执行远端。
- `source-lock.json`：冻结输入、评分器、复用实现、canonical schema及既有Runtime二进制摘要。来源变化必须先审阅，不自动接受新版本。
- `authorization-not-granted.json`：当前任务专属审批输入表，实际模型HTTP、hosted搜索、REST和金额上限全部为0。不是任何执行授权。

## 安全复现

在Host仓运行：

```bash
bash scripts/check-builtin-web-q2.sh
node scripts/feat175-q2.mjs score internal/codex/testdata/feat175-q2/example-synthetic-first-error.json
```

第一条检查同源契约、以普通Go测试重新计算静态profile、核对冻结摘要并执行评分器单元测试；不运行Q1长等待或真实Runtime；新增计量单元测试只启动普通loopback服务。第二条用明确合成的“先Basic后Pro”样本展示首次失败仍保留，其他未执行任务仍留在分母；结果不能用于宣称真实Q2通过。

只在审阅后生成新准备版本时使用：

```bash
YIJIE_FEAT175_Q2_UPDATE_PROFILE=1 go test ./internal/codex -run '^TestFEAT175Q2FrozenProfile$' -count=1
node scripts/feat175-q2.mjs freeze
```

正式检查脚本会清除更新开关，避免用验证过程覆盖快照。未来一旦真实Q2开始，需保留原输入和结果；内容变更建立新包版本，不覆盖已执行版本。

## 原生观测与计分

观察文件绑定package SHA、模型、case ID、实际提交提示的SHA以及真实thread/turn。保留原`item/started`、`item/completed`、`turn/completed`，按started顺序识别第一次外部检索，同一Item完成时只更新结果。不能通过分析Code Mode代码字符串或最终回答猜测发生了调用。

额外原生代理必须从原生thread/read父子关系和turn/started取得归属，再放入`additional_turns`；来自模型参数或“最近活跃线程”的关系不被认可。子任务的检索也计入首次动作及额外hosted统计；其费用仍由整批计量纳入，不另建Host推理调度器。

来源不完整、缺started顺序、hosted可见性未知、缺必要人工query相关性审阅，分别保持INCONCLUSIVE或REVIEW_REQUIRED。MISSING任务留在每模型6项分母；独立检索类分母固定每模型4项。先选错后纠正仍失败，恢复单列。工具执行状态、回答和证据质量独立记录，任何离线分数都不直接授予Q2资格。

`complete`可见性必须有实际provider／原生事件和费用观测依据，不能因为没看到sources就填写complete。日志仅保留本轮公开／合成问题及必要证据，不复用日常聊天或记录凭据。

Q2使用合成MCP内容时，额外hosted需要区分发生在预期MCP之前，还是模型收到明确合成／不充分材料之后。两者均如实记录，但后者不能直接推断真实产品的重复检索率；调用原因也不能只凭事件顺序认定。Q3在真实结果下再应用正式重复率门槛。

## 后续真实执行前的交接

候选规模为12任务、最多48个模型HTTP、每模型24／每任务4；本地MCP每任务最多4次。观察到24次hosted动作时停止新准入。这些是拟用上限，当前批准数仍为0；hosted观察阈值无法截断供应商单次HTTP内部未暴露的搜索。

金额未获批准，保持0。当前提出20元观测后软停发阈值，须明确接受在途超额；它不是硬上限。真实执行前必须明确正数总额、当前资费依据、未知费用处理及可核验的费用约束；不得用HTTP数量冒充金额硬上限。优先复用原生token／rollout／Goal预算；本包保留既有250000 rollout默认，不新增Goal或Host Token累计器。

后续已补齐专属 `feat175_q2_meter.py` 与 `feat175_q2_run.py`，复用既有费用观察／凭据原语及 `kimiK31MeterArguments`，逐字转发模型请求，原生请求／流重试为0。旧计量器的请求改写和历史授权不复用；当前门禁、账本及正常退出验证见[执行说明](feat175-q2-execution.md)。

真实执行还需已获准的精确模型profile与凭据来源资格。Q2可使用技术profile＋显式合成MCP材料，真实Basic/Pro/Fetch调用仍为0；托管native搜索可能收费。Q3/Q4必须重验最终产品profile与真实结果。

静态指令审阅记录：当前developer仍保留“active connector selection／otherwise native retrieval”的既有说明。Q2固定为空市场选集，内置工具描述按任务区分。该措辞对应用内置能力的解释需在真实Q2观察，不在准备阶段添加点名工具提示来保证成功；若发生冲突，保留失败后审阅最小配置／指令修正。
