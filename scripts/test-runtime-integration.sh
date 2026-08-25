#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
runtime_repo="${YIJIE_CODEX_REPO:-$repo_root/../yijie-codex}"
runtime_binary="${YIJIE_CODEX_INTEGRATION_BINARY:-$runtime_repo/.yijie/build/macos/aarch64-apple-darwin/codex}"
runtime_manifest="${YIJIE_CODEX_INTEGRATION_MANIFEST:-$runtime_repo/.yijie/build/macos/aarch64-apple-darwin/runtime-manifest.json}"
skills_repo="${YIJIE_SKILLS_REPO:-$repo_root/../yijie-skills}"
skills_repo="$(cd "$skills_repo" && pwd)"

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

YIJIE_SKILLS_REPO="$skills_repo" "$repo_root/scripts/check-skills-producer.sh" --provenance-only
make -C "$skills_repo" package
make -C "$skills_repo" package-desktop-release
YIJIE_SKILLS_REPO="$skills_repo" "$repo_root/scripts/check-skills-producer.sh"

cd "$repo_root"
YIJIE_CODEX_INTEGRATION_BINARY="$runtime_binary" \
YIJIE_CODEX_INTEGRATION_MANIFEST="$runtime_manifest" \
YIJIE_SKILLS_LOCAL_BUNDLE_ROOT="$skills_repo/dist/skill-packages" \
  go test -count=1 -run '^(TestPinnedRuntimeIntegration|TestPinnedRuntimeMiniMaxConfigurationIntegration|TestPinnedRuntimeManagedSkillLifecycle|TestPinnedRuntimeYijieSkillsV030ThirtyEightProjection)$' ./internal/codex ./internal/integration
