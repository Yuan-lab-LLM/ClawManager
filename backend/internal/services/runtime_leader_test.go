package services

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestRuntimeLeaderConcurrentLoopsDoNotLoseTheirOwnLease(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	s := NewRuntimeLeaderService(client, "runtime-system", "backend-a")
	if !s.IsLeader(ctx) {
		t.Fatal("initial lease acquisition failed")
	}
	resource := schema.GroupVersionResource{Group: "coordination.k8s.io", Version: "v1", Resource: "leases"}
	// Fake Kubernetes does not normally enforce resourceVersion; reproduce the
	// API's optimistic concurrency contract instead of silently accepting races.
	client.PrependReactor("update", "leases", func(action ktesting.Action) (bool, runtime.Object, error) {
		next := action.(ktesting.UpdateAction).GetObject().(*coordinationv1.Lease)
		obj, err := client.Tracker().Get(resource, next.Namespace, next.Name)
		if err != nil {
			return true, nil, err
		}
		current := obj.(*coordinationv1.Lease)
		if next.ResourceVersion != current.ResourceVersion {
			return true, nil, apierrors.NewConflict(resource.GroupResource(), next.Name, fmt.Errorf("stale lease version"))
		}
		n, _ := strconv.Atoi(current.ResourceVersion)
		next.ResourceVersion = strconv.Itoa(n + 1)
		err = client.Tracker().Update(resource, next, next.Namespace)
		return true, next.DeepCopy(), err
	})
	var failed atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for n := 0; n < 20; n++ {
				if !s.IsLeader(ctx) {
					failed.Add(1)
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	if failed.Load() != 0 {
		t.Fatalf("%d renewals misidentified this process's own lease as lost", failed.Load())
	}
}

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
