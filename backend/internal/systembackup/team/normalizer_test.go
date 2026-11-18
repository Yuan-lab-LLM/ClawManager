package team

import (
	"reflect"
	"testing"
	"time"
)

func TestBatchPendingOperationCannotRemainRunnable(t *testing.T) {
	row := Row{"operation_id": Value("operation-1"), "status": Value("batch_pending"), "request_payload": Value(`{"instance_id":7}`), "idempotency_key": Value("original-key")}
	change, err := Normalize("northbound_operations", row, time.Now().UTC())
	if err != nil || string(change.After["status"].Data) != "failed" || !change.Quarantined {
		t.Fatal(change, err)
	}
	if string(change.After["request_payload"].Data) != string(row["request_payload"].Data) || string(change.After["idempotency_key"].Data) != "original-key" {
		t.Fatal("lost operation evidence")
	}
}

func TestLegacyWorkflowPhaseStatesRetainCompletedData(t *testing.T) {
	for _, tc := range []struct{ before, after string }{{"pending", "cancelled"}, {"done", "done"}} {
		row := Row{"id": Value("7"), "status": Value(tc.before), "result_json": Value(`{"partial":"saved artifact"}`)}
		original := Clone(row)
		change, e := Normalize("team_workflow_phases", row, time.Now().UTC())
		if e != nil {
			t.Errorf("historical phase %s rejected: %v", tc.before, e)
			continue
		}
		if string(change.After["status"].Data) != tc.after || !reflect.DeepEqual(change.Before, original) || !reflect.DeepEqual(row, original) || !reflect.DeepEqual(change.After["result_json"], row["result_json"]) {
			t.Error("phase data or completed state changed", tc.before)
		}
	}
}

func TestTaskRestorePreservesOriginalAndPartialResult(t *testing.T) {
	original := Row{"id": Value("9"), "status": Value("running"), "workflow_state": Value("executing"), "ledger_version": Value("9007199254740993"), "result_json": Value(`{"partial":"产物"}`), "error_message": Value("original diagnostic"), "finished_at": Null()}
	copy := Clone(original)
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	result, err := Normalize("team_tasks", original, now)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(copy, original) {
		t.Fatal("mutated source evidence")
	}
	if string(result.After["status"].Data) != "failed" || string(result.After["workflow_state"].Data) != "failed" {
		t.Fatal("unfinished task remains executable")
	}
	for _, key := range []string{"ledger_version", "result_json", "error_message"} {
		if !reflect.DeepEqual(result.After[key], original[key]) {
			t.Fatalf("lost %s", key)
		}
	}
	if !reflect.DeepEqual(result.Before, original) || result.Reason != "restore_interrupted" {
		t.Fatal("missing original evidence")
	}
	second, err := Normalize("team_tasks", result.After, now.Add(time.Hour))
	if err != nil || !reflect.DeepEqual(result.After, second.After) {
		t.Fatal("normalization is not idempotent")
	}
}

func TestRestorePreservesCompletedChildrenAndRejectsUnknownStates(t *testing.T) {
	for _, tc := range []struct{ table, status string }{{"team_tasks", "succeeded"}, {"team_work_items", "succeeded"}, {"team_work_items", "superseded"}, {"team_workflow_phases", "completed"}} {
		row := Row{"id": Value("1"), "status": Value(tc.status), "result_json": Value("partial"), "finished_at": Value("2026-01-01 00:00:00")}
		r, err := Normalize(tc.table, row, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(row, r.After) {
			t.Fatalf("completed history changed: %s", tc.table)
		}
	}
	if _, err := Normalize("team_tasks", Row{"id": Value("1"), "status": Value("new_unregistered_state")}, time.Now()); err == nil {
		t.Fatal("unknown state accepted")
	}
}

func TestOutboxAndOwnerReadinessCannotBeReplayed(t *testing.T) {
	for _, table := range []string{"team_event_outbox", "northbound_operations"} {
		row := Row{"id": Value("2"), "status": Value("pending"), "payload_json": Value(`{"message":"keep"}`)}
		if table == "northbound_operations" {
			row["status"] = Value("queued")
			row["error_code"] = Value("OWNER_TEAM_RUNTIME_WAITING")
			row["operation_id"] = Value("original-op")
			row["lease_owner"] = Value("expired-worker")
		}
		r, err := Normalize(table, row, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if !r.Quarantined || string(r.After["status"].Data) != "failed" {
			t.Fatal("old execution not quarantined")
		}
		if !reflect.DeepEqual(r.After["payload_json"], row["payload_json"]) {
			t.Fatal("payload lost")
		}
	}
}

func TestBinaryAndNullValuesSurviveClone(t *testing.T) {
	r := Row{"binary": {Data: []byte{0, 255, 128}}, "nullable": Null(), "empty": Value("")}
	c := Clone(r)
	c["binary"].Data[0] = 1
	if r["binary"].Data[0] != 0 || !c["nullable"].IsNull || c["empty"].IsNull {
		t.Fatal("lossy row clone")
	}
}
