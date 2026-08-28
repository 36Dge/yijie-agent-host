#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
lock_file="$repo_root/api/contracts.lock"
snapshot_openapi="$repo_root/api/openapi/agent-host.yaml"
snapshot_compatibility="$repo_root/api/compatibility/agent-host-runtime-v1.json"
snapshot_event_schema="$repo_root/api/jsonschema/agent-session-event.schema.json"
snapshot_event_v2_schema="$repo_root/api/jsonschema/agent-session-event-v2.schema.json"
snapshot_event_v3_schema="$repo_root/api/jsonschema/agent-session-event-v3.schema.json"
snapshot_event_v4_schema="$repo_root/api/jsonschema/agent-session-event-v4.schema.json"
snapshot_report_v1_schema="$repo_root/api/jsonschema/report-document-v1.schema.json"
snapshot_skill_bundle_v1_schema="$repo_root/api/jsonschema/skill-bundle-manifest-v1.schema.json"
snapshot_skill_bundle_v2_schema="$repo_root/api/jsonschema/skill-bundle-manifest-v2.schema.json"
snapshot_skill_v1_fixtures="$repo_root/api/fixtures/skills/bundle-v1"
snapshot_skill_v2_fixtures="$repo_root/api/fixtures/skills/bundle-v2"
snapshot_host_skill_fixtures="$repo_root/api/fixtures/agent/host-skills-v1"
snapshot_event_v4_fixtures="$repo_root/api/fixtures/agent/session-event-v4"
snapshot_synthetic_video="$repo_root/internal/session/fixtures/synthetic-video-16x16.mp4.base64"
skill_v1_fixture_files=(
  manifest-valid.json
  manifest-checksum-mismatch.json
  manifest-zip-slip.json
  packages/fixture-model-only-0.1.0.zip
  packages/fixture-model-only-checksum-mismatch.zip
  packages/fixture-zip-slip.zip
)
skill_v2_fixture_files=(
  manifest-catalog-38.json
  packages/fixture-copywriting-0.1.0.zip
)
host_skill_fixture_files=(
  scan-request.json
  install-request.json
  enabled-request.json
  uninstall-request.json
  list-response.json
  mutation-response.json
  error-archive-unsafe.json
  error-skill-not-installable.json
)
event_v4_fixture_files=(
  agent-message-commentary-started.json
  agent-message-final-completed.json
  agent-message-null-phase-started.json
  turn-plan-cleared.json
  turn-plan-updated.json
)

if [ ! -f "$lock_file" ] || [ ! -f "$snapshot_openapi" ] || [ ! -f "$snapshot_compatibility" ] ||
  [ ! -f "$snapshot_event_schema" ] || [ ! -f "$snapshot_event_v2_schema" ] ||
  [ ! -f "$snapshot_event_v3_schema" ] || [ ! -f "$snapshot_event_v4_schema" ] || [ ! -f "$snapshot_report_v1_schema" ] ||
  [ ! -f "$snapshot_skill_bundle_v1_schema" ] || [ ! -f "$snapshot_skill_bundle_v2_schema" ] ||
  [ ! -f "$snapshot_synthetic_video" ]; then
  echo "Agent Host contract snapshot is incomplete; run make sync-contracts." >&2
  exit 1
fi
for relative in "${skill_v1_fixture_files[@]}"; do
  if [ ! -f "$snapshot_skill_v1_fixtures/$relative" ]; then
    echo "Agent Host Skill fixture snapshot is incomplete; run make sync-contracts." >&2
    exit 1
  fi
done
for relative in "${skill_v2_fixture_files[@]}"; do
  if [ ! -f "$snapshot_skill_v2_fixtures/$relative" ]; then
    echo "Agent Host Skill v2 fixture snapshot is incomplete; run make sync-contracts." >&2
    exit 1
  fi
done
for relative in "${host_skill_fixture_files[@]}"; do
  if [ ! -f "$snapshot_host_skill_fixtures/$relative" ]; then
    echo "Agent Host Skill API fixture snapshot is incomplete; run make sync-contracts." >&2
    exit 1
  fi
done
for relative in "${event_v4_fixture_files[@]}"; do
  if [ ! -f "$snapshot_event_v4_fixtures/$relative" ]; then
    echo "Agent Host v4 event fixture snapshot is incomplete; run make sync-contracts." >&2
    exit 1
  fi
done

lock_value() {
  local key="$1"
  local count
  count="$(awk -F= -v key="$key" '$1 == key { count += 1 } END { print count + 0 }' "$lock_file")"
  if [ "$count" -ne 1 ]; then
    echo "Agent Host contract lock must contain exactly one $key entry." >&2
    exit 1
  fi
  awk -F= -v key="$key" '$1 == key { sub(/^[^=]*=/, ""); print }' "$lock_file"
}

CONTRACTS_VERSION="$(lock_value CONTRACTS_VERSION)"
CONTRACTS_REF="$(lock_value CONTRACTS_REF)"
CONTRACTS_COMMIT="$(lock_value CONTRACTS_COMMIT)"
CONTRACTS_GENERATOR="$(lock_value CONTRACTS_GENERATOR)"
CONTRACTS_GENERATOR_VERSION="$(lock_value CONTRACTS_GENERATOR_VERSION)"
OPENAPI_SHA256="$(lock_value OPENAPI_SHA256)"
RUNTIME_COMPATIBILITY_SHA256="$(lock_value RUNTIME_COMPATIBILITY_SHA256)"
AGENT_SESSION_EVENT_SCHEMA_SHA256="$(lock_value AGENT_SESSION_EVENT_SCHEMA_SHA256)"
AGENT_SESSION_EVENT_V2_SCHEMA_SHA256="$(lock_value AGENT_SESSION_EVENT_V2_SCHEMA_SHA256)"
AGENT_SESSION_EVENT_V3_SCHEMA_SHA256="$(lock_value AGENT_SESSION_EVENT_V3_SCHEMA_SHA256)"
AGENT_SESSION_EVENT_V4_SCHEMA_SHA256="$(lock_value AGENT_SESSION_EVENT_V4_SCHEMA_SHA256)"
REPORT_DOCUMENT_V1_SCHEMA_SHA256="$(lock_value REPORT_DOCUMENT_V1_SCHEMA_SHA256)"
SKILL_BUNDLE_MANIFEST_V1_SCHEMA_SHA256="$(lock_value SKILL_BUNDLE_MANIFEST_V1_SCHEMA_SHA256)"
SKILL_BUNDLE_MANIFEST_V2_SCHEMA_SHA256="$(lock_value SKILL_BUNDLE_MANIFEST_V2_SCHEMA_SHA256)"
SKILL_BUNDLE_FIXTURE_TREE="$(lock_value SKILL_BUNDLE_FIXTURE_TREE)"
SKILL_BUNDLE_FIXTURE_SNAPSHOT_SHA256="$(lock_value SKILL_BUNDLE_FIXTURE_SNAPSHOT_SHA256)"
SKILL_BUNDLE_V2_FIXTURE_TREE="$(lock_value SKILL_BUNDLE_V2_FIXTURE_TREE)"
SKILL_BUNDLE_V2_FIXTURE_SNAPSHOT_SHA256="$(lock_value SKILL_BUNDLE_V2_FIXTURE_SNAPSHOT_SHA256)"
HOST_SKILLS_V1_FIXTURE_TREE="$(lock_value HOST_SKILLS_V1_FIXTURE_TREE)"
HOST_SKILLS_V1_FIXTURE_SNAPSHOT_SHA256="$(lock_value HOST_SKILLS_V1_FIXTURE_SNAPSHOT_SHA256)"
AGENT_SESSION_EVENT_V4_FIXTURE_TREE="$(lock_value AGENT_SESSION_EVENT_V4_FIXTURE_TREE)"
AGENT_SESSION_EVENT_V4_FIXTURE_SNAPSHOT_SHA256="$(lock_value AGENT_SESSION_EVENT_V4_FIXTURE_SNAPSHOT_SHA256)"
SYNTHETIC_VIDEO_FIXTURE_SOURCE="$(lock_value SYNTHETIC_VIDEO_FIXTURE_SOURCE)"
SYNTHETIC_VIDEO_FIXTURE_TREE="$(lock_value SYNTHETIC_VIDEO_FIXTURE_TREE)"
SYNTHETIC_VIDEO_FIXTURE_SOURCE_SHA256="$(lock_value SYNTHETIC_VIDEO_FIXTURE_SOURCE_SHA256)"
SYNTHETIC_VIDEO_FIXTURE_RAW_SIZE="$(lock_value SYNTHETIC_VIDEO_FIXTURE_RAW_SIZE)"
SYNTHETIC_VIDEO_FIXTURE_RAW_SHA256="$(lock_value SYNTHETIC_VIDEO_FIXTURE_RAW_SHA256)"
SYNTHETIC_VIDEO_FIXTURE_SNAPSHOT="$(lock_value SYNTHETIC_VIDEO_FIXTURE_SNAPSHOT)"
SYNTHETIC_VIDEO_FIXTURE_SNAPSHOT_SHA256="$(lock_value SYNTHETIC_VIDEO_FIXTURE_SNAPSHOT_SHA256)"
semver_pattern='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'
sha256_pattern='^[0-9a-f]{64}$'
full_commit_pattern='^[0-9a-f]{40}$'
generator_version_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'
expected_generator="github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen"
expected_video_source="tests/fixtures/agent/resources-v3/synthetic-video-16x16.mp4.base64"
expected_video_tree="f447129c08b9b39231e33698afc3f2fd875d6b14"
expected_video_source_sha256="d8a7a55c786dcde9367125d806e0cf5b014fb8f70801c97b80a70d5160f95f8a"
expected_video_raw_size="1642"
expected_video_raw_sha256="96ea070cac612d17927939c22f3c0c593fb26b171f62c4e9cee43fb596177dd5"
expected_video_snapshot="internal/session/fixtures/synthetic-video-16x16.mp4.base64"
if [[ ! "$CONTRACTS_VERSION" =~ $semver_pattern ]] ||
  { [ "$CONTRACTS_REF" != "$CONTRACTS_COMMIT" ] &&
    [ "$CONTRACTS_REF" != "contracts-v$CONTRACTS_VERSION" ]; } ||
  [[ ! "$CONTRACTS_COMMIT" =~ $full_commit_pattern ]] ||
  [ "$CONTRACTS_GENERATOR" != "$expected_generator" ] ||
  [[ ! "$CONTRACTS_GENERATOR_VERSION" =~ $generator_version_pattern ]] ||
  [[ ! "$OPENAPI_SHA256" =~ $sha256_pattern ]] ||
  [[ ! "$RUNTIME_COMPATIBILITY_SHA256" =~ $sha256_pattern ]] ||
  [[ ! "$AGENT_SESSION_EVENT_SCHEMA_SHA256" =~ $sha256_pattern ]]; then
  echo "Agent Host contract lock contains invalid provenance, generator, or digest metadata." >&2
  exit 1
fi
if [[ ! "$AGENT_SESSION_EVENT_V2_SCHEMA_SHA256" =~ $sha256_pattern ]]; then
  echo "Agent Host v2 event contract digest is invalid." >&2
  exit 1
fi
if [[ ! "$AGENT_SESSION_EVENT_V3_SCHEMA_SHA256" =~ $sha256_pattern ]] ||
  [[ ! "$AGENT_SESSION_EVENT_V4_SCHEMA_SHA256" =~ $sha256_pattern ]] ||
  [[ ! "$REPORT_DOCUMENT_V1_SCHEMA_SHA256" =~ $sha256_pattern ]] ||
  [[ ! "$SKILL_BUNDLE_MANIFEST_V1_SCHEMA_SHA256" =~ $sha256_pattern ]] ||
  [[ ! "$SKILL_BUNDLE_MANIFEST_V2_SCHEMA_SHA256" =~ $sha256_pattern ]] ||
  [[ ! "$SKILL_BUNDLE_FIXTURE_TREE" =~ $full_commit_pattern ]] ||
  [[ ! "$SKILL_BUNDLE_FIXTURE_SNAPSHOT_SHA256" =~ $sha256_pattern ]] ||
  [[ ! "$SKILL_BUNDLE_V2_FIXTURE_TREE" =~ $full_commit_pattern ]] ||
  [[ ! "$SKILL_BUNDLE_V2_FIXTURE_SNAPSHOT_SHA256" =~ $sha256_pattern ]] ||
  [[ ! "$HOST_SKILLS_V1_FIXTURE_TREE" =~ $full_commit_pattern ]] ||
  [[ ! "$HOST_SKILLS_V1_FIXTURE_SNAPSHOT_SHA256" =~ $sha256_pattern ]] ||
  [[ ! "$AGENT_SESSION_EVENT_V4_FIXTURE_TREE" =~ $full_commit_pattern ]] ||
  [[ ! "$AGENT_SESSION_EVENT_V4_FIXTURE_SNAPSHOT_SHA256" =~ $sha256_pattern ]]; then
  echo "Agent Host contract digests are invalid." >&2
  exit 1
fi
if [ "$SYNTHETIC_VIDEO_FIXTURE_SOURCE" != "$expected_video_source" ] ||
  [ "$SYNTHETIC_VIDEO_FIXTURE_TREE" != "$expected_video_tree" ] ||
  [ "$SYNTHETIC_VIDEO_FIXTURE_SOURCE_SHA256" != "$expected_video_source_sha256" ] ||
  [ "$SYNTHETIC_VIDEO_FIXTURE_RAW_SIZE" != "$expected_video_raw_size" ] ||
  [ "$SYNTHETIC_VIDEO_FIXTURE_RAW_SHA256" != "$expected_video_raw_sha256" ] ||
  [ "$SYNTHETIC_VIDEO_FIXTURE_SNAPSHOT" != "$expected_video_snapshot" ] ||
  [ "$SYNTHETIC_VIDEO_FIXTURE_SNAPSHOT_SHA256" != "$expected_video_source_sha256" ]; then
  echo "Agent Host synthetic video fixture provenance does not match the immutable FEAT-128 resource." >&2
  exit 1
fi

actual_generator_version="$(go tool oapi-codegen --version | tail -n 1 | tr -d '\r')"
if [ "$actual_generator_version" != "$CONTRACTS_GENERATOR_VERSION" ]; then
  echo "Agent Host contract generator version does not match api/contracts.lock." >&2
  exit 1
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
    if command -v shasum >/dev/null 2>&1; then shasum -a 256 | awk '{ print $1 }'; else sha256sum | awk '{ print $1 }'; fi
}

assert_snapshot_fixture_set() {
  local root="$1"
  shift
  local expected actual
  expected="$(printf '%s\n' "$@" | LC_ALL=C sort)"
  actual="$(cd "$root" && find . -mindepth 1 ! -type d -print | sed 's#^\./##' | LC_ALL=C sort)"
  if [ "$actual" != "$expected" ]; then
    echo "Agent Host fixture snapshot contains a missing or unexpected file: $root" >&2
    exit 1
  fi
}

decode_base64_file() {
  local source="$1"
  local destination="$2"
  if base64 --decode "$source" >"$destination" 2>/dev/null; then
    return
  fi
  if base64 -D -i "$source" -o "$destination" 2>/dev/null; then
    return
  fi
  echo "Unable to decode the Agent Host synthetic video fixture." >&2
  exit 1
}

if [ "$(sha256_file "$snapshot_openapi")" != "$OPENAPI_SHA256" ]; then
  echo "Agent Host OpenAPI snapshot hash does not match api/contracts.lock." >&2
  exit 1
fi
if [ "$(sha256_file "$snapshot_compatibility")" != "$RUNTIME_COMPATIBILITY_SHA256" ]; then
  echo "Runtime compatibility snapshot hash does not match api/contracts.lock." >&2
  exit 1
fi
if [ "$(sha256_file "$snapshot_event_schema")" != "$AGENT_SESSION_EVENT_SCHEMA_SHA256" ]; then
  echo "Agent session event JSON Schema snapshot hash does not match api/contracts.lock." >&2
  exit 1
fi
if [ "$(sha256_file "$snapshot_event_v2_schema")" != "$AGENT_SESSION_EVENT_V2_SCHEMA_SHA256" ]; then
  echo "Agent session event v2 JSON Schema snapshot hash does not match api/contracts.lock." >&2
  exit 1
fi
if [ "$(sha256_file "$snapshot_event_v3_schema")" != "$AGENT_SESSION_EVENT_V3_SCHEMA_SHA256" ]; then
  echo "Agent session event v3 JSON Schema snapshot hash does not match api/contracts.lock." >&2
  exit 1
fi
if [ "$(sha256_file "$snapshot_event_v4_schema")" != "$AGENT_SESSION_EVENT_V4_SCHEMA_SHA256" ]; then
  echo "Agent session event v4 JSON Schema snapshot hash does not match api/contracts.lock." >&2
  exit 1
fi
if [ "$(sha256_file "$snapshot_report_v1_schema")" != "$REPORT_DOCUMENT_V1_SCHEMA_SHA256" ]; then
  echo "Report document v1 JSON Schema snapshot hash does not match api/contracts.lock." >&2
  exit 1
fi
if [ "$(sha256_file "$snapshot_skill_bundle_v1_schema")" != "$SKILL_BUNDLE_MANIFEST_V1_SCHEMA_SHA256" ]; then
  echo "Skill Bundle Manifest v1 JSON Schema snapshot hash does not match api/contracts.lock." >&2
  exit 1
fi
if [ "$(sha256_file "$snapshot_skill_bundle_v2_schema")" != "$SKILL_BUNDLE_MANIFEST_V2_SCHEMA_SHA256" ]; then
  echo "Skill Bundle Manifest v2 JSON Schema snapshot hash does not match api/contracts.lock." >&2
  exit 1
fi
assert_snapshot_fixture_set "$snapshot_skill_v1_fixtures" "${skill_v1_fixture_files[@]}"
assert_snapshot_fixture_set "$snapshot_skill_v2_fixtures" "${skill_v2_fixture_files[@]}"
assert_snapshot_fixture_set "$snapshot_host_skill_fixtures" "${host_skill_fixture_files[@]}"
assert_snapshot_fixture_set "$snapshot_event_v4_fixtures" "${event_v4_fixture_files[@]}"
if [ "$(fixture_snapshot_sha256 "$snapshot_skill_v1_fixtures" "${skill_v1_fixture_files[@]}")" != "$SKILL_BUNDLE_FIXTURE_SNAPSHOT_SHA256" ]; then
  echo "Skill Bundle fixture snapshot digest does not match api/contracts.lock." >&2
  exit 1
fi
if [ "$(fixture_snapshot_sha256 "$snapshot_skill_v2_fixtures" "${skill_v2_fixture_files[@]}")" != "$SKILL_BUNDLE_V2_FIXTURE_SNAPSHOT_SHA256" ]; then
  echo "Skill Bundle v2 fixture snapshot digest does not match api/contracts.lock." >&2
  exit 1
fi
if [ "$(fixture_snapshot_sha256 "$snapshot_host_skill_fixtures" "${host_skill_fixture_files[@]}")" != "$HOST_SKILLS_V1_FIXTURE_SNAPSHOT_SHA256" ]; then
  echo "Host Skill API fixture snapshot digest does not match api/contracts.lock." >&2
  exit 1
fi
if [ "$(fixture_snapshot_sha256 "$snapshot_event_v4_fixtures" "${event_v4_fixture_files[@]}")" != "$AGENT_SESSION_EVENT_V4_FIXTURE_SNAPSHOT_SHA256" ]; then
  echo "Agent session event v4 fixture snapshot digest does not match api/contracts.lock." >&2
  exit 1
fi
if [ "$(sha256_file "$snapshot_synthetic_video")" != "$SYNTHETIC_VIDEO_FIXTURE_SNAPSHOT_SHA256" ]; then
  echo "Synthetic video snapshot hash does not match api/contracts.lock." >&2
  exit 1
fi

temporary_dir="$(mktemp -d)"
trap 'rm -rf "$temporary_dir"' EXIT
decoded_synthetic_video="$temporary_dir/synthetic-video-16x16.mp4"
decode_base64_file "$snapshot_synthetic_video" "$decoded_synthetic_video"
actual_video_size="$(wc -c <"$decoded_synthetic_video" | tr -d '[:space:]')"
if [ "$actual_video_size" != "$SYNTHETIC_VIDEO_FIXTURE_RAW_SIZE" ] ||
  [ "$(sha256_file "$decoded_synthetic_video")" != "$SYNTHETIC_VIDEO_FIXTURE_RAW_SHA256" ]; then
  echo "Synthetic video snapshot raw bytes do not match api/contracts.lock." >&2
  exit 1
fi
contracts_repo="${YIJIE_CONTRACTS_REPO:-$repo_root/../yijie-contracts}"
if git -C "$contracts_repo" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  resolved_commit="$(git -C "$contracts_repo" rev-parse --verify "${CONTRACTS_REF}^{commit}" 2>/dev/null || true)"
  if [ "$resolved_commit" != "$CONTRACTS_COMMIT" ]; then
    echo "Agent Host contract ref does not resolve to the locked commit." >&2
    exit 1
  fi

  source_openapi="$temporary_dir/agent-host.yaml"
  source_compatibility="$temporary_dir/agent-host-runtime-v1.json"
  source_event_schema="$temporary_dir/agent-session-event.schema.json"
  source_event_v2_schema="$temporary_dir/agent-session-event-v2.schema.json"
  source_event_v3_schema="$temporary_dir/agent-session-event-v3.schema.json"
  source_event_v4_schema="$temporary_dir/agent-session-event-v4.schema.json"
  source_report_v1_schema="$temporary_dir/report-document-v1.schema.json"
  source_skill_bundle_v1_schema="$temporary_dir/skill-bundle-manifest-v1.schema.json"
  source_skill_bundle_v2_schema="$temporary_dir/skill-bundle-manifest-v2.schema.json"
  source_skill_v1_fixtures="$temporary_dir/skill-bundle-v1"
  source_skill_v2_fixtures="$temporary_dir/skill-bundle-v2"
  source_host_skill_fixtures="$temporary_dir/host-skills-v1"
  source_event_v4_fixtures="$temporary_dir/session-event-v4"
  source_synthetic_video="$temporary_dir/synthetic-video-16x16.mp4.base64"
  source_package="$temporary_dir/package.json"
  git -C "$contracts_repo" show "$CONTRACTS_COMMIT:openapi/agent-host/agent-host.yaml" >"$source_openapi"
  git -C "$contracts_repo" show "$CONTRACTS_COMMIT:compatibility/agent-host-runtime-v1.json" >"$source_compatibility"
  git -C "$contracts_repo" show "$CONTRACTS_COMMIT:jsonschema/agent/session-event.schema.json" >"$source_event_schema"
  git -C "$contracts_repo" show "$CONTRACTS_COMMIT:jsonschema/agent/session-event-v2.schema.json" >"$source_event_v2_schema"
  git -C "$contracts_repo" show "$CONTRACTS_COMMIT:jsonschema/agent/session-event-v3.schema.json" >"$source_event_v3_schema"
  git -C "$contracts_repo" show "$CONTRACTS_COMMIT:jsonschema/agent/session-event-v4.schema.json" >"$source_event_v4_schema"
  git -C "$contracts_repo" show "$CONTRACTS_COMMIT:jsonschema/report/report-document-v1.schema.json" >"$source_report_v1_schema"
  git -C "$contracts_repo" show "$CONTRACTS_COMMIT:jsonschema/skills/skill-bundle-manifest-v1.schema.json" >"$source_skill_bundle_v1_schema"
  git -C "$contracts_repo" show "$CONTRACTS_COMMIT:jsonschema/skills/skill-bundle-manifest-v2.schema.json" >"$source_skill_bundle_v2_schema"
  mkdir -p "$source_skill_v1_fixtures/packages"
  for relative in "${skill_v1_fixture_files[@]}"; do
    git -C "$contracts_repo" show "$CONTRACTS_COMMIT:tests/fixtures/skills/bundle-v1/$relative" >"$source_skill_v1_fixtures/$relative"
  done
  mkdir -p "$source_skill_v2_fixtures/packages"
  for relative in "${skill_v2_fixture_files[@]}"; do
    git -C "$contracts_repo" show "$CONTRACTS_COMMIT:tests/fixtures/skills/bundle-v2/$relative" >"$source_skill_v2_fixtures/$relative"
  done
  mkdir -p "$source_host_skill_fixtures"
  for relative in "${host_skill_fixture_files[@]}"; do
    git -C "$contracts_repo" show "$CONTRACTS_COMMIT:tests/fixtures/agent/host-skills-v1/$relative" >"$source_host_skill_fixtures/$relative"
  done
  mkdir -p "$source_event_v4_fixtures"
  for relative in "${event_v4_fixture_files[@]}"; do
    git -C "$contracts_repo" show "$CONTRACTS_COMMIT:tests/fixtures/agent/session-event-v4/$relative" >"$source_event_v4_fixtures/$relative"
  done
  git -C "$contracts_repo" show "$CONTRACTS_COMMIT:$SYNTHETIC_VIDEO_FIXTURE_SOURCE" >"$source_synthetic_video"
  git -C "$contracts_repo" show "$CONTRACTS_COMMIT:package.json" >"$source_package"

  cmp "$source_openapi" "$snapshot_openapi"
  cmp "$source_compatibility" "$snapshot_compatibility"
  cmp "$source_event_schema" "$snapshot_event_schema"
  cmp "$source_event_v2_schema" "$snapshot_event_v2_schema"
  cmp "$source_event_v3_schema" "$snapshot_event_v3_schema"
  cmp "$source_event_v4_schema" "$snapshot_event_v4_schema"
  cmp "$source_report_v1_schema" "$snapshot_report_v1_schema"
  cmp "$source_skill_bundle_v1_schema" "$snapshot_skill_bundle_v1_schema"
  cmp "$source_skill_bundle_v2_schema" "$snapshot_skill_bundle_v2_schema"
  for relative in "${skill_v1_fixture_files[@]}"; do
    cmp "$source_skill_v1_fixtures/$relative" "$snapshot_skill_v1_fixtures/$relative"
  done
  for relative in "${skill_v2_fixture_files[@]}"; do
    cmp "$source_skill_v2_fixtures/$relative" "$snapshot_skill_v2_fixtures/$relative"
  done
  for relative in "${host_skill_fixture_files[@]}"; do
    cmp "$source_host_skill_fixtures/$relative" "$snapshot_host_skill_fixtures/$relative"
  done
  for relative in "${event_v4_fixture_files[@]}"; do
    cmp "$source_event_v4_fixtures/$relative" "$snapshot_event_v4_fixtures/$relative"
  done
  cmp "$source_synthetic_video" "$snapshot_synthetic_video"
  if [ "$(sha256_file "$source_synthetic_video")" != "$SYNTHETIC_VIDEO_FIXTURE_SOURCE_SHA256" ]; then
    echo "Contracts synthetic video source hash does not match api/contracts.lock." >&2
    exit 1
  fi
  source_video_tree="$(git -C "$contracts_repo" rev-parse "$CONTRACTS_COMMIT:tests/fixtures/agent/resources-v3")"
  if [ "$source_video_tree" != "$SYNTHETIC_VIDEO_FIXTURE_TREE" ]; then
    echo "Contracts synthetic video resource tree does not match api/contracts.lock." >&2
    exit 1
  fi
  source_skill_fixture_tree="$(git -C "$contracts_repo" rev-parse "$CONTRACTS_COMMIT:tests/fixtures/skills/bundle-v1")"
  if [ "$source_skill_fixture_tree" != "$SKILL_BUNDLE_FIXTURE_TREE" ]; then
    echo "Contracts Skill fixture tree does not match api/contracts.lock." >&2
    exit 1
  fi
  source_skill_v2_fixture_tree="$(git -C "$contracts_repo" rev-parse "$CONTRACTS_COMMIT:tests/fixtures/skills/bundle-v2")"
  if [ "$source_skill_v2_fixture_tree" != "$SKILL_BUNDLE_V2_FIXTURE_TREE" ]; then
    echo "Contracts Skill v2 fixture tree does not match api/contracts.lock." >&2
    exit 1
  fi
  source_host_skill_fixture_tree="$(git -C "$contracts_repo" rev-parse "$CONTRACTS_COMMIT:tests/fixtures/agent/host-skills-v1")"
  if [ "$source_host_skill_fixture_tree" != "$HOST_SKILLS_V1_FIXTURE_TREE" ]; then
    echo "Contracts Host Skill API fixture tree does not match api/contracts.lock." >&2
    exit 1
  fi
  source_event_v4_fixture_tree="$(git -C "$contracts_repo" rev-parse "$CONTRACTS_COMMIT:tests/fixtures/agent/session-event-v4")"
  if [ "$source_event_v4_fixture_tree" != "$AGENT_SESSION_EVENT_V4_FIXTURE_TREE" ]; then
    echo "Contracts Agent session event v4 fixture tree does not match api/contracts.lock." >&2
    exit 1
  fi
  source_version="$(awk -F '"' '/^[[:space:]]*"version"[[:space:]]*:/ { print $4; exit }' "$source_package")"
  if [ "$source_version" != "$CONTRACTS_VERSION" ]; then
    echo "Agent Host contract snapshot version is stale." >&2
    exit 1
  fi
fi

generated="$temporary_dir/agenthost.gen.go"
cd "$repo_root"
go tool oapi-codegen \
  -generate types \
  -package agenthostcontract \
  -o "$generated" \
  api/openapi/agent-host.yaml
gofmt -w "$generated"
cmp "$generated" internal/contracts/agenthost.gen.go

echo "Verified Agent Host contract snapshot $CONTRACTS_REF ($CONTRACTS_COMMIT) and $CONTRACTS_GENERATOR@$CONTRACTS_GENERATOR_VERSION."
