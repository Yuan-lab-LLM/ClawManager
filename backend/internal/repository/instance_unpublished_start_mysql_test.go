package repository

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

type unpublishedStartResumeCapability interface {
	ResumeUnpublishedRuntimeStart(context.Context, models.Instance, models.InstanceRuntimeBinding) (bool, error)
}

func unpublishedStartRepository(t *testing.T, runtimeType, status string) (*instanceRepository, models.Instance, models.InstanceRuntimeBinding) {
	t.Helper()
	r, _ := manualStartIntentRepository(t, runtimeType)
	if _, err := r.sess.SQL().Exec(`DROP TABLE instance_runtime_bindings`); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../db/migrations/023_add_runtime_pool_v2.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(raw), ";") {
		if strings.Contains(statement, "CREATE TABLE IF NOT EXISTS instance_runtime_bindings") {
			if _, err = r.sess.SQL().Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	workspace := "/workspaces/" + runtimeType + "/user-3/instance-7"
	if _, err = r.sess.SQL().Exec(`UPDATE instances SET status=?,runtime_type='gateway',workspace_path=?,runtime_error_code='FAILED',runtime_error_message='prior attempt failed' WHERE id=7`, status, workspace); err != nil {
		t.Fatal(err)
	}
	if err = NewInstanceAutoStopRepository(r.sess).BeginManualStart(t.Context(), 7, time.Now().Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err = r.sess.SQL().Exec(`INSERT INTO instance_runtime_bindings(id,instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(31,7,9,?,'pending-7-2',20000,?,'creating',2)`, runtimeType, workspace); err != nil {
		t.Fatal(err)
	}
	expected, err := r.GetByIDContext(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := NewInstanceRuntimeBindingRepository(r.sess).GetByInstanceID(t.Context(), 7)
	if err != nil || pending == nil {
		t.Fatalf("read pending binding: %+v %v", pending, err)
	}
	release, err := r.AcquireLifecycleLock(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	return r, *expected, *pending
}

func resumeUnpublishedStart(t *testing.T, r *instanceRepository, ctx context.Context, expected models.Instance, pending models.InstanceRuntimeBinding) (bool, error) {
	t.Helper()
	capability, ok := any(r).(unpublishedStartResumeCapability)
	if !ok {
		t.Fatal("repository lacks the narrow unpublished Start resume capability")
	}
	return capability.ResumeUnpublishedRuntimeStart(ctx, expected, pending)
}

func TestUnpublishedStartMySQLResumesSameGenerationWithoutReleasingReservation(t *testing.T) {
	for _, runtimeType := range []string{"openclaw", "opencode", "dsh", "hermes"} {
		for _, status := range []string{"stopped", "error"} {
			t.Run(runtimeType+"/"+status, func(t *testing.T) {
				r, expected, pending := unpublishedStartRepository(t, runtimeType, status)
				ctx := t.Context()
				store := NewInstanceAutoStopRepository(r.sess)
				activity, err := store.GetActivity(ctx, 7)
				if err != nil {
					t.Fatal(err)
				}
				desired, err := NewInstanceDesiredStateRepository(r.sess).GetByInstanceID(7)
				if err != nil {
					t.Fatal(err)
				}
				if status == "stopped" {
					if err = r.UpdateRuntimeState(ctx, 7, "creating", 2, nil); !errors.Is(err, models.ErrInstanceStopping) {
						t.Fatalf("generic stopped same-generation fence was weakened: %v", err)
					}
				}
				resumed, err := resumeUnpublishedStart(t, r, ctx, expected, pending)
				if err != nil || !resumed {
					t.Fatalf("authorized unpublished Start not resumed: %v %v", resumed, err)
				}
				after, err := r.GetByIDContext(ctx, 7)
				if err != nil || after.Status != "creating" || after.RuntimeGeneration != 2 || after.RuntimeErrorCode != nil || after.RuntimeErrorMessage != nil {
					t.Fatalf("wrong resumed instance: %+v %v", after, err)
				}
				binding, err := NewInstanceRuntimeBindingRepository(r.sess).GetByInstanceID(ctx, 7)
				if err != nil || binding == nil || !reflect.DeepEqual(*binding, pending) {
					t.Fatalf("resume changed reserved slot: %+v %v", binding, err)
				}
				afterActivity, err := store.GetActivity(ctx, 7)
				if err != nil || !reflect.DeepEqual(afterActivity, activity) {
					t.Fatalf("resume changed Start or stop proof: %+v %v", afterActivity, err)
				}
				afterDesired, err := NewInstanceDesiredStateRepository(r.sess).GetByInstanceID(7)
				if err != nil || !reflect.DeepEqual(afterDesired, desired) {
					t.Fatalf("resume overwrote desired intent: %+v %v", afterDesired, err)
				}
				if resumed, err = resumeUnpublishedStart(t, r, ctx, expected, pending); err != nil || resumed {
					t.Fatalf("stale retry rewrote creating instance: %v %v", resumed, err)
				}
			})
		}
	}
}

func TestUnpublishedStartMySQLRejectsChangedIdentityOrAuthority(t *testing.T) {
	cases := map[string]string{
		"instance-missing":     `DELETE FROM instances WHERE id=7`,
		"instance-owner":       `UPDATE instances SET user_id=4 WHERE id=7`,
		"instance-type":        `UPDATE instances SET type='openclaw' WHERE id=7`,
		"instance-mode":        `UPDATE instances SET instance_mode='pro' WHERE id=7`,
		"instance-runtime":     `UPDATE instances SET runtime_type='container' WHERE id=7`,
		"instance-generation":  `UPDATE instances SET runtime_generation=3 WHERE id=7`,
		"instance-status":      `UPDATE instances SET status='creating' WHERE id=7`,
		"instance-workspace":   `UPDATE instances SET workspace_path='/different-home' WHERE id=7`,
		"instance-path-case":   `UPDATE instances SET workspace_path='/WORKSPACES/hermes/user-3/instance-7' WHERE id=7`,
		"desired-stop":         `UPDATE instance_desired_state SET desired_power_state='stopped' WHERE instance_id=7`,
		"desired-missing":      `DELETE FROM instance_desired_state WHERE instance_id=7`,
		"activity-generation":  `UPDATE instance_model_activity SET generation=3 WHERE instance_id=7`,
		"deadline-missing":     `UPDATE instance_model_activity SET start_deadline_at=NULL WHERE instance_id=7`,
		"deadline-expired":     `UPDATE instance_model_activity SET start_deadline_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND WHERE instance_id=7`,
		"active-stop":          `UPDATE instance_model_activity SET stop_operation_id='new-stop',stop_phase='stopping' WHERE instance_id=7`,
		"activity-missing":     `DELETE FROM instance_model_activity WHERE instance_id=7`,
		"binding-id":           `UPDATE instance_runtime_bindings SET id=32 WHERE instance_id=7`,
		"binding-generation":   `UPDATE instance_runtime_bindings SET generation=3 WHERE instance_id=7`,
		"binding-gateway":      `UPDATE instance_runtime_bindings SET gateway_id='gw-7-2' WHERE instance_id=7`,
		"binding-port":         `UPDATE instance_runtime_bindings SET gateway_port=20001 WHERE instance_id=7`,
		"binding-workspace":    `UPDATE instance_runtime_bindings SET workspace_path='/other-home' WHERE instance_id=7`,
		"binding-path-case":    `UPDATE instance_runtime_bindings SET workspace_path='/WORKSPACES/hermes/user-3/instance-7' WHERE instance_id=7`,
		"binding-gateway-case": `UPDATE instance_runtime_bindings SET gateway_id='PENDING-7-2' WHERE instance_id=7`,
		"binding-pid":          `UPDATE instance_runtime_bindings SET gateway_pid=1234 WHERE instance_id=7`,
		"binding-runtime":      `UPDATE instance_runtime_bindings SET runtime_type='openclaw' WHERE instance_id=7`,
		"binding-state":        `UPDATE instance_runtime_bindings SET state='running' WHERE instance_id=7`,
		"binding-missing":      `DELETE FROM instance_runtime_bindings WHERE instance_id=7`,
	}
	for name, query := range cases {
		t.Run(name, func(t *testing.T) {
			r, expected, pending := unpublishedStartRepository(t, "hermes", "stopped")
			if _, err := r.sess.SQL().Exec(query); err != nil {
				t.Fatal(err)
			}
			before, err := r.GetByIDContext(t.Context(), 7)
			if err != nil {
				t.Fatal(err)
			}
			resumed, err := resumeUnpublishedStart(t, r, t.Context(), expected, pending)
			if err != nil || resumed {
				t.Fatalf("changed authority resumed: %v %v", resumed, err)
			}
			after, err := r.GetByIDContext(t.Context(), 7)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected resume changed instance: before=%+v after=%+v err=%v", before, after, err)
			}
		})
	}
	t.Run("binding-pod", func(t *testing.T) {
		r, expected, pending := unpublishedStartRepository(t, "hermes", "stopped")
		for _, query := range []string{`INSERT INTO runtime_pods VALUES(10,'other-uid','runtime','other-pod','other-pool','hermes')`, `UPDATE instance_runtime_bindings SET runtime_pod_id=10 WHERE instance_id=7`} {
			if _, err := r.sess.SQL().Exec(query); err != nil {
				t.Fatal(err)
			}
		}
		if resumed, err := resumeUnpublishedStart(t, r, t.Context(), expected, pending); err != nil || resumed {
			t.Fatalf("changed target Pod resumed: %v %v", resumed, err)
		}
	})
}

func TestUnpublishedStartMySQLNewerStopCommitsBeforeLockedResume(t *testing.T) {
	r, expected, pending := unpublishedStartRepository(t, "hermes", "stopped")
	ctx := t.Context()
	capability, ok := any(r).(unpublishedStartResumeCapability)
	if !ok {
		t.Fatal("repository lacks the narrow unpublished Start resume capability")
	}
	result := make(chan struct {
		resumed bool
		err     error
	}, 1)
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT id FROM instances WHERE id=7 FOR UPDATE`)
		if err != nil {
			return err
		}
		var id int
		if err = row.Scan(&id); err != nil {
			return err
		}
		started := make(chan struct{})
		go func() {
			close(started)
			resumed, e := capability.ResumeUnpublishedRuntimeStart(ctx, expected, pending)
			result <- struct {
				resumed bool
				err     error
			}{resumed, e}
		}()
		<-started
		_, err = tx.SQL().ExecContext(ctx, `UPDATE instance_desired_state SET desired_power_state='stopped' WHERE instance_id=7`)
		return err
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := <-result
	if got.err != nil || got.resumed {
		t.Fatalf("older Start overrode newer committed Stop: %+v", got)
	}
	after, err := r.GetByIDContext(ctx, 7)
	if err != nil || after.Status != "stopped" || after.RuntimeGeneration != 2 {
		t.Fatalf("older Start changed stopped state: %+v %v", after, err)
	}
	assertManualStartPower(t, r, "stopped")
}

func TestUnpublishedStartMySQLErrorsDoNotMutateState(t *testing.T) {
	for _, kind := range []string{"cancelled", "unknown-desired-table", "write-failure", "upgrade-fence"} {
		t.Run(kind, func(t *testing.T) {
			r, expected, pending := unpublishedStartRepository(t, "hermes", "stopped")
			ctx := t.Context()
			query := ""
			switch kind {
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "unknown-desired-table":
				query = `DROP TABLE instance_desired_state`
			case "write-failure":
				query = `CREATE TRIGGER reject_resume BEFORE UPDATE ON instances FOR EACH ROW BEGIN IF NEW.status='creating' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected resume failure'; END IF; END`
			case "upgrade-fence":
				query = `INSERT INTO gateway_runtime_upgrades VALUES(7,3,2,'0123456789abcdef0123456789abcdef','stop_intent',UTC_TIMESTAMP(),UTC_TIMESTAMP())`
			}
			if query != "" {
				if _, err := r.sess.SQL().Exec(query); err != nil {
					t.Fatal(err)
				}
			}
			resumed, err := resumeUnpublishedStart(t, r, ctx, expected, pending)
			if resumed || err == nil {
				t.Fatalf("failed/unknown resume authorized: resumed=%v err=%v", resumed, err)
			}
			after, err := r.GetByIDContext(t.Context(), 7)
			if err != nil || after.Status != "stopped" || after.RuntimeGeneration != 2 || after.RuntimeErrorCode == nil || *after.RuntimeErrorCode != "FAILED" {
				t.Fatalf("failed resume changed instance: %+v %v", after, err)
			}
		})
	}
}
