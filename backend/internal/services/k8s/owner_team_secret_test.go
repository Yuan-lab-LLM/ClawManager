package k8s

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestOwnerTeamSecretReplayPreservesTokenAndRejectsForeignObject(t *testing.T) {
	client := &Client{Clientset: fake.NewSimpleClientset(), Namespace: "owner-fixture"}
	_, secrets, _ := NewTeamProvisioningServices(client)
	first, err := secrets.CreateOwnerTeamSecret(context.Background(), 1, 17, "team-17", map[string]string{"token": "original", "redis": "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := secrets.CreateOwnerTeamSecret(context.Background(), 1, 17, "team-17", map[string]string{"token": "replacement", "redis": "changed"})
	if err != nil || first["token"] != second["token"] || second["redis"] != "fixture" {
		t.Fatalf("secret rotated on replay: %v", err)
	}
	obj, err := client.Clientset.CoreV1().Secrets(client.GetNamespace(1)).Get(context.Background(), "team-17", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	obj.Labels["team-id"] = "999"
	if _, err := client.Clientset.CoreV1().Secrets(obj.Namespace).Update(context.Background(), obj, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := secrets.CreateOwnerTeamSecret(context.Background(), 1, 17, "team-17", map[string]string{"token": "bad"}); err == nil {
		t.Fatal("foreign secret adopted")
	}
}
