# Agent Host contract snapshot

The authoritative sources live in the sibling `yijie-contracts` repository.

- `openapi/agent-host.yaml` is the exact HTTP/SSE OpenAPI snapshot;
- `compatibility/agent-host-runtime-v1.json` pins the supported Codex Runtime projection;
- `jsonschema/agent-session-event*.schema.json` are the exact v1/v2/v3 SSE data contracts;
- `jsonschema/report-document-v1.schema.json` is the exact closed structured-report contract;
- `jsonschema/skill-bundle-manifest-v1.schema.json` is retained for compatibility and malicious-archive regression fixtures;
- `jsonschema/skill-bundle-manifest-v2.schema.json` is the exact FEAT-129 catalog contract;
- `fixtures/skills/bundle-v1/`, `fixtures/skills/bundle-v2/`, and `fixtures/agent/host-skills-v1/` are exact allowlisted conformance snapshots, including the synthetic 38-entry blocked catalog, checksum-corrupt, Zip Slip, and `skill_not_installable` cases;
- `contracts.lock` records the immutable tag, full commit, generator identity/version, and SHA-256 identities;
- `skills.lock` independently pins the exact `yijie-skills@0.3.0` producer commit, source tree, dual-channel manifests, and 38-archive inventory;
- `internal/contracts/agenthost.gen.go` is generated from the OpenAPI snapshot.

Run `YIJIE_CONTRACTS_REF=<full-contracts-commit> make sync-contracts` from a checkout whose managed contract targets are clean and with a clean sibling `yijie-contracts`; the current FEAT-129 candidate uses the full Contracts 0.5.1 commit recorded in `contracts.lock`. The sync reads contract sources from that Git object, not from floating worktree files, and rejects fixture-set drift outside the allowlist. Run `make skills-conformance` against the clean sibling producer fixed by `skills.lock`. A `contracts-vX.Y.Z` tag is used only after an approved supported release exists, so this untagged candidate must not be described as published. Normal Host CI runs `make contract-check` without requiring network access. Do not edit snapshot or generated files by hand.
