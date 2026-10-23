package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeGatewayRedisCommander struct {
	reply interface{}
	err   error
	args  []string
	calls int
}

func (f *fakeGatewayRedisCommander) do(_ context.Context, args ...string) (interface{}, error) {
	f.calls++
	f.args = append([]string(nil), args...)
	return f.reply, f.err
}

func TestGatewayRateLimitAcquireChecksAllLimitsAtomically(t *testing.T) {
	bus := &fakeGatewayRedisCommander{reply: []interface{}{int64(1), "12345"}}
	store := &redisGatewayRateLimitStore{bus: bus}
	lease, err := store.Acquire(t.Context(), GatewayRateLimitInput{
		KeyID: "key-1", RPMLimit: 10, TPMLimit: 1000, ConcurrentRequestsLimit: 2,
		EstimatedTokens: 120, RequestID: "request-1",
	})
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if lease.tokenKey == "" || lease.concurrentSet == "" || bus.calls != 1 {
		t.Fatalf("limits were not acquired in one operation: lease=%#v calls=%d", lease, bus.calls)
	}
	if len(bus.args) != 12 || bus.args[0] != "EVAL" || bus.args[2] != "2" || !strings.Contains(bus.args[1], "redis.call('TIME')") {
		t.Fatalf("unexpected atomic acquire command: %#v", bus.args)
	}
	if lease.tokenWindow != "12345" {
		t.Fatalf("lease did not retain the Redis minute window: %#v", lease)
	}
}

func TestGatewayRateLimitAcquireMapsAtomicRejections(t *testing.T) {
	tests := []struct {
		code int64
		want error
	}{{-1, ErrGatewayKeyRPMLimitExceeded}, {-2, ErrGatewayKeyTPMLimitExceeded}, {-3, ErrGatewayKeyConcurrencyExceeded}}
	for _, tc := range tests {
		bus := &fakeGatewayRedisCommander{reply: []interface{}{tc.code, "12345"}}
		store := &redisGatewayRateLimitStore{bus: bus}
		_, err := store.Acquire(t.Context(), GatewayRateLimitInput{KeyID: "key-1", RPMLimit: 1, TPMLimit: 1, ConcurrentRequestsLimit: 1, EstimatedTokens: 1, RequestID: "request-1"})
		if !errors.Is(err, tc.want) {
			t.Fatalf("code %d mapped to %v, want %v", tc.code, err, tc.want)
		}
	}
}

func TestRenewConcurrencyLeaseExtendsExistingSortedSetMember(t *testing.T) {
	bus := &fakeGatewayRedisCommander{reply: int64(1)}
	store := &redisGatewayRateLimitStore{bus: bus}
	lease := &GatewayRateLimitLease{concurrentSet: "gateway:concurrent", concurrentKey: "request-1"}

	renewed, err := store.renewConcurrencyLease(context.Background(), lease, time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatalf("renew concurrency lease: %v", err)
	}
	if !renewed {
		t.Fatal("existing concurrency lease should be renewed")
	}
	if len(bus.args) < 7 || bus.args[0] != "EVAL" || bus.args[3] != lease.concurrentSet || bus.args[4] != lease.concurrentKey {
		t.Fatalf("unexpected Redis renewal command: %#v", bus.args)
	}
}

func TestRenewConcurrencyLeaseStopsWhenMemberNoLongerExists(t *testing.T) {
	bus := &fakeGatewayRedisCommander{reply: int64(0)}
	store := &redisGatewayRateLimitStore{bus: bus}
	lease := &GatewayRateLimitLease{concurrentSet: "gateway:concurrent", concurrentKey: "request-1"}

	renewed, err := store.renewConcurrencyLease(context.Background(), lease, time.Now().UTC())
	if err != nil {
		t.Fatalf("renew concurrency lease: %v", err)
	}
	if renewed {
		t.Fatal("missing concurrency lease must not be recreated by heartbeat")
	}
}

func TestRenewConcurrencyLeasePropagatesRedisFailure(t *testing.T) {
	wantErr := errors.New("redis unavailable")
	bus := &fakeGatewayRedisCommander{err: wantErr}
	store := &redisGatewayRateLimitStore{bus: bus}
	lease := &GatewayRateLimitLease{concurrentSet: "gateway:concurrent", concurrentKey: "request-1"}

	if _, err := store.renewConcurrencyLease(context.Background(), lease, time.Now().UTC()); !errors.Is(err, wantErr) {
		t.Fatalf("renew error = %v, want %v", err, wantErr)
	}
}
