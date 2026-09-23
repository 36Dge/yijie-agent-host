# FEAT-155 read-only native clock

The explicitly assembled local scheduled candidate exposes
`GET /v1/agent-sessions/{agent_session_id}/turns/{runtime_turn_id}/timing` through the
existing owner-only bearer. Ordinary and production registration remain unchanged.
The DTO/schema/source snapshot come from Contracts native-turn-timing v0.1.0;
`api/native-turn-timing.candidate.json` is unreleased source/digest provenance.

ReadNativeTurnTiming uses the managed stable Runtime ReadThread, validates the
stored thread and exact turn, and emits three independent clock facts. Unix second
timestamps and millisecond duration retain their native units. Missing/null stays
unknown; invalid values do not become zero. It never changes session/operation,
starts/resumes a thread, or interprets time as lifecycle evidence. Existing native
v1/v2 and SSE v7/v8 producers are not modified. Store versions 5/6 are unchanged.

The query has a 3 second deadline and fails explicitly at 8 MiB/1024 turns, inside
the existing transport limit. Errors contain only stable codes and safe text; no
history, cwd, inputs, tools or raw errors are returned. no-store also wraps auth
errors. No new dependencies or Runtime artifact changes.

Safe checks: `go test -race ./internal/session ./internal/app -run '^TestFEAT155(Timing|Recovery)' -count=1`,
Contracts source/consumer checks and lint. Tests use declared data, temporary normal
Stores and in-process HTTP; no executable substitute, permission sabotage or
process failure injection. The full legacy fault suite is not implied to pass.

Actual fixed Runtime hot/cold timing evidence and normal cleanup are recorded in
the FEAT-155 4D-1 delivery package. Declared loopback Provider requests are separate
from real text-model usage. This local candidate is not a release pin.
