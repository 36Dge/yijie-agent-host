# Agent Host contract snapshot

The authoritative sources live in the sibling `yijie-contracts` repository.

- `openapi/agent-host.yaml` is the exact HTTP/SSE OpenAPI snapshot;
- `compatibility/agent-host-runtime-v1.json` pins the supported Codex Runtime projection;
- `jsonschema/agent-session-event*.schema.json` are the exact v1/v2/v3 SSE data contracts;
- `jsonschema/report-document-v1.schema.json` is the exact closed structured-report contract;
- `jsonschema/skill-bundle-manifest-v1.schema.json` is the exact bundled Skill catalog contract;
- `fixtures/skills/bundle-v1/` and `fixtures/agent/host-skills-v1/` are exact allowlisted conformance snapshots, including checksum-corrupt and Zip Slip cases;
- `contracts.lock` records the immutable tag, full commit, generator identity/version, and SHA-256 identities;
- `internal/contracts/agenthost.gen.go` is generated from the OpenAPI snapshot.

Run `YIJIE_CONTRACTS_REF=<full-contracts-commit> make sync-contracts` from a checkout whose managed contract targets are clean and with a clean sibling `yijie-contracts`; the current FEAT-129 local candidate uses the full `0.5.0` commit recorded in `contracts.lock`. The sync reads contract sources from that Git object, not from floating worktree files, and rejects fixture-set drift outside the allowlist. A `contracts-vX.Y.Z` tag is used only after an approved supported release exists, so this untagged candidate must not be described as published. Normal Host CI runs `make contract-check` without requiring network access. Do not edit snapshot or generated files by hand.
