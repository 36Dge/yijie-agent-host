#!/usr/bin/env bash
set -euo pipefail
host_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
workspace_root="$(dirname "$host_root")"
runtime_relative="$(python3 - "$host_root/api/builtin-web.candidate.json" <<'PY'
import json,sys
print(json.load(open(sys.argv[1]))['runtime_relative_path'])
PY
)"
export YIJIE_CODEX_INTEGRATION_BINARY="$workspace_root/yijie-codex/$runtime_relative/codex"
export YIJIE_CODEX_INTEGRATION_MANIFEST="$workspace_root/yijie-codex/$runtime_relative/runtime-manifest.json"
export YIJIE_FEAT175_MARKET_MANIFEST="$workspace_root/yijie-connectors/bin/market-worker/current.json"
export GOPROXY=off
export HTTP_PROXY= HTTPS_PROXY= ALL_PROXY= NO_PROXY=127.0.0.1,localhost
cd "$host_root"
test -f "$YIJIE_CODEX_INTEGRATION_BINARY"
test -f "$YIJIE_CODEX_INTEGRATION_MANIFEST"
test -f "$YIJIE_FEAT175_MARKET_MANIFEST"
node "$workspace_root/yijie-contracts/scripts/generate-builtin-web.mjs" --consumer=host --check
# Exact test family: canonical Runtime + ordinary loopback Responses/MCP only.
# No live provider tests, user homes, global model settings, force-kill or restart
# of the daily App. Tests stop their own processes through normal EOF.
go test -race ./internal/codex -run '^TestFEAT175' -count=1 -v -parallel 2 -timeout 0
