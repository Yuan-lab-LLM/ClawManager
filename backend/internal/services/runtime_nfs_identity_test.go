package services

import (
	"clawreef/internal/models"
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestNFSInstancePrimaryGroupDoesNotBecomeTeamGroup(t *testing.T) {
	endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "runtime", UID: "pod-uid", Labels: map[string]string{"clawmanager.io/runtime-protocol": "container-slots-v1", "clawmanager.io/sandbox-profile": "nfs-rootless-v1"}}, Status: corev1.PodStatus{PodIP: "10.0.0.2"}}
	s := &RuntimeScheduler{agentClient: &slotRuntimeAgentClient{kube: fake.NewSimpleClientset(p), namespace: "runtime"}}
	env := map[string]string{"CLAWMANAGER_TEAM_ENABLED": "true", "CLAWMANAGER_TEAM_SHARED_GID": "300007"}
	u, g, profile, err := s.gatewayLinuxIDs(context.Background(), 7, env, models.RuntimePod{PodUID: &uid, AgentEndpoint: &endpoint})
	if err != nil || u != 200007 || g != 200007 || profile != "nfs-rootless-v1" {
		t.Fatalf("uid=%d gid=%d profile=%s err=%v", u, g, profile, err)
	}
	if env["CLAWMANAGER_TEAM_SHARED_GID"] != "300007" {
		t.Fatal("lost authorized supplemental Team group")
	}
	uid = "replaced-pod"
	if _, _, _, err = s.gatewayLinuxIDs(context.Background(), 7, env, models.RuntimePod{PodUID: &uid, AgentEndpoint: &endpoint}); err == nil {
		t.Fatal("trusted a reused Pod IP")
	}
}
