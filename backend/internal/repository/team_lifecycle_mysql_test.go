package repository

import (
	"clawreef/internal/models"
	"errors"
	"github.com/upper/db/v4"
	"sync"
	"testing"
	"time"
)

func teamIdleFixture(t *testing.T) (db.Session, *TeamLifecycleRepository) {
	t.Helper()
	s := idleStopMySQL(t)
	for _, q := range []string{
		`INSERT INTO teams VALUES(9,'running','northbound')`,
		`UPDATE instances SET workspace_path=CONCAT('/runtime/openclaw/user-7/instance-',id)`,
		`INSERT INTO team_members VALUES(90,9,1,'leader','leader','idle'),(91,9,2,'worker','worker','idle')`,
		`INSERT INTO team_tasks VALUES(100,9,7,'succeeded',UTC_TIMESTAMP()-INTERVAL 3 DAY,NULL)`,
		`UPDATE instances SET started_at=UTC_TIMESTAMP()-INTERVAL 3 DAY WHERE id=2`,
	} {
		if _, e := s.SQL().Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	a := NewInstanceAutoStopRepository(s)
	if e := a.Initialize(t.Context()); e != nil {
		t.Fatal(e)
	}
	r := NewTeamLifecycleRepository(s)
	if e := r.Initialize(t.Context()); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{`UPDATE instance_model_activity SET monitoring_since=UTC_TIMESTAMP()-INTERVAL 3 DAY`, `UPDATE team_runtime_lifecycle SET observed_since=UTC_TIMESTAMP()-INTERVAL 3 DAY`} {
		if _, e := s.SQL().Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	return s, r
}

func TestTeamLifecycleReadinessProjectionOffersRepairWithoutRewritingLedgerMySQL(t *testing.T) {
	s, r := teamIdleFixture(t)
	ctx := t.Context()
	if _, err := s.SQL().Exec(`UPDATE instances SET status='error' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	v, err := r.Get(ctx, 9)
	if err != nil || v.State != "recovery_required" || !v.CanStart {
		t.Fatalf("failed member hidden: %+v %v", v, err)
	}
	var state string
	if err = teamQueryRow(ctx, s, `SELECT state FROM team_runtime_lifecycle WHERE team_id=9`).Scan(&state); err != nil || state != "running" {
		t.Fatalf("read rewrote operation: %q %v", state, err)
	}
	if _, err = s.SQL().Exec(`UPDATE instances SET status='running' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	v, err = r.Get(ctx, 9)
	if err != nil || v.State != "running" || v.CanStart {
		t.Fatalf("healthy team remains in repair: %+v %v", v, err)
	}
}

func TestTeamManualStopFencesNewInferenceBeforeDrainingExistingRequestMySQL(t *testing.T) {
	s, r := teamIdleFixture(t)
	ctx := t.Context()
	a := NewInstanceAutoStopRepository(s)
	key, err := a.BeginModelActivity(ctx, 7, 1, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Request(ctx, 9, "stopped", "idle_timeout", 1); err == nil {
		t.Fatal("automatic stop admitted active inference")
	}
	op, err := r.Request(ctx, 9, "stopped", "manual", 0)
	if err != nil {
		t.Fatal(err)
	}
	if op.State != "stopping" {
		t.Fatalf("stop intent not fenced: %+v", op)
	}
	if _, err = a.BeginModelActivity(ctx, 7, 1, 2, ""); !errors.Is(err, models.ErrInstanceStopping) {
		t.Fatalf("new inference entered stopping Team: %v", err)
	}
	active, err := a.GetActivity(ctx, 1)
	if err != nil || active.ActiveRequests != 1 {
		t.Fatalf("current request was cancelled or erased: %+v %v", active, err)
	}
	if err = r.Result(ctx, op, 0, models.ErrInstanceStopping); err != nil {
		t.Fatal(err)
	}
	wait, err := r.Get(ctx, 9)
	if err != nil || wait.State != "stopping" || wait.Progress != 0 {
		t.Fatalf("draining reported stopped: %+v %v", wait, err)
	}
	var running int
	if err = teamQueryRow(ctx, s, `SELECT COUNT(*) FROM instances i JOIN team_members m ON m.instance_id=i.id WHERE m.team_id=9 AND i.status='running'`).Scan(&running); err != nil || running != 2 {
		t.Fatalf("drain prematurely changed members: %d %v", running, err)
	}
	if err = a.FinishModelActivity(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err = teamMembersNoRequests(ctx, s, 9); err != nil {
		t.Fatalf("completed request did not drain: %v", err)
	}
	again, err := r.Request(ctx, 9, "stopped", "manual", 0)
	if err != nil || again.OperationID != op.OperationID {
		t.Fatalf("retry replaced the stop: %+v %v", again, err)
	}
}

func TestTeamManualDrainStillRejectsBusinessTasksAndUnconfirmedWritesMySQL(t *testing.T) {
	for _, tc := range []struct{ name, query string }{
		{"active_task", `UPDATE team_tasks SET status='running' WHERE id=100`},
		{"native_task", `INSERT INTO gateway_runtime_tasks(instance_id,generation,finished) VALUES(1,2,FALSE)`},
		{"workspace_write", `INSERT INTO instance_workspace_write_intents(instance_id) VALUES(1)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r := teamIdleFixture(t)
			if _, err := s.SQL().Exec(tc.query); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Request(t.Context(), 9, "stopped", "manual", 0); err == nil {
				t.Fatal("manual drain bypassed an active task or unconfirmed write")
			}
		})
	}
}

func TestTeamIdleMySQLTerminalParentDoesNotInventCompletionOrBlockManualMaintenance(t *testing.T) {
	s, r := teamIdleFixture(t)
	for _, q := range []string{
		`UPDATE team_tasks SET status='failed',finished_at=UTC_TIMESTAMP()-INTERVAL 3 DAY WHERE id=100`,
		`INSERT INTO team_work_items(team_id,status,root_task_id) VALUES(9,'running',100)`,
	} {
		if _, err := s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Request(t.Context(), 9, "stopped", "idle_timeout", 1); err == nil {
		t.Fatal("failed latest user task admitted automatic stop")
	}
	if _, err := r.Request(t.Context(), 9, "stopped", "manual", 0); err != nil {
		t.Fatalf("terminal parent permanently blocked safe maintenance: %v", err)
	}
	var status string
	if err := teamQueryRow(t.Context(), s, `SELECT status FROM team_work_items WHERE team_id=9`).Scan(&status); err != nil || status != "running" {
		t.Fatalf("lifecycle rewrote business receipt: %s %v", status, err)
	}
}

func TestTeamIdleMySQLTerminalParentRetainsExecutionAndUnknownParentProtection(t *testing.T) {
	for _, tc := range []struct {
		name, parent, blocker string
		allowed               bool
	}{
		{"retired_failure_after_new_success", "failed", "", true},
		{"active_parent", "running", "", false},
		{"success_with_inconsistent_work", "succeeded", "", false},
		{"missing_parent", "missing", "", false},
		{"different_team_parent", "foreign", "", false},
		{"unfinished_model", "failed", `INSERT INTO instance_model_activity_requests(request_id,instance_id,generation,started_at) VALUES('live',2,1,UTC_TIMESTAMP())`, false},
		{"unfinished_command", "failed", `INSERT INTO instance_commands VALUES(2,'running')`, false},
		{"pending_notification", "failed", `INSERT INTO team_event_outbox VALUES(9,'pending')`, false},
		{"pending_write", "failed", `INSERT INTO team_workspace_write_intents VALUES(9)`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r := teamIdleFixture(t)
			parentTeam, parentStatus := 9, tc.parent
			if tc.parent == "foreign" {
				parentTeam, parentStatus = 10, "failed"
			}
			if tc.parent != "missing" {
				if _, err := s.SQL().Exec(`INSERT INTO team_tasks VALUES(99,?,7,?,UTC_TIMESTAMP()-INTERVAL 4 DAY,NULL)`, parentTeam, parentStatus); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.SQL().Exec(`INSERT INTO team_work_items(team_id,status,root_task_id) VALUES(9,'running',99)`); err != nil {
				t.Fatal(err)
			}
			if tc.blocker != "" {
				if _, err := s.SQL().Exec(tc.blocker); err != nil {
					t.Fatal(err)
				}
			}
			_, err := r.Request(t.Context(), 9, "stopped", "idle_timeout", 1)
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v error=%v", tc.allowed, err)
			}
		})
	}
}

func TestTeamIdleMySQLWholeTeamAdmission(t *testing.T) {
	s, r := teamIdleFixture(t)
	ctx := t.Context()
	// Fresh member heartbeats/updated_at are deliberately irrelevant.
	if _, e := s.SQL().Exec(`ALTER TABLE team_members ADD last_seen_at DATETIME(6),ADD updated_at DATETIME(6)`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SQL().Exec(`UPDATE team_members SET last_seen_at=UTC_TIMESTAMP(),updated_at=UTC_TIMESTAMP()`); e != nil {
		t.Fatal(e)
	}
	a := NewInstanceAutoStopRepository(s)
	ordinary, e := a.Candidates(ctx, time.Now().UTC(), 1, 0, 100)
	if e != nil || len(ordinary) != 0 {
		t.Fatalf("Team member leaked into ordinary scan: %+v %v", ordinary, e)
	}
	op, e := r.Request(ctx, 9, "stopped", "idle_timeout", 1)
	if e != nil {
		t.Fatal(e)
	}
	if op.State != "stopping" || len(op.Members) != 2 {
		t.Fatalf("not a whole Team operation: %+v", op)
	}
	same, e := r.Request(ctx, 9, "stopped", "idle_timeout", 1)
	if e != nil || same.OperationID != op.OperationID {
		t.Fatalf("duplicate request: %+v %v", same, e)
	}
	if _, e = a.BeginModelActivity(ctx, 7, 1, 2, "gw-1-2"); !errors.Is(e, models.ErrInstanceStopping) {
		t.Fatalf("model admitted during stop: %v", e)
	}
	if _, e = a.ClaimStop(ctx, 1, 0, models.InstanceIdleStopOperation{InstanceID: 1, UserID: 7, Generation: 2}); e == nil {
		t.Fatal("single member stop bypassed Team operation")
	}
	if e = r.Result(ctx, op, 1, errors.New("unconfirmed worker stop")); e != nil {
		t.Fatal(e)
	}
	state, e := r.Get(ctx, 9)
	if e != nil || state.State != "recovery_required" || state.Progress != 1 {
		t.Fatalf("partial failure became success: %+v %v", state, e)
	}
}

func TestTeamIdleMySQLExplicitStartRepairsFailedMemberWithoutClosingTask(t *testing.T) {
	s, r := teamIdleFixture(t)
	ctx := t.Context()
	initial, e := r.Request(ctx, 9, "running", "manual", 0)
	if e != nil || initial.State != "running" {
		t.Fatalf("healthy replay: %+v %v", initial, e)
	}
	for _, q := range []string{`UPDATE instances SET status='error' WHERE id=1`, `UPDATE team_tasks SET status='running',finished_at=NULL WHERE id=100`, `INSERT INTO instance_commands(instance_id,status) VALUES(2,'running')`} {
		if _, e = s.SQL().Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = r.Request(ctx, 9, "running", "manual", 0); e == nil {
		t.Fatal("repair admitted during another member's command")
	}
	if _, e = s.SQL().Exec(`DELETE FROM instance_commands`); e != nil {
		t.Fatal(e)
	}
	op, e := r.Request(ctx, 9, "running", "manual", 0)
	if e != nil || op.State != "starting" || len(op.Members) != 2 || op.OperationID == "" {
		t.Fatalf("repair not frozen as one Team start: %+v %v", op, e)
	}
	same, e := r.Request(ctx, 9, "running", "manual", 0)
	if e != nil || same.OperationID != op.OperationID {
		t.Fatalf("duplicate repair: %+v %v", same, e)
	}
	var status string
	if e = teamQueryRow(ctx, s, `SELECT status FROM team_tasks WHERE id=100`).Scan(&status); e != nil || status != "running" {
		t.Fatalf("repair rewrote business outcome: %s %v", status, e)
	}
	if _, e = r.Request(ctx, 9, "stopped", "manual", 0); e == nil {
		t.Fatal("repair allowed busy Team to stop")
	}
}

func TestTeamColdStartSnapshotOnlyInheritsConfirmedWholeStop(t *testing.T) {
	s, r := teamIdleFixture(t)
	ctx := t.Context()
	op, e := r.Request(ctx, 9, "stopped", "manual", 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SQL().Exec(`UPDATE instances SET status='stopped'`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SQL().Exec(`DELETE FROM instance_runtime_bindings`); e != nil {
		t.Fatal(e)
	}
	if e = r.Result(ctx, op, len(op.Members), nil); e != nil {
		t.Fatal(e)
	}
	start, e := r.Request(ctx, 9, "running", "manual", 0)
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range start.Members {
		if m.ColdStopOperationID != op.OperationID || m.ColdStoppedAt == nil {
			t.Fatalf("confirmed unit stop not inherited: %+v", m)
		}
	}
	if e = r.Result(ctx, start, 1, errors.New("start failed")); e != nil {
		t.Fatal(e)
	}
	stop, e := r.Request(ctx, 9, "stopped", "manual", 0)
	if e != nil {
		t.Fatal(e)
	}
	if e = r.Result(ctx, stop, 1, errors.New("source stop uncertain")); e != nil {
		t.Fatal(e)
	}
	start, e = r.Request(ctx, 9, "running", "manual", 0)
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range start.Members {
		if m.ColdStopOperationID != "" || m.ColdStoppedAt != nil {
			t.Fatal("partial stop granted a cold proof")
		}
	}
}

func TestTeamIdleMySQLBusinessBlockers(t *testing.T) {
	for _, test := range []struct{ name, q string }{
		{"last_user_not_complete", `UPDATE team_tasks SET status='waiting',finished_at=NULL`},
		{"last_user_failed", `UPDATE team_tasks SET status='failed'`},
		{"fresh_business_activity", `UPDATE instance_model_activity SET last_gateway_activity_at=UTC_TIMESTAMP() WHERE instance_id=2`},
		{"unfinished_request", `INSERT INTO instance_model_activity_requests(request_id,instance_id,generation,started_at) VALUES('active',2,1,UTC_TIMESTAMP()-INTERVAL 3 DAY)`},
		{"waiting_review", `INSERT INTO team_work_items(team_id,status,superseded_by) VALUES(9,'awaiting_review',NULL)`},
		{"pending_notification", `INSERT INTO team_event_outbox VALUES(9,'pending')`},
		{"unknown_status", `UPDATE team_tasks SET status='unrecognized'`},
		{"new_observation_grace", `UPDATE team_runtime_lifecycle SET observed_since=UTC_TIMESTAMP()`},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, r := teamIdleFixture(t)
			if _, e := s.SQL().Exec(test.q); e != nil {
				t.Fatal(e)
			}
			if _, e := r.Request(t.Context(), 9, "stopped", "idle_timeout", 1); e == nil {
				t.Fatal("unsafe idle stop admitted")
			}
			v, e := r.Get(t.Context(), 9)
			if e != nil || v.State != "running" {
				t.Fatalf("blocked request mutated state: %+v %v", v, e)
			}
		})
	}
}

func TestTeamIdleMySQLConcurrentAdmission(t *testing.T) {
	s, r := teamIdleFixture(t)
	ctx := t.Context()
	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	go func() { defer wg.Done(); _, e := r.Request(ctx, 9, "stopped", "idle_timeout", 1); errs <- e }()
	go func() {
		defer wg.Done()
		e := s.Tx(func(tx db.Session) error {
			if e := RequireTeamRunning(ctx, tx, 9); e != nil {
				return e
			}
			_, e := tx.SQL().Exec(`INSERT INTO team_tasks VALUES(101,9,7,'pending',NULL,NULL)`)
			return e
		})
		errs <- e
	}()
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("task and stop must not both be admitted: successes=%d", success)
	}
}

func TestTeamIdleMySQLAdoptsConfirmedColdTeamAndRetriesOriginalStart(t *testing.T) {
	s, r := teamIdleFixture(t)
	for _, q := range []string{`DELETE FROM team_runtime_lifecycle`, `UPDATE instances SET status='stopped' WHERE id IN (1,2)`, `DELETE FROM instance_runtime_bindings`} {
		if _, e := s.SQL().Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	if e := r.Initialize(t.Context()); e != nil {
		t.Fatal(e)
	}
	v, e := r.Get(t.Context(), 9)
	if e != nil || v.State != "stopped" {
		t.Fatalf("cold Team not startable: %+v %v", v, e)
	}
	op, e := r.Request(t.Context(), 9, "running", "manual", 0)
	if e != nil {
		t.Fatal(e)
	}
	if op.State != "starting" || len(op.Members) != 2 {
		t.Fatalf("original member snapshot lost: %+v", op)
	}
	if e = r.Result(t.Context(), op, 1, errors.New("capacity unavailable")); e != nil {
		t.Fatal(e)
	}
	failed, readErr := r.Get(t.Context(), 9)
	if readErr != nil || !failed.CanStart || !failed.CanStop {
		t.Fatalf("failed startup cannot be recovered or cancelled: %+v %v", failed, readErr)
	}
	retry, e := r.Request(t.Context(), 9, "running", "manual", 0)
	if e != nil || retry.OperationID != op.OperationID {
		t.Fatalf("retry replaced operation: %+v %v", retry, e)
	}
}

func TestTeamIdleMySQLRetiredRequestsDoNotCountAsLiveExecution(t *testing.T) {
	s, r := teamIdleFixture(t)
	for _, q := range []string{
		`INSERT INTO instance_model_activity_requests(request_id,instance_id,generation,started_at) VALUES('retired',1,1,UTC_TIMESTAMP()-INTERVAL 3 DAY)`,
		`INSERT INTO gateway_runtime_tasks(instance_id,generation,finished) VALUES(1,1,FALSE)`,
	} {
		if _, e := s.SQL().Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := r.Request(t.Context(), 9, "stopped", "idle_timeout", 1); e != nil {
		t.Fatalf("retired generation blocked current idle Team: %v", e)
	}
}

func TestTeamStartLeaseSurvivesPartialRecoveryWithoutGrantingTasks(t *testing.T) {
	s, r := teamIdleFixture(t)
	ctx := t.Context()
	for _, q := range []string{`DELETE FROM team_runtime_lifecycle`, `UPDATE instances SET status='stopped' WHERE id IN (1,2)`, `DELETE FROM instance_runtime_bindings`} {
		if _, err := s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	op, err := r.Request(ctx, 9, "running", "manual", 0)
	if err != nil {
		t.Fatal(err)
	}
	repo := &teamRepository{sess: s}
	check := func(want bool) {
		t.Helper()
		allowed, err := repo.AuthorizeTeamMemberStartup(ctx, 9, 90, 1)
		if err != nil || allowed != want {
			t.Fatalf("member lease admission=%v want=%v err=%v", allowed, want, err)
		}
		if foreign, err := repo.AuthorizeTeamMemberStartup(ctx, 9, 90, 2); err != nil || foreign {
			t.Fatal("foreign member admitted", err)
		}
	}
	check(true)
	if err := r.Result(ctx, op, 1, errors.New("other member unavailable")); err != nil {
		t.Fatal(err)
	}
	check(true)
	if err := s.Tx(func(tx db.Session) error { return RequireTeamRunning(ctx, tx, 9) }); !errors.Is(err, ErrTeamNotRunning) {
		t.Fatalf("partial recovery admitted task: %v", err)
	}
	op, err = r.Request(ctx, 9, "running", "manual", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Result(ctx, op, len(op.Members), nil); err != nil {
		t.Fatal(err)
	}
	check(true)
	if _, err := r.Request(ctx, 9, "stopped", "manual", 0); err != nil {
		t.Fatal(err)
	}
	check(false)
	if _, err := s.SQL().Exec(`UPDATE team_runtime_lifecycle SET state='recovery_required' WHERE team_id=9`); err != nil {
		t.Fatal(err)
	}
	check(false)
}

func TestTeamDurableStopWaitRemainsPendingAndIsBounded(t *testing.T) {
	_, r := teamIdleFixture(t)
	ctx := t.Context()
	op, err := r.Request(ctx, 9, "stopped", "manual", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Result(ctx, op, 1, models.ErrInstanceStopping); err != nil {
		t.Fatal(err)
	}
	current, err := r.Get(ctx, 9)
	if err != nil || current.State != "stopping" || current.OperationID != op.OperationID || current.Progress != 1 {
		t.Fatalf("async stop lost operation: %+v %v", current, err)
	}
	// Late output from a different operation cannot overwrite pending progress.
	stale := *op
	stale.OperationID = "stale"
	if err := r.Result(ctx, &stale, 2, nil); err != nil {
		t.Fatal(err)
	}
	current, err = r.Get(ctx, 9)
	if err != nil || current.State != "stopping" {
		t.Fatal("stale stop completed current operation", err)
	}
	op.Attempts = 6
	if err := r.Result(ctx, op, 1, models.ErrInstanceStopping); err != nil {
		t.Fatal(err)
	}
	current, err = r.Get(ctx, 9)
	if err != nil || current.State != "recovery_required" || current.Progress != 1 {
		t.Fatalf("unconfirmed stop retried without limit: %+v %v", current, err)
	}
	// A manual retry resumes the same frozen operation and completes only on proof.
	op, err = r.Request(ctx, 9, "stopped", "manual", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Result(ctx, op, len(op.Members), nil); err != nil {
		t.Fatal(err)
	}
	current, err = r.Get(ctx, 9)
	if err != nil || current.State != "stopped" {
		t.Fatal("confirmed stop not completed", err)
	}
}

func TestTeamIdleMySQLColdProtectionIsAtomicAndCannotUpgradeLiveSources(t *testing.T) {
	s, r := teamIdleFixture(t)
	ctx := t.Context()
	for _, q := range []string{`DELETE FROM team_runtime_lifecycle`, `ALTER TABLE instances ADD runtime_security_profile VARCHAR(64) NOT NULL DEFAULT ''`, `ALTER TABLE teams ADD preview_security_profile VARCHAR(64) NOT NULL DEFAULT ''`, `DELETE FROM instance_runtime_bindings WHERE instance_id IN(1,2)`, `UPDATE instances SET status='stopped' WHERE id IN(1,2)`} {
		if _, err := s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	op, err := r.Request(ctx, 9, "running", "manual", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE instances SET status='running' WHERE id=1`, `UPDATE instances SET status='stopped',runtime_security_profile='unknown' WHERE id=1`, `UPDATE instances SET runtime_security_profile='',instance_mode='pro' WHERE id=1`} {
		if _, err = s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
		if err = r.ProtectColdStart(ctx, op, "isolated-network-v1"); err == nil {
			t.Fatal("unsafe protection admitted", q)
		}
		var n int
		if err = teamQueryRow(ctx, s, `SELECT COUNT(*) FROM teams WHERE preview_security_profile<>''`).Scan(&n); err != nil || n != 0 {
			t.Fatal("failed transition wrote marker", err, n)
		}
	}
	if _, err = s.SQL().Exec(`UPDATE instances SET status='stopped',runtime_security_profile='',instance_mode='lite' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err = r.ProtectColdStart(ctx, op, "isolated-network-v1"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = teamQueryRow(ctx, s, `SELECT COUNT(*) FROM instances WHERE id IN(1,2) AND runtime_security_profile='isolated-network-v1'`).Scan(&n); err != nil || n != 2 {
		t.Fatal(err, n)
	}
	repo := &teamRepository{sess: s}
	if allowed, err := repo.AuthorizeTeamMemberStartup(ctx, 9, 90, 1); err != nil || !allowed {
		t.Fatal("original startup rejected", err)
	}
	if allowed, err := repo.AuthorizeTeamMemberStartup(ctx, 9, 90, 2); err != nil || allowed {
		t.Fatal("foreign startup admitted", err)
	}
	if _, err = s.SQL().Exec(`UPDATE instances SET status='creating' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err = r.ProtectColdStart(ctx, op, "isolated-network-v1"); err != nil {
		t.Fatal("safe same-operation resume rejected", err)
	}
	wrong := *op
	wrong.OperationID = "stale"
	if err = r.ProtectColdStart(ctx, &wrong, "isolated-network-v1"); err == nil {
		t.Fatal("stale operation admitted")
	}
}

func TestTeamIdleMySQLUsesSharedSecondsTimeout(t *testing.T) {
	s, r := teamIdleFixture(t)
	if _, err := s.SQL().Exec(`UPDATE team_tasks SET finished_at=UTC_TIMESTAMP()-INTERVAL 5 MINUTE WHERE id=100`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.RequestForTimeout(t.Context(), 9, "stopped", "idle_timeout", time.Hour); err == nil {
		t.Fatal("stopped before configured idle timeout")
	}
	op, err := r.RequestForTimeout(t.Context(), 9, "stopped", "idle_timeout", 2*time.Minute)
	if err != nil || op.State != "stopping" || len(op.Members) != 2 {
		t.Fatalf("seconds setting did not admit the whole Team: %+v %v", op, err)
	}
}
