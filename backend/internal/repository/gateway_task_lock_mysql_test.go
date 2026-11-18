package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// Uses the empty, disposable database created by the lifecycle integration test.
func testGatewayTaskConcurrentWrites(t *testing.T, sess db.Session, model models.LLMModel) {
	t.Helper()
	raw, err := os.ReadFile("../db/migrations/096_add_instance_auto_stop.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(raw), ";") {
		if strings.Contains(statement, "CREATE TABLE IF NOT EXISTS instance_model_activity (") {
			if _, err := sess.SQL().Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(func() { _, _ = sess.SQL().Exec("DROP TABLE IF EXISTS instance_model_activity") })
	tasks := NewGatewayTaskRepository(sess)
	for _, mode := range []string{"pro", "lite"} {
		t.Run("concurrent_check_bind_policy_"+mode, func(t *testing.T) {
			if _, err := sess.SQL().Exec("UPDATE instances SET instance_mode=? WHERE id=10", mode); err != nil {
				t.Fatal(err)
			}
			identities := make([]models.GatewayTaskIdentity, 4)
			for n := range identities {
				identities[n] = models.GatewayTaskIdentity{InstanceID: 10, UserID: 1, Generation: 5, SessionID: "ses_lock_" + mode, TaskID: fmt.Sprintf("turn_%d", n)}
				if err := tasks.Bind(t.Context(), identities[n], model.ID); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
			defer cancel()
			var workers sync.WaitGroup
			failures := make(chan error, 9)
			start := make(chan struct{})
			for worker := 0; worker < 9; worker++ {
				workers.Add(1)
				go func(worker int) {
					defer workers.Done()
					<-start
					copy := model
					copy.UpdateAccessPolicy = false
					for n := 0; n < 60; n++ {
						identity := identities[(worker+n)%len(identities)]
						var err error
						switch {
						case worker < 4:
							var state *models.GatewayTaskState
							state, err = tasks.Check(ctx, identity)
							if err == nil && (state.Revoked || state.Finished) {
								err = fmt.Errorf("live task denied: %+v", state)
							}
						case worker < 8:
							err = tasks.Bind(ctx, identity, model.ID)
						default:
							// Model policy transactions lock model -> tasks, unlike the
							// instance-first runtime path. Exercise both concurrently.
							err = NewLLMModelRepository(sess).Save(&copy)
						}
						if err != nil {
							failures <- fmt.Errorf("worker %d iteration %d: %w", worker, n, err)
							return
						}
					}
				}(worker)
			}
			close(start)
			workers.Wait()
			close(failures)
			for err := range failures {
				t.Error(err)
			}
		})
	}
	if _, err := sess.SQL().Exec("UPDATE instances SET instance_mode='pro' WHERE id=10"); err != nil {
		t.Fatal(err)
	}

	t.Run("revocation_committed_while_check_waits", func(t *testing.T) {
		identity := models.GatewayTaskIdentity{InstanceID: 10, UserID: 1, Generation: 5, SessionID: "ses_policy_wait", TaskID: "turn"}
		if err := tasks.Bind(t.Context(), identity, model.ID); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		locked := make(chan struct{}, 1)
		release := make(chan struct{})
		policyDone := make(chan error, 1)
		go func() {
			policyDone <- sess.TxContext(ctx, func(tx db.Session) error {
				row, err := tx.SQL().QueryRowContext(ctx, "SELECT id FROM llm_models WHERE id=? FOR UPDATE", model.ID)
				if err != nil {
					return err
				}
				var id int
				if err = row.Scan(&id); err != nil {
					return err
				}
				locked <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
				if _, err = tx.SQL().ExecContext(ctx, "UPDATE llm_models SET is_active=FALSE WHERE id=?", id); err != nil {
					return err
				}
				return revokeUnauthorizedTasks(tx, id)
			}, nil)
		}()
		defer func() {
			copy := model
			copy.UpdateAccessPolicy = false
			if err := NewLLMModelRepository(sess).Save(&copy); err != nil {
				t.Error(err)
			}
		}()
		select {
		case <-locked:
		case err := <-policyDone:
			t.Fatalf("policy lock: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		type result struct {
			state *models.GatewayTaskState
			err   error
		}
		checked := make(chan result, 1)
		go func() { state, err := tasks.Check(ctx, identity); checked <- result{state, err} }()
		// The held exclusive lock prevents this shared-lock query completing.
		// Observe that query using our own sessions (no PROCESS/admin grant).
		waiting := false
		for !waiting && ctx.Err() == nil {
			row, err := sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.PROCESSLIST
 WHERE ID<>CONNECTION_ID() AND DB=DATABASE() AND INFO LIKE 'SELECT id FROM llm_models WHERE id IN (% FOR SHARE'`)
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			var count int
			if err = row.Scan(&count); err != nil {
				cancel()
				t.Fatal(err)
			}
			waiting = count > 0
			if !waiting {
				select {
				case <-ctx.Done():
				case <-time.After(5 * time.Millisecond):
				}
			}
		}
		close(release)
		if err := <-policyDone; err != nil {
			t.Fatal(err)
		}
		got := <-checked
		if !waiting || got.err != nil || got.state == nil || !got.state.Revoked {
			t.Fatalf("wait=%v state=%+v error=%v", waiting, got.state, got.err)
		}
	})

	t.Run("finish_races_bind_and_check", func(t *testing.T) {
		for n := 0; n < 30; n++ {
			identity := models.GatewayTaskIdentity{InstanceID: 10, UserID: 1, Generation: 5, SessionID: "ses_finish", TaskID: fmt.Sprint(n)}
			if err := tasks.Bind(t.Context(), identity, model.ID); err != nil {
				t.Fatal(err)
			}
			results := make(chan error, 3)
			go func() { results <- tasks.Bind(t.Context(), identity, model.ID) }()
			go func() { _, err := tasks.Check(t.Context(), identity); results <- err }()
			go func() { results <- tasks.Finish(t.Context(), identity, false) }()
			for range 3 {
				if err := <-results; err != nil && !errors.Is(err, models.ErrModelUnavailable) {
					t.Fatal(err)
				}
			}
			state, err := tasks.Check(t.Context(), identity)
			if err != nil || !state.Finished {
				t.Fatalf("finish lost: %+v %v", state, err)
			}
			if err := tasks.Bind(t.Context(), identity, model.ID); !errors.Is(err, models.ErrModelUnavailable) {
				t.Fatalf("finished task rebound: %v", err)
			}
		}
	})

	t.Run("lite_stop_fence_allows_completion_only", func(t *testing.T) {
		identity := models.GatewayTaskIdentity{InstanceID: 10, UserID: 1, Generation: 5, SessionID: "ses_stop", TaskID: "turn"}
		if err := tasks.Bind(t.Context(), identity, model.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := sess.SQL().Exec("UPDATE instances SET instance_mode='lite',status='stopping' WHERE id=10"); err != nil {
			t.Fatal(err)
		}
		defer func() {
			_, _ = sess.SQL().Exec("UPDATE instances SET instance_mode='pro',status='running' WHERE id=10")
		}()
		if _, err := tasks.Check(t.Context(), identity); !errors.Is(err, models.ErrInstanceStopping) {
			t.Fatalf("check bypassed stop: %v", err)
		}
		if err := tasks.Bind(t.Context(), identity, model.ID); !errors.Is(err, models.ErrInstanceStopping) {
			t.Fatalf("bind bypassed stop: %v", err)
		}
		for range 2 {
			if err := tasks.Finish(t.Context(), identity, false); err != nil {
				t.Fatalf("completion blocked: %v", err)
			}
		}
	})

	t.Run("instance_lock_does_not_block_other_instances", func(t *testing.T) {
		if _, err := sess.SQL().Exec("INSERT INTO instances(id,user_id,runtime_generation) VALUES(19,1,5)"); err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = sess.SQL().Exec("DELETE FROM instances WHERE id=19") }()
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		locked := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- sess.TxContext(ctx, func(tx db.Session) error {
				if _, err := lockActivityInstance(ctx, tx, 10); err != nil {
					return err
				}
				close(locked)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}, nil)
		}()
		select {
		case <-locked:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		identity := models.GatewayTaskIdentity{InstanceID: 19, UserID: 1, Generation: 5, SessionID: "ses_other", TaskID: "turn"}
		state, err := tasks.Check(ctx, identity)
		close(release)
		if lockErr := <-done; lockErr != nil {
			t.Fatal(lockErr)
		}
		if err != nil || state.Revoked || state.Finished {
			t.Fatalf("unrelated instance blocked: %+v %v", state, err)
		}
	})
}
