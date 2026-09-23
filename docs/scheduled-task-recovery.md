# FEAT-155 第一阶段：只读恢复查询

本地候选，contract-impact=additive。源为Contracts独立scheduled-task-recovery v0.1.0；`api/scheduled-task-recovery.candidate.json`记录base/digest/generator，未形成包含本改动的Git commit/tag，不可用于发布。没有修改既有pin、bbolt schema 5或固定Runtime。

仅精确`YIJIE_ENV=local`和`YIJIE_LOCAL_PROFILE=demo_fast`注册：

- `GET /v1/tasks/{task_id}/agent-session-mapping`：同一只读事务读取task索引及会话。reserved表示尚无持久thread关联，bound只表示关联存在。
- `GET /v1/agent-sessions/{agent_session_id}/turn-operations/{operation_id}`：读取既有组合键记录，保留pending/accepted/uncertain；accepted仅返回原turn ID，不代表执行成功或结束。

两个查询沿既有owner-only bearer，不接受query/body中的scope参数；只返回必要ID/状态与可选当前响应Host实例。Trace tenant/user、cwd、输入摘要、正文、内部错误均不返回。Desktop消费者仍须先检查native authority、本地plan/conversation scope及受管Host关联，不声称Host提供独立多租户鉴权。

实例字段只描述responding Host；缺省即unknown，不代表历史generation，不可用来释放未确认执行的预约。404包含正常清理后的缺失；503表示事实不可读，两者不能变成自动重投依据。所有查询/错误no-store；不调用thread/start、turn/start、resume或审批，不刷新Trace、不改operation状态，不启动scheduler。

`make scheduled-recovery-check`验证同源与可再生；`make scheduled-recovery-test`只执行已审查的安全测试名单，使用临时Store正常写入/关闭/重开/清理以及进程内HTTP recorder，无真实Runtime/Provider。旧协议定向回归、静态检查及实际结果见元仓FEAT-155的06实施报告。全量make test含历史禁止fixture，不作为本阶段默认执行项。

回滚：停止使用新查询并回退本阶段Host源码即可，无存储降级。旧Host或缺失端点由未来Desktop保留unknown并阻断依赖它的自动投递。第二阶段计划存储、第三阶段执行、页面/唤醒/模型草案调用均未在此实施。

## FEAT-155 本地版本固化

本地消费来源固定为Contracts `54be9314dce5319b049dc0a236800fd1a1fdd7a1`，原生input-only来源固定为Runtime `fb79b1d53501ec90084b584af5fdbe221c7a25aa`。四份`api/*.candidate.json`通过同源sync生成`source_commit/source_lock_path`，从Git对象验证源lock及生成物；旧`base_commit`仍是生成比较基线。`release:false`、普通Store5和显式候选Store6保持。此前D4源码/Runtime产物字节保持，未因固定来源改变wire、审批或出站行为。
