package repository

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestAutoStopSecondsMySQLSettingsAndDueCadence(t *testing.T) {
	s := idleStopMySQL(t)
	r := NewInstanceAutoStopRepository(s)
	ctx := t.Context()
	legacy, err := r.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.CheckIntervalSeconds != nil || legacy.IdleTimeoutSeconds != nil {
		t.Fatal("migration replaced legacy day settings")
	}
	seconds, idle := int64(2), int64(7)
	before := time.Now().UTC()
	cfg, err := r.SaveSettings(ctx, models.InstanceAutoStopSettings{Enabled: true, Version: legacy.Version, CheckIntervalSeconds: &seconds, IdleTimeoutSeconds: &idle})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NextCheckAt.Before(before.Add(2*time.Second)) || cfg.NextCheckAt.After(time.Now().UTC().Add(2*time.Second)) {
		t.Fatalf("new interval not scheduled from save: %v", cfg.NextCheckAt)
	}
	if _, err := r.SaveSettings(ctx, *legacy); !errors.Is(err, models.ErrAutoStopConflict) {
		t.Fatalf("stale seconds save: %v", err)
	}
	// Reads must not reapply the test-build one-minute override to explicit seconds.
	reread, err := r.Settings(ctx)
	if err != nil || reread.CheckInterval() != 2*time.Second || reread.IdleTimeout() != 7*time.Second || !reread.NextCheckAt.Equal(cfg.NextCheckAt.Truncate(time.Microsecond)) {
		t.Fatalf("seconds did not survive read: %+v %v", reread, err)
	}
	if claim, err := r.ClaimCheck(ctx, cfg.NextCheckAt.Add(-time.Second)); err != nil || claim != nil {
		t.Fatalf("early scan: %+v %v", claim, err)
	}
	due := time.Now().UTC().Truncate(time.Microsecond).Add(-5 * time.Second)
	if _, err := s.SQL().Exec("UPDATE instance_auto_stop_settings SET next_check_at=?", due); err != nil {
		t.Fatal(err)
	}
	claim, err := r.ClaimCheck(ctx, time.Now().UTC())
	if err != nil || claim == nil {
		t.Fatalf("due seconds check not claimed: %+v %v", claim, err)
	}
	if second, err := r.ClaimCheck(ctx, time.Now().UTC()); err != nil || second != nil {
		t.Fatalf("duplicate scan: %+v %v", second, err)
	}
	if err := r.CompleteScan(ctx, *claim); err != nil {
		t.Fatal(err)
	}
	reread, err = r.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reread.NextCheckAt.After(due) || reread.NextCheckAt.Sub(due)%(2*time.Second) != 0 || !reread.NextCheckAt.After(time.Now().UTC()) || reread.NextCheckAt.After(time.Now().UTC().Add(2*time.Second)) {
		t.Fatalf("seconds cadence lost: %v (due %v)", reread.NextCheckAt, due)
	}
	// Legacy clients can still submit day-only values and clear explicit seconds.
	legacy.Version = reread.Version
	legacy.CheckIntervalDays, legacy.IdleTimeoutDays = 2, 7
	saved, err := r.SaveSettings(ctx, *legacy)
	if err != nil || saved.CheckIntervalSeconds != nil || saved.IdleTimeoutSeconds != nil || saved.CheckIntervalDays != 2 || saved.IdleTimeoutDays != 7 {
		t.Fatalf("legacy API incompatible: %+v %v", saved, err)
	}
}

func TestAutoStopSecondsMySQLCandidatesAndFinalClaimShareWindow(t *testing.T) {
	for _, field := range []string{"started_at", "monitoring_since", "last_gateway_activity_at", "none"} {
		t.Run(field, func(t *testing.T) {
			s := idleStopMySQL(t)
			r := NewInstanceAutoStopRepository(s)
			ctx := t.Context()
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := s.SQL().Exec(query, args...); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.Initialize(ctx); err != nil {
				t.Fatal(err)
			}
			cfg, err := r.Settings(ctx)
			if err != nil {
				t.Fatal(err)
			}
			seconds, idle := int64(1), int64(37)
			cfg.CheckIntervalSeconds, cfg.IdleTimeoutSeconds = &seconds, &idle
			cfg, err = r.SaveSettings(ctx, *cfg)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Truncate(time.Microsecond)
			old, recent := now.Add(-50*time.Second), now.Add(-10*time.Second)
			exec("UPDATE instances SET started_at=? WHERE id=1", old)
			exec("UPDATE instance_model_activity SET monitoring_since=?,last_gateway_activity_at=NULL WHERE instance_id=1", old)
			if field == "started_at" {
				exec("UPDATE instances SET started_at=? WHERE id=1", recent)
			} else if field != "none" {
				exec("UPDATE instance_model_activity SET "+field+"=? WHERE instance_id=1", recent)
			}
			candidates, err := r.CandidatesForTimeout(ctx, now, cfg.IdleTimeout(), 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if field == "none" {
				want = 1
			}
			if len(candidates) != want {
				t.Fatalf("37s SQL window: got %d candidates, want %d", len(candidates), want)
			}
			a, err := r.GetActivity(ctx, 1)
			if err != nil {
				t.Fatal(err)
			}
			op := models.InstanceIdleStopOperation{InstanceID: 1, UserID: 7, Generation: 2, RuntimePodID: 11, GatewayID: "gw-1-2", PodUID: "test-pod", Endpoint: "https://test"}
			stop, err := r.ClaimStop(ctx, cfg.Version, a.ActivityVersion, op)
			if field == "none" {
				if err != nil || stop == nil {
					t.Fatalf("50s idle should permit 37s claim: %+v %v", stop, err)
				}
			} else if !errors.Is(err, models.ErrAutoStopConflict) || stop != nil {
				t.Fatalf("recent %s should block final claim: %+v %v", field, stop, err)
			}
		})
	}
}

func TestAutoStopSecondsMySQLCandidateExactBoundary(t *testing.T) {
	s := idleStopMySQL(t)
	r := NewInstanceAutoStopRepository(s)
	ctx := t.Context()
	if err := r.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	anchor := now.Add(-time.Second)
	if _, err := s.SQL().Exec("UPDATE instances SET started_at=? WHERE id=1", anchor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SQL().Exec("UPDATE instance_model_activity SET monitoring_since=?,last_gateway_activity_at=? WHERE instance_id=1", anchor, anchor); err != nil {
		t.Fatal(err)
	}
	for _, delta := range []time.Duration{-time.Microsecond, 0} {
		got, err := r.CandidatesForTimeout(ctx, now.Add(delta), time.Second, 0, 100)
		want := 1
		if delta < 0 {
			want = 0
		}
		if err != nil || len(got) != want {
			t.Fatalf("1s boundary (%v): %d candidates, %v", delta, len(got), err)
		}
	}
}

func TestAutoStopSecondsMySQLUpgradePreservesLegacyDaysAndDeadline(t *testing.T) {
	s := idleStopMySQL(t)
	due := time.Date(2026, 11, 10, 20, 0, 0, 0, time.UTC)
	for _, statement := range []string{
		"ALTER TABLE instance_auto_stop_settings DROP COLUMN check_interval_seconds, DROP COLUMN idle_timeout_seconds",
		"UPDATE instance_auto_stop_settings SET check_interval_days=2,idle_timeout_days=7,version=9,scan_token='existing-scan',scan_cursor=17",
	} {
		if _, err := s.SQL().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SQL().Exec("UPDATE instance_auto_stop_settings SET next_check_at=?", due); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../db/migrations/104_add_instance_auto_stop_seconds.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(statement) != "" {
			if _, err := s.SQL().Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	cfg, err := readAutoStopSettings(t.Context(), s, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CheckIntervalDays != 2 || cfg.IdleTimeoutDays != 7 || cfg.CheckIntervalSeconds != nil || cfg.IdleTimeoutSeconds != nil || cfg.Version != 9 || cfg.ScanID != "existing-scan" || cfg.ScanCursor != 17 || !cfg.NextCheckAt.Equal(due) {
		t.Fatalf("upgrade changed legacy settings or active scan: %+v", cfg)
	}
}
