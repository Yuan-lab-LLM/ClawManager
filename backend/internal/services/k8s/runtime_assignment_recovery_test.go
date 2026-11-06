package k8s

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestStoppedSourceRecoveryRequiresExactUnpublishedAssignmentCAS(t *testing.T) {
	for _, change := range []string{"none", "version", "intent", "successor-secret", "identity", "team-source"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			client := fake.NewSimpleClientset()
			s := NewRuntimeAssignmentService(client, "tenant")
			a := assignmentFixture()
			a.PodUID = "source-uid"
			if err := s.EnsureDesired(ctx, a); err != nil {
				t.Fatal(err)
			}
			cm := assignmentCM(t, client)
			cm.ResourceVersion = "17"
			if _, err := client.CoreV1().ConfigMaps("tenant").Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			snapshot, err := s.StoppedRecoverySource(ctx, 7, 4)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "version":
				cm.ResourceVersion = "18"
			case "intent":
				cm.Data["container_stop_intent"] = "unresolved"
			case "identity":
				cm.Data["pod_uid"] = "other-source"
			case "successor-secret":
				_, err = client.CoreV1().Secrets("tenant").Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: runtimeAssignmentSecretName(7, 4)}}, metav1.CreateOptions{})
				if err != nil {
					t.Fatal(err)
				}
			case "team-source":
				secret, _ := client.CoreV1().Secrets("tenant").Get(ctx, runtimeAssignmentSecretName(7, 3), metav1.GetOptions{})
				secret.Data["CLAWMANAGER_TEAM_SHARED_DIR"] = []byte("/team")
				_, _ = client.CoreV1().Secrets("tenant").Update(ctx, secret, metav1.UpdateOptions{})
			}
			if _, err = client.CoreV1().ConfigMaps("tenant").Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			err = s.ConfirmRecoveredStopped(ctx, snapshot, 1, "observer-uid", strings.Repeat("a", 64), strings.Repeat("b", 64), "storage-proof")
			if change != "none" {
				if err == nil {
					t.Fatal("recovery accepted changed authority")
				}
				if assignmentCM(t, client).Data["desired_state"] == "stopped" {
					t.Fatal("failed recovery retired source")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			cm = assignmentCM(t, client)
			if cm.Data["desired_state"] != "stopped" || cm.Data["generation"] != "3" || cm.Data["secret_ref"] != runtimeAssignmentSecretName(7, 3) || cm.Labels["clawmanager.io/needs-reconcile"] != "false" {
				t.Fatal("recovery changed generation or credentials")
			}
			var audit map[string]any
			if json.Unmarshal([]byte(cm.Annotations[StoppedSourceRecoveryAnnotation]), &audit) != nil || audit["source_pod_uid"] != "source-uid" || audit["observer_pod_uid"] != "observer-uid" {
				t.Fatal("recovery audit was not committed with stop")
			}
			next := a
			next.Generation, next.GatewayID, next.PodUID = 4, "gw-7-4", "observer-uid"
			if err = s.EnsureDesired(ctx, next); err != nil {
				t.Fatal(err)
			}
			if assignmentCM(t, client).Annotations[StoppedSourceRecoveryAnnotation] != cm.Annotations[StoppedSourceRecoveryAnnotation] {
				t.Fatal("normal successor publication lost recovery audit")
			}
		})
	}
}
