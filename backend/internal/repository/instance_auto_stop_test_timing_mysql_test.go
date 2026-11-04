//go:build cm_idle_stop_test_timing

package repository

import (
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestAutoStopTestTimingMySQLScanAndStopShareWindow(t *testing.T) {
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
	exec(`UPDATE instance_auto_stop_settings SET next_check_at=UTC_TIMESTAMP()+INTERVAL 12 HOUR`)
	before := time.Now().UTC()
	cfg, err := r.Settings(ctx)
	if err != nil || cfg.NextCheckAt.After(before.Add(61*time.Second)) || cfg.NextCheckAt.Before(before.Add(59*time.Second)) {
		t.Fatalf("production deadline not shortened for test build: %+v, %v", cfg, err)
	}
	claimed, err := r.ClaimCheck(ctx, before.Add(30*time.Second))
	if err != nil || claimed != nil {
		t.Fatalf("scan ran before its one-minute deadline: %+v, %v", claimed, err)
	}
	claimed, err = r.ClaimCheck(ctx, before.Add(61*time.Second))
	if err != nil || claimed == nil {
		t.Fatalf("scan did not run after one minute: %+v, %v", claimed, err)
	}
	if err = r.CompleteScan(ctx, *claimed); err != nil {
		t.Fatal(err)
	}
	op := models.InstanceIdleStopOperation{InstanceID: 1, UserID: 7, Generation: 2, RuntimePodID: 11, GatewayID: "gw-1-2", PodUID: "pod-uid", Endpoint: "https://runtime"}
	exec(`UPDATE instance_model_activity SET monitoring_since=UTC_TIMESTAMP()-INTERVAL 6 MINUTE WHERE instance_id=1`)
	for _, field := range []string{"started_at", "monitoring_since", "last_gateway_activity_at", "none"} {
		exec(`UPDATE instances SET started_at=UTC_TIMESTAMP()-INTERVAL 6 MINUTE WHERE id=1`)
		exec(`UPDATE instance_model_activity SET monitoring_since=UTC_TIMESTAMP()-INTERVAL 6 MINUTE,last_gateway_activity_at=NULL WHERE instance_id=1`)
		if field == "started_at" {
			exec(`UPDATE instances SET started_at=UTC_TIMESTAMP()-INTERVAL 4 MINUTE WHERE id=1`)
		} else if field != "none" {
			exec(`UPDATE instance_model_activity SET ` + field + `=UTC_TIMESTAMP()-INTERVAL 4 MINUTE WHERE instance_id=1`)
		}
		candidates, err := r.Candidates(ctx, time.Now().UTC(), 1, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if field == "none" {
			want = 1
		}
		if len(candidates) != want {
			t.Fatalf("%s: candidates=%d, want %d", field, len(candidates), want)
		}
		a, err := r.GetActivity(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		stop, err := r.ClaimStop(ctx, cfg.Version, a.ActivityVersion, op)
		if field == "none" {
			if err != nil || stop == nil {
				t.Fatalf("six idle minutes did not permit stop: %+v, %v", stop, err)
			}
		} else if err == nil || stop != nil {
			t.Fatalf("%s: four-minute protection lost in final stop claim", field)
		}
	}
}
