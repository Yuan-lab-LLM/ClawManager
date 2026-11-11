package repository

import (
	"clawreef/internal/models"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestDeletingIntentRejectsManualAndRuntimeStateChangesMySQL(t *testing.T) {
	for _, action := range []string{"manual-start", "manual-stop", "runtime-stopped", "runtime-creating", "runtime-error"} {
		t.Run(action, func(t *testing.T) {
			r, _ := manualStartIntentRepository(t, "openclaw")
			if _, err := r.sess.SQL().Exec("UPDATE instances SET status='deleting' WHERE id=7"); err != nil {
				t.Fatal(err)
			}
			store := NewInstanceAutoStopRepository(r.sess)
			before, err := store.GetActivity(t.Context(), 7)
			if err != nil {
				t.Fatal(err)
			}
			switch action {
			case "manual-start":
				err = store.BeginManualStart(t.Context(), 7, time.Now().Add(time.Minute))
			case "manual-stop":
				_, err = store.BeginManualStop(t.Context(), models.InstanceIdleStopOperation{InstanceID: 7, UserID: 3, Generation: 2})
			case "runtime-stopped":
				err = r.UpdateRuntimeState(t.Context(), 7, "stopped", 2, nil)
			case "runtime-creating":
				err = r.UpdateRuntimeState(t.Context(), 7, "creating", 3, nil)
			case "runtime-error":
				err = r.UpdateRuntimeState(t.Context(), 7, "error", 2, nil)
			}
			if !errors.Is(err, models.ErrInstanceStopping) {
				t.Errorf("deleting mutation was admitted: %v", err)
			}
			after, err := store.GetActivity(t.Context(), 7)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Error("rejected action modified activity")
			}
			var status string
			var generation int
			row, _ := r.sess.SQL().QueryRow("SELECT status,runtime_generation FROM instances WHERE id=7")
			if err := row.Scan(&status, &generation); err != nil {
				t.Fatal(err)
			}
			if status != "deleting" || generation != 2 {
				t.Fatalf("deletion intent overwritten: %s/%d", status, generation)
			}
		})
	}
}
