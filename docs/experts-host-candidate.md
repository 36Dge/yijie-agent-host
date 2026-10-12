# FEAT-158 Host 本地候选适配记录

当前 Git 交付（2026-10-13）：Contracts `experts/1@0.1.0` 完整提交 `e35ae9a0711014e4774a1347413e9a343ce7ab2c`，source-lock SHA-256 `a5056217e4d54310d451ba53c64504ef010e2c616108cf75b606b0de0e4d9033`；Skills 完整提交 `d7a850c9d5c5f8e52b8ab8f631800f671acc60dc`，内容仍为 0.1.3 / bundle 0.4.3。干净源码通过原 canonical 打包器重建，manifest SHA-256 `4fec45edbe0995d0fa809c11c1fb37189176c6e9fc7fa7e8cce6a31cf1eea201`、authority SHA-256 `122486fe96f51a35a47c747d7d89ab70375b057fa39a87bcb22a25dfcb0a7860`。39 项归档及所有 expert metadata 与 phase12 逐字节一致，仅目录来源由工作树改为实际 Git commit；旧安装与会话 ExpertRef 无变化。唯一编译 pin 同步该新 manifest。日常接入已由元仓 phase12 记录，本文其余按阶段保留的未执行说明均为历史。产品继续 local-development / release=false，公开源码授权不等于桌面发行或生产资格。

**历史内容候选（2026-10-11 阶段 6 逻辑收口）**：唯一编译 pin 已更新至 `jd-ops-expert@0.1.3`，来源 `yijie-skills/dist/skills-experts-candidate-v013`，bundle `0.4.3`；manifest SHA-256 `46e3e5b9e101c252a4dcb0bc9d27c1da024ea6457be43e5be5736ccf047c6b58`，authority SHA-256 `351327f5b93b60323fbe7833454c6e53f96f0bacef7da500c9db9c24a6311b53`。所有当前候选入口消费同一已审内容权威；不存在 renderer/env 自选 hash 或第二套绕过 pin 的安装器。旧实际安装、旧数据库及其引用保留，未进行就地替换。下方阶段 1/2 与 0.1.1/0.1.2 版本、摘要、待办和运行状态是历史记录；本次仅本地逻辑验证，不声明 0.1.3 已通过真实模型验收。

状态：2026-10-11，本轮阶段 1 资格证据与阶段 2 Host/Native 资源候选代码。**已有默认关闭的显式本地装配入口，本轮没有启动用户 App，也不是完整 FEAT-158 或真实模型验收通过。** 新入口仅在 `YIJIE_EXPERTS_ENABLED=true` 且既有 Native owned local/demo_fast、市场、模型、权限及受信资源前置成立时传 `WithExpertStorage` / `WithExpertResources`；普通默认不开，不新增 HTTP endpoint。SQLCipher 是产品绑定和 outbox 的真相源，本文 bbolt 状态仅为 Host 适配回执。

## 权威与影响

`contract-impact = breaking`：新增独立 `experts/1` private family、Skills v3 资源分支及专家上下文持久化/重放边界。通过 `node ../yijie-contracts/scripts/sync-experts.mjs --consumer=agent-host` 消费生成类型和精确本地候选；`api/experts.candidate.json` 固定工作树 source digest，不表示发布来源。最终 `compatibility/experts/source.lock.json` SHA-256 为 `762fa86dedad71afef0c2a0e37d7945202ca961cdddb6e7c188aaa6c7d657259`。原 Contracts 0.5.1 lock 与旧 wire 未被扩成可忽略专家字段的请求。

Runtime 无修改。资格使用现有 `api/runtime-chat-models.candidate.json` 指向的真实 binary：SHA-256 `aad49041bd7d34c853fb55c274711e3cc810725720d2c097469822d310fb02f9`，manifest `8b86a1a661f50beda1e0a0c96f81ddbdf959a9a07c2e756cc4b9b0eabc053b27`。测试没有替换可执行文件、修改 Runtime 源码或启停用户 App。

Skills 内容来自 `yijie-skills/dist/skills-experts-candidate`，manifest SHA-256 `156fa3da100f82985087637ea3e94138d1f6ff813fedc8a4275b0dcad591e355`、authority SHA-256 `8cefd1e0fc0c80739018b37293e44550dd2b0c6a0fdb96f7ea08b28e7e356d3b`。这只是 local-development 本地使用授权候选，不继承旧 38 项资产的第三方再分发批准。Host 构造必须提供可信资产 authority 中的 exact manifest digest；renderer/HTTP 不能决定该 pin。

## 已有候选行为

- 沿用 Skills manifest、archive、安装事务及 receipt。v3 `expert_resources` 验证本地授权字段与 exact manifest pin；不进入 Runtime extra roots 或 `skills/config` 投影。按版本、归档、role、knowledge manifest 摘要解析资源；role 和短索引每项上限 32 KiB，知识单文件上限 8 MiB；知识正文不内联到 base。不增加 RAG、插件引擎或权限。
- `composeExpertBaseInstructions` 是唯一组合点。在现有 start/resume 完成普通 model/permission/market 配置后，以完整普通 base 追加短角色和本地索引。明确 none 恢复完整普通 base；不依赖 null 清除 developerInstructions，不撤销既有历史。
- 使用原 Native owned JSONL pipe、market hello/scope、grant 生命周期、撤销、epoch、单次消费和 submit 主流程。`expert_submit` 与旧 bare submit 分别只接受自身 family grant；原始 typed wrapper 参与完整 intent 摘要。读取专家出站回执保留原 snapshot，不能用旧无专家 receipt 推断恢复。
- Apply、market submit、模型切换共享 `modelOperationMu`，Runtime 再复用原 `threadOperation`。Apply 校验 idle、审批、pending turn/market/apply、model revision；先持久 pending receipt，再正常 clear 旧市场配置并同 ID unsubscribe/resume。每个后续 submit 都重新走原 `PrepareMarket`。Apply 的 marketSnapshot 是组合意图，**不重新授予旧市场租约**。
- 模型切换重新解析已有 confirmed expert；专家 none 的后续轮显式投送普通 base。失败/未知不创建替代 thread、不自动重发、不在未知状态推进 binding revision。成功 receipt revision 为 expected+1，其余为 expected，receipt expert 始终表示原请求。
- `WithExpertStorage` 才启用 bbolt schema 10；依次复用 schema 6/7/8/9 依赖桶并正常升级 9→10，新增资源适配操作桶。旧 writer 拒绝 schema 10，不降级。新版普通 market profile 显式带 `WithExpertStorageReader`，只校验已存在 9/10，不创建专家桶、不升级、不启用专家 writer；关闭 gate 后可继续普通会话并读取原专家回执，不能以 bare submit 或普通模型重绑丢弃专家上下文。适配绑定存 ref/revision/pending operation；market operation v2 存冻结 snapshot；不存 role、知识正文、聊天正文或可复用 grant。读取历史 receipt 保留原 HostInstanceId，读取授权使用当前 hello；消费者需核对原出站身份。

## 可复现的固定 Runtime 命令

在 Host 仓执行；四次均仅新建私有测试目录。每次运行先核对上述 binary/manifest pin。历史报告已有 evidence 归档；不要为刷新日期无目的重复执行。

```bash
python3 scripts/verify-expert-runtime-no-turn.py \
  --binary ../yijie-codex/.yijie/build/chat-models-stream-args/aarch64-apple-darwin/codex \
  --manifest ../yijie-codex/.yijie/build/chat-models-stream-args/aarch64-apple-darwin/runtime-manifest.json \
  --output .local/feat158-runtime-no-turn/report.json

python3 scripts/verify-expert-runtime-no-turn.py \
  --binary ../yijie-codex/.yijie/build/chat-models-stream-args/aarch64-apple-darwin/codex \
  --manifest ../yijie-codex/.yijie/build/chat-models-stream-args/aarch64-apple-darwin/runtime-manifest.json \
  --output .local/feat158-runtime-local-responses/report.json --local-responses

python3 scripts/verify-expert-runtime-no-turn.py \
  --binary ../yijie-codex/.yijie/build/chat-models-stream-args/aarch64-apple-darwin/codex \
  --manifest ../yijie-codex/.yijie/build/chat-models-stream-args/aarch64-apple-darwin/runtime-manifest.json \
  --output .local/feat158-runtime-reference/report.json --local-responses --read-reference

python3 scripts/verify-expert-runtime-no-turn.py \
  --binary ../yijie-codex/.yijie/build/chat-models-stream-args/aarch64-apple-darwin/codex \
  --manifest ../yijie-codex/.yijie/build/chat-models-stream-args/aarch64-apple-darwin/runtime-manifest.json \
  --output .local/feat158-runtime-reopen/report.json --local-responses --read-reference --reopen
```

普通 synthetic 指令分支是 3 turn / 3 loopback 请求；资源读取分支是 4 turn / 5 请求；正常重开分支是 5 turn / 6 请求。每次真实付费调用为 0。脚本关闭 stdin 等待正常 EOF；若超时保留进程与目录并报告，不发送 kill/terminate 信号。只读此次新建 metadata，不读取用户历史。

## 正常验证证据

只运行明确列出的普通测试；没有运行历史攻击归档、权限破坏、强杀或 binary 替换 fixture。

1. `scripts/verify-expert-runtime-no-turn.py` 的 no-turn 分支：initialize/start 成功，但零 turn 的空 thread 在正常 unsubscribe 后无 rollout，resume 返回 `no rollout found`。0 本地 HTTP / 0 turn，正常 EOF exit 0。**有效限制，不是该路径资格 PASS**；草稿必须仅在 Native 持绑定意图。
2. 同脚本 `--local-responses --read-reference --reopen`：真实固定 Runtime 对 benign localhost synthetic Responses 共 5 turn / 6 请求，观察普通→专家→正常 EOF→同 ID 重开专家→解除后的 Provider request instructions。真实原生工具在 read-only/never/networkAccess=false 下读取自建普通 reference 文件（workspace 外），未增工具或扩 sandbox；正常 EOF 两次 exit 0。**仅 Runtime 协议资格；synthetic provider 不是模型效果**。报告在本机 ignored `.local/feat158-runtime-reopen/report.json`。
3. `go test ./internal/app ./internal/session ./internal/skills ./internal/codex -run '^TestFEAT158' -count=1`：提供下述真实候选环境变量时 18 项正常单元/资源测试通过；其中 3 项实际候选测试在未提供目录时显式 skip。覆盖私有生成 family/预算、完整 base/显式解除、非全局资源安装、普通 Skill 投影不变、首次专家提交/原回执重放、同 ID 模型切换保留专家、解除后的下一轮、普通会话显式绑定、已完成模型操作在后续专家 pending 时优先返回原回执且新操作预检不污染状态、pending Apply 与 cleanup 的事务互斥、默认关闭/真实候选配置门禁、资源 prepare/read/remove 引用保护、正常 store reopen 与旧 reader 拒绝。
4. 实际候选资源测试（单独执行）通过：`YIJIE_EXPERT_TEST_BUNDLE=<上述候选目录> YIJIE_EXPERT_TEST_MANIFEST_SHA256=<上述manifest摘要> go test ./internal/skills -run '^TestFEAT158ActualCandidateManagedResources$' -count=1 -v`。只安装到 `t.TempDir`，真实 `jd-ops-expert@0.1.0` role 1730 bytes，短索引 4420 bytes，0 全局投影；未运行该实际资源的真实模型读取。
5. 两项既有正常回归 `TestFEAT157MarketNewTurnAcceptedReplayContinuationAndModelClear` / `TestFEAT157MarketExplicitAdoptionAndReadAfterRevocation` 通过。`go build ./...` 全部 Host 包静态编译通过，`go test ./cmd/desktop-host -run '^$' -count=1` 仅编译通过；`git diff --check` 通过。

完整精确命令（Host 仓执行，18 项分别为 app 4、session 8、skills 4、codex 2；另两项既有回归）：

```bash
YIJIE_EXPERT_TEST_BUNDLE=/Users/jack/Downloads/Personal_Info/CrossBSD/yijie-skills/dist/skills-experts-candidate \
YIJIE_EXPERT_TEST_MANIFEST_SHA256=156fa3da100f82985087637ea3e94138d1f6ff813fedc8a4275b0dcad591e355 \
  go test ./internal/app ./internal/session ./internal/skills ./internal/codex -run '^TestFEAT158' -count=1
go test ./internal/session -run '^TestFEAT157Market(NewTurnAcceptedReplayContinuationAndModelClear|ExplicitAdoptionAndReadAfterRevocation)$' -count=1
go build ./...
go vet ./internal/app ./internal/session ./internal/skills ./internal/codex ./cmd/desktop-host
node ../yijie-contracts/scripts/sync-experts.mjs --consumer=agent-host --check
git diff --check
```

上述命令均通过。没有执行历史全套测试；静态编译、vet、生成校验不代表真实 Provider、跨层 UI 或权限矩阵验收。

## 阶段 2 资源与启动装配补充

新增的 4 个资源方法 `expert_status` / `expert_resources_prepare` / `expert_resources_remove` / `expert_resources_operation_read`，连同原 5 个执行方法 `expert_apply_binding` / `expert_operation_read` / `expert_grant_register` / `expert_submit` / `expert_submission_read`，共 9 个源定义 private methods，仍走同一 Native owned pipe。资源 adapter 在完整 scope/action/selection 意图落库后调用原 Skills core；读取原 journal，不通过重复 mutation 或当前目录状态推断成功。status 只返回当前 scope 每专家最新原操作，便于重开后找回已到达 Host 的 operation ID；Host 未收到的 Native 请求仍必须由 Native 出站意图恢复。

Host 仅接受编译期 `skills.ReviewedExpertCatalogDigest`，不接受额外环境 hash。`loadExpertProfile` 在 store writer 打开前验证该 manifest 与独立 `skill-packages-experts` 资源槽；真正安装继续使用原 `skills/installed`，没有第二套安装器。开启后配置依旧不会触发模型 turn。普通 38 项 Skill 的既有资源槽、HTTP family 和默认启动路径保留。新兼容 reader 允许维持原 HostHome 映射；实际 fresh-run 验证仍必须整体隔离 app_data/Host/Runtime，不能拿日常数据库做资格测试。资源 mutation 共用 20 秒正文预算和最多 5 秒正常 rollback/reconcile，落在源定义的 25 秒 Host 方法与 30 秒 Native 响应预算内，超时保持原未知操作。

Desktop 的既有 `scripts/chat-model-build-candidate.mjs` 在精确 local/demo_fast + model/permission/market/expert gate 下消费独立 `contracts/experts-host-build.candidate.json`。本次经源码审查、正常测试、canonical consumer/resource checks 后显式冻结当前 Host `cmd/internal/api/go.mod/go.sum` 全部构建输入；新文件 SHA-256 `4b121a5fe13e2b584760e2f578b72ace5a19d427977782f2492561fff0473832`。它绑定专家、资源、既有模型/定时/市场 families 和权限 lock，`release=false`。原 FEAT-156 构建 pin 未改。默认启动仅验证，不自动刷新摘要；Host 构建输入再变更须重新审查和显式冻结。原 `verifyLocalPermissionCandidate` 完整 committed FEAT-152 权威链也已通过，不增加绕过旧权限契约的路径。

启动静态核查显示：Host 仅 initialize 固定 Runtime 和本机 broker、读取本地 config；固定模型目录使用 Runtime `StaticModelsManager`。Skills snapshot/prepare 只读本地包、更新原受管 receipt 并调用原 `skills/list`/config 方法；专家包不投影全局 Skill，也不发起 turn 或 Provider probe/auth。此结论限首次全新数据目录、没有既有计划、没有点击发送或连接器授权/探测的场景；不是网络流量计量。现有 Provider key 仍为只读共享配置输入，不应描述为凭据隔离。

新增实际候选正常链使用真实 zip + `t.TempDir` + 普通内存 Runtime API：prepare、core 原回执读取、待提交 grant/confirmed binding 禁止删除、正常解除后 remove、旧 prepare 回执重放不重装、status 读取 latest 原 ID、正常 bbolt 重开均通过。没有模型、MCP、可执行文件替换或权限故障。Native 两项精确 Skills 测试也通过：39 项目录中普通 UI 保留 38 项、专家安全视图不含 hash/path、可信 ref 摘要一致；候选槽与普通槽共享原 managed root。只执行这两项，没有运行整个历史 `skills::` 测试集合。

## 下一阶段前仍需闭合

- Desktop source 接线正在同轮推进；本记录不代替 SQLCipher binding/outbox 同事务冻结、8 commands 完整 dispatch/UI 与跨层 fresh-run 验收。`ExpertsEnabled` 只判 adapter 前置条件，不是产品 readiness / D4 PASS。
- unknown/pending Apply 持久阻塞，**尚无人工或正常恢复出口**；不得通过重发新 operation、清空 DB 或强退恢复。后续需源定义状态确认和解除流程。
- 原 Skill HTTP 继续禁止专家替换、卸载与禁用；owned private prepare/remove 已在共享模型锁下检查 Host live binding、pending apply/submission、registered/in-flight grant 与未知资源操作，并复用原安装/卸载 journal。Native 还必须持共享 admission lock 检查 retained SQLCipher 引用，不能把 Host live 检查当作完整历史引用判定；未实现旧版本并存。
- 实际安装知识在受支持的权限模式、macOS sandbox/分发状态下可读性、cold resume 与权限/市场联合切换仍需跨层资格。现有 fixed Runtime read-only synthetic 观察不能推广为 ask/auto/full 与所有安装目录均 PASS。
- 正常重开保存了适配 ref/snapshot；真实 Provider、真实模型角色一致性、检索质量、真实市场/MCP 联合场景均 NOT RUN。未进行破坏性异常验收；因此未声称相关恢复保证。
- 发布前必须形成 Contracts/Skills/Host 可解析不可变来源并完成 Desktop gate。部署采用 source→generated consumers→显式隔离候选→资格→受控启用；回滚保留 schema 10 reader、内容和未知回执，停止新增专家写入。当前新版的 feature-off reader 保留 9/10 读取，只有显式 writer 升级；旧二进制不能打开 schema 10；不降级数据库，不覆盖既有 Runtime binary，不自动转换专家 outbox 为普通请求。

## 2026-10-11 Run 4：真实首发的禁用 MCP 配置修复

上文阶段 2 待办为历史时点；恢复出口等后续实现以元仓 FEAT-158 的 `07-implementation-progress.md` 为准。本轮真实产品首发暴露了此前分层测试未覆盖的配置问题：Native 已冻结专家及附件、公共 task 已绑定、Host 已保存 reserved session，但尚无 Runtime thread/turn。原提交维持 uncertain，没有重发或改库。

根因是 `marketDisabledServers` 给全新 thread 注入 `mcp_servers.yijie_market={enabled:false}`。固定 Runtime 的 MCP 配置解析在检查 enabled 之前要求合法 command/url transport，因而拒绝该新条目。修复保留继承条目的完整 transport 后禁用；不存在受管条目时使用当前真实 owned loopback gateway URL，保持 `enabled=false`，不携带 capability 路径或凭据，不启动 MCP 连接。使用原 native config 机制，不新增管道、授权或 transport DTO。

`contract-impact=semantic`：修复 Host→固定 Runtime 的既有配置映射，JSON/wire/schema 与存储格式不变；上游依据为 `yijie-codex/codex-rs/config/src/mcp_types.rs` 的 `RawMcpServerConfig→McpServerConfig` 校验。未修改 Runtime 或任何 binary/pin。

实际验证：

- `go test ./internal/codex -run '^TestFEAT158(DisabledMarketConfigurationKeepsValidTransports|ExpertInstructions)' -count=1`：3 项正常测试通过。
- 向 `YIJIE_CODEX_INTEGRATION_BINARY` / `YIJIE_CODEX_INTEGRATION_MANIFEST` 指定当前 canonical `chat-models-stream-args/aarch64-apple-darwin` 产物后运行 `go test ./internal/codex -run '^TestFEAT158PinnedRuntimeStartsExpertWithDisabledMarket$' -count=1 -v`：通过。复用真实 Manager 和固定 Runtime，Kimi/MiniMax 两模型各覆盖空及继承 MCP，4 次真实 thread/start 均获确认；0 turn、0 Provider/MCP HTTP 请求，正常 Shutdown。使用新的私有临时 Runtime Home，没有读取或恢复产品原 attempt。
- `go vet ./internal/codex`、`git diff --check`：通过。

本项只证明首发配置可被实际 Runtime 接受，不代表用户消息、内容质量或完整产品验收通过。原 Run 4 unknown 回执仍保留；修复后的产品 fresh run 由主验收流程独立记录。新的 Host 构建输入需要再次显式冻结后才能打包，不自动更新候选 pin。

## 2026-10-11 Run 5：本地参考专家禁用内置网页搜索

真实产品验收发现用户明确要求不联网时仍发生 native `web_search_call`。本期专家范围是本地参考咨询，因此在已解析的非空专家 context 最终组合点加入原生 `config.web_search="disabled"`，同时覆盖 thread/start、每轮 thread/resume、模型切换及正常重开。普通 context 和显式 none 不添加此 override；原生正常 unsubscribe/resume 从原 cwd 配置层重新加载普通值。没有把普通值硬编码为 cached，也没有改变普通请求、模型、权限或市场选择。

`contract-impact=semantic`，复用固定 Runtime 已支持的配置键，不新增 wire/schema、grant 或 Runtime 修改。权威源码：`config/src/config_toml.rs:431` 定义 mode；`core/src/tools/hosted_spec.rs:19` 在 disabled 时不生成 hosted web tool；`core/src/tools/spec_plan.rs:998` 同时排除 standalone web.run；`core/src/config/mod.rs:2813` 保留 full 模式下显式 disabled；`app-server/src/request_processors/thread_processor.rs:2804` 在正常 resume 从 cwd 层重新加载配置。权限与工具网络的边界保持原有规则，本项只证明内置网页搜索被移除，不宣称所有 Native 工具网络都已被禁用。

使用当前 canonical `chat-models-stream-args/aarch64-apple-darwin` binary/manifest 的正常资格命令：

```sh
YIJIE_CODEX_INTEGRATION_BINARY=/Users/jack/Downloads/Personal_Info/CrossBSD/yijie-codex/.yijie/build/chat-models-stream-args/aarch64-apple-darwin/codex \
YIJIE_CODEX_INTEGRATION_MANIFEST=/Users/jack/Downloads/Personal_Info/CrossBSD/yijie-codex/.yijie/build/chat-models-stream-args/aarch64-apple-darwin/runtime-manifest.json \
  go test ./internal/codex -run '^TestFEAT158PinnedRuntimeExpertWebSearchLifecycle$' -count=1 -v
```

PASS：6 次正常合成 turn、6 个 loopback Responses 请求、0 真实 Provider 调用、0 工具调用；两个 Runtime 实例均正常 Shutdown。每个请求实际观察到的完整 tools JSON SHA-256 如下：

| 步骤 | 模型/权限 | 实际 search tools | tools SHA-256 |
|---|---|---|---|
| 普通对照首轮 | Kimi / ask | `web_search`，`external_web_access=false` | `95e702e856f072726f13a4da6e87198011c77f5462864ac714d69b6c4a4bc831` |
| 专家新会话首轮 | Kimi / ask | 空 | `84e2a93842f2cdb6df9c2cca0e76e5f5b940cb0c356feae1dc1b1a95891bb53a` |
| 原 Thread 切模型/权限 | MiniMax / auto | 空 | 同上 |
| 原 Thread 切模型/权限 | Kimi / full | 空 | 同上 |
| 正常关闭并重开原专家 Thread | Kimi / ask | 空 | 同上 |
| 原 Thread 显式解除专家 | Kimi / ask | 与普通对照完全相同 | `95e702e856f072726f13a4da6e87198011c77f5462864ac714d69b6c4a4bc831` |

另有 4 项正常纯配置/指令测试通过：`go test ./internal/codex -run '^TestFEAT158(Expert|DisabledMarket)' -count=1`；`go vet ./internal/codex`、`git diff --check` 通过。该证据来自实际固定 Runtime 的请求工具面，不只是 Go map 测试；修复后的真实产品模型验收仍由主流程另行记录。旧 Run 5 的网页调用不被本次通过结果抵销。

## 内容 0.1.1 的正常目录一致性（阶段 5 历史）

更新唯一 `ReviewedExpertCatalogDigest` 前，独立核对上述 manifest 和 authority 文件实际 SHA-256，与本轮已审阅的内容来源完全一致。实际目录校验绑定编译 pin、39 项目录及专家版本 0.1.1；正常安装解析 ROLE 2156 bytes、短索引 4420 bytes，专家未投影为全局 Skill。实际包生命周期、引用保护、只读重放、显式原 ID 收口和正常启动门禁均继续使用原机制。

```sh
YIJIE_EXPERT_TEST_BUNDLE=/Users/jack/Downloads/Personal_Info/CrossBSD/yijie-skills/dist/skills-experts-candidate-v011 \
YIJIE_EXPERT_TEST_MANIFEST_SHA256=31e7f42193263356c1e4939600ebd95961e6eb682c96f53429b47417dc3d1b6d \
  go test ./internal/skills ./internal/session ./internal/app -run '^TestFEAT158(ActualCandidateManagedResources|ActualResourceLifecycleReusesCoreJournalAndProtectsReferences|ExplicitResourceRecoveryRejectsOnlyUnstartedOriginals|ExpertLaunch)' -count=1 -v
go vet ./internal/skills ./internal/session ./internal/app
git diff --check
```

全部通过：5 个顶层测试、10 个叶子 case，无 skip；vet、diff 检查通过。本次仅在新临时受管目录测试，没有修改原产品数据库/安装、调用真实 Provider 或替换任何 Runtime binary。版本 0.1.0 的合成 fixture 及历史证据保持原样。

## 阶段 6：内容 0.1.2 的精确消费

本次仅更新 `ReviewedExpertCatalogDigest` 和三个实际候选包测试的版本期望，
没有修改 Runtime、schema、绑定/恢复流程或旧合成 fixture。内容/部署来源变化为
`contract-impact = semantic`；整个 FEAT-158 family 仍沿用上文 breaking 分类及
local candidate、release=false 边界。

写入 pin 前独立校验 producer manifest 与 authority 的实际 SHA-256，与本页首段一致。
包内仍为原 38 项普通 Skills 加一个专家；ROLE 2928 bytes、短索引 4420 bytes。
实际资源测试仅在新临时目录安装，专家全局 Runtime projection 仍为空；不会更新既有
0.1.1/0.1.0 受管安装或其历史引用。

本次五项顶层正常测试通过（含恢复测试下六个正常状态子例）：实际包安装/解析、
原 core journal 生命周期和引用保护、原 ID 显式恢复、默认关闭门禁、精确候选启动门禁。
两项既有普通 market 新会话/续轮/原回执/模型清理回归通过。命令如下：

```sh
export YIJIE_EXPERT_TEST_BUNDLE=/Users/jack/Downloads/Personal_Info/CrossBSD/yijie-skills/dist/skills-experts-candidate-v012
export YIJIE_EXPERT_TEST_MANIFEST_SHA256=a176bfba892afbc4de8622ebde955a72aef240ab51a9256d7bfb6c5ec40c0936
go test ./internal/skills ./internal/session ./internal/app -run '^TestFEAT158(ActualCandidateManagedResources|ActualResourceLifecycleReusesCoreJournalAndProtectsReferences|ExplicitResourceRecoveryRejectsOnlyUnstartedOriginals|ExpertLaunch)' -count=1 -v
go test ./internal/session -run '^TestFEAT157Market(NewTurnAcceptedReplayContinuationAndModelClear|ExplicitAdoptionAndReadAfterRevocation)$' -count=1 -v
go build ./...
go vet ./internal/skills ./internal/session ./internal/app ./internal/codex ./cmd/desktop-host
```

实际执行使用已安装 Go 1.26.5 的绝对路径及 `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`，
不下载工具链或模块。最初通过系统 Go 1.26.4 的离线自动工具链选择被校验拒绝，未执行测试；
改用已缓存的固定 1.26.5 后通过。完整结果和源码摘要见
`.local/feat158-phase6-host-checks.json`。

T06 的真实审批及合法连接器选集、T08 的新版正常重开、T09 的真实正常路径由主任务另记；
普通 journal/正向状态测试不冒充真实超时或故障实测。当前完整 base 字节没有直接采样，
不得以最初 `session_meta` 充当当前 base 证明；阶段 5 的正常 Runtime 六轮资格与实际
Native none/成功原回执/逐轮快照仍分别保留。阶段 6 的真实模型输出也不由这些 Host
检查宣称通过。公开发布、签名、公证及不可变发布 pin 不在本次范围。

## 阶段 6 逻辑收口：内容 0.1.3 的精确消费

按用户收窄后的范围，仅同步单一受审 manifest pin、三个实际候选包测试的版本期望，
保持原协议、Runtime、存储、引用保护和恢复逻辑。`contract-impact = semantic`，
原因是受审内容目录版本变化；不新增 profile，不覆盖旧安装，不启动产品或调用模型。
原 0.1.2 producer、真实会话历史与验收结果保留。

写入 pin 前独立核对上文 manifest 和 authority 的实际 SHA-256。包内仍为原 38 项
普通 Skills 加一个专家。五项顶层正常测试通过，含原操作恢复的六个正常状态子例；
新临时目录的实际安装/解析观测 ROLE 为 3188 bytes、生成的短索引为 4420 bytes，
专家没有进入 Runtime 全局 Skill 投影。测试命令：

```sh
export YIJIE_EXPERT_TEST_BUNDLE=/Users/jack/Downloads/Personal_Info/CrossBSD/yijie-skills/dist/skills-experts-candidate-v013
export YIJIE_EXPERT_TEST_MANIFEST_SHA256=46e3e5b9e101c252a4dcb0bc9d27c1da024ea6457be43e5be5736ccf047c6b58
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off /Users/jack/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.5.darwin-arm64/bin/go test ./internal/skills ./internal/session ./internal/app -run '^TestFEAT158(ActualCandidateManagedResources|ActualResourceLifecycleReusesCoreJournalAndProtectsReferences|ExplicitResourceRecoveryRejectsOnlyUnstartedOriginals|ExpertLaunch)' -count=1 -v
```

结果与最终源摘要记录于 `.local/feat158-phase6-v013-host-checks.json`。
测试只验证本地安装/解析、原 journal、引用保护、显式恢复和启动配置门禁，
不构成模型内容质量、真实审批或合法连接器选集通过证据。Shopify 已观察到的
初始化失败仍保留未通过；不因 HTTP 200、keyless 准入测试通过或新内容包而改变结论。
本次没有扩大 Provider、增加诊断请求、修改 Runtime、运行旧故障注入测试或新增真实调用。
