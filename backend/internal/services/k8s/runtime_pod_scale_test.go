package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ktesting "k8s.io/client-go/testing"
)

func TestDefaultRuntimeScaleInheritsDeploymentAndAddsCount(t *testing.T) {
	ctx := context.Background()
	svc, kube, d, p := maintenanceSourceFixture()
	d.ResourceVersion = "7"
	replicas := int32(3)
	d.Spec.Replicas = &replicas
	d.Spec.Template.Annotations["keep"] = "exact"
	if _, err := kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	target := RuntimePodScaleTarget{RuntimeType: "openclaw", Namespace: "ns", DeploymentName: d.Name, PodName: p.Name, PodUID: string(p.UID)}
	snapshot, err := svc.InspectRuntimeScale(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Option.SlotCount != 2 || snapshot.Option.Replicas != 3 || snapshot.Option.DeploymentUID != string(d.UID) {
		t.Fatalf("bad option: %+v", snapshot.Option)
	}
	result, err := svc.ExpandRuntimeScale(ctx, target, snapshot, 4)
	if err != nil {
		t.Fatal(err)
	}
	current, err := kube.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if *current.Spec.Replicas != 7 || result.PreviousReplicas != 3 || result.AddedPods != 4 || result.Replicas != 7 {
		t.Fatalf("wrong counted expansion: %+v deployment=%d", result, *current.Spec.Replicas)
	}
	if !reflect.DeepEqual(current.Spec.Template, d.Spec.Template) {
		t.Fatal("scale changed Deployment template")
	}
}

func TestDefaultRuntimeScaleRejectsCustomAndChangedTemplate(t *testing.T) {
	for _, mode := range []string{"managed", "lab", "standby", "replacement", "template", "image", "wrong-proof"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			svc, kube, d, p := maintenanceSourceFixture()
			d.ResourceVersion = "1"
			replicas := int32(1)
			d.Spec.Replicas = &replicas
			if _, err := kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			target := RuntimePodScaleTarget{RuntimeType: "openclaw", Namespace: "ns", DeploymentName: d.Name, PodName: p.Name, PodUID: string(p.UID)}
			snapshot, err := svc.InspectRuntimeScale(ctx, target)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "managed":
				d.Labels[runtimeManagedPoolLabel] = "true"
			case "lab":
				d.Labels[upgradeLabPurposeLabel] = upgradeLabPurposeValue
			case "standby":
				d.Labels[runtimePoolRoleLabel] = "upgrade-standby"
			case "replacement":
				d.UID = "replacement"
			case "template":
				d.Spec.Template.Annotations = map[string]string{"changed": "yes"}
			case "image":
				d.Spec.Template.Spec.Containers[0].Image = "repo/runtime:mutable"
			case "wrong-proof":
				target.UpgradeID = "5"
				target.SourceDeployment = "old"
			}
			if _, err := kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.ExpandRuntimeScale(ctx, target, snapshot, 1); err == nil {
				t.Fatalf("admitted %s", mode)
			}
			for _, action := range kube.Actions() {
				if action.GetVerb() == "patch" {
					t.Fatal("unsafe scale wrote to Kubernetes")
				}
			}
		})
	}
}

func TestDefaultRuntimeScaleDoesNotRetryUnknownWrite(t *testing.T) {
	ctx := context.Background()
	svc, kube, d, p := maintenanceSourceFixture()
	d.ResourceVersion = "1"
	replicas := int32(1)
	d.Spec.Replicas = &replicas
	_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
	target := RuntimePodScaleTarget{RuntimeType: "openclaw", Namespace: "ns", DeploymentName: d.Name, PodName: p.Name, PodUID: string(p.UID)}
	snapshot, err := svc.InspectRuntimeScale(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	kube.PrependReactor("patch", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		writes++
		return true, nil, errors.New("connection reset after unknown write")
	})
	if _, err := svc.ExpandRuntimeScale(ctx, target, snapshot, 2); err == nil {
		t.Fatal("lost unknown-write failure")
	}
	if writes != 1 {
		t.Fatalf("retried ambiguous write %d times", writes)
	}
}

func TestDefaultRuntimeScalePreservesTemplateDespitePodAdmissionDefaults(t *testing.T) {
	ctx := context.Background()
	svc, kube, d, p := maintenanceSourceFixture()
	d.ResourceVersion = "1"
	replicas := int32(1)
	d.Spec.Replicas = &replicas
	_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
	p = p.DeepCopy()
	p.Spec.NodeName = "node-added-by-scheduler"
	p.Spec.Volumes = append(p.Spec.Volumes, corev1.Volume{Name: "kube-api-access-abc", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{}}})
	for i := range p.Spec.Containers {
		p.Spec.Containers[i].VolumeMounts = append(p.Spec.Containers[i].VolumeMounts, corev1.VolumeMount{Name: "kube-api-access-abc", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true})
	}
	_, _ = kube.CoreV1().Pods("ns").Update(ctx, p, metav1.UpdateOptions{})
	target := RuntimePodScaleTarget{RuntimeType: "openclaw", Namespace: "ns", DeploymentName: d.Name, PodName: p.Name, PodUID: string(p.UID)}
	snapshot, err := svc.InspectRuntimeScale(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExpandRuntimeScale(ctx, target, snapshot, 1); err != nil {
		t.Fatal(err)
	}
	current, _ := kube.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
	if !reflect.DeepEqual(d.Spec.Template, current.Spec.Template) {
		t.Fatal("admitted Pod defaults copied to Deployment")
	}
}

func TestDefaultRuntimeScaleCASFencesConcurrentControllerChanges(t *testing.T) {
	for _, change := range []string{"resourceVersion", "uid"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			svc, kube, d, p := maintenanceSourceFixture()
			d.ResourceVersion = "1"
			replicas := int32(2)
			d.Spec.Replicas = &replicas
			_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
			target := RuntimePodScaleTarget{RuntimeType: "openclaw", Namespace: "ns", DeploymentName: d.Name, PodName: p.Name, PodUID: string(p.UID)}
			expected, err := svc.InspectRuntimeScale(ctx, target)
			if err != nil {
				t.Fatal(err)
			}
			writes := 0
			kube.PrependReactor("patch", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
				writes++
				patch := action.(ktesting.PatchAction).GetPatch()
				var ops []map[string]any
				if err := json.Unmarshal(patch, &ops); err != nil {
					t.Fatal(err)
				}
				if len(ops) != 3 || ops[0]["path"] != "/metadata/uid" || ops[1]["path"] != "/metadata/resourceVersion" || ops[2]["path"] != "/spec/replicas" {
					t.Fatalf("unsafe patch: %s", patch)
				}
				changed := d.DeepCopy()
				if change == "uid" {
					changed.UID = "replacement"
				} else {
					changed.ResourceVersion = "2"
					changed.Labels[runtimeSchedulingLabel] = "false"
				}
				if err := kube.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), changed, "ns"); err != nil {
					t.Fatal(err)
				}
				return false, nil, nil
			})
			if _, err := svc.ExpandRuntimeScale(ctx, target, expected, 3); err == nil {
				t.Fatalf("CAS admitted changed %s", change)
			}
			current, _ := kube.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
			if *current.Spec.Replicas != 2 || writes != 1 {
				t.Fatalf("unsafe CAS result replicas=%d writes=%d", *current.Spec.Replicas, writes)
			}
		})
	}
}

func TestRuntimeScaleAppArmorEquivalence(t *testing.T) {
	ctx := context.Background()
	for _, scenario := range []string{"api-materialized", "typed-only", "different-profile", "different-security", "live-annotation-changed"} {
		t.Run(scenario, func(t *testing.T) {
			svc, kube, d, p := maintenanceSourceFixture()
			p = p.DeepCopy()
			replicas := int32(1)
			d.Spec.Replicas = &replicas
			d.Status.Replicas, d.Status.UpdatedReplicas, d.Status.ReadyReplicas = replicas, replicas, replicas
			d.ResourceVersion = "7"
			key := "container.apparmor.security.beta.kubernetes.io/slot-0"
			d.Spec.Template.Annotations = map[string]string{key: "unconfined"}
			p.Annotations = map[string]string{key: "unconfined"}
			d.Spec.Template.Spec.Containers[1].SecurityContext = &corev1.SecurityContext{}
			p.Spec.Containers[1].SecurityContext = &corev1.SecurityContext{AppArmorProfile: &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined}}
			switch scenario {
			case "typed-only":
				delete(p.Annotations, key)
			case "different-profile":
				p.Spec.Containers[1].SecurityContext.AppArmorProfile.Type = corev1.AppArmorProfileTypeRuntimeDefault
			case "different-security":
				p.Spec.Containers[1].SecurityContext.RunAsUser = func() *int64 { value := int64(42); return &value }()
			case "live-annotation-changed":
				p.Annotations[key] = "runtime/default"
			}
			if _, err := kube.AppsV1().Deployments(d.Namespace).Update(ctx, d, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := kube.CoreV1().Pods(p.Namespace).Update(ctx, p, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			target := RuntimePodScaleTarget{RuntimeType: "openclaw", Namespace: p.Namespace, DeploymentName: d.Name, PodName: p.Name, PodUID: string(p.UID)}
			snapshot, err := svc.InspectRuntimeScale(ctx, target)
			want := scenario == "api-materialized" || scenario == "typed-only"
			if (err == nil) != want {
				t.Fatalf("native source validation: accepted=%v want=%v err=%v", err == nil, want, err)
			}
			if want {
				if pods, err := svc.InspectRuntimeScaleInPods(ctx, target, snapshot); err != nil || len(pods) != 1 {
					t.Fatalf("scale-in preflight rejected equivalent API shape: %+v %v", pods, err)
				}
			}
		})
	}
}

func TestRuntimeScaleOutLargeMembershipKeepsAnnotationBounded(t *testing.T) {
	ctx := context.Background()
	svc, kube, d, p := maintenanceSourceFixture()
	d.ResourceVersion = "71"
	replicas := int32(10000)
	d.Spec.Replicas = &replicas
	d.Spec.Template.Labels["capacity-fixture"] = "yes"
	d.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"capacity-fixture": "yes"}}
	deployments := kube.AppsV1().Deployments(d.Namespace)
	if _, err := deployments.Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	replicaSets := kube.AppsV1().ReplicaSets(d.Namespace)
	rs, err := replicaSets.Get(ctx, "rs", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rs.Labels = d.Spec.Template.Labels
	rs.Spec.Template = *d.Spec.Template.DeepCopy()
	if _, err := replicaSets.Update(ctx, rs, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	snapshot := &RuntimePodScaleSnapshot{
		Option:          RuntimePodScaleOption{DeploymentUID: string(d.UID), Replicas: replicas},
		ResourceVersion: d.ResourceVersion, ApprovalDigest: "approved",
		ExpansionWriteID: "00000000-0000-0000-0000-000000000001",
	}
	patch, err := runtimeExpansionPatch(snapshot, 10001, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(patch) > 1024 {
		t.Fatalf("a 10,000-Pod baseline needs %d bytes in the controller patch", len(patch))
	}
	scaled, err := deployments.Patch(ctx, d.Name, types.JSONPatchType, patch, metav1.PatchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	uids := make([]string, 10000)
	for i := range uids {
		uids[i] = fmt.Sprintf("10000000-0000-0000-0000-%012d", i)
	}
	podAPI := kube.CoreV1().Pods(d.Namespace)
	timestamp := metav1.NewTime(time.Now().UTC())
	for name, uid := range map[string]string{"preexisting": uids[len(uids)-1], "created": "new-attempt-pod"} {
		pod := p.DeepCopy()
		pod.Name, pod.UID = name, types.UID(uid)
		pod.Labels, pod.CreationTimestamp = d.Spec.Template.Labels, timestamp
		pod.Status.Phase = corev1.PodPending
		if _, err := podAPI.Create(ctx, pod, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	snapshot.Option.Replicas = 10001
	snapshot.expansion = scaled.Annotations[runtimeExpansionAnnotation]
	target := RuntimePodScaleTarget{Namespace: d.Namespace, DeploymentName: d.Name}
	unknown, err := svc.InspectRuntimeExpansion(ctx, target, snapshot)
	if err != nil || len(unknown.StartingPodUIDs) != 0 {
		t.Fatalf("startup credited without the matching membership: %+v %v", unknown.StartingPodUIDs, err)
	}
	snapshot.ExpansionBaseline = &RuntimeExpansionBaseline{ID: snapshot.ExpansionWriteID, PodUIDs: uids}
	observed, err := svc.InspectRuntimeExpansion(ctx, target, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := observed.StartingPodUIDs["new-attempt-pod"]; !found || len(observed.StartingPodUIDs) != 1 {
		t.Fatalf("same-second startup was not isolated by membership: %+v", observed.StartingPodUIDs)
	}
}
