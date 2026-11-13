package repository

import (
	"testing"
	"time"
)

func TestAutoStopMySQLReadinessStartsFullWindowOnce(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(map[bool]string{false: "runtime-report", true: "lifecycle-update"}[full], func(t *testing.T) {
			for _, missing := range []bool{false, true} {
				t.Run(map[bool]string{false: "existing-window", true: "missing-window"}[missing], func(t *testing.T) {
					testAutoStopReadinessWindow(t, full, missing)
				})
			}
		})
	}
}

func testAutoStopReadinessWindow(t *testing.T, full, missing bool) {
	r := idleRuntimeRepository(t)
	ctx := t.Context()
	store := NewInstanceAutoStopRepository(r.sess)
	if _, err := r.sess.SQL().Exec("UPDATE instances SET status='creating',started_at=UTC_TIMESTAMP()-INTERVAL 2 DAY WHERE id=7"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.sess.SQL().Exec("UPDATE instance_model_activity SET monitoring_since=UTC_TIMESTAMP()-INTERVAL 2 DAY,start_deadline_at=NULL WHERE instance_id=7"); err != nil {
		t.Fatal(err)
	}
	if missing {
		if _, err := r.sess.SQL().Exec("DELETE FROM instance_model_activity WHERE instance_id=7"); err != nil {
			t.Fatal(err)
		}
	}
	before := time.Now().UTC().Truncate(time.Microsecond)
	ready := func() error {
		if !full {
			return r.UpdateRuntimeState(ctx, 7, "running", 2, nil)
		}
		i, err := r.GetByID(7)
		if err != nil {
			return err
		}
		i.Status = "running"
		return r.Update(i)
	}
	if err := ready(); err != nil {
		t.Fatal(err)
	}
	first, err := store.GetActivity(ctx, 7)
	if err != nil || first == nil || first.MonitoringSince.Before(before) {
		t.Fatalf("delayed initial readiness retained construction-time idle anchor: %+v %v", first, err)
	}
	instance, err := r.GetByID(7)
	if err != nil || instance.StartedAt == nil || instance.StartedAt.Before(before) {
		t.Fatalf("readiness did not update actual start: %+v %v", instance, err)
	}
	for _, elapsed := range []time.Duration{0, 299 * time.Second, 300 * time.Second} {
		candidates, err := store.CandidatesForTimeout(ctx, first.MonitoringSince.Add(elapsed), 300*time.Second, 0, 100)
		want := 0
		if elapsed == 300*time.Second {
			want = 1
		}
		if err != nil || len(candidates) != want {
			t.Fatalf("readiness idle window at %v: %d candidates, %v", elapsed, len(candidates), err)
		}
	}
	if err := ready(); err != nil {
		t.Fatal(err)
	}
	again, err := store.GetActivity(ctx, 7)
	if err != nil || !again.MonitoringSince.Equal(first.MonitoringSince) || again.ActivityVersion != first.ActivityVersion {
		t.Fatalf("heartbeat postponed idle eligibility: before=%+v after=%+v err=%v", first, again, err)
	}
}
