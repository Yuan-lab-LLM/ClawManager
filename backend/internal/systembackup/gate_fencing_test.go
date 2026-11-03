package systembackup

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGateTransitionsRejectWrongTokenWithoutChangingState(t *testing.T) {
	db := controlDB(t)
	g := Gate{db}
	ctx := context.Background()
	for _, action := range []string{"pause", "hold", "begin_release", "resume", "forced_resume", "finish_release"} {
		t.Run(action, func(t *testing.T) {
			if err := g.EnsureScope(ctx, action); err != nil {
				t.Fatal(err)
			}
			if err := g.RegisterParticipant(ctx, action, "runtime-1"); err != nil {
				t.Fatal(err)
			}
			own, err := g.Begin(ctx, action, "backup", RegistryHash(), time.Minute, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if action != "pause" {
				if err = g.AckPaused(ctx, own, "runtime-1", RegistryHash()); err != nil {
					t.Fatal(err)
				}
			}
			if action != "pause" && action != "hold" {
				own, err = g.Hold(ctx, own)
				if err != nil {
					t.Fatal(err)
				}
			}
			if action == "resume" || action == "finish_release" {
				if err = g.BeginRelease(ctx, own); err != nil {
					t.Fatal(err)
				}
			}
			if action == "finish_release" {
				if err = g.AckResumed(ctx, own, "runtime-1", false); err != nil {
					t.Fatal(err)
				}
			}
			if action == "forced_resume" {
				if _, err = db.Exec("UPDATE system_backup_gate SET hold_deadline=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE scope=?", action); err != nil {
					t.Fatal(err)
				}
			}
			snapshot := func() string {
				var state string
				if e := db.QueryRow("SELECT CONCAT(g.phase,':',COALESCE(g.owner,''),':',g.fencing_token,':',p.state,':',p.resume_result) FROM system_backup_gate g JOIN system_backup_participants p ON p.scope=g.scope WHERE g.scope=?", action).Scan(&state); e != nil {
					t.Fatal(e)
				}
				return state
			}
			before := snapshot()
			bad := own
			bad.Token += 100
			switch action {
			case "pause":
				err = g.AckPaused(ctx, bad, "runtime-1", RegistryHash())
			case "hold":
				_, err = g.Hold(ctx, bad)
			case "begin_release":
				err = g.BeginRelease(ctx, bad)
			case "resume":
				err = g.AckResumed(ctx, bad, "runtime-1", false)
			case "forced_resume":
				err = g.AckResumed(ctx, bad, "runtime-1", true)
			case "finish_release":
				err = g.FinishRelease(ctx, bad)
			}
			if !errors.Is(err, ErrFence) {
				t.Errorf("wrong token accepted: %v", err)
			}
			if after := snapshot(); after != before {
				t.Errorf("rejected transition changed state: before=%s after=%s", before, after)
			}
		})
	}
}
