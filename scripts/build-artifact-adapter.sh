#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
target="${1:?canonical output directory required}"
cargo build --locked --manifest-path "$root/artifact-mcp/Cargo.toml"
mkdir -p "$target"
cp "$root/artifact-mcp/target/debug/yijie-artifact-mcp" "$target/yijie-artifact-mcp"
shasum -a 256 "$target/yijie-artifact-mcp" | awk '{print $1}' > "$target/yijie-artifact-mcp.sha256"
