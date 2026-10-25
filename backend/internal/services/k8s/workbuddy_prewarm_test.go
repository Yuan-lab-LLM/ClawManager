package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestWorkbuddyPrewarmReconcileCreatesPoolAndHolders(t *testing.T) {
	ctx := context.Background()
	namespace := "clawreef-user-7"
	storageClass := "longhorn"
	clientset := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "workbuddy-golden-v1", Namespace: namespace},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				StorageClassName: &storageClass,
				Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("80Gi"),
				}},
			},
			Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
		},
	)
	controller := &WorkbuddyPrewarmController{
		client:    &Client{Clientset: clientset, Namespace: "clawreef"},
		poolSize:  2,
		goldenPVC: "workbuddy-golden-v1",
		image:     "registry/windows-workbuddy:v1",
	}

	if err := controller.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	pvcs, err := clientset.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{LabelSelector: workbuddyPrewarmLabel + "=true"})
	if err != nil || len(pvcs.Items) != 2 {
		t.Fatalf("prewarm PVCs = %d, err=%v, want 2", len(pvcs.Items), err)
	}
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: workbuddyPrewarmLabel + "=true"})
	if err != nil || len(pods.Items) != 2 {
		t.Fatalf("prewarm holders = %d, err=%v, want 2", len(pods.Items), err)
	}
	if got := pods.Items[0].Spec.NodeSelector["clawmanager.io/windows-runtime"]; got != "true" {
		t.Fatalf("holder node selector = %q, want true", got)
	}
}

func TestClaimWorkbuddyPrewarmPVCDetachesHolder(t *testing.T) {
	ctx := context.Background()
	namespace := "clawreef-user-7"
	storageClass := "longhorn"
	pvcName := "workbuddy-prewarm-ready1"
	holderName := workbuddyPrewarmHolderName(pvcName)
	clientset := fake.NewSimpleClientset(
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: namespace,
				Labels: map[string]string{
					workbuddyPrewarmLabel:       "true",
					workbuddyPrewarmStateLabel:  workbuddyPrewarmStateReady,
					workbuddyPrewarmGoldenLabel: "workbuddy-golden-v1",
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &storageClass,
				Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("80Gi"),
				}},
			},
			Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: holderName, Namespace: namespace},
			Status: corev1.PodStatus{
				Phase:      corev1.PodRunning,
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			},
		},
	)
	service := &PVCService{client: &Client{Clientset: clientset, Namespace: "clawreef"}}

	claimed, err := service.ClaimWorkbuddyPrewarmPVC(ctx, 7, 123, 80, storageClass, "workbuddy-golden-v1")
	if err != nil {
		t.Fatalf("ClaimWorkbuddyPrewarmPVC returned error: %v", err)
	}
	if claimed == nil || claimed.Name != pvcName {
		t.Fatalf("claimed PVC = %#v, want %s", claimed, pvcName)
	}
	if claimed.Labels[workbuddyPrewarmStateLabel] != workbuddyPrewarmStateClaim || claimed.Labels["instance-id"] != "123" {
		t.Fatalf("unexpected claimed labels: %#v", claimed.Labels)
	}
	if _, err := clientset.CoreV1().Pods(namespace).Get(ctx, holderName, metav1.GetOptions{}); err == nil {
		t.Fatal("expected prewarm holder to be deleted")
	}
}
