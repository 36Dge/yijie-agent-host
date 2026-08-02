#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
lock_file="$repo_root/api/contracts.lock"
snapshot_openapi="$repo_root/api/openapi/agent-host.yaml"
snapshot_compatibility="$repo_root/api/compatibility/agent-host-runtime-v1.json"
snapshot_event_schema="$repo_root/api/jsonschema/agent-session-event.schema.json"
snapshot_event_v2_schema="$repo_root/api/jsonschema/agent-session-event-v2.schema.json"

if [ ! -f "$lock_file" ] || [ ! -f "$snapshot_openapi" ] || [ ! -f "$snapshot_compatibility" ] ||
  [ ! -f "$snapshot_event_schema" ] || [ ! -f "$snapshot_event_v2_schema" ]; then
  echo "Agent Host contract snapshot is incomplete; run make sync-contracts." >&2
  exit 1
fi

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
semver_pattern='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'
sha256_pattern='^[0-9a-f]{64}$'
full_commit_pattern='^[0-9a-f]{40}$'
generator_version_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'
expected_generator="github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen"
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

temporary_dir="$(mktemp -d)"
trap 'rm -rf "$temporary_dir"' EXIT
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
  source_package="$temporary_dir/package.json"
  git -C "$contracts_repo" show "$CONTRACTS_COMMIT:openapi/agent-host/agent-host.yaml" >"$source_openapi"
  git -C "$contracts_repo" show "$CONTRACTS_COMMIT:compatibility/agent-host-runtime-v1.json" >"$source_compatibility"
  git -C "$contracts_repo" show "$CONTRACTS_COMMIT:jsonschema/agent/session-event.schema.json" >"$source_event_schema"
  git -C "$contracts_repo" show "$CONTRACTS_COMMIT:jsonschema/agent/session-event-v2.schema.json" >"$source_event_v2_schema"
  git -C "$contracts_repo" show "$CONTRACTS_COMMIT:package.json" >"$source_package"

  cmp "$source_openapi" "$snapshot_openapi"
  cmp "$source_compatibility" "$snapshot_compatibility"
  cmp "$source_event_schema" "$snapshot_event_schema"
  cmp "$source_event_v2_schema" "$snapshot_event_v2_schema"
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
