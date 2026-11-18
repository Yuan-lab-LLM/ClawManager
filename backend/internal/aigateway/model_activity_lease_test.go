package aigateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"clawreef/internal/models"
)

func activityLeaseRequest() ChatCompletionRequest {
	req := routingRequest("hello")
	id := 1
	req.InstanceID = &id
	req.Model = "cheap"
	return req
}

func TestModelActivityLeaseHealthyRenewal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		activity := &activityRecorder{}
		s := &service{modelActivity: activity}
		ctx, finish, err := s.BeginModelActivityScope(context.Background(), 1, activityLeaseRequest())
		if err != nil {
			t.Fatal(err)
		}
		defer finish()
		for i := 0; i < 20; i++ {
			time.Sleep(15 * time.Second)
			synctest.Wait()
			if ctx.Err() != nil {
				t.Fatalf("healthy lease cancelled at renewal %d: %v", i, ctx.Err())
			}
		}
		if activity.renews.Load() != 20 {
			t.Fatalf("renewals=%d want 20", activity.renews.Load())
		}
	})
}

func TestModelActivityLeaseTemporaryRenewalErrorRecovers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		activity := &activityRecorder{renew: func(context.Context, string) error {
			if calls.Add(1) <= 3 {
				return errors.New("temporary outage")
			}
			return nil
		}}
		s := &service{modelActivity: activity}
		ctx, finish, err := s.BeginModelActivityScope(context.Background(), 1, activityLeaseRequest())
		if err != nil {
			t.Fatal(err)
		}
		defer finish()
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if ctx.Err() != nil || activity.renews.Load() < 7 {
			t.Fatalf("recovery failed: ctx=%v renewals=%d", ctx.Err(), activity.renews.Load())
		}
	})
}

func TestModelActivityLeaseWatchdogCancelsBeforeExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		activity := &activityRecorder{renew: func(context.Context, string) error { return errors.New("outage") }}
		s := &service{modelActivity: activity}
		ctx, finish, err := s.BeginModelActivityScope(context.Background(), 1, activityLeaseRequest())
		if err != nil {
			t.Fatal(err)
		}
		defer finish()
		time.Sleep(79 * time.Second)
		synctest.Wait()
		if ctx.Err() != nil {
			t.Fatal("watchdog cancelled before safety deadline")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if ctx.Err() == nil {
			t.Fatal("watchdog left request active at lease safety deadline")
		}
		calls := activity.renews.Load()
		time.Sleep(time.Minute)
		synctest.Wait()
		if activity.renews.Load() != calls {
			t.Fatal("renewal continued after watchdog cancellation")
		}
	})
}

func TestModelActivityLeaseStopsAndFinishesOnce(t *testing.T) {
	for _, cancelParent := range []bool{false, true} {
		t.Run(fmtBool(cancelParent), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				activity := &activityRecorder{}
				parent, cancel := context.WithCancel(context.Background())
				defer cancel()
				s := &service{modelActivity: activity}
				_, finish, err := s.BeginModelActivityScope(parent, 1, activityLeaseRequest())
				if err != nil {
					t.Fatal(err)
				}
				time.Sleep(15 * time.Second)
				synctest.Wait()
				if cancelParent {
					cancel()
				} else {
					finish()
				}
				synctest.Wait()
				calls := activity.renews.Load()
				time.Sleep(time.Minute)
				synctest.Wait()
				if activity.renews.Load() != calls {
					t.Fatal("renewal continued after scope stopped")
				}
				finish()
				finish()
				if activity.finishes.Load() != 1 {
					t.Fatalf("finishes=%d want 1", activity.finishes.Load())
				}
			})
		})
	}
}

func TestModelActivityLeasePublicInferenceReceivesWatchdogCancellation(t *testing.T) {
	for _, entry := range []string{"chat", "stream", "image"} {
		t.Run(entry, func(t *testing.T) {
			// Build the image fixture outside fake time: server cleanup uses real I/O.
			s, _, _, attr := imageExecutionGateway(t, func(http.ResponseWriter, *http.Request) {})
			imageReq := imageExecutionRequest(t, "")
			if entry != "image" {
				s = routingGateway(routingModels()[:1], &stubAuditEventService{})
			}
			synctest.Test(t, func(t *testing.T) {
				activity := &activityRecorder{renew: func(context.Context, string) error { return errors.New("outage") }}
				WithModelActivity(activity)(s)
				var calls atomic.Int32
				client := &http.Client{Transport: layaRoundTrip(func(req *http.Request) (*http.Response, error) {
					calls.Add(1)
					<-req.Context().Done()
					return nil, req.Context().Err()
				})}
				s.httpClient, s.streamHTTPClient = client, client
				parent, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan error, 1)
				go func() {
					var err error
					switch entry {
					case "chat":
						_, _, err = s.ChatCompletions(parent, 1, activityLeaseRequest())
					case "stream":
						_, err = s.StreamChatCompletions(parent, 1, activityLeaseRequest(), httptest.NewRecorder())
					case "image":
						_, err = s.ExecuteImage(parent, 1, imageReq, attr)
					}
					done <- err
				}()
				synctest.Wait()
				if calls.Load() != 1 {
					t.Fatalf("provider calls=%d", calls.Load())
				}
				time.Sleep(80 * time.Second)
				synctest.Wait()
				select {
				case err := <-done:
					if err == nil {
						t.Fatal("expired activity succeeded")
					}
				default:
					t.Fatal("public inference did not cancel at lease safety deadline")
				}
				if activity.finishes.Load() != 1 {
					t.Fatalf("finishes=%d", activity.finishes.Load())
				}
			})
		})
	}
}

type activityLeaseAdmissionRecorder struct {
	*activityRecorder
	begin func(context.Context) (string, error)
}

func (r *activityLeaseAdmissionRecorder) BeginModelActivity(ctx context.Context, _, _, _ int, _ string) (string, error) {
	return r.begin(ctx)
}

func TestModelActivityLeaseEmptyAdmissionDoesNotRenew(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		activity := &activityRecorder{}
		recorder := &activityLeaseAdmissionRecorder{activityRecorder: activity, begin: func(context.Context) (string, error) { return "", nil }}
		parent := context.Background()
		s := &service{modelActivity: recorder}
		ctx, finish, err := s.beginModelActivity(parent, 1, activityLeaseRequest())
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		finish()
		if ctx != parent || activity.renews.Load() != 0 || activity.finishes.Load() != 0 {
			t.Fatal("empty admission started a lease")
		}
	})
}

func TestModelActivityLeaseAdmissionTimeIsConservative(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		recorder := &activityLeaseAdmissionRecorder{activityRecorder: &activityRecorder{}, begin: func(context.Context) (string, error) {
			time.Sleep(81 * time.Second)
			return "slow-admission", nil
		}}
		s := &service{modelActivity: recorder}
		ctx, finish, err := s.beginModelActivity(context.Background(), 1, activityLeaseRequest())
		if err != nil {
			t.Fatal(err)
		}
		defer finish()
		if ctx.Err() == nil {
			t.Fatal("slow admission returned an active context after safety deadline")
		}
	})
}

func TestModelActivityLeaseSuccessfulRenewalTimeIsConservative(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		recorder := &activityRecorder{renew: func(ctx context.Context, _ string) error {
			deadline, ok := ctx.Deadline()
			if !ok || deadline.Sub(time.Now()) != 5*time.Second {
				t.Error("renewal missing five-second timeout")
			}
			if calls.Add(1) == 1 {
				time.Sleep(4 * time.Second)
				return nil
			}
			return errors.New("later outage")
		}}
		s := &service{modelActivity: recorder}
		ctx, finish, err := s.beginModelActivity(context.Background(), 1, activityLeaseRequest())
		if err != nil {
			t.Fatal(err)
		}
		defer finish()
		time.Sleep(94 * time.Second)
		synctest.Wait()
		if ctx.Err() != nil {
			t.Fatal("successful renewal did not extend safety deadline")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if ctx.Err() == nil {
			t.Fatal("watchdog used renewal response time rather than start time")
		}
	})
}

func TestModelActivityLeaseWatchdogBoundsBlockedRenewal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		blocked := make(chan struct{})
		recorder := &activityRecorder{renew: func(context.Context, string) error {
			<-blocked // Simulate a driver temporarily ignoring cancellation.
			return nil
		}}
		s := &service{modelActivity: recorder}
		ctx, finish, err := s.beginModelActivity(context.Background(), 1, activityLeaseRequest())
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(80 * time.Second)
		synctest.Wait()
		if ctx.Err() == nil {
			t.Error("blocked renewal delayed request cancellation")
		}
		close(blocked)
		finish()
		time.Sleep(time.Minute)
		synctest.Wait()
		if recorder.renews.Load() != 1 {
			t.Fatal("renewal continued after blocked call returned to expired scope")
		}
	})
}

func TestModelActivityLeaseRevokedScopeCancelsImmediately(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		recorder := &activityRecorder{renew: func(context.Context, string) error { return models.ErrActivityUnavailable }}
		s := &service{modelActivity: recorder}
		ctx, finish, err := s.beginModelActivity(context.Background(), 1, activityLeaseRequest())
		if err != nil {
			t.Fatal(err)
		}
		defer finish()
		time.Sleep(15 * time.Second)
		synctest.Wait()
		if !errors.Is(context.Cause(ctx), models.ErrActivityUnavailable) {
			t.Fatalf("revoked scope cause=%v", context.Cause(ctx))
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if recorder.renews.Load() != 1 {
			t.Fatal("revoked scope retried renewal")
		}
	})
}

func TestModelActivityLeaseFinishBoundsBlockedRecorder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		blocked := make(chan struct{})
		recorder := &activityRecorder{renew: func(context.Context, string) error { <-blocked; return nil }}
		s := &service{modelActivity: recorder}
		ctx, finish, err := s.beginModelActivity(context.Background(), 1, activityLeaseRequest())
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(15 * time.Second)
		synctest.Wait()
		// Releasing after six seconds makes an unbounded join observable without
		// leaking a fake-time goroutine when the test fails.
		go func() { time.Sleep(6 * time.Second); close(blocked) }()
		start := time.Now()
		finish()
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("completion blocked for %v", elapsed)
		}
		if ctx.Err() == nil || recorder.finishes.Load() != 1 {
			t.Fatal("completion did not cancel and finish scope")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		finish()
		if recorder.renews.Load() != 1 || recorder.finishes.Load() != 1 {
			t.Fatal("late renewal resumed or completion duplicated")
		}
	})
}

func TestModelActivityLeaseQueuedWatchdogChecksRenewedDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(context.Canceled)
		var timerMu sync.Mutex
		deadline := time.Now().Add(80 * time.Second)
		entered := make(chan struct{}, 2)
		watchdog := time.AfterFunc(time.Until(deadline), func() {
			entered <- struct{}{}
			expireModelActivityLease(&timerMu, &deadline, cancel)
		})
		defer watchdog.Stop()

		// A successful renewal holds the mutex while an already-fired callback
		// waits to inspect the lease. Reset cannot retract that queued callback.
		timerMu.Lock()
		time.Sleep(80 * time.Second)
		<-entered
		deadline = time.Now().Add(80 * time.Second)
		watchdog.Reset(time.Until(deadline))
		timerMu.Unlock()
		synctest.Wait()
		if ctx.Err() != nil {
			t.Fatal("queued old watchdog cancelled a renewed lease")
		}

		time.Sleep(80 * time.Second)
		synctest.Wait()
		if !errors.Is(context.Cause(ctx), models.ErrActivityUnavailable) {
			t.Fatal("renewed watchdog failed to cancel at new deadline")
		}
	})
}
