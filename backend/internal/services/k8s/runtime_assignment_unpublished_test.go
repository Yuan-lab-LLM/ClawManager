package k8s

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestFirstPublicationRequiresAuthoritativeAbsence(t *testing.T) {
	for _, scenario := range []string{"absent", "orphan-secret", "prior-generation", "read-error", "list-error", "later-generation"} {
		t.Run(scenario, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			s := NewRuntimeAssignmentService(client, "tenant")
			a := assignmentFixture()
			a.Generation = 1
			a.GatewayID = "gw-7-1"
			switch scenario {
			case "orphan-secret", "prior-generation":
				generation := "1"
				if scenario == "prior-generation" {
					generation = "0"
				}
				_, err := client.CoreV1().Secrets("tenant").Create(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "lite-instance-7-g" + generation, Labels: map[string]string{"clawmanager.io/instance-id": "7"}}}, metav1.CreateOptions{})
				if err != nil {
					t.Fatal(err)
				}
			case "read-error":
				client.PrependReactor("get", "configmaps", func(clienttesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("unavailable") })
			case "list-error":
				client.PrependReactor("list", "secrets", func(clienttesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("unavailable") })
			case "later-generation":
				a.Generation = 2
				a.GatewayID = "gw-7-2"
			}
			err := s.CanRetryUnpublished(context.Background(), a)
			if scenario == "absent" {
				if err != nil {
					t.Fatalf("unpublished first assignment blocked: %v", err)
				}
			} else if err == nil {
				t.Fatal("uncertain publication history allowed")
			}
		})
	}
}
