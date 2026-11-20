package services

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"clawreef/internal/runtimeauth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
)

func TestRuntimeSecurityLeaseDependencyIsRetryableButRevocationIsNot(t *testing.T) {
	for _, dependency := range []string{"sql", "kube", "generation", "missing"} {
		t.Run(dependency, func(t *testing.T) {
			a, instances, kube, cm, _ := securityFixture(t)
			id := runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod-7"}
			switch dependency {
			case "sql":
				instances.err = errors.New("secret connection failed")
			case "kube":
				kube.PrependReactor("get", "configmaps", func(ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("secret kube failed")
				})
			case "generation":
				instances.instance.RuntimeGeneration++
			case "missing":
				if err := kube.CoreV1().ConfigMaps("test").Delete(context.Background(), cm.Name, metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			lease, err := a.IssueLease(context.Background(), id, netip.MustParseAddr("10.2.0.3"))
			if err == nil || lease.ModelToken != "" || runtimeauth.IsUnavailable(err) != (dependency == "sql" || dependency == "kube") {
				t.Fatalf("classification=%v err=%v", runtimeauth.IsUnavailable(err), err)
			}
		})
	}
}

func TestRuntimeLeaseBatchRechecksRevocationWithoutThreeDuplicateReads(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		a, _, kube, cm, _ := securityFixture(t)
		reads := 0
		kube.PrependReactor("get", "configmaps", func(ktesting.Action) (bool, runtime.Object, error) {
			reads++
			observed := cm.DeepCopy()
			if revoke && reads == 2 {
				observed.Data["desired_state"] = "stopped"
			}
			return true, observed, nil
		})
		id := runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod-7"}
		lease, err := a.IssueLease(context.Background(), id, netip.MustParseAddr("10.2.0.3"))
		wantReads := 3 // Peer snapshot, batch signing check and completion fence.
		if revoke {
			wantReads = 2
		}
		if reads != wantReads {
			t.Fatalf("assignment reads=%d, want %d", reads, wantReads)
		}
		if revoke {
			if err == nil || lease.ModelToken != "" {
				t.Fatal("revocation signed a lease")
			}
		} else if err != nil || lease.ServicesToken == "" {
			t.Fatal("valid batch rejected", err)
		}
	}
}
