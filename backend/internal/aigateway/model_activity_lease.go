package aigateway

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"clawreef/internal/models"
)

const modelActivityLeaseSafetyMargin = 10 * time.Second

// The watchdog runs independently of renewal I/O so a stalled database cannot
// leave inference running after its durable admission expires.
func (s *service) renewModelActivityScope(parent context.Context, key string, instanceID int, admittedAt time.Time) (context.Context, func(), error) {
	ctx, cancel := context.WithCancelCause(parent)
	var timerMu sync.Mutex
	deadline := admittedAt.Add(models.ModelActivityLeaseDuration - modelActivityLeaseSafetyMargin)
	if !deadline.After(time.Now()) {
		cancel(models.ErrActivityUnavailable)
	}
	watchdog := time.AfterFunc(time.Until(deadline), func() {
		expireModelActivityLease(&timerMu, &deadline, cancel)
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(models.ModelActivityRenewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if ctx.Err() != nil {
					return
				}
				started := time.Now()
				renewCtx, stop := context.WithTimeout(ctx, 5*time.Second)
				err := s.modelActivity.RenewModelActivity(renewCtx, key)
				stop()
				if err != nil {
					if errors.Is(err, models.ErrActivityUnavailable) {
						cancel(models.ErrActivityUnavailable)
						return
					}
					continue
				}
				timerMu.Lock()
				deadline = started.Add(models.ModelActivityLeaseDuration - modelActivityLeaseSafetyMargin)
				if ctx.Err() == nil {
					if time.Until(deadline) <= 0 {
						cancel(models.ErrActivityUnavailable)
					} else {
						watchdog.Reset(time.Until(deadline))
					}
				}
				timerMu.Unlock()
			}
		}
	}()
	var finishOnce sync.Once
	return ctx, func() {
		finishOnce.Do(func() {
			timerMu.Lock()
			watchdog.Stop()
			cancel(context.Canceled)
			timerMu.Unlock()
			// SQL honors renewal cancellation. Bound shutdown even if a
			// recorder fails to do so; its late CAS cannot revive this scope.
			joined := time.NewTimer(5 * time.Second)
			select {
			case <-done:
			case <-joined.C:
			}
			joined.Stop()
			finishCtx, stop := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
			defer stop()
			if err := s.modelActivity.FinishModelActivity(finishCtx, key); err != nil {
				// An unrecorded completion recovers through the finite lease.
				log.Printf("model activity completion pending for instance %d: %T", instanceID, err)
			}
		})
	}, nil
}

func expireModelActivityLease(timerMu *sync.Mutex, deadline *time.Time, cancel context.CancelCauseFunc) {
	timerMu.Lock()
	defer timerMu.Unlock()
	// Reset cannot retract a callback already waiting for this lock.
	if !deadline.After(time.Now()) {
		cancel(models.ErrActivityUnavailable)
	}
}
