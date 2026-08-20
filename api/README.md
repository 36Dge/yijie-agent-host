# Agent Host contract snapshot

The authoritative sources live in the sibling `yijie-contracts` repository.

- `openapi/agent-host.yaml` is the exact HTTP/SSE OpenAPI snapshot;
- `compatibility/agent-host-runtime-v1.json` pins the supported Codex Runtime projection;
- `jsonschema/agent-session-event*.schema.json` are the exact v1/v2/v3 SSE data contracts;
- `jsonschema/report-document-v1.schema.json` is the exact closed structured-report contract;
- `contracts.lock` records the immutable tag, full commit, generator identity/version, and SHA-256 identities;
- `internal/contracts/agenthost.gen.go` is generated from the OpenAPI snapshot.

Run `YIJIE_CONTRACTS_REF=<full-contracts-commit> make sync-contracts` from a clean checkout with sibling `yijie-contracts`; the current FEAT-128 local candidate uses the full commit recorded in `contracts.lock`. The sync reads contract sources from that Git object, not from floating worktree files. A `contracts-vX.Y.Z` tag is used only after an approved supported release exists, so the untagged `0.4.0` candidate must not be described as published. Normal Host CI runs `make contract-check` without requiring network access. Do not edit snapshot or generated files by hand.
