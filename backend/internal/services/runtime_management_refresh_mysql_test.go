package services

import (
	"context"
	"testing"
	"time"

	"clawreef/internal/repository"
)

func TestRuntimeManagementMySQLRefreshUnboundStartingIntent(t *testing.T) {
	sess := runtimeManagementMySQLDB(t)
	ctx := context.Background()
	service := runtimeManagementMySQLService(sess)
	cases := []struct {
		status     string
		generation int
		wantFailed bool
		errorCode  string
	}{
		{"stopped", 2, true, ""}, {"deleting", 2, true, ""}, {"deleted", 2, true, ""},
		{"missing", 2, true, ""}, {"creating", 3, true, ""}, {"error", 2, true, ""},
		{"creating", 2, false, ""}, {"running", 2, false, ""},
		{"error", 2, false, "MODEL_CONFIGURATION_REQUIRED"},
		{"error", 2, false, "GATEWAY_START_FAILED"},
		{"error", 2, false, "PROXY_POLICY_NOT_READY"},
	}
	for n, tc := range cases {
		t.Run(tc.status+tc.errorCode, func(t *testing.T) {
			id := 201 + n
			if _, err := sess.SQL().Exec("INSERT INTO instances (id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES (?,1,'opencode','gateway','lite','running',1,NOW())", id); err != nil {
				t.Fatal(err)
			}
			op, err := service.enqueue(ctx, "balance", RuntimeSchedulingRequest{InstanceIDs: []int{id}}, []RuntimeSchedulingItem{{InstanceID: id, TargetPodID: 2, SourceGeneration: 1}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			op.Items[0].State, op.Status = "starting", "starting"
			if _, err = sess.SQL().Exec("UPDATE runtime_management_operations SET status='processing',owner_token=? WHERE id=?", service.token, op.ID); err != nil {
				t.Fatal(err)
			}
			if err = service.save(ctx, op); err != nil {
				t.Fatal(err)
			}
			if tc.status == "missing" {
				if _, err = sess.SQL().Exec("DELETE FROM instances WHERE id=?", id); err != nil {
					t.Fatal(err)
				}
			} else if _, err = sess.SQL().Exec("UPDATE instances SET status=?,runtime_generation=? WHERE id=?", tc.status, tc.generation, id); err != nil {
				t.Fatal(err)
			}
			if tc.errorCode != "" {
				if _, err = sess.SQL().Exec("UPDATE instances SET runtime_error_code=?,runtime_error_message='retry scheduling after backoff',updated_at=NOW() WHERE id=?", tc.errorCode, id); err != nil {
					t.Fatal(err)
				}
			}
			before, err := service.scheduler.instanceRepo.GetByID(id)
			if err != nil {
				t.Fatal(err)
			}
			if tc.errorCode != "" {
				if !isRetryableRuntimeSchedulingError(*before) || isRecoverableRuntimeSchedulingError(*before) {
					t.Fatal("fresh retryable error must retain its backoff")
				}
				mature := *before
				mature.UpdatedAt = time.Now().Add(-time.Minute)
				if !isRecoverableRuntimeSchedulingError(mature) {
					t.Fatal("retry must become due after its backoff")
				}
			}
			if err = service.refresh(ctx); err != nil {
				t.Fatal(err)
			}
			history, err := service.History(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, got := range history {
				if got.ID != op.ID {
					continue
				}
				want := "starting"
				if tc.wantFailed {
					want = "failed"
				}
				if got.Status != want || got.Items[0].State != want {
					t.Fatalf("unbound %s generation %d remained %+v; want %s", tc.status, tc.generation, got, want)
				}
			}
			after, err := service.scheduler.instanceRepo.GetByID(id)
			if err != nil {
				t.Fatal(err)
			}
			if (before == nil) != (after == nil) || (before != nil && (before.Status != after.Status || before.RuntimeGeneration != after.RuntimeGeneration)) {
				t.Fatal("journal refresh changed instance intent")
			}
			release, err := repository.NewNorthboundRepository(sess).ReserveInstanceMutations([]int{id})
			if tc.wantFailed {
				if err != nil {
					t.Fatalf("terminal record retained lifecycle guard: %v", err)
				}
				release()
			} else {
				if err == nil {
					release()
					t.Fatal("uncertain live start lost lifecycle guard")
				}
			}
		})
	}
}
