#!/usr/bin/env bash
set -euo pipefail
host_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$host_root"
unset YIJIE_FEAT175_Q2_UPDATE_PROFILE YIJIE_FEAT175_Q2_RUN_CONFIG YIJIE_FEAT175_Q2_V2_RUN_CONFIG YIJIE_FEAT175_Q2_A0_CONFIG
export GOPROXY=off
node scripts/feat175-q2-a0.mjs preflight
node --test scripts/test-feat175-q2-a0.mjs
python3 -B -m unittest discover -s scripts -p 'test_feat175_q2_a0.py' -v
go test -race ./internal/codex -run '^TestFEAT175Q2(FrozenProfile|HostedReconciliation)$' -count=1
