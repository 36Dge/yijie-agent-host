# 原生公开消息候选：Host 消费与验证

2026-11-05，Owner 已批准限定的显式消息适配候选。`contract-impact = semantic`。
Runtime 来源为 eac5680cc949c7e269bbccd265861fc8d58ac8f3，Contracts 来源为
3b94b32102e00e9041f43fa541d92a27d5d11445；仅本地候选，不推送或生产发布。

Host 不产生开场/进展正文、不拆分 assistant 字符串，也不实现新的生成循环。
普通用户对话在已校验的 KimiK31Candidate/ChatModels/RuntimePermissions 组合下
注入 `[yijie-public-message-v1]` 传输规则；原共享 v3 仍由 developer 单一所有者
提供，start/resume/模型和专家切换共用入口。简单对话可以直接 final，不强制
每轮三段；内部标题、output-schema draft 和原生 Skills 显式/隐式边界保持。

Runtime 的有效公开阶段可以来自模型显式声明。Host 仍直接投影原生 AgentMessage
字段，不把该阶段当作 Provider 原始 phase 存在的证明；原始缺失 phase 与声明
保留在 Runtime 模型历史，公开文本/阶段进入 Native6/SSE13 消费链。

## 实际完成的离线验证

- 固定真实 Runtime 的正常回环覆盖原生同响应多消息、end_turn=false、真实必要
  只读命令后继续、unknown、已有 answer-only 保护，以及新增声明驱动的无工具
  多响应、同响应多 Item、工具后续与直接 final。无工具三消息为同 turn 三次
  请求，工具仍可用，没有进入补答；直接 final 为一次请求。
- 流式 header 分段后，首个公开 delta 在 response.completed 前已到达 Host；
  started 就带有效阶段，控制声明不出现在公开增量或历史正文。
- 下一次模型请求保留完整原文与声明、Provider phase 仍缺失；thread/read 及
  正常关闭/重开保留公开正文与有效阶段，重开零额外模型请求。
- 原生正常 interrupt 保留已看到的公开消息，终态 interrupted，迟到的普通
  response 完成不触发另一请求。fixture 源用正常有限响应收尾；不宣称提供方
  必须确认 TCP 取消或停止计费。最初等待 request context 的测试源清理超时
  日志保留，未使用强杀；按源码边界修正其自身收尾后验证通过。
- 实际组装策略一次性、模型/专家切换、原生 Skills 显式/隐式、input-only draft
  隔离通过。38 项前端定向测试通过。
- 复用跨仓交换测试补入不同 ID 的 opening/interim，Host 原生投影 → Native
  SSE decoder/display buffer → SQLCipher 提交 → 正常重开 → Vue 阅读投影通过。
  此测试为正常协议样本，和 Runtime 测试分别取证，不冒充真实供应商生成。

真实模型是否稳定遵循声明及生成有意义说明尚待验证；本次没有新增真实模型
调用。之前每模型已用 2/24 用户轮，后续仅按既有剩余额度执行与修复直接相关的
能力验证。若候选能力失败则停止对应模型，不能自动追加无关诊断或重跑到绿。

数据、权限、工具选择、Native 保存格式及审批流程不改；不清库、不迁移、不包含
diff。当前 Runtime 全仓旧 FEAT-137 静态门禁及对应旧 Contracts 冻结 HEAD 测试
仍有既有不一致，原检查未改，不能据本候选定向通过宣称这些全仓检查已通过。
