# Agent Host contract snapshot

The authoritative sources live in the sibling `yijie-contracts` repository.

- `openapi/agent-host.yaml` is the exact HTTP/SSE OpenAPI snapshot;
- `compatibility/agent-host-runtime-v1.json` pins the supported Codex Runtime projection;
- `jsonschema/agent-session-event*.schema.json` are the exact v1-v5 SSE data contracts;
- `jsonschema/report-document-v1.schema.json` is the exact closed structured-report contract;
- `jsonschema/skill-bundle-manifest-v1.schema.json` is retained for compatibility and malicious-archive regression fixtures;
- `jsonschema/skill-bundle-manifest-v2.schema.json` is the exact FEAT-129 catalog contract;
- `fixtures/skills/bundle-v1/`, `fixtures/skills/bundle-v2/`, and `fixtures/agent/host-skills-v1/` are exact allowlisted conformance snapshots, including the synthetic 38-entry blocked catalog, checksum-corrupt, Zip Slip, and `skill_not_installable` cases;
- `contracts.lock` records the immutable tag, full commit, generator identity/version, and SHA-256 identities;
- `skills.lock` independently pins the exact `yijie-skills@0.3.0` producer commit, source tree, dual-channel manifests, and 38-archive inventory;
- `internal/contracts/agenthost.gen.go` is generated from the OpenAPI snapshot.

Run `YIJIE_CONTRACTS_REF=<full-contracts-commit> make sync-contracts` from a checkout whose managed contract targets are clean and with a clean sibling `yijie-contracts`. The lock now records the exact FEAT-136 Contracts 0.7.0 commit; it remains an untagged candidate and must not be described as published. The legacy sync reads contract sources from that Git object, not from floating worktree files, and rejects fixture-set drift outside the allowlist. Run `make skills-conformance` against the clean sibling producer fixed by `skills.lock`. A `contracts-vX.Y.Z` tag is used only after an approved supported release exists. Normal Host CI runs `make contract-check` without requiring network access. Do not edit snapshot or generated files by hand.

FEAT-136 uses a narrower safety-scoped path for Contracts `0.7.0` commit
`87f94c9aa6d4848cb67aa8a1265bd21474edb0bb`:

```bash
make sync-feat136-contracts
make generate
make feat136-contract-check
```

This scoped path reads and hashes only ordinary OpenAPI, compatibility and JSON
Schema sources plus the allowlisted v4/v5 session-event JSON fixtures. It never
reads, copies, hashes or unpacks the existing archive, checksum-corruption, Zip
Slip, or archive-error fixture blobs. Their previously reviewed snapshot digests
remain unchanged in `contracts.lock`; the scoped scripts compare only the
immutable Git tree object IDs at the pinned commit and record the legacy digest
baseline explicitly. The legacy `sync-contracts` and `contract-check` targets are
outside the FEAT-136 scoped safety evidence.
