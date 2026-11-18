package repository

import (
	"errors"
	"testing"
	"time"

	"clawreef/internal/models"
)

func manualStartIntentRepository(t *testing.T, runtimeType string) (*instanceRepository, models.RuntimePod) {
	t.Helper()
	r := idleRuntimeRepository(t)
	for _, query := range []string{
		`ALTER TABLE instance_desired_state ADD id INT AUTO_INCREMENT UNIQUE,ADD desired_config_revision_id INT NULL,ADD desired_runtime_action VARCHAR(50) NULL,ADD updated_by INT NULL`,
		`CREATE TABLE runtime_pods (id BIGINT PRIMARY KEY,pod_uid VARCHAR(64),namespace VARCHAR(64),pod_name VARCHAR(64),deployment_name VARCHAR(64),runtime_type VARCHAR(32))`,
		`CREATE TABLE instance_runtime_bindings (instance_id INT PRIMARY KEY,runtime_pod_id BIGINT,gateway_id VARCHAR(128),generation INT,state VARCHAR(32))`,
		`CREATE TABLE runtime_management_admission_lock (id INT PRIMARY KEY,revision INT)`,
		`INSERT INTO runtime_management_admission_lock VALUES (1,0)`,
		`CREATE TABLE runtime_management_operations (id VARCHAR(64) PRIMARY KEY,kind VARCHAR(32),status VARCHAR(32),request_json JSON)`,
		`CREATE TABLE instance_lifecycle_manual_guards(instance_id INT PRIMARY KEY,token VARCHAR(64))`,
		`CREATE TABLE runtime_pod_deletion_intents (namespace VARCHAR(64),deployment_name VARCHAR(64),deployment_uid VARCHAR(64))`,
	} {
		if _, err := r.sess.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.sess.SQL().Exec(`UPDATE instances SET type=? WHERE id=7`, runtimeType); err != nil {
		t.Fatal(err)
	}
	if _, err := r.sess.SQL().Exec(`INSERT INTO runtime_pods VALUES(9,'pod-uid','runtime','runtime-pod','pool',?)`, runtimeType); err != nil {
		t.Fatal(err)
	}
	uid := "pod-uid"
	return r, models.RuntimePod{ID: 9, PodUID: &uid, Namespace: "runtime", PodName: "runtime-pod", DeploymentName: "pool", RuntimeType: runtimeType}
}

func assertManualStartPower(t *testing.T, r *instanceRepository, want string) {
	t.Helper()
	state, err := NewInstanceDesiredStateRepository(r.sess).GetByInstanceID(7)
	if err != nil || state == nil || state.DesiredPowerState != want {
		t.Fatalf("desired power: got=%+v err=%v want=%s", state, err, want)
	}
}

func TestManualStartIntentMySQLCompletedStopAllowsPublication(t *testing.T) {
	for _, runtimeType := range []string{"openclaw", "opencode", "dsh", "hermes"} {
		t.Run(runtimeType, func(t *testing.T) {
			r, pod := manualStartIntentRepository(t, runtimeType)
			ctx := t.Context()
			store := NewInstanceAutoStopRepository(r.sess)
			op, err := store.BeginManualStop(ctx, models.InstanceIdleStopOperation{InstanceID: 7, UserID: 3, Generation: 2, RuntimePodID: 9, GatewayID: "gw-7-2", PodUID: "pod-uid", Endpoint: "http://runtime", WorkspacePath: "/workspaces/original", StorageIdentity: "persistent-home"})
			if err != nil {
				t.Fatal(err)
			}
			if err = store.ConfirmTeardown(ctx, *op); err != nil {
				t.Fatal(err)
			}
			if err = store.CompleteManualStop(ctx, *op); err != nil {
				t.Fatal(err)
			}
			if _, err = r.sess.SQL().Exec(`UPDATE instance_desired_state SET desired_config_revision_id=81,desired_runtime_action='reload_config',updated_by=3 WHERE instance_id=7`); err != nil {
				t.Fatal(err)
			}
			before, err := store.GetActivity(ctx, 7)
			if err != nil {
				t.Fatal(err)
			}
			if err = store.BeginManualStart(ctx, 7, time.Now().Add(2*time.Minute)); err != nil {
				t.Fatal(err)
			}
			assertManualStartPower(t, r, "running")
			state, err := NewInstanceDesiredStateRepository(r.sess).GetByInstanceID(7)
			if err != nil || state.DesiredConfigRevisionID == nil || *state.DesiredConfigRevisionID != 81 || state.DesiredRuntimeAction == nil || *state.DesiredRuntimeAction != "reload_config" || state.UpdatedBy == nil || *state.UpdatedBy != 3 {
				t.Fatalf("Start replaced unrelated desired state: %+v %v", state, err)
			}
			after, err := store.GetActivity(ctx, 7)
			if err != nil || after.StopPhase != "completed" || after.StoppedReason != before.StoppedReason || after.StoppedAt == nil || before.StoppedAt == nil || !after.StoppedAt.Equal(*before.StoppedAt) || after.StartDeadlineAt == nil {
				t.Fatalf("Start lost completed stop receipt: before=%+v after=%+v err=%v", before, after, err)
			}
			if proof, err := store.PreviousIdleStorage(ctx, 7); err != nil || proof != "persistent-home" {
				t.Fatalf("Start lost persistent home proof: %q %v", proof, err)
			}
			if err = r.UpdateRuntimeState(ctx, 7, "creating", 3, nil); err != nil {
				t.Fatal(err)
			}
			if _, err = r.sess.SQL().Exec(`INSERT INTO instance_runtime_bindings VALUES(7,9,'gw-7-3',3,'creating')`); err != nil {
				t.Fatal(err)
			}
			published := false
			err = WithRuntimePodAssignmentAdmission(ctx, r.sess, pod, 7, 3, func() error { published = true; return nil })
			if err != nil || !published {
				t.Fatalf("explicit Start could not publish: published=%v err=%v", published, err)
			}
		})
	}
}

func TestManualStartIntentMySQLCreatesMissingDesiredRow(t *testing.T) {
	r, _ := manualStartIntentRepository(t, "hermes")
	if err := NewInstanceAutoStopRepository(r.sess).BeginManualStart(t.Context(), 7, time.Now().Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertManualStartPower(t, r, "running")
}

func TestManualStartIntentMySQLRejectedOrFailedStartKeepsStopped(t *testing.T) {
	for _, phase := range []string{"stopping", "error", "teardown_confirmed", "write_failure"} {
		t.Run(phase, func(t *testing.T) {
			r, _ := manualStartIntentRepository(t, "hermes")
			ctx := t.Context()
			store := NewInstanceAutoStopRepository(r.sess)
			if phase == "write_failure" {
				if err := store.RecordManualStop(ctx, 7); err != nil {
					t.Fatal(err)
				}
				for _, query := range []string{
					`INSERT INTO instance_desired_state(instance_id,desired_power_state,created_at,updated_at) VALUES(7,'stopped',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
					`CREATE TRIGGER reject_start_intent BEFORE UPDATE ON instance_desired_state FOR EACH ROW BEGIN IF NEW.desired_power_state='running' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected Start intent failure'; END IF; END`,
				} {
					if _, err := r.sess.SQL().Exec(query); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				op, err := store.BeginManualStop(ctx, models.InstanceIdleStopOperation{InstanceID: 7, UserID: 3, Generation: 2})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = r.sess.SQL().Exec(`UPDATE instance_model_activity SET stop_phase=? WHERE instance_id=7`, phase); err != nil {
					t.Fatal(err)
				}
				if _, err = r.sess.SQL().Exec(`UPDATE instance_idle_stop_operations SET phase=? WHERE id=?`, phase, op.ID); err != nil {
					t.Fatal(err)
				}
			}
			before, err := store.GetActivity(ctx, 7)
			if err != nil {
				t.Fatal(err)
			}
			err = store.BeginManualStart(ctx, 7, time.Now().Add(2*time.Minute))
			if err == nil || (phase != "write_failure" && !errors.Is(err, models.ErrInstanceStopping)) {
				t.Fatalf("Start should reject/rollback: phase=%s err=%v", phase, err)
			}
			assertManualStartPower(t, r, "stopped")
			after, err := store.GetActivity(ctx, 7)
			if err != nil || after.StartDeadlineAt != nil || !after.MonitoringSince.Equal(before.MonitoringSince) || after.StopOperationID != before.StopOperationID || after.StopPhase != before.StopPhase || after.StoppedReason != before.StoppedReason {
				t.Fatalf("failed Start changed durable stop/activity: before=%+v after=%+v err=%v", before, after, err)
			}
		})
	}
}

func TestManualStartIntentMySQLNewerDesiredStopWinsBeforePublication(t *testing.T) {
	r, pod := manualStartIntentRepository(t, "hermes")
	ctx := t.Context()
	store := NewInstanceAutoStopRepository(r.sess)
	if err := store.BeginManualStart(ctx, 7, time.Now().Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertManualStartPower(t, r, "running")
	if err := r.UpdateRuntimeState(ctx, 7, "creating", 3, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.sess.SQL().Exec(`INSERT INTO instance_runtime_bindings VALUES(7,9,'gw-7-3',3,'creating')`); err != nil {
		t.Fatal(err)
	}
	desired := NewInstanceDesiredStateRepository(r.sess)
	intent, err := desired.GetByInstanceID(7)
	if err != nil {
		t.Fatal(err)
	}
	// A concurrent command may commit Stop while this older Start is waiting
	// to publish, without changing its creating status or generation.
	committed := make(chan error, 1)
	go func() {
		intent.DesiredPowerState = "stopped"
		intent.UpdatedAt = time.Now().UTC()
		committed <- desired.Update(intent)
	}()
	if err = <-committed; err != nil {
		t.Fatal(err)
	}
	published := false
	err = WithRuntimePodAssignmentAdmission(ctx, r.sess, pod, 7, 3, func() error { published = true; return nil })
	if published || !errors.Is(err, models.ErrInstanceStopping) {
		t.Fatalf("newer committed Stop lost: published=%v err=%v", published, err)
	}
	assertManualStartPower(t, r, "stopped")
}

func TestManualStartIntentMySQLIdleStopAllowsPublication(t *testing.T) {
	r, pod := manualStartIntentRepository(t, "hermes")
	ctx := t.Context()
	store := NewInstanceAutoStopRepository(r.sess)
	for _, query := range []string{
		`CREATE TABLE instance_workspace_write_intents(instance_id INT)`,
		`CREATE TABLE instance_commands(instance_id INT,status VARCHAR(32))`,
		`CREATE TABLE gateway_runtime_tasks(instance_id INT,generation INT,finished BOOLEAN)`,
		`UPDATE instances SET status='running',started_at=UTC_TIMESTAMP()-INTERVAL 2 DAY WHERE id=7`,
		`UPDATE instance_model_activity SET monitoring_since=UTC_TIMESTAMP()-INTERVAL 2 DAY WHERE instance_id=7`,
		`INSERT INTO instance_runtime_bindings VALUES(7,9,'gw-7-2',2,'running')`,
	} {
		if _, err := r.sess.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	activity, err := store.GetActivity(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := store.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	op, err := store.ClaimStop(ctx, cfg.Version, activity.ActivityVersion, models.InstanceIdleStopOperation{InstanceID: 7, UserID: 3, Generation: 2, RuntimePodID: 9, GatewayID: "gw-7-2", PodUID: "pod-uid", StorageIdentity: "persistent-home"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.ConfirmTeardown(ctx, *op); err != nil {
		t.Fatal(err)
	}
	if _, err = r.sess.SQL().Exec(`DELETE FROM instance_runtime_bindings WHERE instance_id=7`); err != nil {
		t.Fatal(err)
	}
	if err = store.CompleteStop(ctx, *op); err != nil {
		t.Fatal(err)
	}
	if err = store.BeginManualStart(ctx, 7, time.Now().Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertManualStartPower(t, r, "running")
	activity, err = store.GetActivity(ctx, 7)
	if err != nil || activity.StoppedReason != "idle_timeout" || activity.StopPhase != "completed" {
		t.Fatalf("Start lost idle stop receipt before readiness: %+v %v", activity, err)
	}
	if err = r.UpdateRuntimeState(ctx, 7, "creating", 3, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = r.sess.SQL().Exec(`INSERT INTO instance_runtime_bindings VALUES(7,9,'gw-7-3',3,'creating')`); err != nil {
		t.Fatal(err)
	}
	published := false
	err = WithRuntimePodAssignmentAdmission(ctx, r.sess, pod, 7, 3, func() error { published = true; return nil })
	if err != nil || !published {
		t.Fatalf("Start after idle Stop could not publish: published=%v err=%v", published, err)
	}
}

func TestManualStartIntentMySQLAbortedStartRestoresStopped(t *testing.T) {
	r, _ := manualStartIntentRepository(t, "hermes")
	ctx := t.Context()
	store := NewInstanceAutoStopRepository(r.sess)
	if err := store.RecordManualStop(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if err := store.BeginManualStart(ctx, 7, time.Now().Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertManualStartPower(t, r, "running")
	if err := store.ClearStartDeadline(ctx, 7); err != nil {
		t.Fatal(err)
	}
	assertManualStartPower(t, r, "stopped")
	after, err := store.GetActivity(ctx, 7)
	if err != nil || after.StartDeadlineAt != nil || after.StoppedReason != "manual" || after.StopPhase != "completed" {
		t.Fatalf("aborted Start lost completed stop/deadline: %+v %v", after, err)
	}
}
