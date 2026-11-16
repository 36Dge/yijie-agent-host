# FEAT-175 Q2 原生执行入口

验证工具已接线，产品开关未激活。首轮一次性获准的真实Q2已在本机限额处停批：模型HTTP 5、hosted 3、供应商REST 0，保守费用2.487164元，未核对账单。授权已消费，不能自动续跑。最新结果见[首轮报告](../../yijie/docs/features/FEAT-175-builtin-web-retrieval/24-q2-first-live-result.md)。详细边界、复审及证据见[元仓记录](../../yijie/docs/features/FEAT-175-builtin-web-retrieval/23-q2-meter-and-native-execution.md)。

`contract-impact=none`：独立、不可发布的 QA 入口，产品／公共协议／Runtime 源码不变。所有模型执行继续走固定 Runtime 和既有 Host Manager，没有增加 planner、选路器、工具审批权威或 Token 调度器。

## 零费用复现

```bash
bash scripts/check-builtin-web-q2.sh
python3 scripts/feat175_q2_run.py local --output /absolute/new/evidence-dir
python3 scripts/feat175_q2_run.py local --local-scenario normal-stop --output /absolute/another/new/evidence-dir
```

输出目录必须不存在。第一条运行限定的离线与 loopback 检查，不启动 Runtime；后两条运行固定真实 Runtime，对接本机合成模型／MCP，不读取或接受供应商 Key。normal-stop 是单任务正常中断验证，预期内部 Go 驱动非零退出，外层以原生 interrupted、无第二请求及正常退出判断停止链。

证据包含逐字原生事件、请求摘要／工具表摘要、usage／hosted 清单、独立 ledger、冻结 source-lock、score 及正常退出回执。只使用本轮公开问题和合成内容；供应商 Key 不进入 Runtime 或输出配置。整个目录为本机私有，不直接作为公共报告发布。

## 真实模式门禁

`live` 必须有当前用户明确授权，不沿用 FEAT-162/164/170/171/172/173 或以前线程的额度。现有 `authorization-not-granted.json` 会在读取凭据、创建运行目录或启动服务前拒绝。

审批材料为[未授权范围表](../internal/codex/testdata/feat175-q2/authorization-not-granted.json)。建议范围是 12 任务、48 个模型 HTTP、每模型 24／每任务 4、本机 MCP 每任务 4、hosted 观察停止阈值 24、真实供应商 REST 0、20 元**软停止阈值**。在途调用可能超额，必须明确同意；它不是硬预算保障。只接受硬上限则不运行此真实入口。

实际授权对象使用闭合字段集：`feature=FEAT-175`、`purpose=Q2-route-feasibility`、`status=approved`、新的 UUID `authorization_id`、`package_sha256`、`source_lock_sha256`、规范 JSON 的 `pricing_sha256`、`approved_by`、实际用户答复来源 `approval_source`、带时区的 `approved_at`／`expires_at`、两个精确 `models`、`tasks`、`model_http_requests`、`per_model_http_requests`、`per_task_http_requests`、`local_mcp_per_task`、`hosted_observation_stop_count`、`supplier_rest_calls=0`、`money_mode=soft_stop_after_observation`、`allow_inflight_overshoot=true`、`soft_stop_micro_cny`。额度不能超过建议范围，有效期不能超过 24 小时，价格核对不超过 7 天。

授权只可使用一次；启动中途失败也保留消耗记录，不以新的输出目录重置。正数授权签定后，才可以运行：

```bash
python3 scripts/feat175_q2_run.py live \
  --authorization /absolute/current-task-approved.json \
  --kimi-key-file /absolute/owner-only-kimi-key \
  --minimax-key-file /absolute/owner-only-minimax-key \
  --output /absolute/new/real-evidence-dir
```

本页命令不是费用授权；不会自动生成 approved 文件。原生请求和流重试均为 0，未知费用／来源变化／不完整 hosted 对账停止继续发出请求。Ctrl-C 只通知本次计量器停发；驱动使用原生 interrupt 和正常退出，不向进程发送终止／强杀信号。STOP_PENDING 时保留现场并报告。

首次 Q2 真实协议或身份／usage 不符合预期时立即停批，保留所有已发费用和原始样本。合成 MCP 的补充搜索与真实产品重复率分开解释；人工 query 审阅前不判 Q2 通过，更不替代 Q3/Q4 与 B01。
