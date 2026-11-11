package k8s

import (
	"clawreef/internal/models"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
	"time"
)

func TestStoppedRecoveryCalicoRequiresUniqueRetainedHostAllocation(t *testing.T) {
	for _, change := range []string{"none", "overlap", "deleted", "deleting", "new-era", "pending", "node", "name", "cidr", "uid", "type"} {
		t.Run(change, func(t *testing.T) {
			ip, node := "10.244.235.238", "k8s-master"
			source := models.RuntimePod{PodIP: &ip, NodeName: &node, CreatedAt: time.Now().Add(-time.Minute)}
			block := recoveryCalicoAffinity{ObjectMeta: metav1.ObjectMeta{Name: "k8s-master-10-244-235-192-26", UID: "affinity", CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour))}}
			block.Spec.CIDR = "10.244.235.192/26"
			block.Spec.Node = node
			block.Spec.State = "confirmed"
			block.Spec.Deleted = "false"
			block.Spec.Type = "host"
			switch change {
			case "deleted":
				block.Spec.Deleted = "true"
			case "deleting":
				now := metav1.Now()
				block.DeletionTimestamp = &now
			case "new-era":
				block.CreationTimestamp = metav1.Now()
			case "pending":
				block.Spec.State = "pending"
			case "node":
				block.Spec.Node = "other"
			case "name":
				block.Name = "other"
			case "cidr":
				block.Spec.CIDR = "10.244.235.200/26"
			case "uid":
				block.UID = ""
			case "type":
				block.Spec.Type = "virtual"
			}
			blocks := []recoveryCalicoAffinity{block}
			if change == "overlap" {
				blocks = append(blocks, block)
			}
			n, identity, err := resolveCalicoRecoveryNode(source, []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: node, UID: "node-uid"}}}, blocks)
			if change == "none" {
				if err != nil || n == nil || identity == "" {
					t.Fatalf("valid allocation rejected: %v", err)
				}
			} else if err == nil {
				t.Fatal("unsafe allocation accepted")
			}
		})
	}
}
