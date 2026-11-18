package services

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"clawreef/internal/repository"
)

func TestModelLeaseRecoveryDoesNotWaitForIdleScanMySQL(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "future_scan"}[enabled], func(t *testing.T) {
			s := upgradeFlowDB(t)
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := s.SQL().Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			raw, err := os.ReadFile("../db/migrations/097_add_instance_auto_stop_scan.sql")
			if err != nil {
				t.Fatal(err)
			}
			for _, q := range strings.Split(string(raw), ";") {
				if strings.TrimSpace(q) != "" {
					exec(q)
				}
			}
			exec(`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(1,7,'openclaw','gateway','lite','running',2,UTC_TIMESTAMP(6))`)
			store := repository.NewInstanceAutoStopRepository(s)
			if err = store.Initialize(t.Context()); err != nil {
				t.Fatal(err)
			}
			exec(`UPDATE instance_auto_stop_settings SET enabled=?,next_check_at=UTC_TIMESTAMP(6)+INTERVAL 1 DAY`, enabled)
			exec(`INSERT INTO instance_model_activity_requests(request_id,instance_id,generation,started_at,last_heartbeat_at,lease_expires_at) VALUES ('expired',1,2,UTC_TIMESTAMP(6)-INTERVAL 10 MINUTE,UTC_TIMESTAMP(6)-INTERVAL 5 MINUTE,UTC_TIMESTAMP(6)-INTERVAL 1 MINUTE),('live',1,2,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)+INTERVAL 1 MINUTE),('legacy',1,2,UTC_TIMESTAMP(6)-INTERVAL 1 DAY,NULL,NULL)`)
			service := &InstanceAutoStopService{store: store, lifecycle: &instanceService{}}
			if err = service.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"expired", "live", "legacy"} {
				row, e := s.SQL().QueryRow(`SELECT finished_at,completion_reason FROM instance_model_activity_requests WHERE request_id=?`, key)
				if e != nil {
					t.Fatal(e)
				}
				var finished sql.NullTime
				var reason string
				if e = row.Scan(&finished, &reason); e != nil {
					t.Fatal(e)
				}
				if key == "expired" {
					if !finished.Valid || reason != "lease_expired" {
						t.Fatalf("expired lease not retired: %v %q", finished, reason)
					}
				} else if finished.Valid {
					t.Fatalf("%s scope was cleared without expiry/exit proof", key)
				}
			}
		})
	}
}
