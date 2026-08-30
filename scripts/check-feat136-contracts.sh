#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
contracts_repo="${YIJIE_CONTRACTS_REPO:-$repo_root/../yijie-contracts}"
required_commit="87f94c9aa6d4848cb67aa8a1265bd21474edb0bb"
required_version="0.7.0"
legacy_fixture_baseline_commit="3832a6c5e99b2a6365f193280fdb887c8fdbc2de"
lock_file="$repo_root/api/contracts.lock"

fail() {
  echo "$1" >&2
  exit 1
}

[ -f "$lock_file" ] || fail "FEAT-136 contract lock is missing."
if ! git -C "$contracts_repo" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  fail "FEAT-136 contract source is not a Git repository: $contracts_repo"
fi
if [ -n "$(git -C "$contracts_repo" status --porcelain)" ]; then
  fail "FEAT-136 contract source must be clean: $contracts_repo"
fi
resolved_commit="$(git -C "$contracts_repo" rev-parse --verify "${required_commit}^{commit}" 2>/dev/null || true)"
[ "$resolved_commit" = "$required_commit" ] || fail "Exact FEAT-136 Contracts commit is unavailable."

lock_value() {
  local key="$1"
  local count
  count="$(awk -F= -v key="$key" '$1 == key { count += 1 } END { print count + 0 }' "$lock_file")"
  if [ "$count" -ne 1 ]; then
    fail "FEAT-136 contract lock must contain exactly one $key entry."
  fi
  awk -F= -v key="$key" '$1 == key { sub(/^[^=]*=/, ""); print }' "$lock_file"
}

[ "$(lock_value CONTRACTS_VERSION)" = "$required_version" ] || fail "Contracts version is not v$required_version."
[ "$(lock_value CONTRACTS_REF)" = "$required_commit" ] || fail "Contracts ref is not the exact FEAT-136 commit."
[ "$(lock_value CONTRACTS_COMMIT)" = "$required_commit" ] || fail "Contracts commit pin drifted."
[ "$(lock_value CONTRACTS_GENERATOR)" = "github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen" ] ||
  fail "FEAT-136 generator identity drifted."
[ "$(lock_value FEAT136_SCOPED_SYNC_VERSION)" = "1" ] || fail "FEAT-136 scoped sync version is invalid."
[ "$(lock_value FEAT136_SCOPED_SOURCES)" = "ordinary-openapi-schema-session-event-v4-v5-json" ] ||
  fail "FEAT-136 scoped source declaration drifted."
[ "$(lock_value EXCLUDED_FIXTURE_POLICY)" = "git-object-id-only-preserve-existing-snapshot-digests" ] ||
  fail "FEAT-136 excluded fixture policy drifted."
[ "$(lock_value EXCLUDED_FIXTURE_BASELINE_COMMIT)" = "$legacy_fixture_baseline_commit" ] ||
  fail "FEAT-136 excluded fixture baseline drifted."

actual_generator_version="$(go tool oapi-codegen --version | tail -n 1 | tr -d '\r')"
[ "$actual_generator_version" = "$(lock_value CONTRACTS_GENERATOR_VERSION)" ] ||
  fail "FEAT-136 generator version does not match api/contracts.lock."

source_paths=(
  openapi/agent-host/agent-host.yaml
  compatibility/agent-host-runtime-v1.json
  jsonschema/agent/session-event.schema.json
  jsonschema/agent/session-event-v2.schema.json
  jsonschema/agent/session-event-v3.schema.json
  jsonschema/agent/session-event-v4.schema.json
  jsonschema/agent/session-event-v5.schema.json
  jsonschema/report/report-document-v1.schema.json
  jsonschema/skills/skill-bundle-manifest-v1.schema.json
  jsonschema/skills/skill-bundle-manifest-v2.schema.json
)
snapshot_paths=(
  api/openapi/agent-host.yaml
  api/compatibility/agent-host-runtime-v1.json
  api/jsonschema/agent-session-event.schema.json
  api/jsonschema/agent-session-event-v2.schema.json
  api/jsonschema/agent-session-event-v3.schema.json
  api/jsonschema/agent-session-event-v4.schema.json
  api/jsonschema/agent-session-event-v5.schema.json
  api/jsonschema/report-document-v1.schema.json
  api/jsonschema/skill-bundle-manifest-v1.schema.json
  api/jsonschema/skill-bundle-manifest-v2.schema.json
)
digest_keys=(
  OPENAPI_SHA256
  RUNTIME_COMPATIBILITY_SHA256
  AGENT_SESSION_EVENT_SCHEMA_SHA256
  AGENT_SESSION_EVENT_V2_SCHEMA_SHA256
  AGENT_SESSION_EVENT_V3_SCHEMA_SHA256
  AGENT_SESSION_EVENT_V4_SCHEMA_SHA256
  AGENT_SESSION_EVENT_V5_SCHEMA_SHA256
  REPORT_DOCUMENT_V1_SCHEMA_SHA256
  SKILL_BUNDLE_MANIFEST_V1_SCHEMA_SHA256
  SKILL_BUNDLE_MANIFEST_V2_SCHEMA_SHA256
)
v4_fixture_files=(
  agent-message-commentary-started.json
  agent-message-final-completed.json
  agent-message-null-phase-started.json
  turn-plan-cleared.json
  turn-plan-updated.json
)
v5_fixture_files=(
  command-completed.json
  command-declined.json
  command-failed-head-tail.json
  command-output-delta.json
  command-started.json
  tool-completed-known.json
  tool-declined-reserved-fixture-only.json
  tool-failed-result.json
  tool-progress.json
  tool-started-known.json
  tool-unknown-failed.json
)

sha256_file() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{ print $1 }'
  else
    sha256sum "$1" | awk '{ print $1 }'
  fi
}

fixture_snapshot_sha256() {
  local root="$1"
  shift
  while IFS= read -r relative; do
    printf '%s\0%s\n' "$relative" "$(sha256_file "$root/$relative")"
  done < <(printf '%s\n' "$@" | LC_ALL=C sort) |
    if command -v shasum >/dev/null 2>&1; then
      shasum -a 256 | awk '{ print $1 }'
    else
      sha256sum | awk '{ print $1 }'
    fi
}

assert_snapshot_fixture_set() {
  local root="$1"
  shift
  local expected actual
  expected="$(printf '%s\n' "$@" | LC_ALL=C sort)"
  actual="$(cd "$root" && find . -mindepth 1 ! -type d -print | sed 's#^\./##' | LC_ALL=C sort)"
  [ "$actual" = "$expected" ] || fail "FEAT-136 JSON fixture snapshot drifted: $root"
}

assert_source_fixture_set() {
  local source_prefix="$1"
  shift
  local expected actual
  expected="$(printf '%s\n' "$@" | LC_ALL=C sort)"
  actual="$(git -C "$contracts_repo" ls-tree -r --name-only "$required_commit" -- "$source_prefix" |
    sed "s#^$source_prefix/##" | LC_ALL=C sort)"
  [ "$actual" = "$expected" ] || fail "FEAT-136 source JSON fixture set drifted: $source_prefix"
}

temporary_dir="$(mktemp -d)"
trap 'rm -rf -- "$temporary_dir"' EXIT

for index in "${!source_paths[@]}"; do
  source_path="${source_paths[$index]}"
  snapshot_path="$repo_root/${snapshot_paths[$index]}"
  digest_key="${digest_keys[$index]}"
  [ -f "$snapshot_path" ] || fail "FEAT-136 contract snapshot is missing: ${snapshot_paths[$index]}"
  [ "$(sha256_file "$snapshot_path")" = "$(lock_value "$digest_key")" ] ||
    fail "FEAT-136 snapshot digest does not match $digest_key."
  git -C "$contracts_repo" show "$required_commit:$source_path" >"$temporary_dir/source-$index"
  cmp "$temporary_dir/source-$index" "$snapshot_path" >/dev/null ||
    fail "FEAT-136 snapshot differs from immutable source: $source_path"
done

source_package="$temporary_dir/package.json"
git -C "$contracts_repo" show "$required_commit:package.json" >"$source_package"
source_version="$(awk -F '"' '/^[[:space:]]*"version"[[:space:]]*:/ { print $4; exit }' "$source_package")"
[ "$source_version" = "$required_version" ] || fail "Contracts package version differs from v$required_version."
compatibility_version="$(awk -F '"' '/^[[:space:]]*"contracts_version"[[:space:]]*:/ { print $4; exit }' \
  "$repo_root/api/compatibility/agent-host-runtime-v1.json")"
[ "$compatibility_version" = "$required_version" ] || fail "Runtime projection version differs from v$required_version."

assert_snapshot_fixture_set "$repo_root/api/fixtures/agent/session-event-v4" "${v4_fixture_files[@]}"
assert_snapshot_fixture_set "$repo_root/api/fixtures/agent/session-event-v5" "${v5_fixture_files[@]}"
assert_source_fixture_set "tests/fixtures/agent/session-event-v4" "${v4_fixture_files[@]}"
assert_source_fixture_set "tests/fixtures/agent/session-event-v5" "${v5_fixture_files[@]}"

for relative in "${v4_fixture_files[@]}"; do
  git -C "$contracts_repo" show \
    "$required_commit:tests/fixtures/agent/session-event-v4/$relative" \
    >"$temporary_dir/v4-$relative"
  cmp "$temporary_dir/v4-$relative" "$repo_root/api/fixtures/agent/session-event-v4/$relative" >/dev/null ||
    fail "FEAT-136 v4 fixture differs from immutable source: $relative"
done
for relative in "${v5_fixture_files[@]}"; do
  git -C "$contracts_repo" show \
    "$required_commit:tests/fixtures/agent/session-event-v5/$relative" \
    >"$temporary_dir/v5-$relative"
  cmp "$temporary_dir/v5-$relative" "$repo_root/api/fixtures/agent/session-event-v5/$relative" >/dev/null ||
    fail "FEAT-136 v5 fixture differs from immutable source: $relative"
done

[ "$(fixture_snapshot_sha256 "$repo_root/api/fixtures/agent/session-event-v4" "${v4_fixture_files[@]}")" = \
  "$(lock_value AGENT_SESSION_EVENT_V4_FIXTURE_SNAPSHOT_SHA256)" ] ||
  fail "FEAT-136 v4 fixture snapshot digest drifted."
[ "$(fixture_snapshot_sha256 "$repo_root/api/fixtures/agent/session-event-v5" "${v5_fixture_files[@]}")" = \
  "$(lock_value AGENT_SESSION_EVENT_V5_FIXTURE_SNAPSHOT_SHA256)" ] ||
  fail "FEAT-136 v5 fixture snapshot digest drifted."
[ "$(git -C "$contracts_repo" rev-parse "$required_commit:tests/fixtures/agent/session-event-v4")" = \
  "$(lock_value AGENT_SESSION_EVENT_V4_FIXTURE_TREE)" ] || fail "FEAT-136 v4 fixture tree drifted."
[ "$(git -C "$contracts_repo" rev-parse "$required_commit:tests/fixtures/agent/session-event-v5")" = \
  "$(lock_value AGENT_SESSION_EVENT_V5_FIXTURE_TREE)" ] || fail "FEAT-136 v5 fixture tree drifted."

# Do not inspect any blob under these paths. Only compare immutable Git tree
# object IDs and fixed legacy lock values.
[ "$(git -C "$contracts_repo" rev-parse "$required_commit:tests/fixtures/skills/bundle-v1")" = \
  "$(lock_value SKILL_BUNDLE_FIXTURE_TREE)" ] || fail "Excluded Skill Bundle v1 tree ID drifted."
[ "$(git -C "$contracts_repo" rev-parse "$required_commit:tests/fixtures/skills/bundle-v2")" = \
  "$(lock_value SKILL_BUNDLE_V2_FIXTURE_TREE)" ] || fail "Excluded Skill Bundle v2 tree ID drifted."
[ "$(git -C "$contracts_repo" rev-parse "$required_commit:tests/fixtures/agent/host-skills-v1")" = \
  "$(lock_value HOST_SKILLS_V1_FIXTURE_TREE)" ] || fail "Excluded Host Skills tree ID drifted."
[ "$(git -C "$contracts_repo" rev-parse "$required_commit:tests/fixtures/agent/resources-v3")" = \
  "$(lock_value SYNTHETIC_VIDEO_FIXTURE_TREE)" ] || fail "Excluded resource tree ID drifted."

[ "$(lock_value SKILL_BUNDLE_FIXTURE_SNAPSHOT_SHA256)" = "be149b821586cc974fe62a09de92a83bddc6f11500839582e2b5b6f9b05b2b4a" ] ||
  fail "Excluded Skill Bundle v1 legacy digest changed."
[ "$(lock_value SKILL_BUNDLE_V2_FIXTURE_SNAPSHOT_SHA256)" = "139f5c999e6ae98f4c47b68e93ff9439a97aa3c5c1372ca99658ad2e49c728dd" ] ||
  fail "Excluded Skill Bundle v2 legacy digest changed."
[ "$(lock_value HOST_SKILLS_V1_FIXTURE_SNAPSHOT_SHA256)" = "c6cf199b18b09f1e73b4594d6844f0f8fa7858761ba1f3bcd089f6644a24727f" ] ||
  fail "Excluded Host Skills legacy digest changed."
[ "$(lock_value SYNTHETIC_VIDEO_FIXTURE_SOURCE)" = "tests/fixtures/agent/resources-v3/synthetic-video-16x16.mp4.base64" ] ||
  fail "Excluded resource source path changed."
[ "$(lock_value SYNTHETIC_VIDEO_FIXTURE_SOURCE_SHA256)" = "d8a7a55c786dcde9367125d806e0cf5b014fb8f70801c97b80a70d5160f95f8a" ] ||
  fail "Excluded resource source digest changed."
[ "$(lock_value SYNTHETIC_VIDEO_FIXTURE_RAW_SIZE)" = "1642" ] || fail "Excluded resource size changed."
[ "$(lock_value SYNTHETIC_VIDEO_FIXTURE_RAW_SHA256)" = "96ea070cac612d17927939c22f3c0c593fb26b171f62c4e9cee43fb596177dd5" ] ||
  fail "Excluded resource raw digest changed."
[ "$(lock_value SYNTHETIC_VIDEO_FIXTURE_SNAPSHOT)" = "internal/session/fixtures/synthetic-video-16x16.mp4.base64" ] ||
  fail "Excluded resource snapshot path changed."
[ "$(lock_value SYNTHETIC_VIDEO_FIXTURE_SNAPSHOT_SHA256)" = "d8a7a55c786dcde9367125d806e0cf5b014fb8f70801c97b80a70d5160f95f8a" ] ||
  fail "Excluded resource snapshot digest changed."

generated="$temporary_dir/agenthost.gen.go"
go tool oapi-codegen \
  -generate types \
  -package agenthostcontract \
  -o "$generated" \
  "$repo_root/api/openapi/agent-host.yaml"
gofmt -w "$generated"
cmp "$generated" "$repo_root/internal/contracts/agenthost.gen.go" >/dev/null ||
  fail "Generated Agent Host DTOs differ; run make generate."

echo "Verified FEAT-136 Contracts v$required_version at $required_commit."
echo "Excluded archive/checksum/Zip-Slip/archive-error fixtures were not read; only immutable Git object IDs were compared."
