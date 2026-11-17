package k8s

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestEnsureDesiredPublicationReturnsExactAPIMetadata(t *testing.T) {
	kube := fake.NewSimpleClientset()
	revision := 100
	stamp := func(object runtime.Object) {
		cm := object.(*corev1.ConfigMap)
		revision++
		cm.UID = "assignment-uid"
		cm.ResourceVersion = fmt.Sprint(revision)
	}
	kube.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		stamp(action.(ktesting.CreateAction).GetObject())
		return false, nil, nil
	})
	kube.PrependReactor("update", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		stamp(action.(ktesting.UpdateAction).GetObject())
		return false, nil, nil
	})
	service := NewRuntimeAssignmentService(kube, "tenant")
	assignment := assignmentFixture()
	publication, err := service.EnsureDesiredPublication(context.Background(), assignment)
	if err != nil || publication == nil || publication.UID != "assignment-uid" || publication.ResourceVersion != "101" {
		t.Fatalf("missing exact creation receipt: %v %v", publication, err)
	}
	publication.Data["request"] = "caller-mutated"
	same, err := service.EnsureDesiredPublication(context.Background(), assignment)
	if err != nil || same.ResourceVersion != "101" || same.Data["request"] == "caller-mutated" {
		t.Fatalf("same-generation publication was replaced or mutated: %v %v", same, err)
	}
	if err := service.MarkDraining(context.Background(), assignment.Endpoint, assignment.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := service.ConfirmStopped(context.Background(), assignment.Endpoint, assignment.GatewayID); err != nil {
		t.Fatal(err)
	}
	stopped, err := service.EnsureDesiredPublication(context.Background(), assignment)
	if err != nil || stopped.Data["desired_state"] != "stopped" || stopped.ResourceVersion != "103" {
		t.Fatalf("same generation resurrected stopped assignment: %v %v", stopped, err)
	}
	assignment.Generation++
	assignment.GatewayID = "gw-7-4"
	next, err := service.EnsureDesiredPublication(context.Background(), assignment)
	if err != nil || next.UID != "assignment-uid" || next.ResourceVersion != "104" || next.Data["generation"] != "4" {
		t.Fatalf("missing exact update receipt: %v %v", next, err)
	}
}
