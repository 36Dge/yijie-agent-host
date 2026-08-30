#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
contracts_repo="${YIJIE_CONTRACTS_REPO:-$repo_root/../yijie-contracts}"
required_commit="87f94c9aa6d4848cb67aa8a1265bd21474edb0bb"
requested_ref="${YIJIE_CONTRACTS_REF:-$required_commit}"
required_version="0.7.0"
legacy_fixture_baseline_commit="3832a6c5e99b2a6365f193280fdb887c8fdbc2de"
generator="github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen"
lock_file="$repo_root/api/contracts.lock"

fail() {
  echo "$1" >&2
  exit 1
}

if ! git -C "$contracts_repo" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  fail "FEAT-136 contract source is not a Git repository: $contracts_repo"
fi
if [ -n "$(git -C "$contracts_repo" status --porcelain)" ]; then
  fail "FEAT-136 contract source must be clean: $contracts_repo"
fi

contracts_commit="$(git -C "$contracts_repo" rev-parse --verify "${requested_ref}^{commit}" 2>/dev/null || true)"
if [ "$contracts_commit" != "$required_commit" ]; then
  fail "FEAT-136 requires exact Contracts commit $required_commit; $requested_ref resolved to ${contracts_commit:-nothing}."
fi

managed_targets=(
  api/contracts.lock
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
  api/fixtures/agent/session-event-v4
  api/fixtures/agent/session-event-v5
  internal/contracts/agenthost.gen.go
)
if [ -n "$(git -C "$repo_root" status --porcelain --untracked-files=all -- "${managed_targets[@]}")" ]; then
  fail "FEAT-136 scoped contract targets contain uncommitted changes."
fi

lock_value() {
  local key="$1"
  local count
  count="$(awk -F= -v key="$key" '$1 == key { count += 1 } END { print count + 0 }' "$lock_file")"
  if [ "$count" -ne 1 ]; then
    fail "Existing contract lock must contain exactly one $key entry."
  fi
  awk -F= -v key="$key" '$1 == key { sub(/^[^=]*=/, ""); print }' "$lock_file"
}

# These values describe pre-existing fixture snapshots that this scoped sync is
# deliberately forbidden to read. Preserve their reviewed digests byte-for-byte.
preserved_skill_v1_snapshot="$(lock_value SKILL_BUNDLE_FIXTURE_SNAPSHOT_SHA256)"
preserved_skill_v2_snapshot="$(lock_value SKILL_BUNDLE_V2_FIXTURE_SNAPSHOT_SHA256)"
preserved_host_skills_snapshot="$(lock_value HOST_SKILLS_V1_FIXTURE_SNAPSHOT_SHA256)"
preserved_video_source="$(lock_value SYNTHETIC_VIDEO_FIXTURE_SOURCE)"
preserved_video_source_sha256="$(lock_value SYNTHETIC_VIDEO_FIXTURE_SOURCE_SHA256)"
preserved_video_raw_size="$(lock_value SYNTHETIC_VIDEO_FIXTURE_RAW_SIZE)"
preserved_video_raw_sha256="$(lock_value SYNTHETIC_VIDEO_FIXTURE_RAW_SHA256)"
preserved_video_snapshot="$(lock_value SYNTHETIC_VIDEO_FIXTURE_SNAPSHOT)"
preserved_video_snapshot_sha256="$(lock_value SYNTHETIC_VIDEO_FIXTURE_SNAPSHOT_SHA256)"

[ "$preserved_skill_v1_snapshot" = "be149b821586cc974fe62a09de92a83bddc6f11500839582e2b5b6f9b05b2b4a" ] ||
  fail "Refusing to replace the reviewed Skill Bundle v1 fixture snapshot digest."
[ "$preserved_skill_v2_snapshot" = "139f5c999e6ae98f4c47b68e93ff9439a97aa3c5c1372ca99658ad2e49c728dd" ] ||
  fail "Refusing to replace the reviewed Skill Bundle v2 fixture snapshot digest."
[ "$preserved_host_skills_snapshot" = "c6cf199b18b09f1e73b4594d6844f0f8fa7858761ba1f3bcd089f6644a24727f" ] ||
  fail "Refusing to replace the reviewed Host Skills fixture snapshot digest."
[ "$preserved_video_source" = "tests/fixtures/agent/resources-v3/synthetic-video-16x16.mp4.base64" ] ||
  fail "Refusing to replace the reviewed synthetic video source path."
[ "$preserved_video_source_sha256" = "d8a7a55c786dcde9367125d806e0cf5b014fb8f70801c97b80a70d5160f95f8a" ] ||
  fail "Refusing to replace the reviewed synthetic video source digest."
[ "$preserved_video_raw_size" = "1642" ] || fail "Refusing to replace the reviewed synthetic video size."
[ "$preserved_video_raw_sha256" = "96ea070cac612d17927939c22f3c0c593fb26b171f62c4e9cee43fb596177dd5" ] ||
  fail "Refusing to replace the reviewed synthetic video raw digest."
[ "$preserved_video_snapshot" = "internal/session/fixtures/synthetic-video-16x16.mp4.base64" ] ||
  fail "Refusing to replace the reviewed synthetic video snapshot path."
[ "$preserved_video_snapshot_sha256" = "$preserved_video_source_sha256" ] ||
  fail "Refusing to replace the reviewed synthetic video snapshot digest."

# Excluded trees may contain archives, checksum-corruption cases, Zip Slip, or
# archive-error fixtures. The only permitted operation is resolving each tree
# object ID from the immutable commit; no blob below these paths is read.
skill_v1_tree="$(git -C "$contracts_repo" rev-parse "$contracts_commit:tests/fixtures/skills/bundle-v1")"
skill_v2_tree="$(git -C "$contracts_repo" rev-parse "$contracts_commit:tests/fixtures/skills/bundle-v2")"
host_skills_tree="$(git -C "$contracts_repo" rev-parse "$contracts_commit:tests/fixtures/agent/host-skills-v1")"
video_tree="$(git -C "$contracts_repo" rev-parse "$contracts_commit:tests/fixtures/agent/resources-v3")"

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

assert_source_fixture_set() {
  local source_prefix="$1"
  shift
  local expected actual
  expected="$(printf '%s\n' "$@" | LC_ALL=C sort)"
  actual="$(git -C "$contracts_repo" ls-tree -r --name-only "$contracts_commit" -- "$source_prefix" |
    sed "s#^$source_prefix/##" | LC_ALL=C sort)"
  if [ "$actual" != "$expected" ]; then
    fail "FEAT-136 ordinary JSON fixture set drifted: $source_prefix"
  fi
}

assert_source_fixture_set "tests/fixtures/agent/session-event-v4" "${v4_fixture_files[@]}"
assert_source_fixture_set "tests/fixtures/agent/session-event-v5" "${v5_fixture_files[@]}"

temporary_dir="$(mktemp -d)"
trap 'rm -rf -- "$temporary_dir"' EXIT
source_package="$temporary_dir/package.json"
git -C "$contracts_repo" show "$contracts_commit:package.json" >"$source_package"

contracts_version="$(awk -F '"' '/^[[:space:]]*"version"[[:space:]]*:/ { print $4; exit }' "$source_package")"
if [ "$contracts_version" != "$required_version" ]; then
  fail "FEAT-136 Contracts package version is $contracts_version, expected $required_version."
fi

for index in "${!source_paths[@]}"; do
  source_path="${source_paths[$index]}"
  snapshot_path="${snapshot_paths[$index]}"
  temporary_source="$temporary_dir/source-$index"
  git -C "$contracts_repo" show "$contracts_commit:$source_path" >"$temporary_source"
  mkdir -p "$(dirname "$repo_root/$snapshot_path")"
  cp "$temporary_source" "$repo_root/$snapshot_path"
done

compatibility_version="$(awk -F '"' '/^[[:space:]]*"contracts_version"[[:space:]]*:/ { print $4; exit }' \
  "$repo_root/api/compatibility/agent-host-runtime-v1.json")"
if [ "$compatibility_version" != "$required_version" ]; then
  fail "FEAT-136 Runtime compatibility version is $compatibility_version, expected $required_version."
fi

mkdir -p "$repo_root/api/fixtures/agent/session-event-v4" "$repo_root/api/fixtures/agent/session-event-v5"
for relative in "${v4_fixture_files[@]}"; do
  git -C "$contracts_repo" show \
    "$contracts_commit:tests/fixtures/agent/session-event-v4/$relative" \
    >"$temporary_dir/v4-$relative"
  cp "$temporary_dir/v4-$relative" "$repo_root/api/fixtures/agent/session-event-v4/$relative"
done
for relative in "${v5_fixture_files[@]}"; do
  git -C "$contracts_repo" show \
    "$contracts_commit:tests/fixtures/agent/session-event-v5/$relative" \
    >"$temporary_dir/v5-$relative"
  cp "$temporary_dir/v5-$relative" "$repo_root/api/fixtures/agent/session-event-v5/$relative"
done

generator_version="$(go tool oapi-codegen --version | tail -n 1 | tr -d '\r')"
if [[ ! "$generator_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]]; then
  fail "Unable to determine the oapi-codegen generator version."
fi

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

temporary_lock="$temporary_dir/contracts.lock"
{
  printf 'CONTRACTS_VERSION=%s\n' "$required_version"
  printf 'CONTRACTS_REF=%s\n' "$required_commit"
  printf 'CONTRACTS_COMMIT=%s\n' "$required_commit"
  printf 'CONTRACTS_GENERATOR=%s\n' "$generator"
  printf 'CONTRACTS_GENERATOR_VERSION=%s\n' "$generator_version"
  printf 'FEAT136_SCOPED_SYNC_VERSION=1\n'
  printf 'FEAT136_SCOPED_SOURCES=ordinary-openapi-schema-session-event-v4-v5-json\n'
  printf 'EXCLUDED_FIXTURE_POLICY=git-object-id-only-preserve-existing-snapshot-digests\n'
  printf 'EXCLUDED_FIXTURE_BASELINE_COMMIT=%s\n' "$legacy_fixture_baseline_commit"
  printf 'OPENAPI_SHA256=%s\n' "$(sha256_file "$repo_root/api/openapi/agent-host.yaml")"
  printf 'RUNTIME_COMPATIBILITY_SHA256=%s\n' "$(sha256_file "$repo_root/api/compatibility/agent-host-runtime-v1.json")"
  printf 'AGENT_SESSION_EVENT_SCHEMA_SHA256=%s\n' "$(sha256_file "$repo_root/api/jsonschema/agent-session-event.schema.json")"
  printf 'AGENT_SESSION_EVENT_V2_SCHEMA_SHA256=%s\n' "$(sha256_file "$repo_root/api/jsonschema/agent-session-event-v2.schema.json")"
  printf 'AGENT_SESSION_EVENT_V3_SCHEMA_SHA256=%s\n' "$(sha256_file "$repo_root/api/jsonschema/agent-session-event-v3.schema.json")"
  printf 'AGENT_SESSION_EVENT_V4_SCHEMA_SHA256=%s\n' "$(sha256_file "$repo_root/api/jsonschema/agent-session-event-v4.schema.json")"
  printf 'AGENT_SESSION_EVENT_V5_SCHEMA_SHA256=%s\n' "$(sha256_file "$repo_root/api/jsonschema/agent-session-event-v5.schema.json")"
  printf 'REPORT_DOCUMENT_V1_SCHEMA_SHA256=%s\n' "$(sha256_file "$repo_root/api/jsonschema/report-document-v1.schema.json")"
  printf 'SKILL_BUNDLE_MANIFEST_V1_SCHEMA_SHA256=%s\n' "$(sha256_file "$repo_root/api/jsonschema/skill-bundle-manifest-v1.schema.json")"
  printf 'SKILL_BUNDLE_MANIFEST_V2_SCHEMA_SHA256=%s\n' "$(sha256_file "$repo_root/api/jsonschema/skill-bundle-manifest-v2.schema.json")"
  printf 'SKILL_BUNDLE_FIXTURE_TREE=%s\n' "$skill_v1_tree"
  printf 'SKILL_BUNDLE_FIXTURE_SNAPSHOT_SHA256=%s\n' "$preserved_skill_v1_snapshot"
  printf 'SKILL_BUNDLE_V2_FIXTURE_TREE=%s\n' "$skill_v2_tree"
  printf 'SKILL_BUNDLE_V2_FIXTURE_SNAPSHOT_SHA256=%s\n' "$preserved_skill_v2_snapshot"
  printf 'HOST_SKILLS_V1_FIXTURE_TREE=%s\n' "$host_skills_tree"
  printf 'HOST_SKILLS_V1_FIXTURE_SNAPSHOT_SHA256=%s\n' "$preserved_host_skills_snapshot"
  printf 'AGENT_SESSION_EVENT_V4_FIXTURE_TREE=%s\n' \
    "$(git -C "$contracts_repo" rev-parse "$contracts_commit:tests/fixtures/agent/session-event-v4")"
  printf 'AGENT_SESSION_EVENT_V4_FIXTURE_SNAPSHOT_SHA256=%s\n' \
    "$(fixture_snapshot_sha256 "$repo_root/api/fixtures/agent/session-event-v4" "${v4_fixture_files[@]}")"
  printf 'AGENT_SESSION_EVENT_V5_FIXTURE_TREE=%s\n' \
    "$(git -C "$contracts_repo" rev-parse "$contracts_commit:tests/fixtures/agent/session-event-v5")"
  printf 'AGENT_SESSION_EVENT_V5_FIXTURE_SNAPSHOT_SHA256=%s\n' \
    "$(fixture_snapshot_sha256 "$repo_root/api/fixtures/agent/session-event-v5" "${v5_fixture_files[@]}")"
  printf 'SYNTHETIC_VIDEO_FIXTURE_SOURCE=%s\n' "$preserved_video_source"
  printf 'SYNTHETIC_VIDEO_FIXTURE_TREE=%s\n' "$video_tree"
  printf 'SYNTHETIC_VIDEO_FIXTURE_SOURCE_SHA256=%s\n' "$preserved_video_source_sha256"
  printf 'SYNTHETIC_VIDEO_FIXTURE_RAW_SIZE=%s\n' "$preserved_video_raw_size"
  printf 'SYNTHETIC_VIDEO_FIXTURE_RAW_SHA256=%s\n' "$preserved_video_raw_sha256"
  printf 'SYNTHETIC_VIDEO_FIXTURE_SNAPSHOT=%s\n' "$preserved_video_snapshot"
  printf 'SYNTHETIC_VIDEO_FIXTURE_SNAPSHOT_SHA256=%s\n' "$preserved_video_snapshot_sha256"
} >"$temporary_lock"
cp "$temporary_lock" "$lock_file"

echo "Synchronized FEAT-136 ordinary Contracts snapshots from $required_commit (v$required_version)."
echo "Excluded archive/checksum/Zip-Slip/archive-error fixtures were not read; only immutable Git object IDs were resolved."
