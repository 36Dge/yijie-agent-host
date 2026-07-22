# Agent Host contract snapshot

The authoritative sources live in the sibling `yijie-contracts` repository.

- `openapi/agent-host.yaml` is the exact HTTP/SSE OpenAPI snapshot;
- `compatibility/agent-host-runtime-v1.json` pins the supported Codex Runtime projection;
- `jsonschema/agent-session-event.schema.json` is the exact SSE data contract;
- `contracts.lock` records the candidate contract version and SHA-256 identities;
- `internal/contracts/agenthost.gen.go` is generated from the OpenAPI snapshot.

Run `make sync-contracts` from a checkout with sibling `yijie-contracts`. Normal Host CI runs `make contract-check` without requiring network access. Do not edit snapshot or generated files by hand.
