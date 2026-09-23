# FEAT-155 3C-3B1 · fixed-purpose draft candidate

Shared authority and generated snapshots: sibling Contracts `draft-execution-v1.schema.json`, `scheduled-plan-draft.yaml`, and `api/scheduled-draft.candidate.json`. Local candidate, no release pin. Ordinary startup remains Store5; `WithScheduledDraftStorage` explicitly migrates temporary/candidate data to Store6. Compatible ordinary readers preserve 6 and cannot create draft records. Existing sessions are backfilled as ordinary; immutable purpose/policy/schema/workspace is checked on read and every write.

The existing service/Store reserve, task mapping, turn operation, thread, notification, interruption and history paths are reused. Draft creation reserves purpose before Runtime I/O. Ordinary resume, v1/v2/permission turns, approval decisions and title generation reject draft purpose. Accepted operation replay returns the original turn. Changed input conflicts; pending/unknown is not another send. There is no second executor, database, queue, SSE machine or direct Provider.

Native construction may supply `WithScheduledDraftWorkspaceRoot`, pointing at a qualified Desktop `scheduled-workspaces/<tenant>/<owner>` root. The opaque ID must resolve to an existing canonical empty child. HTTP cannot pass paths or directory roots. The ordinary launcher supplies no candidate root/writer. Native reconstruction rechecks directory identity and emptiness on start/resume/turn.

`internal/codex/scheduled_draft.go` builds the fixed stable config and outputSchema. Report29's exact native candidate now proves its effective policy; old artifacts still refuse drafts. See [native qualification](scheduled-draft-input-only.md). Actual Provider outputSchema behavior remains unqualified.

## 3C-3B3B native assembly and read-only recovery

The existing `cmd/desktop-host` entry accepts a private `YIJIE_SCHEDULED_CANDIDATE` JSON descriptor: schema_version=1, owner_user_id, tenant_id, workspace_root. It is native-only, local/demo_fast, bounded and rejects unknown fields. Parent PID and instance UUID must be valid; homes/root must be canonical existing directories owned by this OS user with mode0700, separate and scoped as `scheduled-workspaces/<tenant>/<owner>`. Native startup verifies the exact Contracts binary/manifest pin before opening Store6. Without this descriptor the target stays Store5; compatible readers preserve Store6. No migration7 or permission override exists.

`GET /v1/scheduled-plan-draft-session-mappings/{task_id}` checks owner bearer and returns no-store, generated `RecoveryMapping` from the same Store snapshot. It requires neither Runtime nor directory resolution and never calls create/resume. Purpose/version/workspace mismatch is not a missing-record retry signal. Current responder nonce does not replace the original execution generation.

Explicit resume is serialized with draft turn admission and refuses a non-idle/pending session. A newly created, loaded thread with no turn attempt reuses its original same-generation receipt only after actual policy revalidation. On cold reopen an empty thread without durable Runtime history can remain unavailable; no replacement thread is created, and failed resume does not overwrite durable execution state with a fabricated failed result. Existing ordinary behavior is retained.


Focused tests use temporary bbolt stores and declared HTTP/protocol fixtures. B3B also checks the real Host entry and exact Runtime with no model turn, normal stop/reopen and preserved mapping, separately from protocol fixtures. No daily database, Keychain, real model, image or merchant calls. The final FEAT-155 report records targeted race/vet, ordinary regressions and canonical producer evidence. To disable candidates, stop producers normally and retain compatible Store6 readers; do not downgrade metadata or remove purpose.
