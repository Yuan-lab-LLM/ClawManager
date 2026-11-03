package services

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

type securityAssignmentFunc func(context.Context, int) (*corev1.ConfigMap, []corev1.Pod, error)

func (f securityAssignmentFunc) Assignment(ctx context.Context, id int) (*corev1.ConfigMap, []corev1.Pod, error) {
	return f(ctx, id)
}

func TestRuntimeAuthorityUsesSharedAssignmentAndFailsClosed(t *testing.T) {
	a, instances, kube, cm, pod := securityFixture(t)
	fail := false
	source := securityAssignmentFunc(func(ctx context.Context, id int) (*corev1.ConfigMap, []corev1.Pod, error) {
		if fail {
			return nil, nil, errors.New("watch unavailable")
		}
		return cm.DeepCopy(), []corev1.Pod{*pod.DeepCopy()}, nil
	})
	a, err := NewRuntimeSecurityAuthority(kube, "test", instances, a.key, WithRuntimeSecurityAssignments(source))
	if err != nil {
		t.Fatal(err)
	}
	kube.ClearActions()
	if _, err := a.CurrentIdentity(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if len(kube.Actions()) != 0 {
		t.Fatal("authority performed per-capability Kubernetes I/O")
	}
	fail = true
	if _, err := a.CurrentIdentity(context.Background(), 7); err == nil {
		t.Fatal("watch loss used fallback Kubernetes reads")
	}
	if len(kube.Actions()) != 0 {
		t.Fatal("unavailable snapshot triggered direct fallback")
	}
}
