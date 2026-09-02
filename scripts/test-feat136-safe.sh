#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

run_allowlist() {
  local package="$1"
  shift
  local pattern expected actual
  pattern="$(IFS='|'; printf '%s' "$*")"
  expected="$(printf '%s\n' "$@" | LC_ALL=C sort)"
  actual="$(
    cd "$repo_root"
    go test "$package" -list "^(${pattern})$" | awk '/^Test/ { print }' | LC_ALL=C sort
  )"
  if [ "$actual" != "$expected" ]; then
    echo "FEAT-136 safe allowlist no longer resolves exactly in $package." >&2
    diff -u <(printf '%s\n' "$expected") <(printf '%s\n' "$actual") >&2 || true
    exit 1
  fi
  (
    cd "$repo_root"
    go test -race "$package" -run "^(${pattern})$" -count=1
  )
}

# These are in-memory event, redaction, schema, gate, and route checks. The
# failed-command case consumes a closed lifecycle value; it does not inject a
# process/filesystem/network failure or execute a command.
run_allowlist ./internal/session \
  TestFEAT136CanonicalFixturesValidate \
  TestFEAT136CommandProjectionLifecycleRedactionAndOutputs \
  TestFEAT136CommandFailedNonzeroLifecycleProjection \
  TestFEAT136CommandProjectionUTF8AndLiveCaps \
  TestFEAT136ToolProjectionIsMetadataOnlyAndBounded \
  TestFEAT136GenericAllowlistIsV5ClosedAndV4Isolated \
  TestFEAT136EventHubReplayUsesEventIdentity \
  TestFEAT136NativeValidatorRejectsExtraFieldsAndIllegalStatus \
  TestFEAT136CompletedReconcilesMissingAndConflictingStart \
  TestFEAT136RedactionDropsWholeSensitiveLines \
  TestFEAT136SplitDeltaAndCwdSecretsFailClosed \
  TestFEAT136CompletedSnapshotRecomputesSafeAuthority \
  TestFEAT136CompactJSONCapAdaptsCompletedOutput \
  TestFEAT136V5GateOffDoesNotTouchLegacyOrPending \
  TestFEAT136LateLifecycleAfterTurnTerminalIsIgnored \
  TestFEAT136MetadataProjectionIsStreamingAndBounded \
  TestFEAT136PendingStartCannotBeOvertakenByCompleted

run_allowlist ./internal/app \
  TestFEAT136CommandToolItemsProfileRequiresExactDependentGate \
  TestFEAT136LoadConfigWiresV5WithoutExperimentalTools \
  TestFEAT136V5RouteRequiresDependentGateAndExactNegotiation \
  TestRuntimeCompatibilityProjectionMatchesHostAdapter
