package repository

import (
	"encoding/json"
	"testing"
)

func TestTeamStartMySQLPublishesExplicitIntentWithoutChangingExecutionOrStopProof(t *testing.T) {
	for _, name := range []string{"resume", "upgraded-generation", "unfinished-stop", "changed-workspace", "older-generation"} {
		t.Run(name, func(t *testing.T) {
			s, r := teamIdleFixture(t)
			ctx := t.Context()
			exec := func(q string, args ...any) {
				t.Helper()
				if _, e := s.SQL().Exec(q, args...); e != nil {
					t.Fatal(e)
				}
			}
			members, err := frozenTeamMembers(ctx, s, 9)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(members)
			exec(`UPDATE team_runtime_lifecycle SET state='recovery_required',desired_state='running',operation_id='original-start',members_json=? WHERE team_id=9`, string(raw))
			exec(`INSERT INTO instance_desired_state(instance_id,desired_power_state) VALUES(1,'stopped'),(2,'stopped'),(3,'stopped')`)
			wantGeneration := 2
			switch name {
			case "upgraded-generation":
				exec(`UPDATE instances SET runtime_generation=3 WHERE id=1`)
				wantGeneration = 3
			case "unfinished-stop":
				exec(`UPDATE instance_model_activity SET stop_operation_id='original-unconfirmed-stop' WHERE instance_id=2`)
			case "changed-workspace":
				exec(`UPDATE instances SET workspace_path='/different' WHERE id=2`)
			case "older-generation":
				exec(`UPDATE instances SET runtime_generation=1 WHERE id=1`)
				wantGeneration = 1
			}
			v, err := r.Request(ctx, 9, "running", "manual", 0)
			good := name == "resume" || name == "upgraded-generation"
			if (err == nil) != good {
				t.Fatalf("start intent %s: %+v %v", name, v, err)
			}
			for _, id := range []int{1, 2, 3} {
				var power string
				row, _ := s.SQL().QueryRow(`SELECT desired_power_state FROM instance_desired_state WHERE instance_id=?`, id)
				if e := row.Scan(&power); e != nil {
					t.Fatal(e)
				}
				want := "stopped"
				if good && id != 3 {
					want = "running"
				}
				if power != want {
					t.Fatalf("instance %d intent=%s want=%s", id, power, want)
				}
			}
			var generation, bindings int
			row, _ := s.SQL().QueryRow(`SELECT runtime_generation FROM instances WHERE id=1`)
			row.Scan(&generation)
			row, _ = s.SQL().QueryRow(`SELECT COUNT(*) FROM instance_runtime_bindings`)
			row.Scan(&bindings)
			if generation != wantGeneration || bindings != 2 {
				t.Fatal("Start intent altered physical execution")
			}
			if good && v.OperationID != "original-start" {
				t.Fatal("continuation created a new operation")
			}
		})
	}
}
