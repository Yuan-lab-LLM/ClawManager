package services

import (
	"context"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRuntimeLeaderServiceFirstCallerBecomesLeader(t *testing.T) {
	service := NewRuntimeLeaderService(fake.NewSimpleClientset(), "runtime-system", "backend-a")

	if !service.IsLeader(context.Background()) {
		t.Fatalf("expected first caller to become leader")
	}
}

func TestRuntimeLeaderServiceDifferentCallerCannotStealUnexpiredLease(t *testing.T) {
	now := metav1.NewMicroTime(time.Now())
	holder := "backend-a"
	client := fake.NewSimpleClientset(&coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: "clawmanager-runtime-scheduler", Namespace: "runtime-system"},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       &holder,
			RenewTime:            &now,
			LeaseDurationSeconds: int32Ptr(15),
		},
	})
	service := NewRuntimeLeaderService(client, "runtime-system", "backend-b")

	if service.IsLeader(context.Background()) {
		t.Fatalf("expected different caller not to steal unexpired lease")
	}
}

func TestRuntimeLeaderServiceDifferentCallerCanAcquireExpiredLease(t *testing.T) {
	renewTime := metav1.NewMicroTime(time.Now().Add(-30 * time.Second))
	holder := "backend-a"
	client := fake.NewSimpleClientset(&coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: "clawmanager-runtime-scheduler", Namespace: "runtime-system"},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       &holder,
			RenewTime:            &renewTime,
			LeaseDurationSeconds: int32Ptr(15),
		},
	})
	service := NewRuntimeLeaderService(client, "runtime-system", "backend-b")

	if !service.IsLeader(context.Background()) {
		t.Fatalf("expected different caller to acquire expired lease")
	}

	lease, err := client.CoordinationV1().Leases("runtime-system").Get(context.Background(), "clawmanager-runtime-scheduler", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to load lease: %v", err)
	}
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity != "backend-b" {
		t.Fatalf("expected lease holder backend-b, got %#v", lease.Spec.HolderIdentity)
	}
}

func int32Ptr(value int32) *int32 {
	return &value
}

func TestRuntimeIdlePoolLeaderObservationDistinguishesOwnershipFromRenewal(t *testing.T) {
	for _, tc := range []struct {
		name, holder string
		age          time.Duration
		held         bool
	}{
		{"same holder", "backend-a", 0, true},
		{"different holder", "backend-b", 0, false},
		{"expired", "backend-a", time.Minute, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			renew := metav1.NewMicroTime(time.Now().Add(-tc.age))
			client := fake.NewSimpleClientset(&coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{Name: runtimeLeaderLeaseName, Namespace: "runtime-system"},
				Spec:       coordinationv1.LeaseSpec{HolderIdentity: &tc.holder, RenewTime: &renew, LeaseDurationSeconds: int32Ptr(15)},
			})
			service := NewRuntimeLeaderService(client, "runtime-system", "backend-a")
			observer, ok := service.(interface{ HoldsLease(context.Context) bool })
			if !ok {
				t.Fatal("lease ownership cannot be distinguished from a renewal CAS conflict")
			}
			if observer.HoldsLease(context.Background()) != tc.held {
				t.Fatal("incorrect current lease ownership")
			}
		})
	}
}
