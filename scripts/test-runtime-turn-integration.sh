#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
runtime_repo="${YIJIE_CODEX_REPO:-$repo_root/../yijie-codex}"
runtime_binary="${YIJIE_CODEX_INTEGRATION_BINARY:-$runtime_repo/.yijie/build/macos/aarch64-apple-darwin/codex}"
runtime_manifest="${YIJIE_CODEX_INTEGRATION_MANIFEST:-$runtime_repo/.yijie/build/macos/aarch64-apple-darwin/runtime-manifest.json}"
default_key_file="$repo_root/.local/secrets/minimax-api-key"

if [ ! -x "$runtime_binary" ]; then
  echo "Pinned Runtime binary is missing or not executable: $runtime_binary" >&2
  exit 2
fi
if [ ! -f "$runtime_manifest" ]; then
  echo "Pinned Runtime manifest is missing: $runtime_manifest" >&2
  exit 2
fi
if [ -z "${YIJIE_MINIMAX_API_KEY:-}" ] && [ -z "${YIJIE_MINIMAX_API_KEY_FILE:-}" ]; then
  if [ ! -f "$default_key_file" ]; then
    echo "MiniMax key is unavailable. Set YIJIE_MINIMAX_API_KEY_FILE or create the owner-only default key file." >&2
    echo "Default path: $default_key_file" >&2
    exit 2
  fi
  export YIJIE_MINIMAX_API_KEY_FILE="$default_key_file"
fi

cd "$repo_root"
YIJIE_RUN_MINIMAX_INTEGRATION=1 \
YIJIE_CODEX_INTEGRATION_BINARY="$runtime_binary" \
YIJIE_CODEX_INTEGRATION_MANIFEST="$runtime_manifest" \
  go test -count=1 -run '^TestPinnedRuntimeMiniMaxTurnIntegration$' ./internal/codex
