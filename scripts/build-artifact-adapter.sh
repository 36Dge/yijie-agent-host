#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
target="${1:?canonical output directory required}"
if [[ -n "${YIJIE_ARTIFACT_BUILD_DIR:-}" ]]; then
  [[ "$YIJIE_ARTIFACT_BUILD_DIR" == /* ]] || { echo "Absolute artifact build cache required" >&2; exit 1; }
  export CARGO_BUILD_BUILD_DIR="$YIJIE_ARTIFACT_BUILD_DIR"
fi
messages="$(mktemp -t yijie-artifact-build)"
trap 'rm -f "$messages"' EXIT
cargo build --locked --manifest-path "$root/artifact-mcp/Cargo.toml" --bin yijie-artifact-mcp --message-format=json > "$messages"
python3 -B "$root/scripts/artifact_build_output.py" --messages "$messages" --manifest "$root/artifact-mcp/Cargo.toml" --output "$target"
