package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"clawreef/internal/models"
	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/upper/db/v4"
)

type stopRetrySession struct {
	db.Session
	attempts       atomic.Int32
	committedError error
	after          func(int, error) error
	barrier        chan struct{}
	arrivals       atomic.Int32
}

func (s *stopRetrySession) TxContext(ctx context.Context, fn func(db.Session) error, opts *sql.TxOptions) error {
	attempt := int(s.attempts.Add(1))
	err := s.Session.TxContext(ctx, func(tx db.Session) error {
		var wrapped db.Session = tx
		if s.barrier != nil {
			wrapped = stopBarrierSession{Session: tx, owner: s}
		}
		err := fn(wrapped)
		if s.after != nil {
			return s.after(attempt, err)
		}
		return err
	}, opts)
	if err == nil && s.committedError != nil {
		return s.committedError
	}
	return err
}

type stopBarrierSession struct {
	db.Session
	owner *stopRetrySession
}

func (s stopBarrierSession) SQL() db.SQL { return stopBarrierSQL{SQL: s.Session.SQL(), owner: s.owner} }

type stopBarrierSQL struct {
	db.SQL
	owner *stopRetrySession
}

func (s stopBarrierSQL) ExecContext(ctx context.Context, q interface{}, args ...interface{}) (sql.Result, error) {
	result, err := s.SQL.ExecContext(ctx, q, args...)
	if err == nil && strings.HasPrefix(fmt.Sprint(q), "UPDATE instance_idle_stop_operations SET stop_kind='manual'") {
		n := s.owner.arrivals.Add(1)
		if n == 2 {
			close(s.owner.barrier)
		}
		if n <= 2 {
			select {
			case <-s.owner.barrier:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return result, err
}
func manualStopRetryInput(id int) models.InstanceIdleStopOperation {
	return models.InstanceIdleStopOperation{InstanceID: id, UserID: 3, Generation: 2}
}
func assertOneManualStop(t *testing.T, r *instanceRepository, id int) {
	t.Helper()
	var count int
	row, err := r.sess.SQL().QueryRow("SELECT COUNT(*) FROM instance_idle_stop_operations WHERE instance_id=?", id)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&count); err != nil || count != 1 {
		t.Fatalf("operations=%d err=%v", count, err)
	}
}
func TestManualStopRetriesConcurrentGapDeadlock(t *testing.T) {
	r := idleRuntimeRepository(t)
	if _, err := r.sess.SQL().Exec("UPDATE instances SET status='running' WHERE id=7"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.sess.SQL().Exec("INSERT INTO instances(id,user_id,name,status,runtime_generation,instance_mode,type) VALUES(8,3,'second','running',2,'lite','openclaw')"); err != nil {
		t.Fatal(err)
	}
	wrapper := &stopRetrySession{Session: r.sess, barrier: make(chan struct{})}
	store := NewInstanceAutoStopRepository(wrapper)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, id := range []int{7, 8} {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, err := store.BeginManualStop(t.Context(), manualStopRetryInput(id))
			errs <- err
		}(id)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent stop failed: %v", err)
		}
	}
	if wrapper.attempts.Load() < 3 {
		t.Fatal("fixture did not exercise transaction retry")
	}
	assertOneManualStop(t, r, 7)
	assertOneManualStop(t, r, 8)
}
func TestManualStopRetriesOnlyRolledBackDeadlock(t *testing.T) {
	for _, tc := range []struct {
		name         string
		failure      error
		wantAttempts int
		success      bool
	}{
		{"deadlock", &drivermysql.MySQLError{Number: 1213, Message: "deadlock"}, 2, true},
		{"lock-timeout", &drivermysql.MySQLError{Number: 1205, Message: "lock timeout"}, 1, false},
		{"transport", errors.New("connection reset after commit"), 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := idleRuntimeRepository(t)
			wrapper := &stopRetrySession{Session: r.sess, after: func(attempt int, err error) error {
				if err != nil {
					return err
				}
				if attempt == 1 {
					return tc.failure
				}
				return nil
			}}
			op, err := NewInstanceAutoStopRepository(wrapper).BeginManualStop(t.Context(), manualStopRetryInput(7))
			if (err == nil) != tc.success || int(wrapper.attempts.Load()) != tc.wantAttempts {
				t.Fatalf("op=%v err=%v attempts=%d", op, err, wrapper.attempts.Load())
			}
			if tc.success {
				assertOneManualStop(t, r, 7)
			} else if op != nil {
				t.Fatal("rolled back operation returned")
			}
		})
	}
}
func TestManualStopDeadlockRetryBoundAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			r := idleRuntimeRepository(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			wrapper := &stopRetrySession{Session: r.sess, after: func(_ int, err error) error {
				if err != nil {
					return err
				}
				if cancelled {
					cancel()
				}
				return &drivermysql.MySQLError{Number: 1213}
			}}
			_, err := NewInstanceAutoStopRepository(wrapper).BeginManualStop(ctx, manualStopRetryInput(7))
			want := int32(3)
			if cancelled {
				want = 1
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancel err=%v", err)
				}
			}
			if err == nil || wrapper.attempts.Load() != want {
				t.Fatalf("err=%v attempts=%d", err, wrapper.attempts.Load())
			}
		})
	}
}

func TestManualStopDoesNotReplayUnknownCommitResult(t *testing.T) {
	r := idleRuntimeRepository(t)
	unknown := errors.New("connection reset after committed transaction")
	wrapper := &stopRetrySession{Session: r.sess, committedError: unknown}
	op, err := NewInstanceAutoStopRepository(wrapper).BeginManualStop(t.Context(), manualStopRetryInput(7))
	if !errors.Is(err, unknown) || op != nil || wrapper.attempts.Load() != 1 {
		t.Fatalf("unknown commit replayed: op=%v err=%v attempts=%d", op, err, wrapper.attempts.Load())
	}
	assertOneManualStop(t, r, 7)
}
