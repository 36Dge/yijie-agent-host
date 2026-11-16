# FEAT-175 Q2-v2 本机验证

v2 仅用于本机 QA，产品和固定 Runtime 不变。完整设计、实际验证与限制见[元仓复审](../../yijie/docs/features/FEAT-175-builtin-web-retrieval/25-q2-v2-sampling-and-diagnostics.md)。

## 执行

```bash
bash scripts/check-builtin-web-q2-v2.sh
python3 -B scripts/feat175_q2_v2_run.py local --sampling first_action --output /absolute/new/first-action
python3 -B scripts/feat175_q2_v2_run.py local --sampling full_turn --scenario repeated-hosted --output /absolute/new/full-turn
python3 -B scripts/feat175_q2_v2_run.py local --sampling full_turn --scenario mcp-quota --output /absolute/new/quota
```

所有输出目录必须不存在。没有 live 命令、费用授权或供应商 Key 参数；模型与 MCP 都是明确的本机协议材料。源码／输入锁变化必须先审阅再显式 `node scripts/feat175-q2-v2.mjs freeze`。原 v1 源锁和执行记录不变，不能用 `freeze` 覆盖旧来源来解释已发生的失败。

`first_action` 观察原生首次 webSearch／MCP Item，然后关闭该任务新准入、调用原生 `turn/interrupt`；MCP 等待中断返回，不交付合成正文。自然结束抢先发生则单独记录。无检索结论必须等待自然完成。关闭边界是本机确认序号，不是假定的供应商瞬时撤销。

`full_turn` 继续执行合成流程，用于验证终态和 hosted 统计。正常完成仍不构成真实完整任务成功。`mcp-quota` 发送五个不同的正常只读请求，验证每任务四次上限；内部 Go 测试非零结束是预期拒绝，外层只在明确观察到目标诊断及正常退出时通过该场景。原生防重复保护保持开启。

输出包含原始 native events、独立 ledger/journal、`score-v2.json`、源锁、运行日志和正常退出 receipt。拒绝日志使用闭合原因码、准入面、原生绑定、当时计数和首要原因引用，不含任意异常正文或工具参数。晚到请求仍计入原任务。

## 只读重评

```bash
node scripts/feat175-q2-v2.mjs score /absolute/evidence.json /absolute/bound-review.json
```

可读 schema 1 的历史完整运行或 schema 2 的采样记录；不会修改输入或发送请求。审阅必须匹配原始文件 SHA、case、thread 与 turn。不提供审阅时保留 REVIEW_REQUIRED。首次动作、完整任务、执行终态、样本来源与评分器是否发请求是独立字段。先错后改不能覆盖首次错误，failed 也不能因为首次匹配而变成任务 PASS。

v1 与 v2 输出保留在不同文件中，历史 `live_execution_performed=false` 不会被回写或改成新的实际调用数。原先一次性批准的真实运行已经消费授权，不能用于 v2。真实供应商接线、自然选路、内容质量、实际子代理完整退出与产品分发另行资格验证。
