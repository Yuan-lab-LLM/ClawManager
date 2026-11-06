package repository

import (
	"clawreef/internal/models"
	"errors"
	"fmt"
	"github.com/upper/db/v4"
	"os"
	"strings"
	"testing"
	"time"
)

func idleRuntimeRepository(t *testing.T) *instanceRepository {
	t.Helper()
	r := runtimeSecurityMySQL(t)
	raw, err := os.ReadFile("../db/migrations/096_add_instance_auto_stop.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(q) != "" {
			if _, err = r.sess.SQL().Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err = r.sess.SQL().Exec("UPDATE instances SET instance_mode='lite',type='openclaw',status='stopped' WHERE id=7"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`CREATE TABLE instance_agents (instance_id INT,status VARCHAR(32),session_token VARCHAR(255),session_expires_at DATETIME(6),updated_at DATETIME(6))`, `CREATE TABLE instance_desired_state(instance_id INT PRIMARY KEY,desired_power_state VARCHAR(32),created_at DATETIME(6),updated_at DATETIME(6))`} {
		if _, err = r.sess.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err = NewInstanceAutoStopRepository(r.sess).Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestIdleStartConfirmationClearsDeadlineAndResetsWindow(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(map[bool]string{false: "runtime", true: "full-heartbeat"}[full], func(t *testing.T) {
			r := idleRuntimeRepository(t)
			store := NewInstanceAutoStopRepository(r.sess)
			ctx := t.Context()
			if err := store.BeginManualStart(ctx, 7, time.Now().Add(120*time.Second)); err != nil {
				t.Fatal(err)
			}
			if err := r.UpdateRuntimeState(ctx, 7, "creating", 3, nil); err != nil {
				t.Fatal(err)
			}
			before := time.Now().UTC().Add(-time.Second)
			if _, err := r.sess.SQL().Exec("UPDATE instance_model_activity SET monitoring_since=UTC_TIMESTAMP()-INTERVAL 2 DAY WHERE instance_id=7"); err != nil {
				t.Fatal(err)
			}
			if full {
				i, err := r.GetByID(7)
				if err != nil {
					t.Fatal(err)
				}
				i.Status = "running"
				if err = r.Update(i); err != nil {
					t.Fatal(err)
				}
			} else if err := r.UpdateRuntimeState(ctx, 7, "running", 3, nil); err != nil {
				t.Fatal(err)
			}
			a, err := store.GetActivity(ctx, 7)
			if err != nil {
				t.Fatal(err)
			}
			if a.StartDeadlineAt != nil || a.MonitoringSince.Before(before) {
				t.Fatalf("confirmed Start retained deadline/window: %+v", a)
			}
			i, err := r.GetByID(7)
			if err != nil {
				t.Fatal(err)
			}
			if i.StartedAt == nil || i.StartedAt.Before(before) {
				t.Fatalf("readiness StartedAt not persisted: %+v", i.StartedAt)
			}
			if err = store.BeginManualStart(ctx, 7, time.Now().Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			if err = r.UpdateRuntimeState(ctx, 7, "running", 2, nil); !errors.Is(err, ErrStaleRuntimeGeneration) {
				t.Fatalf("stale update=%v", err)
			}
			a, _ = store.GetActivity(ctx, 7)
			if a.StartDeadlineAt == nil {
				t.Fatal("stale generation cleared newer deadline")
			}
		})
	}
}

func TestIdleManualStopCannotConvertAcceptedGracefulStop(t *testing.T) {
	s := idleStopMySQL(t)
	r := NewInstanceAutoStopRepository(s)
	ctx := t.Context()
	if err := r.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SQL().Exec("UPDATE instance_model_activity SET monitoring_since=UTC_TIMESTAMP()-INTERVAL 2 DAY WHERE instance_id=1"); err != nil {
		t.Fatal(err)
	}
	a, _ := r.GetActivity(ctx, 1)
	cfg, _ := r.Settings(ctx)
	op, err := r.ClaimStop(ctx, cfg.Version, a.ActivityVersion, models.InstanceIdleStopOperation{InstanceID: 1, UserID: 7, Generation: 2, RuntimePodID: 11, GatewayID: "gw-1-2", PodUID: "pod"})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.RecordManualStop(ctx, 1); err == nil {
		t.Fatal("manual Stop completed accepted graceful operation without teardown proof")
	}
	after, _ := r.GetActivity(ctx, 1)
	if after.StopOperationID != op.ID {
		t.Fatal("manual Stop erased graceful operation")
	}
}

func TestIdleExpiredStartRemainsStoppedAfterDeadlineClear(t *testing.T) {
	r := idleRuntimeRepository(t)
	store := NewInstanceAutoStopRepository(r.sess)
	ctx := t.Context()
	if err := store.BeginManualStart(ctx, 7, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateRuntimeState(ctx, 7, "creating", 3, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.sess.SQL().Exec("UPDATE instance_model_activity SET stopped_reason='',start_deadline_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND WHERE instance_id=7"); err != nil {
		t.Fatal(err)
	}
	changed, err := store.FenceExpiredStart(ctx, 7)
	if err != nil || !changed {
		t.Fatalf("expire=%v %v", changed, err)
	}
	if err = store.ClearStartDeadline(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if err = r.UpdateRuntimeState(ctx, 7, "running", 3, nil); err == nil {
		t.Fatal("expired Start accepted a late heartbeat after deadline clear")
	}
}

func TestIdleTaskAdmissionAndManagedWritesRejectStoppedTarget(t *testing.T) {
	s := idleStopMySQL(t)
	ctx := t.Context()
	store := NewInstanceAutoStopRepository(s)
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"DROP TABLE gateway_runtime_tasks", "CREATE TABLE users(id INT PRIMARY KEY,is_active BOOLEAN)", "INSERT INTO users VALUES(7,TRUE)", "CREATE TABLE llm_models(id INT PRIMARY KEY,is_active BOOLEAN,visibility_scope VARCHAR(32))", "CREATE TABLE llm_model_user_grants(model_id INT,user_id INT)", "UPDATE instances SET status='stopped' WHERE id=1"} {
		if _, err := s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile("../db/migrations/063_add_gateway_runtime_tasks.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(q) != "" {
			if _, err = s.SQL().Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
	identity := models.GatewayTaskIdentity{InstanceID: 1, UserID: 7, Generation: 2, SessionID: "s", TaskID: "t"}
	if _, err = NewGatewayTaskRepository(s).Check(ctx, identity); err == nil {
		t.Error("task entered stopped target without stop reason")
	}
	if err = checkInstanceAutoStopMutation(ctx, s, 1); err == nil {
		t.Error("managed command/workspace write entered stopped target")
	}
}

func prepareIdleTaskSchema(t *testing.T, s db.Session) {
	t.Helper()
	for _, q := range []string{"DROP TABLE gateway_runtime_tasks", "CREATE TABLE users(id INT PRIMARY KEY,is_active BOOLEAN)", "INSERT INTO users VALUES(7,TRUE)", "CREATE TABLE llm_models(id INT PRIMARY KEY,is_active BOOLEAN,visibility_scope VARCHAR(32))", "CREATE TABLE llm_model_user_grants(model_id INT,user_id INT)"} {
		if _, err := s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile("../db/migrations/063_add_gateway_runtime_tasks.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(q) != "" {
			if _, err = s.SQL().Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestIdleTaskAdmissionSerializesWithStopClaim(t *testing.T) {
	s := idleStopMySQL(t)
	ctx := t.Context()
	store := NewInstanceAutoStopRepository(s)
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	prepareIdleTaskSchema(t, s)
	cfg, err := store.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 12; n++ {
		for _, q := range []string{"UPDATE instances SET status='running' WHERE id=1", "DELETE FROM gateway_runtime_tasks", "UPDATE instance_model_activity SET stop_operation_id='',stop_phase='',monitoring_since=UTC_TIMESTAMP()-INTERVAL 2 DAY WHERE instance_id=1"} {
			if _, err = s.SQL().Exec(q); err != nil {
				t.Fatal(err)
			}
		}
		a, _ := store.GetActivity(ctx, 1)
		start := make(chan struct{})
		admitted := make(chan error, 1)
		claimed := make(chan error, 1)
		go func() {
			<-start
			_, e := NewGatewayTaskRepository(s).Check(ctx, models.GatewayTaskIdentity{InstanceID: 1, UserID: 7, Generation: 2, SessionID: "s", TaskID: fmt.Sprint(n)})
			admitted <- e
		}()
		go func() {
			<-start
			_, e := store.ClaimStop(ctx, cfg.Version, a.ActivityVersion, models.InstanceIdleStopOperation{InstanceID: 1, UserID: 7, Generation: 2, RuntimePodID: 11, GatewayID: "gw-1-2", PodUID: "pod"})
			claimed <- e
		}()
		close(start)
		admitErr, claimErr := <-admitted, <-claimed
		if admitErr == nil && claimErr == nil {
			t.Fatal("task and automatic stop both admitted")
		}
		if admitErr != nil && claimErr != nil {
			t.Fatalf("both rejected: task=%v stop=%v", admitErr, claimErr)
		}
	}
}

func TestIdleAcceptedCommandsBlockAutomaticStop(t *testing.T) {
	for _, status := range []string{"accepted", "batch_pending"} {
		t.Run(status, func(t *testing.T) {
			s := idleStopMySQL(t)
			r := NewInstanceAutoStopRepository(s)
			ctx := t.Context()
			if err := r.Initialize(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SQL().Exec("UPDATE instance_model_activity SET monitoring_since=UTC_TIMESTAMP()-INTERVAL 2 DAY WHERE instance_id=1"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SQL().Exec("INSERT INTO instance_commands(instance_id,status) VALUES(1,?)", status); err != nil {
				t.Fatal(err)
			}
			cfg, _ := r.Settings(ctx)
			a, _ := r.GetActivity(ctx, 1)
			if _, err := r.ClaimStop(ctx, cfg.Version, a.ActivityVersion, models.InstanceIdleStopOperation{InstanceID: 1, UserID: 7, Generation: 2, RuntimePodID: 11, GatewayID: "gw-1-2", PodUID: "pod"}); err == nil {
				t.Fatal("automatic stop ignored nonterminal command")
			}
		})
	}
}

// Added after the user stopped all local execution. This regression has NOT run.
func TestIdleDuplicateStartCannotResetExistingDeadlineOrGeneration(t *testing.T) {
	r := idleRuntimeRepository(t)
	store := NewInstanceAutoStopRepository(r.sess)
	ctx := t.Context()
	deadline := time.Now().UTC().Add(120 * time.Second)
	if err := store.BeginManualStart(ctx, 7, deadline); err != nil {
		t.Fatal(err)
	}
	original, err := store.GetActivity(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	// A concurrent caller before creating is persisted must not replace intent.
	if err = store.BeginManualStart(ctx, 7, deadline.Add(time.Hour)); !errors.Is(err, models.ErrAutoStopConflict) {
		t.Fatalf("duplicate pending intent accepted: %v", err)
	}
	if err = r.UpdateRuntimeState(ctx, 7, "creating", 3, nil); err != nil {
		t.Fatal(err)
	}
	// A later explicit Start must leave the first native attempt and its window intact.
	if err = store.BeginManualStart(ctx, 7, deadline.Add(2*time.Hour)); !errors.Is(err, models.ErrAutoStopConflict) {
		t.Fatalf("duplicate creating Start accepted: %v", err)
	}
	after, err := store.GetActivity(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if after.StartDeadlineAt == nil || original.StartDeadlineAt == nil || !after.StartDeadlineAt.Equal(*original.StartDeadlineAt) || !after.MonitoringSince.Equal(original.MonitoringSince) || after.ActivityVersion != original.ActivityVersion {
		t.Fatalf("duplicate Start changed first attempt: before=%+v after=%+v", original, after)
	}
	var generation int
	row, err := r.sess.SQL().QueryRow("SELECT runtime_generation FROM instances WHERE id=7")
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if generation != 3 {
		t.Fatalf("duplicate Start advanced generation: %d", generation)
	}
}
