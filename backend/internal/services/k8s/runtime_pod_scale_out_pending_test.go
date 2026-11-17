package k8s

import (
	"context"
	"errors"
	"strings"
	"syscall"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ktesting "k8s.io/client-go/testing"
)

func TestScaleOutPendingRequiresObservedVersionChange(t *testing.T) {
	for _, mode := range []string{"version", "unchanged", "uid", "fence", "field", "forbidden", "conflict", "transport", "transport-uid", "transport-fence"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			svc, kube, d, _ := maintenanceSourceFixture()
			d.ResourceVersion = "1"
			n := int32(1)
			d.Spec.Replicas = &n
			_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
			writes := 0
			kube.PrependReactor("patch", "deployments", func(a ktesting.Action) (bool, runtime.Object, error) {
				writes++
				changed := d.DeepCopy()
				if mode != "unchanged" {
					changed.ResourceVersion = "2"
				}
				if mode == "uid" || mode == "transport-uid" {
					changed.UID = "replacement"
				}
				if mode == "fence" || mode == "transport-fence" {
					changed.Labels[RuntimeScaleInOperationLabel] = "active"
				}
				if err := kube.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), changed, "ns"); err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(mode, "transport") {
					return true, nil, syscall.ECONNRESET
				}
				if mode == "conflict" {
					return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "deployments"}, d.Name, errors.New("version changed"))
				}
				if mode == "forbidden" {
					return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "deployments"}, d.Name, errors.New("denied"))
				}
				if mode == "field" {
					return true, nil, &apierrors.StatusError{ErrStatus: metav1.Status{Code: 422, Reason: metav1.StatusReasonInvalid, Details: &metav1.StatusDetails{Causes: []metav1.StatusCause{{Type: metav1.CauseTypeFieldValueInvalid, Field: "spec.replicas", Message: "invalid value"}}}}}
				}
				return true, nil, &apierrors.StatusError{ErrStatus: metav1.Status{Code: 422, Reason: metav1.StatusReasonInvalid, Message: "the server rejected our request due to an error in our request"}}
			})
			err := svc.ScaleOut(ctx, "ns", d.Name, 3)
			if err == nil {
				t.Fatal("lost write error")
			}
			for _, field := range []string{"runtime_type=", "namespace=ns", "deployment=", "floor=3", "uid=", "resourceVersion=1"} {
				if !strings.Contains(err.Error(), field) {
					t.Fatalf("missing diagnostic %s: %v", field, err)
				}
			}
			got := errors.Is(err, ErrRuntimeScaleOutPending)
			if got != (mode == "version" || mode == "conflict" || mode == "transport") {
				t.Fatalf("pending=%v, err=%v", got, err)
			}
			if writes != 1 {
				t.Fatalf("writes=%d", writes)
			}
		})
	}
}

func TestScaleOutInitialGetTimeoutDoesNotWrite(t *testing.T) {
	svc, kube, d, _ := maintenanceSourceFixture()
	kube.PrependReactor("get", "deployments", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, context.DeadlineExceeded })
	err := svc.ScaleOut(context.Background(), "ns", d.Name, 3)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost initial GET error: %v", err)
	}
	for _, a := range kube.Actions() {
		if a.GetVerb() == "patch" {
			t.Fatal("wrote after failed GET")
		}
	}
}
