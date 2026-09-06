# Skill journal v2 — local capacity recovery

Status: local bug-fix candidate, 2026-09-06. User request: investigate and fix the
unavailable Plugins page after ordinary Desktop startup.

## Evidence and scope

The current default Desktop journal contains 4096 completed successful operations:
4091 scans, two installs, two enabled changes, and one uninstall (48,819,651 bytes).
Host readiness and authenticated GET /v1/skills succeed with 38 entries, while a
new POST scan returns 409 skill_busy. This is retained-history exhaustion, not an
active operation or a missing service. The records date from August 25; the
previous watcher feedback-loop fix stopped new feedback but did not recover a
full journal.

Automatic Desktop refreshes will use the existing GET reconciliation, preserving
native reason validation and automatic installation upgrades. Explicit user
rescans and existing API callers retain POST scan's idempotent semantics.
HTTP/native shapes, permissions,
error enums, and the no-expiry replay promise are unchanged.

Contract-impact: breaking for **private storage downgrade**, not for the public
HTTP protocol. Authority: this Host document and internal/skills storage code.
No new public DTO or Contracts schema is required. Existing v1 journals remain
readable; a migrated journal deliberately prevents an old Host from reopening it
and forgetting newer idempotency keys. This is a local candidate, not a release
or a claim of approved production migration.

## Storage and compatibility

Keep v1 for journals below their existing 4096-record limit. At capacity, import
all records transactionally into an owner-only bbolt store using the dependency
already used by Host session storage. Preserve the original v1 JSON as an exact
backup, then atomically publish a schema_version=2 marker in operations.json.
Import and sync must finish before the marker is published or any new operation
is admitted. An incomplete pre-marker import is safe to retry from v1.

V2 retains all completed IDs, fingerprints, results, and recovery phases on disk.
Only unfinished operations and a bounded replay cache stay in memory. Removing
a completed record from that cache never deletes its durable replay value.
New journal transactions update cached records and explicitly remove only the
same transient reservations that v1 already discards. Startup loads unfinished
records for the existing recovery algorithm; completed records are read by ID.
Per-record size remains bounded and disk errors fail closed. No TTL, pruning,
manual journal reset, resource deletion, new dependency, or paid model call.

Rollback before migration is unchanged. After migration, retain a v2-capable
reader when reverting application changes: the old binary must fail closed.
The v1 backup is evidence/recovery material, not a safe way to discard operations
accepted after migration. Do not replace the active journal with that backup.

## Acceptance and verification

- The existing 4096 completed records survive migration and replay identically.
- A new scan and normal install/enable/uninstall operations work after capacity.
- Same ID/different input still conflicts, including after normal close/reopen.
- Concurrent callers observe a single immutable result; pending records retain
  their recovery phases. No operation is silently expired.
- Repeated automatic native refreshes return the live catalog without growing
  the journal; one explicit rescan creates exactly one replayable operation.
- Normal Desktop startup/restart reaches the 38-entry Plugins page.
- Use normal fixtures and normal shutdown only. Attack fixtures, permission
  sabotage, process fault injection, and paid provider calls are not run.

Verification results are recorded in the Desktop bug-fix report after execution.
