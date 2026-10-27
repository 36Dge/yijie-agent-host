# FEAT-164 S1 candidate activation

Contract impact: semantic, within the already pinned chat-models-v2 and Runtime K3.1 projection at Contracts `f2819c66a2368aaff37216a46c48d5cd81df1747`. No new public wire shape or Runtime binary. The overall migration remains breaking and unpublished.

The explicit local/demo_fast K3.1 candidate keeps its two active identities and separately recognizes retired K3 history. `YIJIE_KIMI_K31_EXECUTION_PAUSED=true` disables new Kimi execution while retaining v2 catalog/history reads and MiniMax. The existing `activation_required` value explains the pause. Missing/false keeps the reviewed candidate default; unknown flag values or use outside the candidate are rejected. Change this startup setting only after normal shutdown; it is not a hot cancellation mechanism.

`YIJIE_KIMI_K31_ALLOW_MANAGED_PREDECESSOR=true` permits preparing v2 configuration alongside an exact known v1 pair. Frozen bytes and source digests are in `internal/codex/kimi_k31_predecessors/`, captured from Host `5022435a6272263e66e46a41b7ed7c3f9fb6caf9`. The sole template substitution is the quoted canonical catalog path. Old v1 files and user trust remain unchanged. This private opt-in does not migrate Host/Desktop databases or grant model execution; Desktop still restricts the candidate to its existing isolated storage boundary.

Both old files must be absent or an exact recognized pair. With the exclusive managed-Home lease held, v2 preparation accepts absent/absent, current catalog/absent, and current/current. A config without its catalog, unknown bytes, or half a predecessor pair fails before new model files are applied. The catalog is written first, then TOML, using existing atomic managed-file operations; the complete pair and predecessor are revalidated before Runtime spawn. Do not use an old Host as a reader of newly written product data.

The candidate requires an explicit active profile for start/resume/title work. Every ordinary turn, including the legacy entry point, also requires the same Manager generation's complete native model/provider/effort receipt for that thread and the profile's exact reasoning effort. A resume/switch removes the prior receipt before configuration is sent; an unknown result does not leave the old receipt usable. Native history reads cannot establish model identity. Permission policies and tool approval remain Runtime-owned.

Safe checks: `TestFEAT164ActivationAndRetiredAdmission`, the FEAT164 session/API controls, and `TestFEAT164NativeCandidateLifecycle` / `TestFEAT164NativePredecessorAndPausedKimi`. Integration uses the fixed canonical Runtime with ordinary local HTTP responses, never a substituted executable or real provider credentials. All processes use normal shutdown. This proves local behavior only; fresh real-model G3 evidence and later migration/plan slices remain outstanding.

## Reviewed daily predecessor (FEAT-164 report63)

The Owner's request to resolve report62 pre-D3 blockers authorizes this private
managed-file semantic correction. No public DTO, Runtime patch, database or
active v2 prompt changes. A third exact catalog is recognized only under the
existing explicit predecessor flag and alongside the exact original v1 TOML.
`v1-feat163-reviewed.json` is reconstructed from the frozen current recipe with
the reviewed original producer's Kimi `base_instructions`; its bytes match the
retained daily backup, and the original Host binary matches its canonical receipt.
The companion provenance records the source-file, artifact and catalog digests;
the original dirty producer is not represented as a released commit or imported
as FEAT-163 implementation.

This accepts only the reviewed whole-file SHA/bytes. Unknown edits, incomplete
pairs and implicit activation still fail before any writes. The old v1 catalog,
TOML and user trust remain untouched; the existing v2 catalog remains unchanged.
The new unit case prepares v2 twice in a normal synthetic Home and verifies those
preservation and rejection conditions. No real daily Home is opened by the test.
