package k8s

import (
	"strconv"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFreshRuntimePoolPreservesSlotGeometryAndSource(t *testing.T) {
	for _, slots := range []int{2, 8} {
		t.Run(strconv.Itoa(slots), func(t *testing.T) {
			old := "registry/opencode-lite@sha256:" + strings.Repeat("a", 64)
			image := "registry/opencode-lite@sha256:" + strings.Repeat("b", 64)
			d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "pool", Namespace: "tenant", RuntimeType: "opencode", Image: old, SlotCount: slots, Replicas: 2, WorkspacePVCClaimName: "workspace"})
			d.UID = "source-uid"
			d.Generation = 1
			kube := fake.NewSimpleClientset(d)
			svc := &runtimeDeploymentService{client: kube}
			scope := FreshRuntimePoolScope{Source: RuntimePodScaleTarget{Namespace: d.Namespace, DeploymentName: d.Name, DeploymentUID: string(d.UID), RuntimeType: "opencode"}, SourceGeneration: d.Generation, Name: "pool-new-7", Image: image, InstanceID: 7}
			got, err := svc.freshRuntimeTemplates(t.Context(), scope)
			if err != nil {
				t.Fatal(err)
			}
			if *got.Spec.Replicas != 1 || len(got.Spec.Template.Spec.Containers) != slots+1 || got.Labels[runtimeSchedulingLabel] != "false" || got.Labels[upgradeLabPurposeLabel] != "fresh-instance-7" || got.Spec.Selector.MatchLabels[runtimeUpgradeIDLabel] != "" {
				t.Fatalf("incorrect scoped pool spec %+v", got)
			}
			for _, c := range got.Spec.Template.Spec.Containers {
				if c.Image != image {
					t.Fatalf("wrong candidate image for %s: %s", c.Name, c.Image)
				}
			}
			still, err := kube.AppsV1().Deployments(d.Namespace).Get(t.Context(), d.Name, metav1.GetOptions{})
			if err != nil || *still.Spec.Replicas != 2 || runtimeContainerImage(still.Spec.Template.Spec.Containers) != old {
				t.Fatalf("source changed: %v", err)
			}
			scope.Image = "other/opencode-lite@sha256:" + strings.Repeat("b", 64)
			if _, err := svc.freshRuntimeTemplates(t.Context(), scope); err == nil {
				t.Fatal("allowed a different image repository")
			}
		})
	}
}

func TestFreshRuntimePoolRequiresActivationEvenWhenSiteSetsSchedulingLabel(t *testing.T) {
	image := "registry/opencode-lite@sha256:" + strings.Repeat("b", 64)
	d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "pool-new-7", Namespace: "tenant", RuntimeType: "opencode", Image: image, SlotCount: 2})
	d.UID = "fresh-controller"
	d.Generation = 2
	d.Labels[upgradeLabPurposeLabel] = "fresh-instance-7"
	d.Labels[runtimeSchedulingLabel] = "true"
	pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "fresh", UID: "fresh-pod", Namespace: d.Namespace}}
	svc := &runtimeDeploymentService{client: fake.NewSimpleClientset()}
	observed := func() bool {
		items := svc.renderDeploymentPods(t.Context(), d, []corev1.Pod{pod}, make(map[string]*corev1.PersistentVolumeClaim))
		return len(items) == 1 && items[0].SchedulingEnabled != nil && *items[0].SchedulingEnabled
	}
	if observed() || ordinaryRuntimeDeployment(*d) {
		t.Fatal("site scheduling label admitted an unverified or ordinary pool")
	}
	d.Annotations = map[string]string{freshPoolSourceUID: "source-uid", freshPoolActivationUID: string(d.UID)}
	if !observed() || ordinaryRuntimeDeployment(*d) {
		t.Fatal("verified fresh pool became unavailable to its member or an ordinary rollout target")
	}
	d.Generation++
	if observed() {
		t.Fatal("legacy activation approved a subsequent template change")
	}
	d.Annotations[freshPoolActivationImage] = image
	d.Annotations[freshPoolActivationGeneration] = strconv.FormatInt(d.Generation, 10)
	if !observed() {
		t.Fatal("matching immutable admission could not be verified")
	}
	d.Spec.Template.Spec.Containers[0].Image = "registry/opencode-lite@sha256:" + strings.Repeat("c", 64)
	if observed() {
		t.Fatal("admitted digest was reused after the coordinator image changed")
	}
}
