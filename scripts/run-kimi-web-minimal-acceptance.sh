#!/usr/bin/env bash
# Explicit test-only launcher; never disables toolchain checksum validation.
set -euo pipefail
kimi_host_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
kimi_workspace_root="$(cd "$kimi_host_root/.." && pwd -P)"
[[ $# == 2 && ( "$1" == "--preflight" || "$1" == "--execute-approved" ) && "$2" == /* ]] || { echo 'Usage: bash scripts/run-kimi-web-minimal-acceptance.sh --preflight|--execute-approved ABSOLUTE_FRESH_RUN_ROOT' >&2; exit 2; }
kimi_mode="$1"
kimi_run_root="$2"
[[ -d "$kimi_run_root" && ! -e "$kimi_run_root/started.json" ]] || { echo 'Fresh one-shot run root required; do not retry a previous run.' >&2; exit 2; }
cd "$kimi_host_root"
# Resolve the already installed official toolchain with normal verification,
# then use that exact binary locally. No global go env setting is changed.
kimi_go_root="$(GOSUMDB=sum.golang.org go env GOROOT)"
kimi_go="$kimi_go_root/bin/go"
kimi_required_go="$(awk '$1 == "go" {print $2; exit}' go.mod)"
kimi_actual_go="$(GOTOOLCHAIN=local "$kimi_go" env GOVERSION)"
[[ "$kimi_actual_go" == "go$kimi_required_go" ]] || { echo 'Installed Go toolchain differs from go.mod; stop before acceptance.' >&2; exit 2; }
export GOTOOLCHAIN=local GOSUMDB=sum.golang.org GOPROXY=off
export YIJIE_FEAT162_LIVE_ROOT="$kimi_run_root"
export YIJIE_MARKET_BROKER_TEST_MANIFEST="${YIJIE_MARKET_BROKER_TEST_MANIFEST:-$kimi_workspace_root/yijie-connectors/bin/market-worker/current.json}"
[[ "$YIJIE_MARKET_BROKER_TEST_MANIFEST" == /* && -f "$YIJIE_MARKET_BROKER_TEST_MANIFEST" ]] || { echo 'Canonical worker manifest must be an existing absolute file.' >&2; exit 2; }
export YIJIE_FEAT162_TOOLS_JSON="$kimi_workspace_root/yijie-connectors/api/web-retrieval.tools.json"
if [[ "$kimi_mode" == "--preflight" ]]; then
  unset YIJIE_KIMI_API_KEY YIJIE_KIMI_API_KEY_FILE
  export YIJIE_FEAT162_LIVE_TEST=local-preflight-no-credentials-no-supplier-calls
else
  [[ "${YIJIE_FEAT162_LIVE_TEST:-}" == "approved-search1-fetch1-cny002-no-model" ]] || { echo 'Explicit task-specific approval flag required.' >&2; exit 2; }
  [[ -z "${YIJIE_KIMI_API_KEY:-}" ]] || { echo 'Use the existing canonical model file source only.' >&2; exit 2; }
  export YIJIE_KIMI_API_KEY_FILE="$kimi_host_root/.local/secrets/kimi-api-key"
fi
exec "$kimi_go" test ./internal/app -run '^TestFEAT162LiveMinimalSearchFetch$' -count=1 -timeout=0 -v
