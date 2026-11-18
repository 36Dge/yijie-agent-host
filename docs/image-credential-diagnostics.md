# 图片服务本地凭据诊断（2026-11-17）

用户要求审计最新图片生成失败并修复。当前日常 Host 的失败事件为
`authentication_failed`，没有 HTTP 状态；同一可执行文件的只读凭据检查返回
`credential_unavailable`。`SeedreamClient.Generate` 在读取凭据成功前不会发送 HTTP。
这证明本次失败在本机凭据读取阶段，不是供应商审图或余额拒绝。

`contract-impact = additive`，仅限既有运营 CLI 的可选诊断字段。权威源为
`cmd/desktop-host/credential_check.go`；消费者是有人值守的本机运维。
原 `ready`、`code`、`provider_requests` 和退出码不变；失败时可增加固定枚举
`reason` 与数值 `keychain_status`，已有消费者仍可只读取原字段。
公共 MCP/HTTP/SSE/Artifact 状态、错误码、存储格式与重放语义不变；没有新增公共 DTO。
Artifact 原有自由文本诊断保留本地失败阶段与 OSStatus，不包含凭据、账户或文件路径。

| reason | 含义 |
| --- | --- |
| authorization_required | 系统要求交互授权，但普通生成禁止弹窗 |
| access_denied | 本机构建的凭据访问未获允许 |
| not_found | 当前 Host scope 没有可匹配的条目 |
| authorization_cancelled | 本人取消系统授权 |
| keychain_unavailable | 系统钥匙串不可用 |
| read_failed | 其他系统读取失败，保留 OSStatus 供排查 |
| invalid_value | 本机返回的值不满足既有凭据格式要求 |
| unsupported_platform | 当前平台不提供受管凭据读取 |

只读检查仍使用正在交付的真实 Host：`--check-media-credential`。
恢复复用已有 `--authorize-media-credential`，由本人完成系统窗口，再对同一
二进制运行无交互检查。正常生成不弹窗，不自动重试、不替换供应商，不修改钥匙串
条目或权限，也不复制密钥到文件。新构建必须重新核实访问，不能沿用旧构建结论。

现有原生 MCP、Code Mode、轮次绑定、Artifact 提交/ACK 链完全复用；无 Runtime 修改。
正常退出后由固定开发入口标准构建并重开；不替换运行中的二进制、不清聊天。
回滚源代码即可，CLI 旧字段与数据库格式保持兼容；旧失败记录不被改写为成功。

验证只覆盖普通 Keychain 状态值、生成前零 HTTP、真实 HTTP 401 的独立诊断、
既有单次派发与前端失败提示。系统授权及实际生图分别记录，不以只读就绪冒充生图成功。
不执行强杀、权限破坏、二进制替身或攻击注入用例；不运行无关全量回归。

依据：[Apple Keychain 结果码](https://developer.apple.com/documentation/security/security-framework-result-codes)、
[Apple SecItemCopyMatching](https://developer.apple.com/documentation/security/secitemcopymatching(_:_:))。

本轮实际恢复：canonical 构建后同一 Host 的无交互检查明确返回
`access_denied/-25293`。用户完成现有系统授权后，授权命令及随后独立无交互检查
均返回 `ready=true`，Host/Runtime ready。Host SHA-256 为
`4bf76dcc67a65999bc0c3503f86cc9a1fa870a13d27a3807bb016e2cf5f9db0d`。
本轮新增生图 HTTP 请求 0；凭据恢复已确认，实际新图片交付未冒称验证通过。
