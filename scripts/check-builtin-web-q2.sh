#!/usr/bin/env bash
set -euo pipefail
task_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
workspace_root="$(dirname "$task_root")"
cd "$task_root"
unset YIJIE_FEAT175_Q2_UPDATE_PROFILE
export GOPROXY=off
node "$workspace_root/yijie-contracts/scripts/generate-builtin-web.mjs" --consumer=host --check
# Exact pure projection test: never starts Runtime or a model client.
go test ./internal/codex -run '^TestFEAT175Q2(FrozenProfile|HostedReconciliation)$' -count=1
node scripts/feat175-q2.mjs preflight
node --test scripts/test-feat175-q2.mjs
node --test scripts/test-feat175-q2-fixtures.mjs
python3 -m unittest discover -s scripts -p 'test_feat175_q2_meter.py' -v
