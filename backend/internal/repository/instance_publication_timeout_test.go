package repository

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRuntimePublicationTimeoutCASMySQL(t *testing.T) {
	for _, scenario := range []string{"expired", "running-won-race", "new-generation", "new-origin", "timely", "deleting"} {
		t.Run(scenario, func(t *testing.T) {
			r, _ := manualStartIntentRepository(t, "openclaw")
			marker, ok := interface{}(r).(interface {
				MarkRuntimePublicationTimeout(context.Context, int, int, time.Time, string) (bool, error)
			})
			if !ok {
				t.Fatal("conditional publication-timeout capability missing")
			}
			origin := time.Now().UTC().Add(-6 * time.Minute).Truncate(time.Microsecond)
			if _, err := r.sess.SQL().Exec(`UPDATE instances SET status='creating',runtime_generation=1,runtime_security_profile='isolated-network-v1',created_at=?,updated_at=NOW(),runtime_error_code=NULL,runtime_error_message=NULL WHERE id=7`, origin); err != nil {
				t.Fatal(err)
			}
			if _, err := r.sess.SQL().Exec(`INSERT INTO instance_runtime_bindings VALUES(7,9,'pending-7-1',1,'creating')`); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "running-won-race":
				_, _ = r.sess.SQL().Exec(`UPDATE instances SET status='running' WHERE id=7`)
			case "new-generation":
				_, _ = r.sess.SQL().Exec(`UPDATE instances SET runtime_generation=2 WHERE id=7`)
			case "new-origin":
				_, _ = r.sess.SQL().Exec(`UPDATE instances SET created_at=NOW() WHERE id=7`)
			case "timely":
				origin = time.Now().UTC().Truncate(time.Microsecond)
				_, _ = r.sess.SQL().Exec(`UPDATE instances SET created_at=? WHERE id=7`, origin)
			case "deleting":
				_, _ = r.sess.SQL().Exec(`UPDATE instances SET status='deleting' WHERE id=7`)
			}
			if _, err := r.sess.SQL().Exec(`DELETE FROM instance_model_activity WHERE instance_id=7`); err != nil {
				t.Fatal(err)
			}
			changed, err := marker.MarkRuntimePublicationTimeout(t.Context(), 7, 1, origin, "runtime startup confirmation timed out; ownership retained; manual inspection required")
			if scenario == "deleting" {
				if err == nil {
					t.Fatal("deleting intent not fenced")
				}
				return
			}
			if scenario == "new-generation" && errors.Is(err, ErrStaleRuntimeGeneration) {
				err = nil
			}
			if err != nil || changed != (scenario == "expired") {
				t.Fatalf("unexpected CAS outcome: changed=%t err=%v", changed, err)
			}
			var state, code string
			row, _ := r.sess.SQL().QueryRow(`SELECT status,COALESCE(runtime_error_code,'') FROM instances WHERE id=7`)
			if err := row.Scan(&state, &code); err != nil {
				t.Fatal(err)
			}
			if scenario == "expired" && (state != "error" || code != "RUNTIME_PUBLICATION_TIMEOUT") {
				t.Fatalf("timeout not visible: %s/%s", state, code)
			}
			if scenario == "running-won-race" && (state != "running" || code != "") {
				t.Fatal("timeout overwrote running")
			}
			var count int
			row, _ = r.sess.SQL().QueryRow(`SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id=7 AND runtime_pod_id=9 AND generation=1 AND gateway_id='pending-7-1'`)
			if err := row.Scan(&count); err != nil || count != 1 {
				t.Fatal("timeout released binding")
			}
		})
	}
}
