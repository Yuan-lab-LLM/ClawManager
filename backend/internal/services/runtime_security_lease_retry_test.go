package services

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"clawreef/internal/runtimeauth"
	"clawreef/internal/runtimeinventory"
	corev1 "k8s.io/api/core/v1"
)

func unavailableInventoryError() error {
	_, _, err := (&runtimeinventory.Inventory{}).Assignment(context.Background(), 7)
	return err
}

func TestRuntimeLeaseRetriesWholeGroupOnlyForInventoryUnavailable(t *testing.T) {
	for _, scenario := range []string{"recover-initial", "recover-signing", "recover-complete", "persistent", "generation", "missing", "arbitrary-error", "peer", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			a, _, _, cm, pod := securityFixture(t)
			calls := 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			temporary := unavailableInventoryError()
			a.assignments = securityAssignmentFunc(func(context.Context, int) (*corev1.ConfigMap, []corev1.Pod, error) {
				calls++
				failAt := map[string]int{"recover-initial": 1, "recover-signing": 2, "recover-complete": 3}[scenario]
				if calls == failAt || scenario == "persistent" {
					return nil, nil, temporary
				}
				if scenario == "cancel" {
					cancel()
					return nil, nil, temporary
				}
				if scenario == "arbitrary-error" {
					return nil, nil, errors.New("secret raw error")
				}
				if scenario == "missing" {
					return nil, nil, nil
				}
				observed := cm.DeepCopy()
				if scenario == "generation" {
					observed.Data["generation"] = "3"
				}
				return observed, []corev1.Pod{*pod}, nil
			})
			identity := runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod-7"}
			peer := netip.MustParseAddr("10.2.0.3")
			if scenario == "peer" {
				peer = netip.MustParseAddr("10.2.0.99")
			}
			started := time.Now()
			lease, err := a.IssueLease(ctx, identity, peer)
			if len(scenario) >= 8 && scenario[:8] == "recover-" {
				if err != nil || lease.ModelToken == "" || lease.EgressToken == "" || lease.ServicesToken == "" {
					t.Fatalf("temporary observation did not recover full group: calls=%d lease=%+v err=%v", calls, lease.Identity(), err)
				}
				want := map[string]int{"recover-initial": 4, "recover-signing": 5, "recover-complete": 6}[scenario]
				if calls != want {
					t.Fatalf("did not restart whole group: calls=%d want=%d", calls, want)
				}
			} else {
				if err == nil || lease != (RuntimeSecurityLease{}) {
					t.Fatalf("failure returned credentials: calls=%d err=%v", calls, err)
				}
				want := 1
				if scenario == "persistent" {
					want = 8
				}
				if calls != want {
					t.Fatalf("wrong retry count: calls=%d want=%d", calls, want)
				}
				if scenario == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatalf("lost cancellation: %v", err)
				}
			}
			if time.Since(started) > 1600*time.Millisecond {
				t.Fatal("retry exceeded lease budget")
			}
		})
	}
}

func TestRuntimeLeaseRetryHonorsParentDeadline(t *testing.T) {
	a, _, _, _, _ := securityFixture(t)
	calls := 0
	a.assignments = securityAssignmentFunc(func(context.Context, int) (*corev1.ConfigMap, []corev1.Pod, error) {
		calls++
		return nil, nil, unavailableInventoryError()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	started := time.Now()
	lease, err := a.IssueLease(ctx, runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod-7"}, netip.MustParseAddr("10.2.0.3"))
	if !errors.Is(err, context.DeadlineExceeded) || lease != (RuntimeSecurityLease{}) || calls != 1 || time.Since(started) > 100*time.Millisecond {
		t.Fatalf("parent deadline not honored: calls=%d err=%v", calls, err)
	}
}

func TestRuntimeLeaseKnownInstanceRefusalNeverRetriesInventory(t *testing.T) {
	for _, status := range []string{"stopping", "stopped", "deleting", "error"} {
		t.Run(status, func(t *testing.T) {
			a, instances, _, _, _ := securityFixture(t)
			instances.instance.Status = status
			calls := 0
			a.assignments = securityAssignmentFunc(func(context.Context, int) (*corev1.ConfigMap, []corev1.Pod, error) {
				calls++
				return nil, nil, unavailableInventoryError()
			})
			lease, err := a.IssueLease(context.Background(), runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod-7"}, netip.MustParseAddr("10.2.0.3"))
			if err == nil || calls != 0 || lease != (RuntimeSecurityLease{}) {
				t.Fatalf("known SQL refusal reached unavailable inventory: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestRuntimeLeaseNeverRetriesMidIssuanceIdentityConflict(t *testing.T) {
	for _, failAt := range []int{2, 3} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			a, _, _, cm, pod := securityFixture(t)
			reads := 0
			a.assignments = securityAssignmentFunc(func(context.Context, int) (*corev1.ConfigMap, []corev1.Pod, error) {
				reads++
				observed := cm.DeepCopy()
				if reads == failAt {
					observed.Data["generation"] = "3"
				}
				return observed, []corev1.Pod{*pod}, nil
			})
			lease, err := a.IssueLease(context.Background(), runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod-7"}, netip.MustParseAddr("10.2.0.3"))
			if err == nil || lease != (RuntimeSecurityLease{}) || reads != failAt {
				t.Fatalf("identity conflict retried or leaked partial credentials: reads=%d err=%v", reads, err)
			}
		})
	}
}

func TestRuntimeLeaseChecksPeerAgainAfterIssuingWholeGroup(t *testing.T) {
	a, _, _, cm, pod := securityFixture(t)
	reads := 0
	a.assignments = securityAssignmentFunc(func(context.Context, int) (*corev1.ConfigMap, []corev1.Pod, error) {
		reads++
		observed, observedPod := cm.DeepCopy(), pod.DeepCopy()
		if reads > 1 {
			observed.Data["endpoint"] = "https://10.2.0.99:19090"
			observedPod.Status.PodIP = "10.2.0.99"
		}
		return observed, []corev1.Pod{*observedPod}, nil
	})
	lease, err := a.IssueLease(context.Background(), runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod-7"}, netip.MustParseAddr("10.2.0.3"))
	if err == nil || reads != 3 || lease != (RuntimeSecurityLease{}) {
		t.Fatalf("peer change escaped completion validation: reads=%d err=%v", reads, err)
	}
}

func TestRuntimeLeaseRejectsKnownSQLIdentityBeforeUnavailableInventory(t *testing.T) {
	for _, scenario := range []string{"generation", "user", "changed-after-temporary"} {
		t.Run(scenario, func(t *testing.T) {
			a, instances, _, _, _ := securityFixture(t)
			expected := runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod-7"}
			if scenario == "generation" {
				instances.instance.RuntimeGeneration = 3
			}
			if scenario == "user" {
				instances.instance.UserID = 4
			}
			reads := 0
			a.assignments = securityAssignmentFunc(func(context.Context, int) (*corev1.ConfigMap, []corev1.Pod, error) {
				reads++
				if scenario == "changed-after-temporary" {
					instances.instance.RuntimeGeneration = 3
				}
				return nil, nil, unavailableInventoryError()
			})
			lease, err := a.IssueLease(context.Background(), expected, netip.MustParseAddr("10.2.0.3"))
			want := 0
			if scenario == "changed-after-temporary" {
				want = 1
			}
			if err == nil || lease != (RuntimeSecurityLease{}) || reads != want {
				t.Fatalf("known SQL identity mismatch retried: reads=%d want=%d err=%v", reads, want, err)
			}
		})
	}
}
