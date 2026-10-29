# FEAT-162 Host Basic/Pro 本地候选

2026-10-29。Host 从生成 Broker 常量发送 prepare 的成对版本0.3.0/60000，并使用相同60000/1000配置 Runtime MCP tool_timeout_sec。prepare 成功先于 startTurnV2；Broker以收到prepare的自身单调时刻+60s固定保守下界，早于真实MCP调用开始+60s，不修改固定Runtime。

新审批投影保持 expiresAtUnixMs=D，另携 A/D双字段。按钮准入、待批观察和计时器使用A；owner决定正在处理或已写入resolve的调用在A之后继续等待至D或正常完成，不把及时批准当过期。旧审批不携带双字段，保留原expires语义。审批权威、identity/revision、选集和逐次消费规则不变。

本轮使用Contracts显式 local sync，不改写既有不可变来源批准。`api/feat162.local-candidate.json`记录真实基线和生成摘要，source_commit:null / activation_qualified:false。源契约先发布、消费者再固定并完成真实链路后才能激活；新Host对旧严格Broker失败关闭。无数据迁移、Runtime补丁、模型HTTP或付费调用。

定向race测试、go vet和代码复审见元仓FEAT-162/24及25号。真实IPC投递时延、同源App与新历史重开未执行；prepare下界可能使模型首轮超过约23秒后安全拒绝新模式，需要真实时延验收，不能宣传固定可批准窗口。
