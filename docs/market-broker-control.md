# FEAT-157 私有 worker owner 本地候选

> 本文保留上一阶段未接产品路径的历史实现和验证事实。2026-10-07 后续已接 Native 私管道、版本化提交/审批/结果、Store8 与单 worker OAuth；当前边界见 [产品接线记录](market-product-integration.md)，不要把下文历史“未装配”表述当成现状。

2026-10-07。Owner：段成威。整体 FEAT-157 仍按 breaking 管理；本次独立私有 family 为 additive，原 HTTP/SSE、Sorftime、模型 profile 字节与 Runtime 制品不变。现行 Runtime 启动仅新增移除 ambient `YIJIE_MARKET_RUNTIME_CAPABILITY`，不注入新市场能力。

## 已实现的边界

`internal/marketbroker` 直接持有 Connectors canonical Rust worker，一生只创建一个子进程。构建入口仍为 Connectors `scripts/build-market-worker.sh`；Host 校验候选 manifest 的固定 Codex source、candidate/external 标记、显式 `qualificationOnly=false`、普通文件、content-addressed 路径、大小及 SHA-256。该新增私有部署字段由 Connectors canonical 脚本提供；Host 新 owner 不接受没有此字段的旧状态制品或专用资格测试程序。它不接受 renderer 路径，不把本地 manifest 当成已签名发布证明。

新模式为 `--broker-control-stdio`。父子控制管道仅占 stdin/stdout，Go `os.Pipe` 保持 close-on-exec，`ExtraFiles` 为空；worker 子进程只接收 PATH、LANG 和 owner 生成的 256-bit 数据面能力，不继承 Host 的平台/模型环境。stderr 持续丢弃，不保存、转发原文。stdout 最大帧来自共享源 `MaxFrameBytes`（64 KiB），每次只有一个请求在途；响应的严格解码、请求 ID 和完整关联核验结束后才放行下一次请求。错误只返回闭集 safe code。

8 个控制方法均使用 Contracts 生成类型及校验：initialize、prepare、bind_turn、revoke、status、pending_call、decide_call、shutdown。prepare 校验生成的完整 snapshot/digest，并比对响应的 context 和快照；bind/status/revoke 比对原 capability association；pending/decision 比对真实 call identity/Turn/decision。Host 不从 metadata 生成调用，不重新计算 worker 参数摘要。worker 的 pending-call 登记与双决定准入由 Connectors 实现。

新会话的 `context.nativeThreadId` 必须显式为 null，表示尚未创建原生 thread，不能省略；先 prepare 再以完整 Gateway 配置执行 thread/start。Host 拿到真实 thread/Turn 后，通过一次 bind 冻结两个编号，响应的 `boundNativeThreadId` 与 `nativeTurnId` 必须都匹配。已有会话的 context 带真实 thread，绑定时必须相同。原 context/snapshot 不变，已绑定 prepare 重读可以返回当前 bound 观察，不再要求原 prepared 状态，也不能延长租期。包含 nullable 指针的生成 Go 关联结构按值深比较，避免把反序列化后的不同指针地址误判为 scope 变化。

5 秒控制请求超时或在途取消会关闭 stdin、永久停止准入，并返回结果未知；不自动重发、重启或换 generation。Close 先关闭准入和正常 EOF，再等待该子进程；超时返回 STOP_PENDING，保留 owner 和完成通知，后续 Close 可以继续等待。正常 shutdown 回执只表示停止准入，不表示外部操作完成；还必须观察退出。没有 Process.Kill、信号升级或替代可执行文件。

固定 mutation receipt 容量满时不能逐出旧回执换取一个新 revoke/shutdown。对此，Host 在收到经过同源校验和请求关联的 `capacity_exceeded` 后，只有这两个终止动作会在原串行 gate 内关闭 stdin、永久退役整个 generation，再把原 typed 错误返回调用方；调用方仍须 Close 等待正常退出或保留 STOP_PENDING。其他方法/错误没有此生命周期副作用，不重发、不补造回执，也不宣称外部结果已完成或回滚。

数据面随机值只保存在 owner 私有内存及目标 worker 环境，不进控制 DTO、日志、状态或存储。Owner 格式化诊断始终脱敏。`ScrubRuntimeCapability` 按完整变量名移除 ambient 旧值，已接入现有 `runtimeEnvironment`，不匹配相似前缀。**本轮没有把新值注入实际 Runtime**；后续正式装配仍须绑定同一 Runtime generation，并校验精确 managed shell exclude/set 策略及工具子进程隔离。普通 Runtime 不获得市场能力。

## 来源与产品门禁

唯一新 wire 源为 Contracts `compatibility/market-broker-control/wire.schema.json`，framing/动作/租期与容量语义为共址 `control.json`。借用的 SelectionRef、SelectionSnapshot/digest 仍使用原 market-connectors/market-selection 源，没有 shadow DTO。`api/market-broker-control.candidate.json` 记录实际 base commit、各源及生成投影的 SHA-256，mode=`local_worktree_candidate`、release=false；没有 fabricated release pin。

本包还没有接入 app/session 路由、Native 可信 grant、版本化提交或审批权威。构造一个格式正确的 scope/approval UUID 不赋予产品权限。所有 51 项真实服务仍未 qualified，当前 worker 对非空产品选择返回 not_qualified。新 owner 不改变 Desktop 的 selection submit/UI guard，不更改任何 installation readiness。

后续顺序是可信 Native 快照/权限投影 → 版本化 Host submit 的原回执优先与实际 Turn 绑定 → generation carrier 和完整配置核验 → 通用审批/结果投影 → 独立 OAuth/真实服务资格。accepted 原操作不能再次 prepare 或重发工具；pending/uncertain 先查原事实。

## 验证入口

```bash
make market-broker-test
YIJIE_MARKET_BROKER_TEST_MANIFEST=/absolute/path/to/yijie-connectors/bin/market-worker/current.json make market-broker-integration
make lint
```

普通单元覆盖关闭后不启动、预取消/排队取消、ambient 完整名清理与诊断脱敏，以及只有终止动作容量错误才退役。集成必须显式给实际 canonical manifest，缺少该参数的单独 Go 测试会跳过，Makefile 集成目标则拒绝运行；跳过不能算通过。

集成使用真实 Rust worker 与声明式合成 scope/thread/turn 数据，验证 initialize、空选集 prepare/原意图重读、bind、并发只读 status、未知 call 查询不建记录、revoke、撤销不复活、shutdown/EOF 与正常退出；另一个进程验证非空选集未资格并正常 EOF，第三个验证新会话 null 意图、双 ID 绑定及绑定后 prepare 重读。它不是实际 Native/Runtime Turn 资格，更不是真实账户/OAuth/平台工具 D4。

新增容量边界用例对 revoke/shutdown 各用一个实际 canonical worker：合法初始化及同一空选集 prepare 使用不同合法 mutation ID，共得到 1024 个真实回执，先只读确认 lease 仍 prepared，再发终止动作。断言仍返回 typed capacity_exceeded、后续 owner 永久关闭，并由正常 EOF 观察 exit0。每个已终止 owner 都不复用，也没有伪造可执行文件、强杀或故障注入。

2026-10-07 首版已运行两项真实 worker 集成并通过，两进程均观察到正常退出。初次运行因 canonical manifest 新增 `qualificationOnly:false` 被 Host 旧闭合 manifest reader 在 spawn 前拒绝；源适配后重跑通过，该失败保留在证据中。随后根据实际 Runtime 观测补新会话 prepare(null) 流程与第三个用例；最终源/制品与实跑次数以证据 JSON 为准。普通线程和轮次编号属于声明式合成控制数据，没有冒充实际 Runtime 事实。

上一轮产品 worker `685ce16cda2aceb92d50c9c0f39aaed2577662874448fe6b0c2d57e11fafa681` 的三项 owner 集成与 source-sync 通过。该制品包含实际 Runtime 资格发现的官方 rmcp `context.meta` 适配；Host 没有改成解析或信任 metadata 来授予控制权限。该轮结束累计十三个 worker 均正常退出，无强杀。独立只读审查核对新会话意图、真实双 ID 绑定、实际调用登记、双决定及单次消耗/撤销/到期边界；这不扩大真实 provider 资格。

随后合同审查发现容量满会阻止新的撤销/关闭，Host 只返回错误将留下租期内的旧能力。已按共享源补上述终止动作 EOF 规则。最终产品 worker `f36db7e719175d4a60b7f0133776704cfe5d30faaf930f8981f75dff4322f415` 的四个顶层 owner 集成用例通过（容量用例各验证 revoke/shutdown，共五个真实 child），六个顶层单元及 lint/source-sync/diff 检查通过。累计十八个 worker 均观察到正常退出；完整历史与最终来源摘要保留在证据 JSON。

最终 Clippy 将 Gateway 取消检查的两层 if 合并成等价 let-chain。只读复核通过反向替换精确重建原文件 SHA，确认没有其他源码差异；同一锁内的判断顺序和 consume/return 行为保持。标准重建后产品与专用资格二进制 SHA 均不变，故未重跑测试、未新增 child。证据另存改写前后源摘要及同制品衔接，当前源摘要已更新。

未进行进程强杀、权限破坏、攻击注入或可执行文件替换。超时后的真实故障进程行为未通过故障注入验收；当前证据是普通生命周期及代码的有界等待/保留所有权设计。最终实跑及产物/源摘要见 `market-broker-verification.json`。
