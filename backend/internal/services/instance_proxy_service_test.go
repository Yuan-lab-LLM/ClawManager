package services

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/models"
)

// --- fakes ---

type fakeInstanceLookup struct {
	mu        sync.Mutex
	instances map[int]*models.Instance
	callCount int
}

func newFakeInstanceLookup() *fakeInstanceLookup {
	return &fakeInstanceLookup{instances: make(map[int]*models.Instance)}
}

func (f *fakeInstanceLookup) add(id, userID int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.instances[id] = &models.Instance{ID: id, UserID: userID}
}

func (f *fakeInstanceLookup) GetByID(id int) (*models.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount++
	inst, ok := f.instances[id]
	if !ok {
		return nil, nil
	}
	return inst, nil
}

func (f *fakeInstanceLookup) getCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callCount
}

// --- owner cache tests ---

// Test 4: resolveOwnerID happy path — OwnerID matches DB
func TestResolveOwnerID_HappyPath(t *testing.T) {
	lookup := newFakeInstanceLookup()
	lookup.add(4, 3)

	now := time.Now()
	cache := newOwnerCache(lookup, 30*time.Second, func() time.Time { return now })
	svc := &InstanceProxyService{cache: cache}

	token := &AccessToken{InstanceID: 4, OwnerID: 3}
	ownerID, err := svc.resolveOwnerID(context.Background(), token)
	if err != nil {
		t.Fatalf("resolveOwnerID() error = %v", err)
	}
	if ownerID != 3 {
		t.Fatalf("ownerID = %d, want 3", ownerID)
	}
}

// Test 5: resolveOwnerID legacy fallback — OwnerID=0, trust DB
func TestResolveOwnerID_LegacyFallback(t *testing.T) {
	lookup := newFakeInstanceLookup()
	lookup.add(4, 3)

	now := time.Now()
	cache := newOwnerCache(lookup, 30*time.Second, func() time.Time { return now })
	svc := &InstanceProxyService{cache: cache}

	token := &AccessToken{InstanceID: 4, OwnerID: 0}
	ownerID, err := svc.resolveOwnerID(context.Background(), token)
	if err != nil {
		t.Fatalf("resolveOwnerID() error = %v", err)
	}
	if ownerID != 3 {
		t.Fatalf("ownerID = %d, want 3 (from DB)", ownerID)
	}
}

// Test 6: resolveOwnerID mismatch — OwnerID=2, DB returns 3
func TestResolveOwnerID_Mismatch(t *testing.T) {
	lookup := newFakeInstanceLookup()
	lookup.add(4, 3)

	now := time.Now()
	cache := newOwnerCache(lookup, 30*time.Second, func() time.Time { return now })
	svc := &InstanceProxyService{cache: cache}

	token := &AccessToken{InstanceID: 4, OwnerID: 2}
	_, err := svc.resolveOwnerID(context.Background(), token)
	if err == nil {
		t.Fatal("resolveOwnerID() expected error, got nil")
	}
	if err != ErrOwnerMismatch {
		t.Fatalf("resolveOwnerID() error = %v, want ErrOwnerMismatch", err)
	}
}

// Test 7: resolveOwnerID instance missing — returns ErrInstanceNotFound
func TestResolveOwnerID_InstanceNotFound(t *testing.T) {
	lookup := newFakeInstanceLookup()
	// Do not add any instance

	now := time.Now()
	cache := newOwnerCache(lookup, 30*time.Second, func() time.Time { return now })
	svc := &InstanceProxyService{cache: cache}

	token := &AccessToken{InstanceID: 999, OwnerID: 1}
	_, err := svc.resolveOwnerID(context.Background(), token)
	if err == nil {
		t.Fatal("resolveOwnerID() expected error, got nil")
	}
	if err != ErrInstanceNotFound {
		t.Fatalf("resolveOwnerID() error = %v, want ErrInstanceNotFound", err)
	}
}

// Test 8: Cache hit — same instanceID twice; fake repo invocation count == 1
func TestOwnerCache_Hit(t *testing.T) {
	lookup := newFakeInstanceLookup()
	lookup.add(4, 3)

	now := time.Now()
	cache := newOwnerCache(lookup, 30*time.Second, func() time.Time { return now })

	ctx := context.Background()
	owner1, err := cache.Get(ctx, 4)
	if err != nil {
		t.Fatalf("first Get() error = %v", err)
	}
	owner2, err := cache.Get(ctx, 4)
	if err != nil {
		t.Fatalf("second Get() error = %v", err)
	}

	if owner1 != 3 || owner2 != 3 {
		t.Fatalf("owners = %d, %d; want 3, 3", owner1, owner2)
	}
	if count := lookup.getCallCount(); count != 1 {
		t.Fatalf("GetByID call count = %d, want 1 (cached)", count)
	}
}

// Test 9: Cache expiry — injectable clock advances 30s; second call hits repo again
func TestOwnerCache_Expiry(t *testing.T) {
	lookup := newFakeInstanceLookup()
	lookup.add(4, 3)

	now := time.Now()
	clock := &now
	cache := newOwnerCache(lookup, 30*time.Second, func() time.Time { return *clock })

	ctx := context.Background()
	_, err := cache.Get(ctx, 4)
	if err != nil {
		t.Fatalf("first Get() error = %v", err)
	}
	if count := lookup.getCallCount(); count != 1 {
		t.Fatalf("after first call: count = %d, want 1", count)
	}

	// Advance clock past TTL
	expired := now.Add(31 * time.Second)
	clock = &expired

	_, err = cache.Get(ctx, 4)
	if err != nil {
		t.Fatalf("second Get() error = %v", err)
	}
	if count := lookup.getCallCount(); count != 2 {
		t.Fatalf("after expiry: count = %d, want 2", count)
	}
}

// Test 10: Cache concurrency — 100 parallel goroutines, same instanceID/token; -race clean
func TestOwnerCache_Concurrency(t *testing.T) {
	lookup := newFakeInstanceLookup()
	lookup.add(4, 3)

	now := time.Now()
	cache := newOwnerCache(lookup, 30*time.Second, func() time.Time { return now })
	svc := &InstanceProxyService{cache: cache}

	token := &AccessToken{InstanceID: 4, OwnerID: 3}

	var wg sync.WaitGroup
	var errCount atomic.Int32
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ownerID, err := svc.resolveOwnerID(context.Background(), token)
			if err != nil || ownerID != 3 {
				errCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if errCount.Load() != 0 {
		t.Fatalf("concurrent resolveOwnerID had %d failures", errCount.Load())
	}
}

// Test 11: Regression — admin accessing another user's instance uses owner namespace
func TestProxyRouting_AdminAccessingOtherUsersInstance_UsesOwnerNamespace(t *testing.T) {
	t.Setenv("INSTANCE_ACCESS_TOKEN_SECRET", "cluster-shared-secret")

	lookup := newFakeInstanceLookup()
	lookup.add(4, 3) // instance 4 owned by user 3

	now := time.Now()
	cache := newOwnerCache(lookup, 30*time.Second, func() time.Time { return now })

	// Issue token as admin (caller=2) for instance owned by user 3
	accessService := NewInstanceAccessService()
	token, err := accessService.GenerateToken(2, 3, 4, "openclaw", "/api/v1/instances/4/proxy/", 3001, 5*time.Minute)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	// Validate token
	validated, err := accessService.ValidateToken(token.Token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}

	// resolveOwnerID should return 3 (the owner), not 2 (the admin)
	svc := &InstanceProxyService{cache: cache}
	ownerID, err := svc.resolveOwnerID(context.Background(), validated)
	if err != nil {
		t.Fatalf("resolveOwnerID() error = %v", err)
	}
	if ownerID != 3 {
		t.Fatalf("ownerID = %d, want 3 (instance owner, not admin caller 2)", ownerID)
	}
}

// --- error lookup fake that returns an error ---

type errorInstanceLookup struct{}

func (f *errorInstanceLookup) GetByID(id int) (*models.Instance, error) {
	return nil, fmt.Errorf("database connection refused")
}

// Test: resolveOwnerID wraps DB errors as ErrInstanceNotFound
func TestResolveOwnerID_DBError(t *testing.T) {
	cache := newOwnerCache(&errorInstanceLookup{}, 30*time.Second, time.Now)
	svc := &InstanceProxyService{cache: cache}

	token := &AccessToken{InstanceID: 4, OwnerID: 3}
	_, err := svc.resolveOwnerID(context.Background(), token)
	if err == nil {
		t.Fatal("resolveOwnerID() expected error, got nil")
	}
	// The error should wrap ErrInstanceNotFound
	if !isOrContains(err, ErrInstanceNotFound) {
		t.Fatalf("resolveOwnerID() error = %v, want wrapped ErrInstanceNotFound", err)
	}
}

func isOrContains(err, target error) bool {
	for err != nil {
		if err == target || err.Error() == target.Error() {
			return true
		}
		err = unwrapOnce(err)
	}
	return false
}

func unwrapOnce(err error) error {
	if u, ok := err.(interface{ Unwrap() error }); ok {
		return u.Unwrap()
	}
	return nil
}
