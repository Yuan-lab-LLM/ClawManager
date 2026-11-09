package services

import (
	"context"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	runtimeLeaderLeaseName = "clawmanager-runtime-scheduler"
	runtimeLeaderLeaseTTL  = 15 * time.Second
)

type RuntimeLeaderService interface {
	IsLeader(ctx context.Context) bool
}

type runtimeLeaderService struct {
	client    kubernetes.Interface
	namespace string
	holder    string
}

func NewRuntimeLeaderService(client kubernetes.Interface, namespace, holder string) RuntimeLeaderService {
	return &runtimeLeaderService{
		client:    client,
		namespace: namespace,
		holder:    holder,
	}
}

func (s *runtimeLeaderService) IsLeader(ctx context.Context) bool {
	if s == nil || s.client == nil || s.namespace == "" || s.holder == "" {
		return false
	}

	leases := s.client.CoordinationV1().Leases(s.namespace)
	now := metav1.NewMicroTime(time.Now())
	lease, err := leases.Get(ctx, runtimeLeaderLeaseName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, createErr := leases.Create(ctx, s.newLease(now), metav1.CreateOptions{})
		if createErr == nil {
			return true
		}
		if !errors.IsAlreadyExists(createErr) {
			return false
		}
		lease, err = leases.Get(ctx, runtimeLeaderLeaseName, metav1.GetOptions{})
	}
	if err != nil {
		return false
	}

	if !s.canAcquire(lease, now.Time) {
		return false
	}

	lease.Spec.HolderIdentity = &s.holder
	lease.Spec.RenewTime = &now
	ttlSeconds := int32(runtimeLeaderLeaseTTL / time.Second)
	lease.Spec.LeaseDurationSeconds = &ttlSeconds
	if _, err := leases.Update(ctx, lease, metav1.UpdateOptions{}); err != nil {
		return false
	}
	return true
}

// HoldsLease observes ownership without attempting another renewal. Concurrent
// renewals by this process can conflict without transferring leadership.
func (s *runtimeLeaderService) HoldsLease(ctx context.Context) bool {
	if s == nil || s.client == nil || s.namespace == "" || s.holder == "" {
		return false
	}
	lease, err := s.client.CoordinationV1().Leases(s.namespace).Get(ctx, runtimeLeaderLeaseName, metav1.GetOptions{})
	return err == nil && lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity == s.holder &&
		lease.Spec.RenewTime != nil && lease.Spec.LeaseDurationSeconds != nil && *lease.Spec.LeaseDurationSeconds > 0 &&
		time.Now().Before(lease.Spec.RenewTime.Add(time.Duration(*lease.Spec.LeaseDurationSeconds)*time.Second))
}

func (s *runtimeLeaderService) newLease(now metav1.MicroTime) *coordinationv1.Lease {
	ttlSeconds := int32(runtimeLeaderLeaseTTL / time.Second)
	return &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      runtimeLeaderLeaseName,
			Namespace: s.namespace,
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       &s.holder,
			AcquireTime:          &now,
			RenewTime:            &now,
			LeaseDurationSeconds: &ttlSeconds,
		},
	}
}

func (s *runtimeLeaderService) canAcquire(lease *coordinationv1.Lease, now time.Time) bool {
	if lease == nil {
		return true
	}
	if lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity == s.holder {
		return true
	}
	if lease.Spec.RenewTime == nil {
		return true
	}
	return lease.Spec.RenewTime.Add(runtimeLeaderLeaseTTL).Before(now)
}
