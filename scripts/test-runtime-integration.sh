#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
runtime_repo="${YIJIE_CODEX_REPO:-$repo_root/../yijie-codex}"
runtime_binary="${YIJIE_CODEX_INTEGRATION_BINARY:-$runtime_repo/.yijie/build/macos/aarch64-apple-darwin/codex}"
runtime_manifest="${YIJIE_CODEX_INTEGRATION_MANIFEST:-$runtime_repo/.yijie/build/macos/aarch64-apple-darwin/runtime-manifest.json}"

if [ ! -x "$runtime_binary" ]; then
  echo "Pinned Runtime binary is missing or not executable: $runtime_binary" >&2
  echo "Build Runtime Baseline 0 in yijie-codex first." >&2
  exit 2
fi
if [ ! -f "$runtime_manifest" ]; then
  echo "Pinned Runtime manifest is missing: $runtime_manifest" >&2
  echo "Run Runtime Baseline 0 tests in yijie-codex first." >&2
  exit 2
fi

cd "$repo_root"
YIJIE_CODEX_INTEGRATION_BINARY="$runtime_binary" \
YIJIE_CODEX_INTEGRATION_MANIFEST="$runtime_manifest" \
  go test -count=1 -run '^(TestPinnedRuntimeIntegration|TestPinnedRuntimeMiniMaxConfigurationIntegration)$' ./internal/codex
