#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
producer_root="${YIJIE_SKILLS_REPO:-$repo_root/../yijie-skills}"
producer_root="$(cd "$producer_root" && pwd)"

# shellcheck disable=SC1091
source "$repo_root/api/skills.lock"

mode="${1:-full}"
[[ "$mode" == "full" || "$mode" == "--provenance-only" ]] || {
  echo "usage: $0 [--provenance-only]" >&2
  exit 2
}

fail() {
  echo "skills producer check failed: $*" >&2
  exit 1
}

actual_commit="$(git -C "$producer_root" rev-parse HEAD)"
[[ "$actual_commit" == "$SKILLS_COMMIT" ]] || fail "producer commit $actual_commit != $SKILLS_COMMIT"
[[ -z "$(git -C "$producer_root" status --porcelain=v1 --untracked-files=all)" ]] || fail "producer working tree is not clean"

grep -qx "CONTRACTS_COMMIT=$CONTRACTS_COMMIT" "$repo_root/api/contracts.lock" || fail "Contracts commit pin differs"
grep -qx "SKILL_BUNDLE_MANIFEST_V2_SCHEMA_SHA256=$MANIFEST_V2_SHA256" "$repo_root/api/contracts.lock" || fail "Manifest v2 pin differs"
[[ "$SKILLS_VERSION" == "0.3.0" && "$SKILL_COUNT" == "38" && "$INSTALLABLE_COUNT" == "38" &&
  "$BLOCKED_COUNT" == "0" && "$CATEGORY_COUNTS" == "5/9/7/9/8" ]] || fail "producer lock invariants differ"

if [[ "$mode" == "--provenance-only" ]]; then
  echo "skills producer provenance passed: yijie-skills@$SKILLS_VERSION ($SKILLS_COMMIT)"
  exit 0
fi

local_root="$producer_root/dist/skill-packages"
release_root="$producer_root/dist/skill-packages-desktop-release"
[[ -d "$local_root/packages" && -d "$release_root/packages" ]] || fail "both packaged channels are required"

validate_manifest_identity() {
  node - "$1" "$2" "$SKILLS_VERSION" "$SKILLS_REPOSITORY" "$SKILLS_COMMIT" \
    "$SKILLS_SOURCE_TREE_SHA256" "$SKILL_COUNT" "$INSTALLABLE_COUNT" "$BLOCKED_COUNT" "$CATEGORY_COUNTS" <<'NODE'
const fs = require("node:fs");
const [path, channel, version, repository, commit, tree, skillCount, installableCount, blockedCount, categoryCounts] = process.argv.slice(2);
const manifest = JSON.parse(fs.readFileSync(path, "utf8"));
const counts = new Map();
let installable = 0;
let blocked = 0;
for (const skill of manifest.skills ?? []) {
  counts.set(skill.category, (counts.get(skill.category) ?? 0) + 1);
  if (skill.release?.catalog_status === "installable") installable += 1;
  if (skill.release?.catalog_status === "blocked") blocked += 1;
}
const orderedCategories = ["sourcing-selection", "market-research", "content-marketing", "traffic-advertising", "store-operations"];
const actualCategories = orderedCategories.map((category) => counts.get(category) ?? 0).join("/");
const valid = manifest.schema_version === 2 &&
  manifest.bundle_version === version &&
  manifest.distribution_channel === channel &&
  manifest.source?.repository === repository &&
  manifest.source?.revision_kind === "git-commit" &&
  manifest.source?.revision === commit &&
  manifest.source?.tree_sha256 === tree &&
  manifest.skills?.length === Number(skillCount) &&
  installable === Number(installableCount) && blocked === Number(blockedCount) &&
  actualCategories === categoryCounts;
if (!valid) {
  process.stderr.write(`invalid producer manifest identity: ${path}\n`);
  process.exit(1);
}
NODE
}

validate_manifest_identity "$local_root/bundle-manifest.json" "local-development" || fail "local-development manifest identity differs"
validate_manifest_identity "$release_root/bundle-manifest.json" "desktop-release" || fail "desktop-release manifest identity differs"

sha256_file() {
  shasum -a 256 "$1" | awk '{print $1}'
}

[[ "$(sha256_file "$local_root/bundle-manifest.json")" == "$LOCAL_DEVELOPMENT_MANIFEST_SHA256" ]] || fail "local-development manifest digest differs"
[[ "$(sha256_file "$release_root/bundle-manifest.json")" == "$DESKTOP_RELEASE_MANIFEST_SHA256" ]] || fail "desktop-release manifest digest differs"

archive_inventory() {
  local root="$1"
  local file relative digest
  while IFS= read -r file; do
    relative="${file#"$root"/}"
    digest="$(sha256_file "$file")"
    printf '%s %s\n' "$relative" "$digest"
  done < <(find "$root/packages" -type f -name '*.zip' | LC_ALL=C sort)
}

local_count="$(find "$local_root/packages" -type f -name '*.zip' | wc -l | tr -d ' ')"
release_count="$(find "$release_root/packages" -type f -name '*.zip' | wc -l | tr -d ' ')"
[[ "$local_count" == "$SKILL_COUNT" && "$release_count" == "$SKILL_COUNT" ]] || fail "archive count is not $SKILL_COUNT in both channels"
[[ "$(find "$local_root/packages" -type f | wc -l | tr -d ' ')" == "$SKILL_COUNT" ]] || fail "local-development packages contain unexpected files"
[[ "$(find "$release_root/packages" -type f | wc -l | tr -d ' ')" == "$SKILL_COUNT" ]] || fail "desktop-release packages contain unexpected files"

local_inventory="$(archive_inventory "$local_root")"
release_inventory="$(archive_inventory "$release_root")"
[[ "$local_inventory" == "$release_inventory" ]] || fail "channel archive inventories differ"
inventory_sha="$(printf '%s\n' "$local_inventory" | shasum -a 256 | awk '{print $1}')"
[[ "$inventory_sha" == "$ARCHIVE_INVENTORY_SHA256" ]] || fail "archive inventory digest $inventory_sha != $ARCHIVE_INVENTORY_SHA256"

echo "skills producer check passed: yijie-skills@$SKILLS_VERSION ($SKILLS_COMMIT), $SKILL_COUNT identical archives per channel"
