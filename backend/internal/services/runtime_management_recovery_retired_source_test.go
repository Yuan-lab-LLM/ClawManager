package services

import (
	"clawreef/internal/models"
	"context"
	"encoding/json"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRetiredFaultSourceCannotBypassLivePodIdentity(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".clawmanager-slot-locks")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	data, _ := json.Marshal(map[string]any{"instance_id": 7, "user_id": 1, "generation": 2, "runtime_type": "opencode", "pod_uid": "old", "slot": "slot-0", "state": "running"})
	file := filepath.Join(dir, "opencode-1-7.json")
	if e := os.WriteFile(file, data, 0600); e != nil {
		t.Fatal(e)
	}
	before := metav1.NewTime(time.Now().Add(-time.Hour))
	uid, nodeName, ip := "old", "node", "10.244.0.2"
	source := models.RuntimePod{Namespace: "ns", PodName: "gone", PodUID: &uid, NodeName: &nodeName, PodIP: &ip, CreatedAt: time.Now(), ImageRef: "old-image"}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node", UID: "node-uid", CreationTimestamp: before}, Spec: corev1.NodeSpec{PodCIDR: "10.244.0.0/24"}}
	app := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "clawmanager-app", Namespace: "ns"}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "workspaces", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}, Containers: []corev1.Container{{Name: "clawmanager-app", VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: root}}}}}}}}
	kube := fake.NewSimpleClientset(node, app)
	c := &slotRuntimeAgentClient{kube: kube}
	instance := models.Instance{ID: 7, UserID: 1, Type: "opencode", Status: "stopped"}
	binding := models.InstanceRuntimeBinding{Generation: 2, GatewayPort: 20000}
	pod, e := retiredFaultSourcePod(context.Background(), c, source, instance, binding, root)
	if e != nil {
		t.Fatal(e)
	}
	if string(pod.UID) != "old" || pod.Spec.Containers[0].Name != "slot-0" {
		t.Fatal("lost retired source identity")
	}
	instance.Status = "running"
	if _, e = retiredFaultSourcePod(context.Background(), c, source, instance, binding, root); e == nil {
		t.Fatal("retirement evidence authorized restart")
	}
	instance.Status = "stopped"
	live := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "gone", Namespace: "ns", UID: "old"}, Spec: corev1.PodSpec{NodeName: "other-node"}}
	if _, e = kube.CoreV1().Pods("ns").Create(context.Background(), live, metav1.CreateOptions{}); e != nil {
		t.Fatal(e)
	}
	if _, e = retiredFaultSourcePod(context.Background(), c, source, instance, binding, root); e == nil {
		t.Fatal("fallback bypassed an existing source with conflicting live node identity")
	}
	after, e := os.ReadFile(file)
	if e != nil || string(after) != string(data) {
		t.Fatal("read-only source evidence was mutated")
	}
}
