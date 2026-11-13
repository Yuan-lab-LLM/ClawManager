package repository

import (
	"context"
	"errors"
	"testing"

	"clawreef/internal/models"
)

func TestMarkDeletingMySQLSupersedesBusyStopWithoutErasingProof(t *testing.T) {
	r := idleRuntimeRepository(t)
	marker, ok := interface{}(r).(interface {
		MarkDeleting(context.Context, int, int, int) error
	})
	if !ok {
		t.Fatal("repository lacks atomic deletion-intent admission")
	}
	ctx := t.Context()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := r.sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE instances SET agent_bootstrap_token='bootstrap' WHERE id=7`)
	exec(`INSERT INTO instance_agents VALUES(7,'online','session',NULL,NOW())`)
	exec(`INSERT INTO instance_desired_state VALUES(7,'running',NOW(),NOW())`)
	exec(`INSERT INTO instance_idle_stop_operations(id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,stop_kind,created_at,updated_at) VALUES('old-stop',7,3,2,9,'gw-7-2','old-pod','https://old','/workspace','home','error','idle',NOW(),NOW())`)
	exec(`UPDATE instance_model_activity SET stop_operation_id='old-stop',stop_phase='error' WHERE instance_id=7`)
	stale, err := r.GetByID(7)
	if err != nil {
		t.Fatal(err)
	}
	release, err := r.acquireLifecycleLock(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := marker.MarkDeleting(ctx, 7, 4, 2); err == nil {
		t.Fatal("changed owner accepted")
	}
	if err := marker.MarkDeleting(ctx, 7, 3, 1); err == nil {
		t.Fatal("stale generation accepted")
	}
	exec(`INSERT INTO gateway_runtime_upgrades VALUES(7,3,2,'12345678901234567890123456789012','starting',NOW(),NOW())`)
	if err := marker.MarkDeleting(ctx, 7, 3, 2); !errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
		t.Fatalf("deletion bypassed active upgrade guard: %v", err)
	}
	exec(`DELETE FROM gateway_runtime_upgrades WHERE instance_id=7`)
	if err := marker.MarkDeleting(ctx, 7, 3, 2); err != nil {
		t.Fatal(err)
	}
	if err := marker.MarkDeleting(ctx, 7, 3, 2); err != nil {
		t.Fatal("repeat deletion intent is not idempotent", err)
	}
	current, err := r.GetByID(7)
	if err != nil || current.Status != "deleting" || current.RuntimeGeneration != 2 || current.AgentBootstrapToken != nil {
		t.Fatalf("deletion intent lost identity/fence: %+v %v", current, err)
	}
	stale.Status = "running"
	if err := r.Update(stale); !errors.Is(err, models.ErrInstanceStopping) {
		t.Fatalf("stale snapshot overwrote deleting: %v", err)
	}
	activity, err := NewInstanceAutoStopRepository(r.sess).GetActivity(ctx, 7)
	if err != nil || activity.StopOperationID != "old-stop" {
		t.Fatalf("stop audit was erased: %+v %v", activity, err)
	}
	pending, err := NewInstanceAutoStopRepository(r.sess).PendingStops(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("deleting instance re-entered old graceful stop: %+v %v", pending, err)
	}
	var power string
	row, _ := r.sess.SQL().QueryRow(`SELECT desired_power_state FROM instance_desired_state WHERE instance_id=7`)
	if err := row.Scan(&power); err != nil || power != "stopped" {
		t.Fatalf("deleting retained desired-running authority: %s %v", power, err)
	}
}

func TestMarkDeletingMySQLProStaleUpdateCannotResurrect(t *testing.T) {
	r := idleRuntimeRepository(t)
	if _, err := r.sess.SQL().Exec(`UPDATE instances SET instance_mode='pro' WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	stale, err := r.GetByID(7)
	if err != nil {
		t.Fatal(err)
	}
	marker, ok := interface{}(r).(interface {
		MarkDeleting(context.Context, int, int, int) error
	})
	if !ok {
		t.Fatal("repository lacks atomic deletion-intent admission")
	}
	if err := marker.MarkDeleting(t.Context(), 7, 3, 2); err != nil {
		t.Fatal(err)
	}
	stale.Status = "running"
	if err := r.Update(stale); !errors.Is(err, models.ErrInstanceStopping) {
		t.Fatalf("stale Pro snapshot overwrote deleting: %v", err)
	}
}
