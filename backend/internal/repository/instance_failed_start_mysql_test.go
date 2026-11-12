package repository

import (
	"testing"
	"time"
)

func TestExpiredStartCompletionRechecksGenerationDeadlineAndActiveAuthority(t *testing.T) {
	s := idleStopMySQL(t)
	ctx := t.Context()
	r := NewInstanceAutoStopRepository(s)
	if err := r.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	reset := func() {
		t.Helper()
		for _, q := range []string{"DELETE FROM instance_runtime_bindings WHERE instance_id=1", "DELETE FROM instance_model_activity_requests WHERE instance_id=1", "DELETE FROM gateway_runtime_tasks WHERE instance_id=1", "UPDATE instances SET status='stopped',runtime_generation=3 WHERE id=1"} {
			if _, err := s.SQL().Exec(q); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.SQL().Exec(`UPDATE instance_model_activity SET generation=2,start_deadline_at=?,stop_operation_id='',blocker='old failed cleanup' WHERE instance_id=1`, deadline); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name, q string
		want    bool
	}{
		{"current stopped old activity generation", "", true},
		{"binding reappeared", `INSERT INTO instance_runtime_bindings VALUES(1,11,'gw-1-3',3,'running')`, false},
		{"generation advanced", `UPDATE instances SET runtime_generation=4 WHERE id=1`, false},
		{"new start", `UPDATE instance_model_activity SET start_deadline_at=UTC_TIMESTAMP()+INTERVAL 3 MINUTE WHERE instance_id=1`, false},
		{"stop still pending", `UPDATE instance_model_activity SET stop_operation_id='pending-stop' WHERE instance_id=1`, false},
		{"instance restarted", `UPDATE instances SET status='running' WHERE id=1`, false},
		{"model active", `INSERT INTO instance_model_activity_requests(request_id,instance_id,generation,started_at,lease_expires_at) VALUES('active',1,3,UTC_TIMESTAMP(),UTC_TIMESTAMP()+INTERVAL 1 MINUTE)`, false},
		{"gateway active", `INSERT INTO gateway_runtime_tasks VALUES(1,3,FALSE)`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reset()
			if c.q != "" {
				if _, err := s.SQL().Exec(c.q); err != nil {
					t.Fatal(err)
				}
			}
			err := r.ClearExpiredStartDeadline(ctx, 1, 3, deadline)
			if (err == nil) != c.want {
				t.Fatalf("completion=%v want=%v", err, c.want)
			}
			a, e := r.GetActivity(ctx, 1)
			if e != nil {
				t.Fatal(e)
			}
			if c.want {
				if a.StartDeadlineAt != nil || a.Generation != 3 || a.Blocker != "" {
					t.Fatalf("stale protection remained: %+v", a)
				}
			} else if a.StartDeadlineAt == nil {
				t.Fatal("uncertain new operation protection erased")
			}
		})
	}
}
