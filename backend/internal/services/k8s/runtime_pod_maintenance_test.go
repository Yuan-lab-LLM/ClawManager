package k8s

import (
	"context"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"testing"
)

func maintenanceBool(v bool) *bool { return &v }
func maintenanceReplicaSet(source *appsv1.Deployment, name string, uid types.UID) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: source.Namespace, UID: uid, OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: source.Name, UID: source.UID, Controller: maintenanceBool(true)}}}}
}

func TestRuntimeMaintenanceDeleteChecksAssignmentsAndRetainsPVC(t *testing.T) {
	ctx := context.Background()
	one := int32(1)
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "managed", Namespace: "ns", UID: types.UID("deployment-uid"), ResourceVersion: "7", Labels: map[string]string{runtimeManagedPoolLabel: "true"}}, Spec: appsv1.DeploymentSpec{Replicas: &one}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "managed-pod", Namespace: "ns", UID: types.UID("pod-uid"), Labels: map[string]string{"app": "managed"}, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "managed-rs", UID: types.UID("rs"), Controller: maintenanceBool(true)}}}}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "workspace", Namespace: "ns"}}
	assignment := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "assignment", Namespace: "ns", Labels: map[string]string{"clawmanager.io/purpose": "lite-instance"}}, Data: map[string]string{"pod_uid": "pod-uid", "desired_state": "running"}}
	kube := fake.NewSimpleClientset(deployment, pod, claim, assignment, maintenanceReplicaSet(deployment, "managed-rs", types.UID("rs")))
	svc := &runtimeDeploymentService{client: kube}
	if err := svc.DeleteManagedRuntimePool(ctx, "ns", "managed", "wrong-uid"); err == nil {
		t.Fatal("accepted stale Deployment identity")
	}
	if err := svc.DeleteManagedRuntimePool(ctx, "ns", "managed", "deployment-uid"); err == nil {
		t.Fatal("deleted pool with native assignment")
	}
	if err := kube.CoreV1().ConfigMaps("ns").Delete(ctx, "assignment", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteManagedRuntimePod(ctx, "ns", "managed", "managed-pod", "wrong-pod"); err == nil {
		t.Fatal("accepted stale Pod identity")
	}
	if err := svc.DeleteManagedRuntimePool(ctx, "ns", "managed", "deployment-uid"); err != nil {
		t.Fatal(err)
	}
	foundDelete := false
	for _, action := range kube.Actions() {
		if action.GetVerb() == "delete" && action.GetResource().Resource == "deployments" {
			opts := action.(clienttesting.DeleteAction).GetDeleteOptions()
			if opts.Preconditions == nil || opts.Preconditions.UID == nil || string(*opts.Preconditions.UID) != "deployment-uid" || opts.Preconditions.ResourceVersion == nil || *opts.Preconditions.ResourceVersion != "7" {
				t.Fatalf("missing immutable deletion preconditions: %#v", opts)
			}
			foundDelete = true
		}
		if action.GetVerb() == "delete" && action.GetResource().Resource == "persistentvolumeclaims" {
			t.Fatal("deleted PVC")
		}
	}
	if !foundDelete {
		t.Fatal("did not delete controller")
	}
	if _, err := kube.CoreV1().PersistentVolumeClaims("ns").Get(ctx, "workspace", metav1.GetOptions{}); err != nil {
		t.Fatal("persistent workspace removed")
	}
}

func TestRuntimeMaintenanceListsExistingManagedPools(t *testing.T) {
	ctx := context.Background()
	svc, kube, deployment, _ := maintenanceSourceFixture()
	pools, err := svc.ListManagedRuntimePools(ctx, "ns")
	if err != nil || pools == nil || len(pools) != 0 {
		t.Fatalf("want empty list for default pool, got %v %v", pools, err)
	}
	deployment.Labels[runtimeManagedPoolLabel] = "true"
	if _, err := kube.AppsV1().Deployments("ns").Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	pools, err = svc.ListManagedRuntimePools(ctx, "ns")
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 1 || pools[0].Namespace != "ns" || pools[0].DeploymentName != deployment.Name || pools[0].DeploymentUID != string(deployment.UID) || pools[0].Replicas != *deployment.Spec.Replicas {
		t.Fatalf("unexpected managed pool inventory: %+v", pools)
	}
}

func TestRuntimeMaintenanceSourceOwnershipAndImage(t *testing.T) {
	ctx := context.Background()
	svc, _, deployment, pod := maintenanceSourceFixture()
	actual, source, image, err := svc.maintenanceSource(ctx, "ns", pod.Name, string(pod.UID))
	if err != nil {
		t.Fatal(err)
	}
	if actual.UID != deployment.UID || source.UID != pod.UID || image != pod.Spec.Containers[0].Image {
		t.Fatalf("unexpected verified source: %s %s %s", actual.UID, source.UID, image)
	}
	if _, _, _, err := svc.maintenanceSource(ctx, "ns", pod.Name, "stale"); err == nil {
		t.Fatal("accepted stale Pod UID")
	}
	if err := svc.DeleteManagedRuntimePod(ctx, "ns", deployment.Name, pod.Name, string(pod.UID)); err == nil {
		t.Fatal("deleted unowned source")
	}
}

func maintenanceSourceFixture() (*runtimeDeploymentService, *fake.Clientset, *appsv1.Deployment, *corev1.Pod) {
	spec := RuntimeDeploymentSpec{Name: "source", Namespace: "ns", RuntimeType: "openclaw", Image: "repo/runtime@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SlotCount: 2, AgentTokenSecretName: "agent", WorkspacePVCClaimName: "workspace"}
	deployment := BuildRuntimeDeployment(spec)
	deployment.UID = types.UID("source-uid")
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "source-pod", Namespace: "ns", UID: types.UID("pod-uid"), OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "rs", UID: types.UID("rs"), Controller: maintenanceBool(true)}}}, Spec: deployment.Spec.Template.Spec, Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
	for _, c := range pod.Spec.Containers {
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{Name: c.Name, Ready: true, ImageID: spec.Image})
	}
	kube := fake.NewSimpleClientset(deployment, pod, maintenanceReplicaSet(deployment, "rs", types.UID("rs")), maintenanceWorkspaceClaim())
	return &runtimeDeploymentService{client: kube}, kube, deployment, pod
}
func maintenanceWorkspaceClaim() *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "workspace", Namespace: "ns", UID: types.UID("workspace-uid")}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "workspace-pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
}
func TestRuntimeMaintenanceRejectsInvalidSourceRoleAndStorage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*appsv1.Deployment, *corev1.Pod)
	}{
		{"unknown pool role", func(d *appsv1.Deployment, p *corev1.Pod) { d.Labels[runtimePoolRoleLabel] = "mystery" }},
		{"pod lab role missing purpose", func(d *appsv1.Deployment, p *corev1.Pod) {
			p.Labels = map[string]string{runtimePoolRoleLabel: "upgrade-lab"}
		}},
		{"lab role missing purpose", func(d *appsv1.Deployment, p *corev1.Pod) { d.Labels[runtimePoolRoleLabel] = "upgrade-lab" }},
		{"inactive target", func(d *appsv1.Deployment, p *corev1.Pod) {
			d.Labels[runtimePoolRoleLabel] = "upgrade-target"
			delete(d.Labels, runtimeSchedulingLabel)
		}},
		{"coordinator wrong native role", func(d *appsv1.Deployment, p *corev1.Pod) {
			upsertEnvVar(&p.Spec.Containers[0], "RUNTIME_AGENT_ROLE", "standalone")
		}},
		{"slot wrong index", func(d *appsv1.Deployment, p *corev1.Pod) {
			upsertEnvVar(&p.Spec.Containers[1], "RUNTIME_SLOT_INDEX", "1")
		}},
		{"emptyDir plus unrelated PVC", func(d *appsv1.Deployment, p *corev1.Pod) {
			for i := range p.Spec.Volumes {
				if p.Spec.Volumes[i].Name == runtimeWorkspaceVolume {
					p.Spec.Volumes[i].VolumeSource = corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}
				}
			}
			p.Spec.Volumes = append(p.Spec.Volumes, corev1.Volume{Name: "unrelated", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "workspace"}}})
		}},
		{"readonly workspace volume", func(d *appsv1.Deployment, p *corev1.Pod) {
			for i := range p.Spec.Volumes {
				if p.Spec.Volumes[i].Name == runtimeWorkspaceVolume {
					p.Spec.Volumes[i].PersistentVolumeClaim.ReadOnly = true
				}
			}
		}},
		{"different slot workspace root", func(d *appsv1.Deployment, p *corev1.Pod) {
			for i := range p.Spec.Containers[1].VolumeMounts {
				if p.Spec.Containers[1].VolumeMounts[i].Name == runtimeWorkspaceVolume {
					p.Spec.Containers[1].VolumeMounts[i].MountPath = "/other"
				}
			}
		}},
		{"unsafe subpath", func(d *appsv1.Deployment, p *corev1.Pod) {
			for i := range p.Spec.Containers[1].VolumeMounts {
				if p.Spec.Containers[1].VolumeMounts[i].Name == runtimeWorkspaceVolume {
					p.Spec.Containers[1].VolumeMounts[i].SubPath = "other-tenant"
				}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, kube, deployment, pod := maintenanceSourceFixture()
			tc.change(deployment, pod)
			ctx := context.Background()
			if _, err := kube.AppsV1().Deployments("ns").Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := kube.CoreV1().Pods("ns").Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := svc.maintenanceSource(ctx, "ns", pod.Name, string(pod.UID)); err == nil {
				t.Fatal("unsafe source accepted")
			}
		})
	}
}
