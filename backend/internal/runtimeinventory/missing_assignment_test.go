package runtimeinventory

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"testing"
)

func TestHealthyMissingAssignmentIsDistinctFromTemporaryInventoryFailure(t *testing.T) {
	i := readyInventory(t)
	_, _, missing := i.Assignment(context.Background(), 7)
	i.assignments.unhealthy()
	_, _, unavailable := i.Assignment(context.Background(), 7)
	if missing == nil || unavailable == nil || missing == unavailable {
		t.Fatalf("missing and temporary inventory failure must differ: %v / %v", missing, unavailable)
	}
}

func TestMalformedAssignmentIsNotTemporaryInventoryFailure(t *testing.T) {
	i := readyInventory(t)
	i.assignments.replace([]runtime.Object{&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "lite-instance-7"}, Data: map[string]string{"endpoint": "secret-invalid-endpoint"}}})
	_, _, err := i.Assignment(context.Background(), 7)
	if err != ErrAssignmentInvalid {
		t.Fatalf("malformed assignment classified as temporary: %v", err)
	}
}
