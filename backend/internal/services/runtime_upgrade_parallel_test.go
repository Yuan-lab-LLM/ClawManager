package services

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestBoundedUpgradeLargeTeamNeverExceedsLimit(t *testing.T) {
	team := 9
	items := make([]models.RuntimeUpgradeItem, 500)
	for i := range items {
		items[i] = models.RuntimeUpgradeItem{InstanceID: i + 1, TeamID: &team, State: "compatibility_checked", IsTeamLeader: i == 0}
	}
	for remaining := 500; remaining > 0; {
		batch := boundedUpgradeBatch(items, 8, "compatibility_checked")
		if len(batch) == 0 || len(batch) > 8 {
			t.Fatalf("batch length %d", len(batch))
		}
		for _, item := range batch {
			if item.IsTeamLeader && remaining > 1 {
				t.Fatal("leader admitted before workers completed")
			}
			items[item.InstanceID-1].State = "migrated"
			remaining--
		}
	}
}

func TestUpgradeBoundedConcurrencyAndFailureDrain(t *testing.T) {
	var active, peak, completed atomic.Int32
	err := runUpgradeBounded(context.Background(), 500, 4, func(_ context.Context, i int) error {
		n := active.Add(1)
		defer active.Add(-1)
		for p := peak.Load(); p < n && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		time.Sleep(time.Millisecond)
		completed.Add(1)
		return nil
	})
	if err != nil || completed.Load() != 500 || peak.Load() > 4 || peak.Load() < 2 || active.Load() != 0 {
		t.Fatalf("err=%v completed=%d peak=%d active=%d", err, completed.Load(), peak.Load(), active.Load())
	}
	completed.Store(0)
	err = runUpgradeBounded(context.Background(), 500, 2, func(_ context.Context, i int) error {
		active.Add(1)
		defer active.Add(-1)
		defer completed.Add(1)
		if i == 0 {
			return errors.New("deterministic fixture failure")
		}
		time.Sleep(20 * time.Millisecond)
		return nil
	})
	if err == nil || completed.Load() > 2 || active.Load() != 0 {
		t.Fatalf("did not stop/settle: %v completed=%d active=%d", err, completed.Load(), active.Load())
	}
}

func TestUpgradeExecutionPolicyIsFrozenAndLegacySafe(t *testing.T) {
	if p := runtimeUpgradePolicy(&models.RuntimeRollout{}); p.Version != 0 || p.Check != 1 {
		t.Fatalf("legacy policy %+v", p)
	}
	raw := `{"execution":{"version":1,"check":4,"migrate":2,"start":2,"restore":2}}`
	p := runtimeUpgradePolicy(&models.RuntimeRollout{PreflightJSON: &raw, BatchSize: 8})
	if p.Check != 4 || p.Migrate != 2 || p.Start != 2 || p.Restore != 2 {
		t.Fatalf("frozen policy %+v", p)
	}
}

func TestUpgradeRecoveryRequiresFreshGatewayAndPodEvidence(t *testing.T) {
	now := time.Now().UTC()
	fresh := sql.NullTime{Valid: true, Time: now}
	if !upgradeRecoveryObservationFresh(now, fresh, fresh) {
		t.Fatal("fresh evidence rejected")
	}
	for _, bad := range []sql.NullTime{{}, {Valid: true, Time: now.Add(-3 * time.Minute)}, {Valid: true, Time: now.Add(time.Minute)}} {
		if upgradeRecoveryObservationFresh(now, bad, fresh) || upgradeRecoveryObservationFresh(now, fresh, bad) {
			t.Fatal("stale or missing evidence accepted")
		}
	}
}

func TestParallelUpgradeFailureCannotBeHiddenByRetryableSibling(t *testing.T) {
	ctx := context.Background()
	fatal := errors.New("migration capsule checksum mismatch")
	for _, other := range []error{ErrRuntimeUpgradeBusy, context.DeadlineExceeded, context.Canceled} {
		joined := errors.Join(fatal, other)
		if runtimeUpgradeInfrastructureRetryable(joined) || runtimeUpgradeReconcileInterrupted(ctx, joined) {
			t.Fatalf("deterministic failure hidden: %v", joined)
		}
	}
	if !runtimeUpgradeInfrastructureRetryable(errors.Join(ErrRuntimeUpgradeBusy, context.Canceled)) {
		t.Fatal("ownership loss was treated as data failure")
	}
}
