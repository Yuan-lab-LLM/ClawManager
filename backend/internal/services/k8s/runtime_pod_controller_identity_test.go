package k8s

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"testing"
)

func TestRuntimePodControllerIdentityRequiresCurrentOwnerUIDChain(t *testing.T) {
	for _, scenario := range []string{"valid", "wrong-pod-uid", "wrong-namespace", "wrong-rs-uid", "wrong-deployment-uid"} {
		t.Run(scenario, func(t *testing.T) {
			svc, kube, deployment, pod := maintenanceSourceFixture()
			ctx := context.Background()
			namespace, uid := "ns", string(pod.UID)
			switch scenario {
			case "wrong-pod-uid":
				uid = "stale"
			case "wrong-namespace":
				namespace = "other"
			case "wrong-rs-uid":
				rs, err := kube.AppsV1().ReplicaSets("ns").Get(ctx, "rs", metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				rs.UID = types.UID("replacement")
				if _, err = kube.AppsV1().ReplicaSets("ns").Update(ctx, rs, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			case "wrong-deployment-uid":
				deployment.UID = types.UID("replacement")
				if _, err := kube.AppsV1().Deployments("ns").Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			actual, err := svc.ResolveRuntimePodController(ctx, namespace, pod.Name, uid)
			if scenario == "valid" {
				if err != nil || actual.DeploymentName != deployment.Name || actual.PodName != pod.Name || actual.RuntimeType != "openclaw" {
					t.Fatalf("bad verified identity %+v %v", actual, err)
				}
				byUID, err := svc.ResolveRuntimePodController(ctx, namespace, "", uid)
				if err != nil || byUID != actual {
					t.Fatalf("UID resolution differs: %+v %v", byUID, err)
				}
			} else if err == nil {
				t.Fatalf("invalid owner chain accepted: %+v", actual)
			}
		})
	}
}
