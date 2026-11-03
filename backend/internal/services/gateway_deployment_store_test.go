package services

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNoopDeploymentStoreCannotBypassConfiguredCapacity(t *testing.T) {
	store := NewNoopGatewayDeploymentStore()
	lease, err := store.Acquire(t.Context(), GatewayDeploymentAcquireInput{DeploymentID: 7, MaxInflight: 2})
	if lease != nil || !errors.Is(err, ErrGatewayDeploymentUnavailable) {
		t.Fatalf("configured capacity bypassed without Redis: lease=%+v err=%v", lease, err)
	}
	lease, err = store.Acquire(t.Context(), GatewayDeploymentAcquireInput{DeploymentID: 7})
	if err != nil || lease == nil || lease.DeploymentID != 7 {
		t.Fatalf("unlimited deployment should remain available: lease=%+v err=%v", lease, err)
	}
}

func TestDeploymentSnapshotsDecodeSharedMetrics(t *testing.T) {
	bus := &fakeGatewayRedisCommander{reply: []interface{}{
		"healthy", "125.5", "850", "42.25", int64(9), "1", "0", "1000", "900", int64(3), "1200",
	}}
	store := &redisGatewayDeploymentStore{bus: bus}

	items, err := store.Snapshots(t.Context(), []int{7})
	if err != nil {
		t.Fatalf("Snapshots returned error: %v", err)
	}
	got := items[7]
	if got.State != "healthy" || got.EWMATTFTMs != 125.5 || got.EWMATokensPerSecond != 42.25 || got.Inflight != 3 || got.Samples != 9 || got.LastSuccessMs != 1000 || got.SnapshotAtMs != 1200 {
		t.Fatalf("unexpected snapshot: %#v", got)
	}
	if len(bus.args) != 5 || bus.args[0] != "EVAL" || bus.args[2] != "2" {
		t.Fatalf("snapshot must atomically prune leases and read metrics: %#v", bus.args)
	}
	if !strings.Contains(bus.args[1], "redis.call('TIME')") {
		t.Fatalf("snapshot must use Redis server time: %#v", bus.args)
	}
}

func TestDeploymentAcquireEnforcesLuaCapacityDecision(t *testing.T) {
	bus := &fakeGatewayRedisCommander{reply: int64(-1)}
	store := &redisGatewayDeploymentStore{bus: bus}

	lease, err := store.Acquire(t.Context(), GatewayDeploymentAcquireInput{DeploymentID: 4, RequestID: "request-1", MaxInflight: 2})
	if !errors.Is(err, ErrGatewayDeploymentUnavailable) || lease != nil {
		t.Fatalf("Acquire = %#v, %v; want unavailable", lease, err)
	}
	if len(bus.args) != 9 || bus.args[0] != "EVAL" || bus.args[2] != "2" || bus.args[6] != "2" {
		t.Fatalf("unexpected atomic acquire command: %#v", bus.args)
	}
	if !strings.Contains(bus.args[1], "redis.call('TIME')") {
		t.Fatalf("acquire must use Redis server time: %#v", bus.args)
	}
}

func TestDeploymentObservationUsesAtomicEWMAAndCircuitUpdate(t *testing.T) {
	bus := &fakeGatewayRedisCommander{reply: int64(1)}
	store := &redisGatewayDeploymentStore{bus: bus}
	err := store.Observe(context.Background(), GatewayDeploymentObservation{
		DeploymentID: 3, Success: true, TTFTMs: 200, LatencyMs: 1200, TokensPerSecond: 55.5,
	})
	if err != nil {
		t.Fatalf("Observe returned error: %v", err)
	}
	if len(bus.args) != 12 || bus.args[0] != "EVAL" || bus.args[2] != "1" || bus.args[6] != "200" || bus.args[8] != "55.5000" || bus.args[11] != "0" {
		t.Fatalf("unexpected observation command: %#v", bus.args)
	}
	if !strings.Contains(bus.args[1], "redis.call('TIME')") {
		t.Fatalf("observation must use Redis server time: %#v", bus.args)
	}
	if !strings.Contains(bus.args[1], "if ttft > 0 or latency > 0 or tps > 0") {
		t.Fatalf("health-only successes must not increment performance samples: %#v", bus.args)
	}
}

func TestDeploymentPermissionFailureEntersImmediateCooldown(t *testing.T) {
	for _, tc := range []struct {
		success, penalize, immediate bool
		want                         string
	}{
		{false, true, true, "1"}, {false, true, false, "0"}, {true, true, true, "0"}, {false, false, true, "0"},
	} {
		bus := &fakeGatewayRedisCommander{reply: int64(1)}
		store := &redisGatewayDeploymentStore{bus: bus}
		if err := store.Observe(t.Context(), GatewayDeploymentObservation{DeploymentID: 7, Success: tc.success, PenalizeFailure: tc.penalize, ImmediateCooldown: tc.immediate}); err != nil {
			t.Fatal(err)
		}
		if len(bus.args) != 12 || bus.args[11] != tc.want || bus.args[9] != "30000" {
			t.Fatalf("cooldown arguments: %+v", bus.args)
		}
		if !strings.Contains(bus.args[1], "failures >= 3 or ARGV[8] == '1'") {
			t.Fatal("permission denial must not wait for three failures")
		}
		if !strings.Contains(bus.args[1], "if cooldownUntil <= now then") || !strings.Contains(bus.args[1], "elseif cooldownUntil <= now then") {
			t.Fatal("in-flight successes/neutral failures must not clear active cooldown")
		}
	}
}
