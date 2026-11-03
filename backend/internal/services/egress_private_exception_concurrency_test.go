package services

import (
	"context"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/models"
)

type controlledEgressExceptionRepo struct {
	*stubEgressPrivateExceptionRepo
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func TestEgressPrivateExceptionLeaderCancellationDoesNotPoisonSharedRefresh(t *testing.T) {
	repo := newControlledEgressRepo()
	svc := NewEgressPrivateExceptionService(repo, nil, nil).(*egressPrivateExceptionService)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leader := make(chan int, 1)
	go func() { leader <- len(svc.SnapshotRulesContext(ctx)) }()
	select {
	case <-repo.entered:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	follower := make(chan int, 1)
	go func() { follower <- len(svc.SnapshotRulesContext(context.Background())) }()
	cancel()
	select {
	case n := <-leader:
		if n != 0 {
			t.Fatal("canceled leader received permission")
		}
	case <-time.After(time.Second):
		t.Fatal("leader did not cancel its wait")
	}
	close(repo.release)
	select {
	case n := <-follower:
		if n != 1 {
			t.Fatal("leader cancellation denied an unrelated request")
		}
	case <-time.After(time.Second):
		t.Fatal("follower did not receive the shared result")
	}
	if len(svc.SnapshotRules()) != 1 || repo.calls.Load() != 1 {
		t.Fatal("valid shared snapshot was lost or refreshed twice")
	}
	svc.mu.RLock()
	backoff := !svc.retryAt.IsZero()
	svc.mu.RUnlock()
	if backoff {
		t.Fatal("request cancellation triggered a global database retry interval")
	}
}

func (r *controlledEgressExceptionRepo) ListEnabled(ctx context.Context) ([]models.EgressPrivateException, error) {
	r.calls.Add(1)
	items, err := r.stubEgressPrivateExceptionRepo.ListEnabled(ctx)
	select {
	case r.entered <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.release:
		return items, err
	}
}

func newControlledEgressRepo() *controlledEgressExceptionRepo {
	return &controlledEgressExceptionRepo{
		stubEgressPrivateExceptionRepo: &stubEgressPrivateExceptionRepo{items: []models.EgressPrivateException{{ID: 1, ScopeType: "instance", ScopeID: 8, CIDR: "10.255.25.3/32", Port: 18080, Enabled: true}}},
		entered:                        make(chan struct{}, 4), release: make(chan struct{}),
	}
}

func TestEgressPrivateExceptionRefreshWaiterCanCancel(t *testing.T) {
	repo := newControlledEgressRepo()
	svc := NewEgressPrivateExceptionService(repo, nil, nil)
	leader := make(chan int, 1)
	go func() { leader <- len(svc.SnapshotRulesContext(context.Background())) }()
	select {
	case <-repo.entered:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	waiter := make(chan int, 1)
	go func() { waiter <- len(svc.SnapshotRulesContext(ctx)) }()
	cancel()
	select {
	case n := <-waiter:
		if n != 0 {
			t.Fatal("canceled waiter received permission")
		}
	case <-time.After(time.Second):
		t.Fatal("waiter was stuck behind database I/O")
	}
	if repo.calls.Load() != 1 {
		t.Fatal("concurrent request duplicated the refresh")
	}
	close(repo.release)
	select {
	case n := <-leader:
		if n != 1 {
			t.Fatal("leader did not load valid rules")
		}
	case <-time.After(time.Second):
		t.Fatal("refresh did not complete")
	}
}

func TestEgressPrivateExceptionSlowRefreshExpiresAndBacksOff(t *testing.T) {
	repo := newControlledEgressRepo()
	svc := NewEgressPrivateExceptionService(repo, nil, nil).(*egressPrivateExceptionService)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if rules := svc.SnapshotRulesContext(ctx); len(rules) != 0 {
		t.Fatal("timed-out query granted permissions")
	}
	for i := 0; i < 5; i++ {
		if len(svc.SnapshotRules()) != 0 {
			t.Fatal("failed refresh reused stale rules")
		}
	}
	if repo.calls.Load() != 1 {
		t.Fatal("failure caused an immediate query storm")
	}
	close(repo.release)
	svc.mu.Lock()
	svc.retryAt = time.Now().Add(-time.Second)
	svc.mu.Unlock()
	if len(svc.SnapshotRules()) != 1 || repo.calls.Load() != 2 {
		t.Fatal("rules did not recover after the retry interval")
	}
}

func TestEgressPrivateExceptionRevocationDuringRefreshDiscardsOldSnapshot(t *testing.T) {
	repo := newControlledEgressRepo()
	svc := NewEgressPrivateExceptionService(repo, &stubEgressInstanceLookup{instance: &models.Instance{ID: 8}}, nil)
	result := make(chan int, 1)
	go func() { result <- len(svc.SnapshotRules()) }()
	select {
	case <-repo.entered:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	updated := make(chan error, 1)
	go func() {
		_, err := svc.Update(1, SaveEgressPrivateExceptionRequest{ScopeType: "instance", ScopeID: 8, CIDR: "10.255.25.3/32", Port: 18080, Enabled: false})
		updated <- err
	}()
	select {
	case err := <-updated:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("rule mutation was blocked by the refresh lock")
	}
	close(repo.release)
	select {
	case n := <-result:
		if n != 0 {
			t.Fatal("old in-flight snapshot restored a revoked rule")
		}
	case <-time.After(time.Second):
		t.Fatal("refresh did not reload after mutation")
	}
	id := 8
	if svc.Allows(&id, nil, netip.MustParseAddr("10.255.25.3"), 18080) {
		t.Fatal("revoked rule remained available")
	}
	if repo.calls.Load() != 2 {
		t.Fatal("in-flight snapshot was not discarded and refreshed")
	}
}
