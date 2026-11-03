package k8s

import (
	"clawreef/internal/config"
	"context"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestSlotBootstrapDisabledDoesNotRequireCluster(t *testing.T) {
	if err := EnsureRuntimeSlotPools(context.Background(), nil, config.RuntimePoolConfig{}); err != nil {
		t.Fatal(err)
	}
}
func TestSlotBootstrapPreservesScaledServingPools(t *testing.T) {
	cfg := config.RuntimePoolConfig{Namespace: "tenant", ContainerSlots: 2, WorkspaceRoot: "/workspaces", WorkspacePVCClaimName: "workspace", AgentControlToken: "control", AgentReportToken: "report", OpenClawImage: "oc", HermesImage: "hermes", OpenCodeImage: "code", DeepSeekHarnessImage: "dsh"}
	specs, err := RuntimeSlotDeploymentSpecs(cfg, "clawmanager-lite-agent")
	if err != nil {
		t.Fatal(err)
	}
	sc := "longhorn"
	objects := []runtime.Object{&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "one"}}, &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: sc}, Provisioner: "driver.longhorn.io"}, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "workspace", Namespace: "tenant"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "volume", StorageClassName: &sc, AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}, &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "volume"}, Spec: corev1.PersistentVolumeSpec{StorageClassName: sc, ClaimRef: &corev1.ObjectReference{Namespace: "tenant", Name: "workspace"}, PersistentVolumeSource: corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{Driver: "driver.longhorn.io"}}}, Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound}}}
	for _, spec := range specs {
		spec.Replicas = 3
		objects = append(objects, BuildRuntimeDeployment(spec))
	}
	client := fake.NewSimpleClientset(objects...)
	if err := EnsureRuntimeSlotPools(context.Background(), client, cfg); err != nil {
		t.Fatal(err)
	}
	account, err := client.CoreV1().ServiceAccounts("tenant").Get(context.Background(), "clawmanager-lite-runtime", metav1.GetOptions{})
	if err != nil || account.AutomountServiceAccountToken == nil || *account.AutomountServiceAccountToken {
		t.Fatal("bootstrap must provision a dedicated runtime account without mounted API credentials")
	}
	got, _ := client.AppsV1().Deployments("tenant").Get(context.Background(), "openclaw-runtime", metav1.GetOptions{})
	if *got.Spec.Replicas != 3 {
		t.Fatal("backend restart reduced serving pool replicas")
	}
	cfg.AgentControlToken = "rotated"
	if err := EnsureRuntimeSlotPools(context.Background(), client, cfg); err == nil {
		t.Fatal("credentials rotated without a coordinated rollout")
	}
	secret, _ := client.CoreV1().Secrets("tenant").Get(context.Background(), "clawmanager-lite-agent", metav1.GetOptions{})
	if string(secret.Data["control-token"]) != "control" {
		t.Fatal("active pool credentials overwritten")
	}
}
