#!/usr/bin/env bash
set -euo pipefail

if [ "${YIJIE_FEAT137_OWNER_AUTHORIZED_TEST_INJECTION:-}" != "true" ]; then
  echo "FEAT-137 fault/drift tests require exact Owner authorization." >&2
  exit 2
fi

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
    echo "FEAT-137 owner-authorized allowlist no longer resolves exactly in $package." >&2
    diff -u <(printf '%s\n' "$expected") <(printf '%s\n' "$actual") >&2 || true
    exit 1
  fi
  (
    cd "$repo_root"
    go test -race "$package" -run "^(${pattern})$" -count=1
  )
}

# Every selected injection is confined to Go test hooks and t.TempDir(). The
# allowlist deliberately excludes force-kill, permission sabotage, binary
# replacement, archive/attack fixtures, real Runtime, Provider, and model use.
run_allowlist ./internal/codex \
  TestFEAT137GateOffRejectsMarkerPrefixDriftWithoutMutation \
  TestFEAT137RejectsUnexpectedSecondRuleWithoutMutation \
  TestFEAT137RuleApplyRequiresFinalExactPostcondition \
  TestFEAT137RuleApplyPinsPreflightDirectoryIdentity \
  TestFEAT137D4StartRejectsManagedProfileMissingClosedFeature \
  TestFEAT137D4StartRejectsManagedRetryPolicyDrift \
  TestFEAT137StartRejectsPostPrepareManagedCatalogDrift \
  TestFEAT137ShutdownTimeoutLeavesLeaseWithExitWatcher \
  TestFEAT137InitializeFailureThenShutdownConvergesStopped \
  TestFEAT137ShutdownCancelsVerificationBeforeCodexHomeMutation \
  TestFEAT137ShutdownBeforeStartPermanentlyRejectsStartup \
  TestFEAT137ConcurrentShutdownWinsBeforeHomeMutationAndProcessStart \
  TestFEAT137AuthorityAcquisitionUsesClosedProviderFailureCode \
  TestFEAT137ShutdownSurfacesExactRuleCleanupFailure \
  TestFEAT137DirectorySyncFailureIsVisibleAfterNormalRuntimeExit

run_allowlist ./internal/session \
  TestFEAT137TTLResponseWriteFailureDiscardsWithoutForgedTerminal

run_allowlist ./internal/app \
  TestFEAT137ApprovalHTTPUsesStableErrors

run_allowlist ./cmd/desktop-host \
  TestFEAT137HostStopsIngressThenWaitsPastRuntimeDeadline
