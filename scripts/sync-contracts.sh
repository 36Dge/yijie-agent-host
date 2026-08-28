#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
contracts_repo="${YIJIE_CONTRACTS_REPO:-$repo_root/../yijie-contracts}"
requested_ref="${YIJIE_CONTRACTS_REF:-HEAD}"
generator="github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen"

if ! git -C "$contracts_repo" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "Agent Host contract source is not a Git repository: $contracts_repo" >&2
  exit 2
fi
if [ -n "$(git -C "$contracts_repo" status --porcelain)" ]; then
  echo "Agent Host contract source must be clean before synchronization: $contracts_repo" >&2
  exit 1
fi
managed_targets=(
  api/contracts.lock
  api/openapi/agent-host.yaml
  api/compatibility/agent-host-runtime-v1.json
  api/jsonschema/agent-session-event.schema.json
  api/jsonschema/agent-session-event-v2.schema.json
  api/jsonschema/agent-session-event-v3.schema.json
  api/jsonschema/agent-session-event-v4.schema.json
  api/jsonschema/report-document-v1.schema.json
  api/jsonschema/skill-bundle-manifest-v1.schema.json
  api/jsonschema/skill-bundle-manifest-v2.schema.json
  api/fixtures/skills/bundle-v1
  api/fixtures/skills/bundle-v2
  api/fixtures/agent/host-skills-v1
  api/fixtures/agent/session-event-v4
  internal/contracts/agenthost.gen.go
  internal/session/fixtures/synthetic-video-16x16.mp4.base64
)
if [ -n "$(git -C "$repo_root" status --porcelain --untracked-files=all -- "${managed_targets[@]}")" ]; then
  echo "Agent Host contract targets contain uncommitted changes; synchronize in a clean worktree." >&2
  exit 1
fi

contracts_commit="$(git -C "$contracts_repo" rev-parse --verify "${requested_ref}^{commit}" 2>/dev/null || true)"
full_commit_pattern='^[0-9a-f]{40}$'
if [[ ! "$contracts_commit" =~ $full_commit_pattern ]]; then
  echo "Agent Host contract ref does not resolve to a full commit: $requested_ref" >&2
  exit 1
fi
if [ "$requested_ref" = "HEAD" ]; then
  contracts_ref="$contracts_commit"
else
  contracts_ref="$requested_ref"
fi

temporary_dir="$(mktemp -d)"
trap 'rm -rf "$temporary_dir"' EXIT
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
decoded_synthetic_video="$temporary_dir/synthetic-video-16x16.mp4"
source_package="$temporary_dir/package.json"
git -C "$contracts_repo" show "$contracts_commit:openapi/agent-host/agent-host.yaml" >"$source_openapi"
git -C "$contracts_repo" show "$contracts_commit:compatibility/agent-host-runtime-v1.json" >"$source_compatibility"
git -C "$contracts_repo" show "$contracts_commit:jsonschema/agent/session-event.schema.json" >"$source_event_schema"
git -C "$contracts_repo" show "$contracts_commit:jsonschema/agent/session-event-v2.schema.json" >"$source_event_v2_schema"
git -C "$contracts_repo" show "$contracts_commit:jsonschema/agent/session-event-v3.schema.json" >"$source_event_v3_schema"
git -C "$contracts_repo" show "$contracts_commit:jsonschema/agent/session-event-v4.schema.json" >"$source_event_v4_schema"
git -C "$contracts_repo" show "$contracts_commit:jsonschema/report/report-document-v1.schema.json" >"$source_report_v1_schema"
git -C "$contracts_repo" show "$contracts_commit:jsonschema/skills/skill-bundle-manifest-v1.schema.json" >"$source_skill_bundle_v1_schema"
git -C "$contracts_repo" show "$contracts_commit:jsonschema/skills/skill-bundle-manifest-v2.schema.json" >"$source_skill_bundle_v2_schema"
mkdir -p "$source_skill_v1_fixtures/packages"
skill_v1_fixture_files=(
  manifest-valid.json
  manifest-checksum-mismatch.json
  manifest-zip-slip.json
  packages/fixture-model-only-0.1.0.zip
  packages/fixture-model-only-checksum-mismatch.zip
  packages/fixture-zip-slip.zip
)
for relative in "${skill_v1_fixture_files[@]}"; do
  git -C "$contracts_repo" show "$contracts_commit:tests/fixtures/skills/bundle-v1/$relative" >"$source_skill_v1_fixtures/$relative"
done
mkdir -p "$source_skill_v2_fixtures/packages"
skill_v2_fixture_files=(
  manifest-catalog-38.json
  packages/fixture-copywriting-0.1.0.zip
)
for relative in "${skill_v2_fixture_files[@]}"; do
  git -C "$contracts_repo" show "$contracts_commit:tests/fixtures/skills/bundle-v2/$relative" >"$source_skill_v2_fixtures/$relative"
done
mkdir -p "$source_host_skill_fixtures"
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
for relative in "${host_skill_fixture_files[@]}"; do
  git -C "$contracts_repo" show "$contracts_commit:tests/fixtures/agent/host-skills-v1/$relative" >"$source_host_skill_fixtures/$relative"
done
mkdir -p "$source_event_v4_fixtures"
event_v4_fixture_files=(
  agent-message-commentary-started.json
  agent-message-final-completed.json
  agent-message-null-phase-started.json
  turn-plan-cleared.json
  turn-plan-updated.json
)
for relative in "${event_v4_fixture_files[@]}"; do
  git -C "$contracts_repo" show "$contracts_commit:tests/fixtures/agent/session-event-v4/$relative" >"$source_event_v4_fixtures/$relative"
done
git -C "$contracts_repo" show "$contracts_commit:tests/fixtures/agent/resources-v3/synthetic-video-16x16.mp4.base64" >"$source_synthetic_video"
git -C "$contracts_repo" show "$contracts_commit:package.json" >"$source_package"

contracts_version="$(awk -F '"' '/^[[:space:]]*"version"[[:space:]]*:/ { print $4; exit }' "$source_package")"
compatibility_version="$(awk -F '"' '/^[[:space:]]*"contracts_version"[[:space:]]*:/ { print $4; exit }' "$source_compatibility")"
semver_pattern='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'
if [[ ! "$contracts_version" =~ $semver_pattern ]] || [ "$contracts_version" != "$compatibility_version" ]; then
  echo "contracts package and Runtime compatibility versions do not match" >&2
  exit 1
fi
if [ "$contracts_ref" != "$contracts_commit" ] &&
  [ "$contracts_ref" != "contracts-v$contracts_version" ]; then
  echo "Agent Host contract ref must be the full candidate commit or release tag." >&2
  exit 1
fi

generator_version="$(go tool oapi-codegen --version | tail -n 1 | tr -d '\r')"
generator_version_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'
if [[ ! "$generator_version" =~ $generator_version_pattern ]]; then
  echo "Unable to determine the oapi-codegen generator version." >&2
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

assert_source_fixture_set() {
  local source_prefix="$1"
  shift
  local expected actual
  expected="$(printf '%s\n' "$@" | LC_ALL=C sort)"
  actual="$(git -C "$contracts_repo" ls-tree -r --name-only "$contracts_commit" -- "$source_prefix" |
    sed "s#^$source_prefix/##" | LC_ALL=C sort)"
  if [ "$actual" != "$expected" ]; then
    echo "Contracts fixture set drifted from the synchronized allowlist: $source_prefix" >&2
    exit 1
  fi
}

assert_source_fixture_set "tests/fixtures/skills/bundle-v1" "${skill_v1_fixture_files[@]}"
assert_source_fixture_set "tests/fixtures/skills/bundle-v2" "${skill_v2_fixture_files[@]}"
assert_source_fixture_set "tests/fixtures/agent/host-skills-v1" "${host_skill_fixture_files[@]}"
assert_source_fixture_set "tests/fixtures/agent/session-event-v4" "${event_v4_fixture_files[@]}"

decode_base64_file() {
  local source="$1"
  local destination="$2"
  if base64 --decode "$source" >"$destination" 2>/dev/null; then
    return
  fi
  if base64 -D -i "$source" -o "$destination" 2>/dev/null; then
    return
  fi
  echo "Unable to decode the Contracts synthetic video fixture." >&2
  exit 1
}

decode_base64_file "$source_synthetic_video" "$decoded_synthetic_video"
synthetic_video_tree="$(git -C "$contracts_repo" rev-parse "$contracts_commit:tests/fixtures/agent/resources-v3")"
synthetic_video_raw_size="$(wc -c <"$decoded_synthetic_video" | tr -d '[:space:]')"

mkdir -p "$repo_root/api/openapi" "$repo_root/api/compatibility" "$repo_root/api/jsonschema" \
  "$repo_root/api/fixtures/skills/bundle-v1/packages" "$repo_root/api/fixtures/skills/bundle-v2/packages" \
  "$repo_root/api/fixtures/agent/host-skills-v1" \
  "$repo_root/api/fixtures/agent/session-event-v4" \
  "$repo_root/internal/session/fixtures"
cp "$source_openapi" "$repo_root/api/openapi/agent-host.yaml"
cp "$source_compatibility" "$repo_root/api/compatibility/agent-host-runtime-v1.json"
cp "$source_event_schema" "$repo_root/api/jsonschema/agent-session-event.schema.json"
cp "$source_event_v2_schema" "$repo_root/api/jsonschema/agent-session-event-v2.schema.json"
cp "$source_event_v3_schema" "$repo_root/api/jsonschema/agent-session-event-v3.schema.json"
cp "$source_event_v4_schema" "$repo_root/api/jsonschema/agent-session-event-v4.schema.json"
cp "$source_report_v1_schema" "$repo_root/api/jsonschema/report-document-v1.schema.json"
cp "$source_skill_bundle_v1_schema" "$repo_root/api/jsonschema/skill-bundle-manifest-v1.schema.json"
cp "$source_skill_bundle_v2_schema" "$repo_root/api/jsonschema/skill-bundle-manifest-v2.schema.json"
for relative in "${skill_v1_fixture_files[@]}"; do
  cp "$source_skill_v1_fixtures/$relative" "$repo_root/api/fixtures/skills/bundle-v1/$relative"
done
for relative in "${skill_v2_fixture_files[@]}"; do
  cp "$source_skill_v2_fixtures/$relative" "$repo_root/api/fixtures/skills/bundle-v2/$relative"
done
for relative in "${host_skill_fixture_files[@]}"; do
  cp "$source_host_skill_fixtures/$relative" "$repo_root/api/fixtures/agent/host-skills-v1/$relative"
done
for relative in "${event_v4_fixture_files[@]}"; do
  cp "$source_event_v4_fixtures/$relative" "$repo_root/api/fixtures/agent/session-event-v4/$relative"
done
cp "$source_synthetic_video" "$repo_root/internal/session/fixtures/synthetic-video-16x16.mp4.base64"

lock_file="$repo_root/api/contracts.lock"
temporary_lock="$lock_file.tmp"
{
  printf 'CONTRACTS_VERSION=%s\n' "$contracts_version"
  printf 'CONTRACTS_REF=%s\n' "$contracts_ref"
  printf 'CONTRACTS_COMMIT=%s\n' "$contracts_commit"
  printf 'CONTRACTS_GENERATOR=%s\n' "$generator"
  printf 'CONTRACTS_GENERATOR_VERSION=%s\n' "$generator_version"
  printf 'OPENAPI_SHA256=%s\n' "$(sha256_file "$repo_root/api/openapi/agent-host.yaml")"
  printf 'RUNTIME_COMPATIBILITY_SHA256=%s\n' "$(sha256_file "$repo_root/api/compatibility/agent-host-runtime-v1.json")"
  printf 'AGENT_SESSION_EVENT_SCHEMA_SHA256=%s\n' "$(sha256_file "$repo_root/api/jsonschema/agent-session-event.schema.json")"
  printf 'AGENT_SESSION_EVENT_V2_SCHEMA_SHA256=%s\n' "$(sha256_file "$repo_root/api/jsonschema/agent-session-event-v2.schema.json")"
  printf 'AGENT_SESSION_EVENT_V3_SCHEMA_SHA256=%s\n' "$(sha256_file "$repo_root/api/jsonschema/agent-session-event-v3.schema.json")"
  printf 'AGENT_SESSION_EVENT_V4_SCHEMA_SHA256=%s\n' "$(sha256_file "$repo_root/api/jsonschema/agent-session-event-v4.schema.json")"
  printf 'REPORT_DOCUMENT_V1_SCHEMA_SHA256=%s\n' "$(sha256_file "$repo_root/api/jsonschema/report-document-v1.schema.json")"
  printf 'SKILL_BUNDLE_MANIFEST_V1_SCHEMA_SHA256=%s\n' "$(sha256_file "$repo_root/api/jsonschema/skill-bundle-manifest-v1.schema.json")"
  printf 'SKILL_BUNDLE_MANIFEST_V2_SCHEMA_SHA256=%s\n' "$(sha256_file "$repo_root/api/jsonschema/skill-bundle-manifest-v2.schema.json")"
  printf 'SKILL_BUNDLE_FIXTURE_TREE=%s\n' "$(git -C "$contracts_repo" rev-parse "$contracts_commit:tests/fixtures/skills/bundle-v1")"
  printf 'SKILL_BUNDLE_FIXTURE_SNAPSHOT_SHA256=%s\n' "$(fixture_snapshot_sha256 "$repo_root/api/fixtures/skills/bundle-v1" "${skill_v1_fixture_files[@]}")"
  printf 'SKILL_BUNDLE_V2_FIXTURE_TREE=%s\n' "$(git -C "$contracts_repo" rev-parse "$contracts_commit:tests/fixtures/skills/bundle-v2")"
  printf 'SKILL_BUNDLE_V2_FIXTURE_SNAPSHOT_SHA256=%s\n' "$(fixture_snapshot_sha256 "$repo_root/api/fixtures/skills/bundle-v2" "${skill_v2_fixture_files[@]}")"
  printf 'HOST_SKILLS_V1_FIXTURE_TREE=%s\n' "$(git -C "$contracts_repo" rev-parse "$contracts_commit:tests/fixtures/agent/host-skills-v1")"
  printf 'HOST_SKILLS_V1_FIXTURE_SNAPSHOT_SHA256=%s\n' "$(fixture_snapshot_sha256 "$repo_root/api/fixtures/agent/host-skills-v1" "${host_skill_fixture_files[@]}")"
  printf 'AGENT_SESSION_EVENT_V4_FIXTURE_TREE=%s\n' "$(git -C "$contracts_repo" rev-parse "$contracts_commit:tests/fixtures/agent/session-event-v4")"
  printf 'AGENT_SESSION_EVENT_V4_FIXTURE_SNAPSHOT_SHA256=%s\n' "$(fixture_snapshot_sha256 "$repo_root/api/fixtures/agent/session-event-v4" "${event_v4_fixture_files[@]}")"
  printf 'SYNTHETIC_VIDEO_FIXTURE_SOURCE=%s\n' 'tests/fixtures/agent/resources-v3/synthetic-video-16x16.mp4.base64'
  printf 'SYNTHETIC_VIDEO_FIXTURE_TREE=%s\n' "$synthetic_video_tree"
  printf 'SYNTHETIC_VIDEO_FIXTURE_SOURCE_SHA256=%s\n' "$(sha256_file "$source_synthetic_video")"
  printf 'SYNTHETIC_VIDEO_FIXTURE_RAW_SIZE=%s\n' "$synthetic_video_raw_size"
  printf 'SYNTHETIC_VIDEO_FIXTURE_RAW_SHA256=%s\n' "$(sha256_file "$decoded_synthetic_video")"
  printf 'SYNTHETIC_VIDEO_FIXTURE_SNAPSHOT=%s\n' 'internal/session/fixtures/synthetic-video-16x16.mp4.base64'
  printf 'SYNTHETIC_VIDEO_FIXTURE_SNAPSHOT_SHA256=%s\n' "$(sha256_file "$repo_root/internal/session/fixtures/synthetic-video-16x16.mp4.base64")"
} >"$temporary_lock"
mv "$temporary_lock" "$lock_file"

echo "Synchronized Agent Host contracts $contracts_ref ($contracts_commit) with $generator@$generator_version."
