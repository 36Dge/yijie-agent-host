package session

import "testing"

// TCONF-164-02: reviewed normal state/store cases, without process faults.
// Desktop SQL40 and the real Native/Host boundary are required companion checks.
func TestFEAT164MigrationReceiptAndRecovery(t *testing.T) {
	t.Run("terminal_receipt_and_higher_current_revision", TestFEAT164V2SelectionAndTerminalReceipt)
	t.Run("incomplete_receipt_explicit_recovery", TestFEAT164V2IncompleteReceiptRemainsUnknownUntilExplicitRecovery)
	t.Run("durable_windows_and_unrelated_work", TestFEAT164V2RecoveryWindowsAndUnrelatedWork)
	t.Run("legacy_bytes_and_unfinished_v1", TestFEAT164V1ReceiptPreservedAndIncompleteNeverReapplied)
	t.Run("store_7_8_9_10_to_11", TestFEAT164Store11RejectsOldReaderAndPreservesDomainBase)
}
