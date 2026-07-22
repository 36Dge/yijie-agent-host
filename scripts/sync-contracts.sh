#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
contracts_repo="${YIJIE_CONTRACTS_REPO:-$repo_root/../yijie-contracts}"
source_openapi="$contracts_repo/openapi/agent-host/agent-host.yaml"
source_compatibility="$contracts_repo/compatibility/agent-host-runtime-v1.json"
source_event_schema="$contracts_repo/jsonschema/agent/session-event.schema.json"

if [ ! -f "$source_openapi" ] || [ ! -f "$source_compatibility" ] || [ ! -f "$source_event_schema" ]; then
  echo "Agent Host contract sources are unavailable under: $contracts_repo" >&2
  exit 2
fi

contracts_version="$(awk -F '"' '/^[[:space:]]*"version"[[:space:]]*:/ { print $4; exit }' "$contracts_repo/package.json")"
compatibility_version="$(awk -F '"' '/^[[:space:]]*"contracts_version"[[:space:]]*:/ { print $4; exit }' "$source_compatibility")"
semver_pattern='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'
if [[ ! "$contracts_version" =~ $semver_pattern ]] || [ "$contracts_version" != "$compatibility_version" ]; then
  echo "contracts package and Runtime compatibility versions do not match" >&2
  exit 1
fi

sha256_file() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{ print $1 }'
  else
    sha256sum "$1" | awk '{ print $1 }'
  fi
}

mkdir -p "$repo_root/api/openapi" "$repo_root/api/compatibility" "$repo_root/api/jsonschema"
cp "$source_openapi" "$repo_root/api/openapi/agent-host.yaml"
cp "$source_compatibility" "$repo_root/api/compatibility/agent-host-runtime-v1.json"
cp "$source_event_schema" "$repo_root/api/jsonschema/agent-session-event.schema.json"

lock_file="$repo_root/api/contracts.lock"
temporary_lock="$lock_file.tmp"
{
  printf 'CONTRACTS_VERSION=%s\n' "$contracts_version"
  printf 'CONTRACTS_REF=contracts-v%s\n' "$contracts_version"
  printf 'OPENAPI_SHA256=%s\n' "$(sha256_file "$repo_root/api/openapi/agent-host.yaml")"
  printf 'RUNTIME_COMPATIBILITY_SHA256=%s\n' "$(sha256_file "$repo_root/api/compatibility/agent-host-runtime-v1.json")"
  printf 'AGENT_SESSION_EVENT_SCHEMA_SHA256=%s\n' "$(sha256_file "$repo_root/api/jsonschema/agent-session-event.schema.json")"
} >"$temporary_lock"
mv "$temporary_lock" "$lock_file"

echo "Synchronized Agent Host contracts candidate $contracts_version."
