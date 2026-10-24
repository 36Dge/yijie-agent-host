package chatmodelsv2

import "testing"

func TestFEAT164HistoryAndExecutionConformance(t *testing.T) {
	const id = "00000000-0000-4000-8000-000000000164"
	original := `{"schema_version":2,"agent_session_id":"` + id + `","operation_id":"` + id + `","state":"committed","request":{"schema_version":1,"operation_id":"` + id + `","expected_revision":1,"profile_id":"kimi-k3-max-v1"},"result":{"schema_version":1,"agent_session_id":"` + id + `","state":"ready","revision":2,"profile_id":"kimi-k3-max-v1"}}`
	var receipt ModelOperationReceipt
	if err := Decode("ModelOperationReceipt", []byte(original), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Result.SchemaVersion != 1 || receipt.Result.Availability != nil {
		t.Fatal("historical result changed")
	}
	if Validate("ActiveProfileId", "kimi-k3-max-v1") == nil {
		t.Fatal("retired identity admitted")
	}
	if Validate("ActiveProfileId", "kimi-k3-1-pro-max-v1") != nil {
		t.Fatal("new identity rejected")
	}
	for _, data := range []string{`{"schema_version":2,"operation_id":"` + id + `","expected_revision":1,"profile_id":"kimi-k3-max-v1"}`, `{"schema_version":2,"operation_id":"` + id + `","expected_revision":1,"profile_id":"future"}`, `{"schema_version":2,"operation_id":"` + id + `","expected_revision":1,"profile_id":"kimi-k3-1-pro-max-v1","extra":true}`} {
		var value SelectRequest
		if Decode("SelectRequest", []byte(data), &value) == nil {
			t.Fatal("invalid execution request accepted")
		}
	}
}
