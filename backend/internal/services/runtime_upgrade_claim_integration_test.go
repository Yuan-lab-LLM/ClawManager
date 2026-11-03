package services

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestUpgradeClaimsRealMySQL500Items(t *testing.T) {
	db := upgradeFlowDB(t)
	ctx := context.Background()
	if _, err := db.SQL().Exec(`INSERT INTO runtime_rollouts(id,runtime_type,target_image_ref,status,phase,batch_size) VALUES(1,'openclaw','fixture','running','compatibility_check',8)`); err != nil {
		t.Fatal(err)
	}
	items := make([]models.RuntimeUpgradeItem, 500)
	for i := range items {
		id := i + 1
		items[i] = models.RuntimeUpgradeItem{InstanceID: id, State: "prepared"}
		if _, err := db.SQL().Exec(`INSERT INTO instances(id,user_id,type,runtime_generation,status) VALUES(?,1,'openclaw',1,'running')`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.SQL().Exec(`INSERT INTO runtime_upgrade_items(rollout_id,instance_id,state,created_at,updated_at) VALUES(1,?,'prepared',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, id); err != nil {
			t.Fatal(err)
		}
	}
	svc := &RuntimeUpgradeService{sess: db}
	var active, peak atomic.Int32
	started := time.Now()
	err := svc.runUpgradeItems(ctx, 1, "compatibility_check", items, 4, func(ctx context.Context, index int) error {
		n := active.Add(1)
		defer active.Add(-1)
		for p := peak.Load(); p < n && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		time.Sleep(2 * time.Millisecond)
		result, err := svc.execClaimedUpgradeState(ctx, `UPDATE runtime_upgrade_items SET state='compatibility_checked' WHERE rollout_id=1 AND instance_id=?`, index+1)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err == nil && rows != 1 {
			return fmt.Errorf("lost claim for item %d", index+1)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if peak.Load() > 4 || peak.Load() < 2 || active.Load() != 0 {
		t.Fatalf("invalid peak/active %d/%d", peak.Load(), active.Load())
	}
	row, err := db.SQL().QueryRow(`SELECT COUNT(*) FROM runtime_upgrade_items WHERE rollout_id=1 AND state='compatibility_checked' AND operation_token IS NULL AND operation_attempts=1`)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 500 {
		t.Fatalf("completed %d/500", count)
	}
	t.Logf("500 actual MySQL claims/receipts completed in %s, peak %d", time.Since(started), peak.Load())
}

func TestUpgradeClaimRejectsDuplicateAndFencesStaleReceipt(t *testing.T) {
	db := upgradeFlowDB(t)
	ctx := context.Background()
	svc := &RuntimeUpgradeService{sess: db}
	for _, q := range []string{
		`INSERT INTO runtime_rollouts(id,runtime_type,target_image_ref,status,phase,batch_size) VALUES(1,'openclaw','fixture','running','session_migration',2)`,
		`INSERT INTO instances(id,user_id,type,runtime_generation,status) VALUES(1,1,'openclaw',1,'stopped')`,
		`INSERT INTO runtime_upgrade_items(rollout_id,instance_id,state,created_at,updated_at) VALUES(1,1,'migration_started',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
	} {
		if _, err := db.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.withUpgradeClaim(ctx, 1, 1, "session_migration", func(claimCtx context.Context) error {
		err := svc.withUpgradeClaim(ctx, 1, 1, "rollback_restore", func(context.Context) error { t.Error("duplicate operation executed"); return nil })
		if !errors.Is(err, ErrRuntimeUpgradeBusy) {
			t.Fatalf("duplicate result %v", err)
		}
		// Simulate a successor's claim after expiration; an old controller must
		// not publish state using the successor's operation token.
		if _, err := db.SQL().Exec(`UPDATE runtime_upgrade_items SET operation_token='successor' WHERE instance_id=1`); err != nil {
			return err
		}
		r, err := svc.execClaimedUpgradeState(claimCtx, `UPDATE runtime_upgrade_items SET state='migrated' WHERE instance_id=1`)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if n != 0 {
			t.Error("stale controller overwrote new execution")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	row, err := db.SQL().QueryRow(`SELECT operation_token,state FROM runtime_upgrade_items WHERE instance_id=1`)
	if err != nil {
		t.Fatal(err)
	}
	var token, state string
	if err := row.Scan(&token, &state); err != nil {
		t.Fatal(err)
	}
	if token != "successor" || state != "migration_started" {
		t.Fatalf("stale cleanup/receipt changed successor: %s %s", token, state)
	}
}
