# Security Policy

FEAT-155只读恢复仅在精确local/demo_fast注册，保持既有owner-only loopback bearer；不把可变Trace tenant/user视为资源授权，Desktop native须在消费时校验本地scope。返回当前Host实例不能作为历史执行generation或终止证明；unknown不重投。详细边界见[只读恢复](docs/scheduled-task-recovery.md)。

`yijie-agent-host` 不直接持有电商平台 token。所有高风险工具调用必须经过策略检查、用户审批和审计日志。

Runtime Baseline 2 可在内存中持有用户显式配置的 MiniMax 模型 API Key，并只通过 `MINIMAX_API_KEY` 传给受管 Runtime 子进程。推荐来源是 owner-only 原始 Key 文件；Host 拒绝符号链接、组/其他用户可读文件、过大内容和换行控制字符。Key 不写入 Runtime 配置、Host bbolt、事件、状态或日志。

Host 会在独立状态目录生成 256-bit 本机 API token，文件权限为 `0600`。会话和 SSE API 要求 bearer token；health/readiness/status 只返回脱敏诊断信息。Host 只监听 loopback，但 loopback 不是认证替代品。

该 bearer 与 MiniMax Key 都是 Desktop 自动管理的机器凭据，不是用户登录。local `demo_fast` 可以零用户
凭据直达业务页面，但不能删除 Host loopback bearer。Desktop 启动 Host 时还传入与真实 PPID 精确匹配的
parent PID；值不匹配即拒绝启动，父进程消失后 Host 自行关闭，避免残留 listener 阻断下次启动。

FEAT-129 Skill 管理只在显式 `YIJIE_ENV=local`、`YIJIE_LOCAL_PROFILE=demo_fast` 且 bundle/install 根同时通过规范绝对路径、不重叠、owner 与祖先不可替换检查时启用。Desktop 的零登录体验只表示它透明携带 owner-only bearer；Host 仍固定按 bearer `401`、capability `403`、请求/服务校验的顺序执行。安装源、manifest 与 archive 拒绝 group/other writable，受管状态使用 owner-only 权限；解包会拒绝路径穿越、绝对路径、反斜杠、符号链接、特殊文件、额外嵌套 `SKILL.md`、Unicode/大小写碰撞和超限内容，并通过同一受管根中的 staging/rollback 与目录 fsync 完成原子提交。Runtime 只注册 catalog 中 receipt 有效的具体 Skill 目录；清单外、损坏或外部注入的目录不注册。卸载只处理清单控制的受管副本，且先确认 Runtime 不可见，不删除 bundle source。

Baseline 2 固定 Runtime 为 `read-only`、`approvalPolicy=never`，不接 MCP 或命令审批。动态工具默认关闭；只有
`YIJIE_FEAT128_IMAGE_GENERATION_ENABLED=true` 且 local + MiniMax + multimodal v2 + v3 Artifact 条件同时成立时，
才注册单一 `generate_image` 反向调用，其余 Runtime 反向请求仍默认拒绝。模型参数不能提供参考图片：Host 只使用
当前轮经校验的单张 PNG/JPEG 附件。生成结果先校验 base64、MIME、尺寸和大小，再写入 encrypted Artifact spool；
图片 bytes、Key、prompt 和 provider 原始错误不进入 Runtime 响应、SSE、bbolt 或日志。

FEAT-155受限草案只接纳精确产物与当前受管generation的原生input-only证据；创建/恢复和每次turn校验thread/cwd、无额外指令、无工具及权限。Host提示词不能充当权限边界，退出后缓存证据不再有效。原生内部历史及文本Provider通道与被禁用的任务文件/工具网络区分；真实Provider资格另验。见[草案资格](docs/scheduled-draft-input-only.md)。


### FEAT-155 3C-3B3B 本地候选

B3B原生候选部署描述只选择Store6和受管scope目录，必须先核对精确Runtime产物；不是权限开关。草案mapping只读回执的当前responder不得冒充历史发送来源。显式resume与turn序列化，冷历史缺失保留unknown。 真实Provider与产品D4没有因本批装配而获得资格。

4B显式本地验收的草案start/resume复用原固定计数Provider通道，request/stream retries均为0；计数服务不可用不退回直连。只继承Provider地址与重试配置，不继承普通权限验收的auto_review.policy。input-only、never、精确产物、工作目录、当前generation和每轮权限回执保持，Store6和公共wire不变。单批计数上限来自已批准验收预算，不是产品运行授权或完整Provider/D4通过结论。

固定输出schema同时作为受限线程的显式baseInstructions传给原生start/resume；这是结构化输出指引，不是任务文件/skills/MCP继承或权限证明。旧developer历史可能保持原文，因此不依赖修改该历史来更新指引。Provider返回仍须由Desktop对完整原生事实严格校验，不能以提示词替代schema或放宽input-only限制。

FEAT-155日常入口经Owner明确授权后复用原数据目录及Store6兼容迁移。普通图片能力与受限草案共用同一Runtime；全局dynamic-tool协商不构成草案工具权限，草案每轮仍须取得原生空工具、无继承和input-only证据。普通图片回调只接受原image turn绑定。启动选择不是计划执行grant，不放宽固定产物、有限授权、审批或零重发边界。
