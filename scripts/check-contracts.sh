#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
lock_file="$repo_root/api/contracts.lock"
snapshot_openapi="$repo_root/api/openapi/agent-host.yaml"
snapshot_compatibility="$repo_root/api/compatibility/agent-host-runtime-v1.json"
snapshot_event_schema="$repo_root/api/jsonschema/agent-session-event.schema.json"

if [ ! -f "$lock_file" ] || [ ! -f "$snapshot_openapi" ] || [ ! -f "$snapshot_compatibility" ] ||
  [ ! -f "$snapshot_event_schema" ]; then
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
OPENAPI_SHA256="$(lock_value OPENAPI_SHA256)"
RUNTIME_COMPATIBILITY_SHA256="$(lock_value RUNTIME_COMPATIBILITY_SHA256)"
AGENT_SESSION_EVENT_SCHEMA_SHA256="$(lock_value AGENT_SESSION_EVENT_SCHEMA_SHA256)"
semver_pattern='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'
sha256_pattern='^[0-9a-f]{64}$'
if [[ ! "$CONTRACTS_VERSION" =~ $semver_pattern ]] ||
  [ "$CONTRACTS_REF" != "contracts-v$CONTRACTS_VERSION" ] ||
  [[ ! "$OPENAPI_SHA256" =~ $sha256_pattern ]] ||
  [[ ! "$RUNTIME_COMPATIBILITY_SHA256" =~ $sha256_pattern ]] ||
  [[ ! "$AGENT_SESSION_EVENT_SCHEMA_SHA256" =~ $sha256_pattern ]]; then
  echo "Agent Host contract lock contains an invalid version, ref, or digest." >&2
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

contracts_repo="${YIJIE_CONTRACTS_REPO:-$repo_root/../yijie-contracts}"
if [ -f "$contracts_repo/openapi/agent-host/agent-host.yaml" ]; then
  cmp "$contracts_repo/openapi/agent-host/agent-host.yaml" "$snapshot_openapi"
  cmp "$contracts_repo/compatibility/agent-host-runtime-v1.json" "$snapshot_compatibility"
  cmp "$contracts_repo/jsonschema/agent/session-event.schema.json" "$snapshot_event_schema"
  source_version="$(awk -F '"' '/^[[:space:]]*"version"[[:space:]]*:/ { print $4; exit }' "$contracts_repo/package.json")"
  if [ "$source_version" != "$CONTRACTS_VERSION" ]; then
    echo "Agent Host contract snapshot version is stale." >&2
    exit 1
  fi
fi

temporary_dir="$(mktemp -d)"
trap 'rm -rf "$temporary_dir"' EXIT
generated="$temporary_dir/agenthost.gen.go"
cd "$repo_root"
go tool oapi-codegen \
  -generate types \
  -package agenthostcontract \
  -o "$generated" \
  api/openapi/agent-host.yaml
gofmt -w "$generated"
cmp "$generated" internal/contracts/agenthost.gen.go

echo "Verified Agent Host contract snapshot $CONTRACTS_REF and generated types."
