# Security Policy

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
