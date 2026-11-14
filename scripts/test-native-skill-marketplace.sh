#!/usr/bin/env bash
# Normal, bounded FEAT-174 verification. Actual pinned Runtime; local loopback
# responses only for loading/admission tests, with zero supplier model calls.
set -euo pipefail
task_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workspace_root="$(cd "$task_root/.." && pwd)"
node "$workspace_root/yijie-contracts/scripts/generate-runtime-skill-marketplace.mjs" --check
runtime_relative="$(python3 - "$task_root/api/runtime-skill-marketplace.candidate.json" <<'PY'
import json,re,sys
value=json.load(open(sys.argv[1]))['runtime_relative_path']
assert re.fullmatch(r'\.yijie/build/native-skill-marketplace-v1/[a-f0-9]{64}/aarch64-apple-darwin',value)
print(value)
PY
)"
export YIJIE_CODEX_INTEGRATION_BINARY="$workspace_root/yijie-codex/$runtime_relative/codex"
export YIJIE_CODEX_INTEGRATION_MANIFEST="$workspace_root/yijie-codex/$runtime_relative/runtime-manifest.json"
export YIJIE_FEAT174_TEST_BUNDLE="$workspace_root/yijie-skills/dist/native-skill-marketplace"
export YIJIE_FEAT174_LEGACY_BUNDLE="$workspace_root/yijie-skills/dist/skill-packages"
export YIJIE_MARKET_BROKER_TEST_MANIFEST="$workspace_root/yijie-connectors/bin/market-worker/current.json"
cd "$task_root"
mkdir -p .local/native-skill-marketplace
go build -o .local/native-skill-marketplace/desktop-host ./cmd/desktop-host
export YIJIE_FEAT174_HOST_BINARY="$task_root/.local/native-skill-marketplace/desktop-host"
go test -race ./internal/skillmarket ./internal/skills ./internal/codex ./internal/app ./internal/session -run '^(TestFEAT174|TestFEAT158ExpertResourcesUseManagedInstallWithoutGlobalProjection)' -count=1 -v
