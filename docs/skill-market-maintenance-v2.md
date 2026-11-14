# FEAT-174 Host maintenance journal v2

Owner: Agent Runtime Team. 2026-11-14 local candidate; paired Contracts/Desktop/Host only. Highest contract-impact=breaking. No release tag or deployment is implied.

`skill-market-v2/state.db` reads schema 1 and 2, writes schema 2. The legacy v3 route reader supports both before the new writer opens. A schema-1 writer rejects 2; rollback means the current compatible reader, never a downgrade to an old binary. Original legacy JSON/bbolt snapshots, completed receipts, source bytes and identities remain unchanged.

The `maintenance` bucket stores UUID-scoped rollback/resume records and their original per-installation epoch, native package version and enabled intent. `meta.active-maintenance` points to unfinished work; completed cycles remain immutable. The versioned phases are `rollback_rules`, `rollback_uninstall`, `rollback_disable`, `rollback_verify`, `resume_migrate`, `resume_restore`, `resume_verify`, `complete`. Each normal checkpoint is durable. Startup `Migrate` continues this same journal under the existing serial lock and native maintenance admission before readiness. Mode becomes native only after every restored toggle is verified.

Rollback verifies **all** receipt-backed legacy sources, byte digests, paths, names, versions and desired states before any edit/uninstall; it validates current native states and the complete inverse rule chain and snapshots all remaining installations in the same preflight. Sources are rechecked before uninstall. Successful completion additionally verifies native legacy discovery. Source absence never authorizes removal of a good native copy.

All actual installation, removal, enablement, discovery and refresh reuse pinned native RPCs and native config CAS. The journal records ownership and progress only; it does not parse skill metadata, evaluate precedence or copy the native installer/cache layout.

A proven schema-1 completed compatibility cycle is adopted once without changing its legacy record. Native-mode partial schema-1 toggle residue has insufficient ownership evidence: startup rejects with `legacy_maintenance_review_required`, preserves all records, and requires review. It is not silently treated as a finished resume. No unconditional automatic repair of historical unknown intent is provided.

Ordinary market submission rejection is separate from the accepted/pending/uncertain receipt codec. First persisted proof wraps `sessions.db` in schema 14 (`submission_rejection_base_schema` retains the original domain/model envelope), so old writers cannot ignore the operation-ID fence. New readers preserve all existing record bytes, accepted replay and model/expert codecs. Read-only same-owner proof requires the exact task, local session, submission operation, turn operation, selection digest and original Host instance. No proof/transport timeout remains unknown and is never automatically resubmitted.

Installation capacity is the union of the pinned N2 inventory and durable pending reservations, capped at 512. Current offers and tombstone history do not occupy installation slots. Up to 512 offers plus 512 installations/reservations are projected through the same-source 1024-row Catalog; no user identity/history is deleted to fit an upgrade.

Native CWD behavior is preserved, including project-local discovery. The pinned native skill rules intentionally consume User and SessionFlags layers; Host does not invent support for project-layer `skills.config` or reimplement rule evaluation. Compatibility selection now forwards the real CWD to that same native resolver.

Verification uses ordinary sources, legal initial history fixtures, source-generated releases, native CAS and normal close/reopen. Forbidden process/permission/attack fault injection and supplier calls are excluded. See meta feature `11-six-fixes-verification.md` for final commands, limits and evidence.

完整GUI验证补齐首轮拒绝后的继续聊天：Native复用现有消息/outbox事务及Host身份绑定函数，仅为显式新操作追加schema46初次Host意图。未绑定本地会话的所有前序轮次必须具有经完整Host证明核验后保存的失败分类；未知、在途、已绑定或普通错误都不能获得此入口。再次检查create权限，本地revision1保持，Host新建映射revision0；原失败操作不会被改成成功。schema46父记录级联清理，旧reader拒绝新格式。
