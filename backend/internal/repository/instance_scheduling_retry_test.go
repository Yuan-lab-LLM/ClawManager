package repository

import (
	"context"
	"testing"
	"time"
)

func TestSchedulingRetryMySQLExactUnboundErrorCAS(t *testing.T) {
	r := runtimeSecurityMySQL(t)
	ctx := context.Background()
	if _, err := r.sess.SQL().Exec(`CREATE TABLE instance_runtime_bindings (id INT PRIMARY KEY,instance_id INT)`); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"recover", "stop", "restart", "binding", "new-error", "new-timestamp", "case-change", "unknown-code", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			if _, err := r.sess.SQL().Exec(`DELETE FROM instance_runtime_bindings`); err != nil {
				t.Fatal(err)
			}
			if _, err := r.sess.SQL().Exec(`UPDATE instances SET status='error',runtime_generation=2,runtime_error_code='RUNTIME_POOL_UNAVAILABLE',runtime_error_message='no schedulable pod',runtime_security_profile='isolated-network-v1',updated_at=? WHERE id=7`, time.Now().UTC().Truncate(time.Microsecond)); err != nil {
				t.Fatal(err)
			}
			expected, err := r.GetByIDContext(ctx, 7)
			if err != nil {
				t.Fatal(err)
			}
			checkCtx := ctx
			switch scenario {
			case "stop":
				_, err = r.sess.SQL().Exec(`UPDATE instances SET status='stopped' WHERE id=7`)
			case "restart":
				_, err = r.sess.SQL().Exec(`UPDATE instances SET runtime_generation=3 WHERE id=7`)
			case "binding":
				_, err = r.sess.SQL().Exec(`INSERT INTO instance_runtime_bindings VALUES (1,7)`)
			case "new-error":
				_, err = r.sess.SQL().Exec(`UPDATE instances SET runtime_error_message='different failure' WHERE id=7`)
			case "new-timestamp":
				_, err = r.sess.SQL().Exec(`UPDATE instances SET updated_at=? WHERE id=7`, expected.UpdatedAt.Add(time.Second))
			case "case-change":
				_, err = r.sess.SQL().Exec(`UPDATE instances SET runtime_error_code='runtime_pool_unavailable' WHERE id=7`)
			case "unknown-code":
				code := "UNKNOWN"
				expected.RuntimeErrorCode = &code
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				checkCtx = c
			}
			if err != nil {
				t.Fatal(err)
			}
			changed, err := r.RetryRuntimeSchedulingError(checkCtx, *expected)
			if scenario == "cancelled" {
				if changed || err == nil {
					t.Fatal("cancelled CAS succeeded")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			current, err := r.GetByIDContext(ctx, 7)
			if err != nil {
				t.Fatal(err)
			}
			if changed != (scenario == "recover") {
				t.Fatal("CAS violated current error/intent/placement snapshot")
			}
			if current.RuntimeSecurityProfile != "isolated-network-v1" {
				t.Fatal("CAS changed sticky protection")
			}
			if changed && (current.Status != "creating" || current.RuntimeGeneration != 2 || current.RuntimeErrorMessage != nil || current.RuntimeErrorCode != nil) {
				t.Fatal("successful CAS lost clean same-generation creating intent")
			}
		})
	}
}
