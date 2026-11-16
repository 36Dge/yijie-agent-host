# FEAT-175 S1／Q1 本机候选

本轮只实现修订3的独立契约消费与零费用原生协议接线。BuiltinWebEnabled默认false，没有新增App／环境自动激活入口；普通日常App和旧市场行为不切换。新的公共检索API、托管Key、账户委派、共享worker通信及前端展示属于S2／S3以后。

复用Manager已有thread/start/resume、模型目录、tool_search、Code Mode、原生per-tool auto/prompt、当前root／turn索引、回调响应及正常EOF。BuiltinWebTurnEvent只是本机I/O生命周期通知，不能作为外部服务身份。S3调用入口仍须依照修订3复核Service持久会话／专家绑定和服务端委派，不能把Q1本机fixture用作产品执行器。

三项工具由Contracts独立builtin-web-retrieval/1源生成；输入和成功输出仍来自web-retrieval/1 0.3.0，稳定名称保持。Host在其他配置组合后追加builtin配置与MiniMax namespace；真实明确离线政策最后生效。此候选要求已选专家有已解析的网页政策，正式ResolveExpert到policy的产品接线仍在S3。

默认普通只读自动执行；显式prompt只映射原生配置。S1的确认回调复用完整canonical生成类型、原opaque request ID及真实活跃turn；空turn拒绝，晚到确认不恢复操作。S1只支持单次答复，实际UI和记忆选项协商尚未激活，不复活旧Sorftime审批路径。

标题候选显式使用web_search=disabled及关闭yijie_web；input-only继续使用已有原生空工具集。两个模型名称只是本机协议profile，真实供应商资格和自然语言路由仍需Q2。

复现：在Host仓运行 `bash scripts/test-builtin-web.sh`。脚本校验同源候选，选定精确Runtime，再执行仅TestFEAT175的race检查。90秒确认与120秒模型等待是正常本机等待；并行执行这两个独立场景。市场共存检查启动已有canonical worker和空的临时library，正常EOF结束；共享worker内新增web transport本身仍需S3验收。

定向Rust暂停计时测试使用已核验canonical准备目录中的codex-rmcp-client原测试，固定名称筛选；不运行其余77项测试，不改Runtime源码、补丁或已审Runtime制品。

所有本机响应均明确为合成资料；不访问真实模型、Kimi REST、Keychain或日常数据库。完整阶段与失败记录见元仓FEAT-175/19和机器证据。
