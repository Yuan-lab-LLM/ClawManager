package services

import (
	"context"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestUpgradeTargetReadinessMySQLRecovery(t *testing.T) {
	s := upgradeFlowDB(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO runtime_rollouts (id,runtime_type,target_image_ref,status,phase,error_message,created_at,updated_at) VALUES (1,'openclaw','repo','running','gateway_restart','UPGRADE_TARGET_UNAVAILABLE: target not ready',NOW(),NOW())`)
	for _, id := range []int{1, 2, 3} {
		exec(`INSERT INTO instances VALUES (?,1,'openclaw','gateway','lite','stopped',1,'/isolated',NULL,NULL,NOW())`, id)
		exec(`INSERT INTO runtime_upgrade_items (rollout_id,instance_id,state,source_generation,recovery_status,recovery_error_code,error_message,restart_next_retry_at,recovery_started_at,created_at,updated_at) VALUES (1,?,'migrated',1,'retrying','UPGRADE_TARGET_UNAVAILABLE','target not ready',NOW()+INTERVAL 15 SECOND,NOW(),NOW(),NOW())`, id)
	}
	exec(`UPDATE runtime_upgrade_items SET state='restart_ready',restart_attempts=1,restart_deadline_at=NOW()+INTERVAL 5 MINUTE WHERE instance_id=2`)
	exec(`UPDATE runtime_upgrade_items SET recovery_error_code='PROXY_POLICY_NOT_READY',error_message='policy not ready' WHERE instance_id=3`)
	service := &RuntimeUpgradeService{sess: s}
	read := func(id int) models.RuntimeUpgradeItem {
		t.Helper()
		var item models.RuntimeUpgradeItem
		if err := s.Collection("runtime_upgrade_items").Find("instance_id", id).One(&item); err != nil {
			t.Fatal(err)
		}
		return item
	}
	startedBefore := read(2)
	policyBefore := read(3)
	if err := service.clearUnavailableUpgradeTargets(ctx, 1); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{1, 2} {
		item := read(id)
		if item.ErrorMessage != nil || item.RecoveryErrorCode != nil || item.RestartNextRetryAt != nil {
			t.Fatalf("stale readiness failure retained: %+v", item)
		}
		want := "pending"
		if id == 2 {
			want = "starting"
			if item.RestartAttempts != 1 || !item.RestartDeadlineAt.Equal(*startedBefore.RestartDeadlineAt) || !item.RecoveryStartedAt.Equal(*startedBefore.RecoveryStartedAt) || item.State != "restart_ready" {
				t.Fatalf("readiness reset an existing attempt: %+v", item)
			}
		}
		if stringValue(item.RecoveryStatus) != want {
			t.Fatalf("recovery status=%s want=%s", stringValue(item.RecoveryStatus), want)
		}
	}
	policyAfter := read(3)
	if stringValue(policyAfter.RecoveryErrorCode) != "PROXY_POLICY_NOT_READY" || stringValue(policyAfter.ErrorMessage) != "policy not ready" || !policyAfter.RestartNextRetryAt.Equal(*policyBefore.RestartNextRetryAt) {
		t.Fatal("readiness erased an unrelated startup failure")
	}
	var rollout models.RuntimeRollout
	if err := s.Collection("runtime_rollouts").Find("id", 1).One(&rollout); err != nil || rollout.ErrorMessage != nil {
		t.Fatalf("stale rollout error retained: %+v %v", rollout, err)
	}
	// A genuine lost target still reports a failure without losing its fence.
	if err := service.recordUnavailableUpgradeTargets(ctx, &rollout); err != nil {
		t.Fatal(err)
	}
	if item := read(1); stringValue(item.RecoveryErrorCode) != "UPGRADE_TARGET_UNAVAILABLE" || stringValue(item.RecoveryStatus) != "retrying" || item.RestartNextRetryAt == nil {
		t.Fatalf("lost target was hidden: %+v", item)
	}
	// Standby publication may wait, but the existing 15-minute deadline remains.
	exec(`UPDATE runtime_upgrade_items SET restart_attempts=0,recovery_started_at=NOW()-INTERVAL 16 MINUTE WHERE rollout_id=1`)
	items, err := service.listUpgradeItems(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.waitForUpgradeTargetActivation(ctx, &rollout, items); err != nil {
		t.Fatal(err)
	}
	item := read(1)
	if stringValue(item.RecoveryStatus) != "needs_attention" || item.RestartNextRetryAt != nil {
		t.Fatalf("standby waits indefinitely: %+v", item)
	}
	if err := service.clearUnavailableUpgradeTargets(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if item = read(1); stringValue(item.RecoveryStatus) != "needs_attention" || item.RecoveryErrorCode == nil {
		t.Fatal("readiness erased a held recovery failure")
	}
	if err := s.Collection("runtime_rollouts").Find("id", 1).One(&rollout); err != nil || rollout.ErrorMessage == nil {
		t.Fatalf("held recovery disappeared from rollout: %+v %v", rollout, err)
	}
	// No terminal state can be changed by this cleanup.
	exec(`UPDATE runtime_rollouts SET status='finished',finished_at=NOW() WHERE id=1`)
	exec(`UPDATE runtime_upgrade_items SET state='verified' WHERE instance_id=1`)
	exec(`UPDATE runtime_upgrade_items SET recovery_status='retrying',recovery_error_code='UPGRADE_TARGET_UNAVAILABLE',error_message='retained terminal evidence' WHERE instance_id=2`)
	if err := service.clearUnavailableUpgradeTargets(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if item = read(1); item.State != "verified" || item.RecoveryErrorCode == nil || time.Since(*item.RecoveryStartedAt) < 15*time.Minute {
		t.Fatalf("cleanup rewrote terminal recovery evidence: %+v", item)
	}
	if item = read(2); stringValue(item.RecoveryErrorCode) != "UPGRADE_TARGET_UNAVAILABLE" || stringValue(item.ErrorMessage) != "retained terminal evidence" {
		t.Fatal("cleanup changed an item belonging to a finished rollout")
	}
}
