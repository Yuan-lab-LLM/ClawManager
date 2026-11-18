package repository

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestModelActivityExpiredScopeNoLongerBlocksIdle(t *testing.T) {
	s := idleStopMySQL(t)
	r := NewInstanceAutoStopRepository(s)
	if err := r.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	key, err := r.BeginModelActivity(t.Context(), 7, 1, 2, "gw-1-2")
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-10 * time.Minute)
	if _, err = s.SQL().Exec(`UPDATE instance_model_activity_requests SET lease_expires_at=?,last_heartbeat_at=? WHERE request_id=?`, old, old, key); err != nil {
		t.Fatal(err)
	}
	a, err := r.GetActivity(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if a.ActiveRequests != 0 {
		t.Fatalf("expired request still permanently blocks sleep: active=%d", a.ActiveRequests)
	}
}

func TestModelActivityFinalStopClaimChecksLiveAndLegacyScopes(t *testing.T) {
	s := idleStopMySQL(t)
	r := NewInstanceAutoStopRepository(s)
	ctx := t.Context()
	if err := r.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	key, err := r.BeginModelActivity(ctx, 7, 1, 2, "gw-1-2")
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-3 * 24 * time.Hour)
	if _, err = s.SQL().Exec(`UPDATE instance_model_activity SET monitoring_since=?,last_gateway_activity_at=? WHERE instance_id=1`, old, old); err != nil {
		t.Fatal(err)
	}
	cfg, err := r.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	op := models.InstanceIdleStopOperation{InstanceID: 1, UserID: 7, Generation: 2, RuntimePodID: 11, GatewayID: "gw-1-2", PodUID: "fixture-pod", Endpoint: "https://fixture", WorkspacePath: "/workspace", StorageIdentity: "pvc:fixture"}
	a, err := r.GetActivity(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.ClaimStop(ctx, cfg.Version, a.ActivityVersion, op); !errors.Is(err, models.ErrModelActivityInProgress) {
		t.Fatalf("live request did not protect inference: %v", err)
	}
	if _, err = s.SQL().Exec(`UPDATE instance_model_activity_requests SET lease_expires_at=NULL,last_heartbeat_at=NULL,started_at=? WHERE request_id=?`, old, key); err != nil {
		t.Fatal(err)
	}
	a, err = r.GetActivity(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.ClaimStop(ctx, cfg.Version, a.ActivityVersion, op); !errors.Is(err, models.ErrLegacyModelActivityUnverified) {
		t.Fatalf("legacy scope lacked explicit proof blocker: %v", err)
	}
	cutoff := old.Add(time.Hour)
	if err = r.ReconcileModelActivities(ctx, &cutoff); err != nil {
		t.Fatal(err)
	}
	a, err = r.GetActivity(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.ClaimStop(ctx, cfg.Version, a.ActivityVersion, op); err != nil {
		t.Fatalf("recovered legacy scope still blocks idle stop: %v", err)
	}
}

func TestModelActivityExpiredLeaseCannotRaceBackToLife(t *testing.T) {
	s := idleStopMySQL(t)
	r := NewInstanceAutoStopRepository(s)
	ctx := t.Context()
	if err := r.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	key, err := r.BeginModelActivity(ctx, 7, 1, 2, "gw-1-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SQL().Exec(`UPDATE instance_model_activity_requests SET lease_expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 MINUTE WHERE request_id=?`, key); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.RenewModelActivity(ctx, key); !errors.Is(err, models.ErrActivityUnavailable) {
				t.Errorf("expired concurrent renewal accepted: %v", err)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := r.ReconcileModelActivities(ctx, nil); err != nil {
			t.Error(err)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := r.FinishModelActivity(ctx, key); err != nil {
			t.Error(err)
		}
	}()
	wg.Wait()
	a, err := r.GetActivity(ctx, 1)
	if err != nil || a.ActiveRequests != 0 {
		t.Fatalf("scope revived after concurrent cleanup: %+v %v", a, err)
	}
}

func TestModelActivityLeaseMigrationRetainsUnverifiedLegacy(t *testing.T) {
	s := idleStopMySQL(t)
	if _, err := s.SQL().Exec(`ALTER TABLE instance_model_activity_requests DROP INDEX idx_model_activity_lease,DROP COLUMN lease_expires_at,DROP COLUMN last_heartbeat_at,DROP COLUMN completion_reason`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SQL().Exec(`INSERT INTO instance_model_activity_requests(request_id,instance_id,generation,started_at) VALUES('pre-migration',1,2,UTC_TIMESTAMP(6))`); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../db/migrations/109_add_model_activity_leases.sql")
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
	row, err := s.SQL().QueryRow(`SELECT COUNT(*) FROM instance_model_activity_requests WHERE request_id='pre-migration' AND lease_expires_at IS NULL AND finished_at IS NULL AND completion_reason=''`)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = row.Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration discarded unidentified request: %d %v", count, err)
	}
}

func TestModelActivityLeaseRenewalAndRecovery(t *testing.T) {
	s := idleStopMySQL(t)
	r := NewInstanceAutoStopRepository(s)
	ctx := t.Context()
	if err := r.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	key, err := r.BeginModelActivity(ctx, 7, 1, 2, "gw-1-2")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.RenewModelActivity(ctx, key); err != nil {
		t.Fatal(err)
	}
	a, err := r.GetActivity(ctx, 1)
	if err != nil || a.ActiveRequests != 1 {
		t.Fatalf("live scope: %+v %v", a, err)
	}
	anchor := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Microsecond)
	for _, q := range []string{
		`UPDATE instance_model_activity_requests SET last_heartbeat_at=?,lease_expires_at=? WHERE request_id=?`,
	} {
		if _, err = s.SQL().Exec(q, anchor, anchor.Add(time.Minute), key); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.SQL().Exec(`UPDATE instance_model_activity SET last_gateway_activity_at=? WHERE instance_id=1`, anchor); err != nil {
		t.Fatal(err)
	}
	if err = r.RenewModelActivity(ctx, key); !errors.Is(err, models.ErrActivityUnavailable) {
		t.Fatalf("revived expired scope: %v", err)
	}
	if err = r.ReconcileModelActivities(ctx, nil); err != nil {
		t.Fatal(err)
	}
	a, err = r.GetActivity(ctx, 1)
	if err != nil || a.ActiveRequests != 0 || !a.LastGatewayActivityAt.Equal(anchor) {
		t.Fatalf("recovery refreshed idle clock: %+v %v", a, err)
	}
	if err = r.FinishModelActivity(ctx, key); err != nil {
		t.Fatal(err)
	}
	a, err = r.GetActivity(ctx, 1)
	if err != nil || !a.LastGatewayActivityAt.Equal(anchor) {
		t.Fatalf("late completion refreshed idle clock: %+v %v", a, err)
	}
	row, err := s.SQL().QueryRow(`SELECT completion_reason FROM instance_model_activity_requests WHERE request_id=?`, key)
	if err != nil {
		t.Fatal(err)
	}
	var reason string
	if err = row.Scan(&reason); err != nil || reason != "lease_expired" {
		t.Fatalf("missing expiry receipt: %q %v", reason, err)
	}
}

func TestModelActivityLegacyNeedsProcessProofAndPreservesLiveScopes(t *testing.T) {
	s := idleStopMySQL(t)
	r := NewInstanceAutoStopRepository(s)
	ctx := t.Context()
	if err := r.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	cutoff := old.Add(time.Minute)
	if _, err := s.SQL().Exec(`INSERT INTO instance_model_activity_requests(request_id,instance_id,generation,started_at) VALUES('legacy-old',1,2,?),('legacy-new',1,2,?)`, old, cutoff.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	key, err := r.BeginModelActivity(ctx, 7, 1, 2, "gw-1-2")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ReconcileModelActivities(ctx, nil); err != nil {
		t.Fatal(err)
	}
	a, err := r.GetActivity(ctx, 1)
	if err != nil || a.ActiveRequests != 3 {
		t.Fatalf("unverified legacy silently discarded: %+v %v", a, err)
	}
	if err = r.ReconcileModelActivities(ctx, &cutoff); err != nil {
		t.Fatal(err)
	}
	a, err = r.GetActivity(ctx, 1)
	if err != nil || a.ActiveRequests != 2 {
		t.Fatalf("proof closed a newer/live scope: %+v %v", a, err)
	}
	if err = r.RenewModelActivity(ctx, key); err != nil {
		t.Fatal(err)
	}
	row, err := s.SQL().QueryRow(`SELECT completion_reason FROM instance_model_activity_requests WHERE request_id='legacy-old'`)
	if err != nil {
		t.Fatal(err)
	}
	var reason string
	if err = row.Scan(&reason); err != nil || reason != "legacy_process_exited" {
		t.Fatalf("legacy receipt=%q err=%v", reason, err)
	}
}

func TestModelActivityLeaseStaleGenerationCannotRenewOrRefresh(t *testing.T) {
	s := idleStopMySQL(t)
	r := NewInstanceAutoStopRepository(s)
	ctx := t.Context()
	if err := r.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	key, err := r.BeginModelActivity(ctx, 7, 1, 2, "gw-1-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SQL().Exec(`UPDATE instances SET runtime_generation=3 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err = r.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	a, err := r.GetActivity(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	version := a.ActivityVersion
	if err = r.RenewModelActivity(ctx, key); !errors.Is(err, models.ErrActivityUnavailable) {
		t.Fatalf("stale generation renewed: %v", err)
	}
	if err = r.FinishModelActivity(ctx, key); err != nil {
		t.Fatal(err)
	}
	a, err = r.GetActivity(ctx, 1)
	if err != nil || a.ActivityVersion != version {
		t.Fatalf("old generation completion touched new generation: %+v %v", a, err)
	}
}
