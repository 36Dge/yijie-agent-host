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
    echo "FEAT-137 safe allowlist no longer resolves exactly in $package." >&2
    diff -u <(printf '%s\n' "$expected") <(printf '%s\n' "$actual") >&2 || true
    exit 1
  fi
  (
    cd "$repo_root"
    go test -race "$package" -run "^(${pattern})$" -count=1
  )
}

# Closed wire/source mapping, ordinary temporary-file authority lifecycles,
# deterministic clocks/races, and normal helper startup/shutdown only. Tests
# using injected write/sync/cleanup/postcondition failures live in the separate
# owner-authorized target.
run_allowlist ./internal/codex \
  TestFEAT137PinnedRuntimeCommandApprovalWireFixture \
  TestFEAT137RuntimeApprovalV3ContractMatchesAdapter \
  TestFEAT137ExactServerRequestEnvelopeAndTypedRequestID \
  TestFEAT137ClientPreservesTypedRequestIDOnResponse \
  TestFEAT137CommandApprovalReportsResponseAfterTransportWrite \
  TestFEAT137CommandApprovalParamsClosedDecoder \
  TestFEAT137CommandApprovalMapper \
  TestFEAT137DoesNotTightenExistingDynamicToolEnvelope \
  TestFEAT137ResolutionAckMappingPreservesTypedRequestID \
  TestFEAT137SessionWireUsesOnRequestReadOnlyPolicy \
  TestFEAT137D4SessionWireUsesZeroArgumentOneShotInstructions \
  TestFEAT137GateOffPreservesBaselineSessionWire \
  TestFEAT137CodexHomeLeaseRejectsSecondManagerInProcess \
  TestFEAT137CodexHomeAuthorityPinsPhysicalOwnerOnlyRoot \
  TestFEAT137CodexHomeLeaseIsReleasedByNormalProcessExit \
  TestFEAT137LegacyRuleIsMigratedAndCleanedOnlyWhenExact \
  TestFEAT137GateOffFakeProviderCleansExactStaleRule \
  TestFEAT137ExactRuleRemovalSyncsParentDirectory \
  TestMiniMaxManagedConfigGateOffPreservesBaselineBytes \
  TestFEAT137MiniMaxManagedConfigClosesAmbientProductSurfacesExactly \
  TestFEAT137ManagedConfigValidationRejectsIncompleteClosedAuthority \
  TestFEAT137ManagedConfigValidationRejectsRetryExpansion \
  TestFEAT137ExecPolicyRejectsGateOffManagedConfig \
  TestPrepareMiniMaxCodexHomeOwnsExactFEAT137ExecPolicyLifecycle \
  TestRuntimeEnvironmentScopesMiniMaxCredential \
  TestRuntimeEnvironmentRemovesBaselineCredentials \
  TestFEAT137ManagerHoldsRuleAndLeaseAcrossRuntimeSessions \
  TestFEAT137EveryRuntimeProfileUsesTheSameGateOffLease \
  TestFEAT137ShutdownCancelsInitializeAndWaitsForNormalExit \
  TestFEAT137PinnedRuntimeFourPatchManifestAuthority \
  TestFEAT137RuntimeEnvironmentStripsAmbientDeterministicProducerWhenGateOff \
  TestFEAT137RuntimeEnvironmentInjectsDeterministicProducerExactlyOnce \
  TestFEAT137RuntimeConfigRejectsDeterministicProducerWithoutCommandApprovalAuthority

run_allowlist ./internal/session \
  TestFEAT137RuntimeApprovalV3EligibilityMatchesSessionAuthority \
  TestFEAT137DecisionWaitsForMatchingTypedAckReplayAndIdempotency \
  TestFEAT137CancelDecisionCommittedWritersShareOneAckAndRuntimeResponse \
  TestFEAT137DecisionCommitTimeStaysAuthoritativeWhenRuntimeAckCrossesDeadline \
  TestFEAT137DeadlineWinsLateRuntimeAndAuthorityCleanup \
  TestFEAT137DeadlineCleanupTeardownNeverEmitsLateResolvedElsewhere \
  TestFEAT137TTLWaitsForMatchingRuntimeAckBeforeExpiredProjection \
  TestFEAT137TTLWinnerSurvivesCleanupBeforeRuntimeAck \
  TestFEAT137V6ApprovalProjectionRejectsOversizedOptionalIdentity \
  TestFEAT137GenerationCleanupResolvesElsewhereWithoutResponse \
  TestFEAT137ClosedGenerationSuppressesLateRequestAndKeepsNewGenerationIndependent \
  TestFEAT137GenerationCloseWinsAdmissionValidationRace \
  TestFEAT137InvalidRequestsAndCanonicalUUIDsFailClosed \
  TestFEAT137RuntimeReplayRetentionHasNoUncontractedCardinalityCutoff

run_allowlist ./internal/app \
  TestFEAT137RuntimeStatusProjectionNeverLeaksInternalFailureCode \
  TestFEAT137CommandApprovalProfileRequiresExactConjunction \
  TestFEAT137D4DeterministicProducerRequiresExactApprovalAuthority \
  TestFEAT137D4DeterministicProducerCannotBypassRejectedProfiles \
  TestFEAT137LoadConfigWiresApprovalWithoutExperimentalTools \
  TestFEAT137V6RoutesRequireExactConjunctionAndCapability \
  TestFEAT137V6EventsRejectAmbiguousQueries \
  TestFEAT137V6EventsNormalizeUUIDsAndRejectAmbiguousHeaders \
  TestFEAT137V6GETSurfacesRejectHEADBeforeService \
  TestFEAT137PendingHTTPProjectionIsClosedAndOwnerBound \
  TestFEAT137DecisionHTTPRejectsIncompleteOrAmbiguousInput \
  TestFEAT137DecisionHTTPProjectionIsStrictlyCorrelated
