# FEAT-162 Host Basic/Pro 本地候选

2026-10-29。Host 从生成 Broker 常量发送 prepare 的成对版本0.3.0/60000，并使用相同60000/1000配置 Runtime MCP tool_timeout_sec。prepare 成功先于 startTurnV2；Broker以收到prepare的自身单调时刻+60s固定保守下界，早于真实MCP调用开始+60s，不修改固定Runtime。

新审批投影保持 expiresAtUnixMs=D，另携 A/D双字段。按钮准入、待批观察和计时器使用A；owner决定正在处理或已写入resolve的调用在A之后继续等待至D或正常完成，不把及时批准当过期。旧审批不携带双字段，保留原expires语义。审批权威、identity/revision、选集和逐次消费规则不变。

本轮使用Contracts显式 local sync，不改写既有不可变来源批准。`api/feat162.local-candidate.json`记录真实基线和生成摘要，source_commit:null / activation_qualified:false。源契约先发布、消费者再固定并完成真实链路后才能激活；新Host对旧严格Broker失败关闭。无数据迁移、Runtime补丁、模型HTTP或付费调用。

定向race测试、go vet和代码复审见元仓FEAT-162/24及25号。真实IPC投递时延、同源App与新历史重开未执行；prepare下界可能使模型首轮超过约23秒后安全拒绝新模式，需要真实时延验收，不能宣传固定可批准窗口。

## 2026-10-30 条件路由与产物收集

本轮 contract-impact=semantic；公共 DTO/工具 schema 不变。K3.1 当前授权 Services 含 kimi-web、snapshot 引用一致并通过 worker prepare 后，Host 在 thread/start 与 resume 的正常产品配置设置 `web_search=disabled`。这是固定 r5 的原生配置，关闭供应商预检索；三 MCP 工具和 auto choice 保持。未选、清空选择、切模型时不写覆盖，原生 resume 重新加载 cwd 配置；专家最终覆盖仍优先。失效准备返回错误，不改走 native search。

固定 Runtime + 正规第 09 代 worker 的 6 个 loopback 请求验证选中/恢复/清空/重新选择/切模型/返回 Kimi，以及三工具发现；普通、标题、草稿、专家和本地读取延续另有 6 个 loopback 回归。全部使用合成 Key，零供应商调用。旧第 08 代 E1 FAIL 和 D4 未完成保持。

`scripts/build-artifact-adapter.sh` 只收集本次成功 Cargo JSON 报告的匹配 bin executable，不再从硬编码旧 target 复制。收集摘要和 build.json 同时落在目标 bin；新 primary descriptor v2 的 packaged receipt 校验并覆盖该记录。`YIJIE_ARTIFACT_BUILD_DIR` 仅控制该组件中间缓存，最终 `CARGO_TARGET_DIR` 仍由 canonical generation 指定；失败不回退旧产物。本轮只验证选择/复制规则，未重编 adapter 或 App。
