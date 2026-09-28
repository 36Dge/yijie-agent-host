# 日常原生对话推理

contract-impact = semantic。Owner：段成威。用户要求日常开启推理并输出至前端。

审计：日常 Desktop 未选择 effort，Host 将其映射为 none；受管 Runtime 同样配置 none，原生 raw 投影关闭。因此只有回答的轮次没有可展开过程，UI 没有丢掉已收到的推理。

部署配置权威在本仓：新增 `YIJIE_NATIVE_REASONING_ENABLED=true`，只接受显式 local + demo_fast、MiniMax、RuntimePermissions、独立 Host/Runtime Home；不允许 fake provider 或已退役审批。Desktop 日常原生 sidecar 显式开启。此模式将新轮次实际 effort 固定为 high，并开启已有原生 reasoning 投影；不启用旧 v2/v4/v5/v6 路由，不借用 FEAT-134 的隔离入口，不改变图片、工具、审批权限。

复用现有 high/raw 受管配置字节和固定 Runtime 的 stable reasoning Item/delta。公共 HTTP/SSE 形状、枚举、版本、数据库均不新增，contracts 新 schema/generator 为 N/A；现有源 pin 保留。新部署模式是对正常运行的显式配置，不把 API 的默认 none 扩大为全局 high。

幂等：输入摘要仍使用原请求归一化 effort（包括缺省 none），仅在新操作实际派发时选择 high。既有 accepted operation 重试返回原 Turn；pending/uncertain 保持原有拒绝规则，不重发、不改写旧记录。

接收：沿既有 native v7 事件及索引展示 summary/content；不由正文猜 phase，不伪造模型未返回的过程。Desktop 当前 reader 已支持两种内容及整轮展开；Host 不在日志或 bbolt 保存正文。历史未产生的推理不能补出。

顺序：Host 实现与测试 → 固化本地 Host 来源并更新 Desktop candidate pin → canonical 构建与正常重启 → 一次普通真实请求验证 Runtime/Host/前端。只使用标准构建产物，不改 Runtime 二进制。回滚先关闭日常 native 推理环境，再回滚 Host/Desktop；旧 managed high/raw 字节已在旧模板白名单，原生 trust、存储版本和请求身份不变。

验证结果、实际源码来源及未执行项在交付时补充；在真实请求产生推理且前端可展开之前，不宣称端到端通过。
