package k8s

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const testReceiptBinding = `[{"instance_id":42,"user_id":7,"claim_name":"receipts-42"}]`

func receiptTestConfig() PodConfig {
	return PodConfig{InstanceID: 42, UserID: 7, Type: "openclaw", RuntimeType: "desktop", CPUCores: 1, MemoryGB: 1, MountPath: "/config", Image: "openclaw:test"}
}

func receiptTestClaim() *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "receipts-42", Namespace: "clawreef-user-7", UID: "receipt-pvc-uid", Labels: map[string]string{"clawmanager.io/storage-purpose": "task-receipts", "instance-id": "42", "user-id": "7"}},
		Spec:       corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOncePod}},
	}
}

func receiptTestService(objects ...runtime.Object) (*InstanceDeploymentService, *fake.Clientset) {
	clientset := fake.NewSimpleClientset(objects...)
	client := &Client{Clientset: clientset, Namespace: "clawreef"}
	return &InstanceDeploymentService{client: client, namespaceService: &NamespaceService{client: client}}, clientset
}

func TestOpenClawReceiptBindingValidation(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"invalid_json", `secret-invalid-json`},
		{"unknown_field", `[{"instance_id":42,"user_id":7,"claim_name":"r","extra":1}]`},
		{"invalid_claim", `[{"instance_id":42,"user_id":7,"claim_name":"../r"}]`},
		{"missing_id", `[{"user_id":7,"claim_name":"r"}]`},
		{"owner_change", `[{"instance_id":42,"user_id":8,"claim_name":"r"}]`},
		{"duplicate_instance", `[{"instance_id":42,"user_id":7,"claim_name":"r"},{"instance_id":42,"user_id":7,"claim_name":"s"}]`},
		{"shared_claim", `[{"instance_id":42,"user_id":7,"claim_name":"r"},{"instance_id":43,"user_id":7,"claim_name":"r"}]`},
		{"trailing_data", testReceiptBinding + `{}`},
		{"too_large", strings.Repeat(" ", 65537) + "[]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := selectOpenClawReceiptBinding(tc.raw, receiptTestConfig()); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
	for _, raw := range []string{"", "[]", `[{"instance_id":43,"user_id":7,"claim_name":"r"}]`} {
		binding, err := selectOpenClawReceiptBinding(raw, receiptTestConfig())
		if err != nil || binding != nil {
			t.Fatalf("unexpected binding: %v %v", binding, err)
		}
	}
}

func TestOpenClawReceiptDefaultLeavesManifestUnchanged(t *testing.T) {
	t.Setenv(openClawReceiptBindingsEnv, "")
	service, clientset := receiptTestService()
	config := receiptTestConfig()
	desired := BuildInstanceDeployment(service.client, config, 1)
	before := desired.DeepCopy()
	if err := service.prepareOpenClawReceiptStorage(context.Background(), config, desired); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, desired) || len(clientset.Actions()) != 0 {
		t.Fatal("disabled feature changed manifest or accessed Kubernetes")
	}
	// Non-OpenClaw workloads are outside the opt-in feature, even with bad config.
	t.Setenv(openClawReceiptBindingsEnv, "invalid")
	config.Type = "hermes"
	if err := service.prepareOpenClawReceiptStorage(context.Background(), config, desired); err != nil {
		t.Fatal(err)
	}
}

func TestOpenClawReceiptEnsureCreateAndReconcile(t *testing.T) {
	t.Setenv(openClawReceiptBindingsEnv, testReceiptBinding)
	service, clientset := receiptTestService(receiptTestClaim())
	config := receiptTestConfig()
	config.ExtraEnv = map[string]string{openClawReceiptRootEnv: "/config/unsafe", "CLAWMANAGER_TASK_RECEIPT_DIR": "/config/unsafe/receipts", "INSTANCE_ID": "999", "USER_ID": "999"}
	ctx := context.Background()
	deployment, err := service.EnsureDeployment(ctx, config, 1)
	if err != nil {
		t.Fatal(err)
	}
	assertReceiptDeployment(t, deployment)
	// Prove that an existing rolling strategy is replaced, not retained.
	deployment.Spec.Strategy = appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType, RollingUpdate: &appsv1.RollingUpdateDeployment{}}
	if _, err := clientset.AppsV1().Deployments(deployment.Namespace).Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	config.Image = "openclaw:new"
	deployment, err = service.EnsureDeployment(ctx, config, 1)
	if err != nil {
		t.Fatal(err)
	}
	assertReceiptDeployment(t, deployment)
	if deployment.Spec.Template.Spec.Containers[0].Image != config.Image {
		t.Fatal("image not reconciled")
	}
	for _, action := range clientset.Actions() {
		if action.GetResource().Resource == "persistentvolumeclaims" && action.GetVerb() != "get" {
			t.Fatal("must not create/update/delete receipt PVC")
		}
	}
}

func assertReceiptDeployment(t *testing.T, deployment *appsv1.Deployment) {
	t.Helper()
	if deployment.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType || deployment.Spec.Strategy.RollingUpdate != nil {
		t.Fatal("expected Recreate strategy")
	}
	if deployment.Spec.Template.Annotations[openClawReceiptAnnotation] != "receipts-42" || deployment.Spec.Template.Annotations[openClawReceiptUIDAnnotation] != "receipt-pvc-uid" {
		t.Fatal("missing binding annotation")
	}
	pod := deployment.Spec.Template.Spec
	if len(pod.Volumes) != 2 || pod.Volumes[1].PersistentVolumeClaim == nil || pod.Volumes[1].PersistentVolumeClaim.ClaimName != "receipts-42" {
		t.Fatal("dedicated PVC missing")
	}
	mounts := pod.Containers[0].VolumeMounts
	if len(mounts) != 2 || mounts[1].MountPath != openClawReceiptRoot || mounts[1].SubPath != "" || mounts[1].ReadOnly {
		t.Fatalf("unsafe receipt mount: %v", mounts)
	}
	rootCount := 0
	for _, env := range pod.Containers[0].Env {
		if env.Name == "INSTANCE_ID" && env.Value != "42" || env.Name == "USER_ID" && env.Value != "7" {
			t.Fatal("untrusted receipt identity retained")
		}
		if env.Name == openClawReceiptRootEnv {
			rootCount++
			if env.Value != openClawReceiptRoot {
				t.Fatal("untrusted receipt root retained")
			}
		}
		if env.Name == "CLAWMANAGER_TASK_RECEIPT_DIR" || env.Name == "CLAWMANAGER_OPENCLAW_TASK_PROTOCOL" {
			t.Fatal("must not inject receipt directory or enable protocol")
		}
	}
	if rootCount != 1 {
		t.Fatal("receipt root must appear exactly once")
	}
}

func TestOpenClawReceiptReplacementPVCDoesNotChangeDeployment(t *testing.T) {
	t.Setenv(openClawReceiptBindingsEnv, testReceiptBinding)
	service, clientset := receiptTestService(receiptTestClaim())
	ctx := context.Background()
	if _, err := service.EnsureDeployment(ctx, receiptTestConfig(), 1); err != nil {
		t.Fatal(err)
	}
	claim := receiptTestClaim()
	claim.UID = "recreated-claim-uid"
	if _, err := clientset.CoreV1().PersistentVolumeClaims(claim.Namespace).Update(ctx, claim, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	clientset.ClearActions()
	if _, err := service.EnsureDeployment(ctx, receiptTestConfig(), 1); err == nil {
		t.Fatal("PVC recreation must require reconciliation")
	}
	for _, action := range clientset.Actions() {
		if action.GetVerb() != "get" {
			t.Fatal("replacement PVC mutated deployment")
		}
	}
}

func TestOpenClawReceiptInvalidStorageMakesNoWrites(t *testing.T) {
	for _, name := range []string{"missing", "missing_uid", "wrong_owner", "wrong_purpose", "rwo", "block", "terminating", "lost", "fs_group", "workspace_claim", "extra_claim", "overlap", "init_overlap", "reserved_volume", "replicas", "wrong_runtime", "bad_bindings"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(openClawReceiptBindingsEnv, testReceiptBinding)
			claim := receiptTestClaim()
			config := receiptTestConfig()
			replicas := int32(1)
			switch name {
			case "missing_uid":
				claim.UID = ""
			case "wrong_owner":
				claim.Labels["user-id"] = "8"
			case "wrong_purpose":
				delete(claim.Labels, "clawmanager.io/storage-purpose")
			case "rwo":
				claim.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
			case "block":
				mode := corev1.PersistentVolumeBlock
				claim.Spec.VolumeMode = &mode
			case "terminating":
				now := metav1.Now()
				claim.DeletionTimestamp = &now
			case "lost":
				claim.Status.Phase = corev1.ClaimLost
			case "fs_group":
				group := int64(1000)
				config.FSGroup = &group
			case "workspace_claim":
				config.PVCName = "receipts-42"
			case "extra_claim":
				config.ExtraPVCMounts = []PVCMount{{Name: "extra", ClaimName: "receipts-42", MountPath: "/extra"}}
			case "overlap":
				config.MountPath = "/var/lib"
			case "init_overlap":
				config.VolumeInitScripts = []VolumeInitScript{{Name: "data", MountPath: openClawReceiptRoot + "/child", Script: "true"}}
			case "reserved_volume":
				config.ExtraPVCMounts = []PVCMount{{Name: openClawReceiptVolume, ClaimName: "other", MountPath: "/extra"}}
			case "replicas":
				replicas = 2
			case "wrong_runtime":
				config.RuntimeType = "gateway"
			case "bad_bindings":
				t.Setenv(openClawReceiptBindingsEnv, "invalid-json")
			}
			var objects []runtime.Object
			if name != "missing" {
				objects = append(objects, claim)
			}
			service, clientset := receiptTestService(objects...)
			if _, err := service.EnsureDeployment(context.Background(), config, replicas); err == nil {
				t.Fatal("expected rejection")
			}
			for _, action := range clientset.Actions() {
				if action.GetVerb() != "get" {
					t.Fatalf("unexpected action on failure: %v", action)
				}
			}
		})
	}
}

func TestOpenClawReceiptBindingRemovalAndScalingRejected(t *testing.T) {
	t.Setenv(openClawReceiptBindingsEnv, testReceiptBinding)
	service, clientset := receiptTestService(receiptTestClaim())
	ctx := context.Background()
	config := receiptTestConfig()
	if _, err := service.EnsureDeployment(ctx, config, 1); err != nil {
		t.Fatal(err)
	}
	clientset.ClearActions()
	if err := service.ScaleDeployment(ctx, 7, 42, 2); err == nil {
		t.Fatal("must reject multiple writers")
	}
	for _, action := range clientset.Actions() {
		if action.GetVerb() != "get" {
			t.Fatal("scale mutated deployment")
		}
	}
	if err := service.ScaleDeployment(ctx, 7, 42, 0); err != nil {
		t.Fatal(err)
	}
	if err := service.ScaleDeployment(ctx, 7, 42, 1); err != nil {
		t.Fatal(err)
	}
	clientset.ClearActions()
	t.Setenv(openClawReceiptBindingsEnv, "")
	if _, err := service.EnsureDeployment(ctx, config, 1); err == nil {
		t.Fatal("must reject removal of binding")
	}
	for _, action := range clientset.Actions() {
		if action.GetVerb() != "get" {
			t.Fatal("binding removal mutated resources")
		}
	}
}

func TestOpenClawReceiptTransitionRejectsRebinding(t *testing.T) {
	service, _ := receiptTestService()
	current := BuildInstanceDeployment(service.client, receiptTestConfig(), 1)
	current.Spec.Template.Annotations = map[string]string{openClawReceiptAnnotation: "receipts-42", openClawReceiptUIDAnnotation: "receipt-pvc-uid"}
	for _, change := range []string{"claim", "owner", "uid"} {
		desired := current.DeepCopy()
		if change == "claim" {
			desired.Spec.Template.Annotations[openClawReceiptAnnotation] = "new-receipts"
		} else if change == "owner" {
			desired.Spec.Template.Labels["user-id"] = "8"
		} else {
			desired.Spec.Template.Annotations[openClawReceiptUIDAnnotation] = "replacement-pvc-uid"
		}
		if err := validateOpenClawReceiptTransition(current, desired); err == nil {
			t.Fatal("rebind accepted")
		}
	}
}

func TestOpenClawReceiptScaleCheckUsesResourceVersion(t *testing.T) {
	service, clientset := receiptTestService()
	current := BuildInstanceDeployment(service.client, receiptTestConfig(), 1)
	current.ResourceVersion = "12"
	ctx := context.Background()
	if _, err := clientset.AppsV1().Deployments(current.Namespace).Create(ctx, current, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	checked := false
	clientset.PrependReactor("patch", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		var patch struct {
			Metadata struct {
				ResourceVersion string `json:"resourceVersion"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(action.(k8stesting.PatchAction).GetPatch(), &patch); err != nil {
			t.Fatal(err)
		}
		if patch.Metadata.ResourceVersion != "12" {
			t.Fatal("scale patch is not conditional on inspected version")
		}
		checked = true
		return false, nil, nil
	})
	if err := service.ScaleDeployment(ctx, 7, 42, 2); err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("missing scale patch")
	}
}

func TestOpenClawReceiptContainerSelection(t *testing.T) {
	t.Setenv(openClawReceiptBindingsEnv, testReceiptBinding)
	for _, name := range []string{"reordered", "missing", "duplicate", "init_environment", "sidecar_environment"} {
		t.Run(name, func(t *testing.T) {
			service, client := receiptTestService(receiptTestClaim())
			config := receiptTestConfig()
			desired := BuildInstanceDeployment(service.client, config, 1)
			pod := &desired.Spec.Template.Spec
			switch name {
			case "reordered":
				pod.Containers = append([]corev1.Container{{Name: "sidecar"}}, pod.Containers...)
			case "missing":
				pod.Containers = nil
			case "duplicate":
				pod.Containers = append(pod.Containers, pod.Containers[0])
			case "init_environment":
				pod.InitContainers = []corev1.Container{{Name: "setup", Env: []corev1.EnvVar{{Name: openClawReceiptRootEnv, Value: "/old-root"}}}}
			case "sidecar_environment":
				pod.Containers = append(pod.Containers, corev1.Container{Name: "sidecar", Env: []corev1.EnvVar{{Name: "CLAWMANAGER_TASK_RECEIPT_DIR", Value: "/old-root"}}})
			}
			before := desired.DeepCopy()
			err := service.prepareOpenClawReceiptStorage(context.Background(), config, desired)
			if name == "reordered" {
				if err != nil {
					t.Fatal(err)
				}
				if len(pod.Containers[0].VolumeMounts) != 0 || len(pod.Containers[0].Env) != 0 {
					t.Fatal("sidecar received receipt configuration")
				}
				if err := validateOpenClawReceiptLayout(desired); err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !reflect.DeepEqual(before, desired) {
				t.Fatal("invalid layout accepted or partially modified")
			}
			assertRuntimeReceiptNoWrites(t, client)
		})
	}
}

func TestOpenClawReceiptReconcileRejectsLostBindingAndChangedLayout(t *testing.T) {
	for _, name := range []string{"annotation", "uid", "volume", "subpath", "mount_path", "root", "direct_directory", "sidecar", "init", "missing_mount", "instance_env", "owner_env", "fs_group"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(openClawReceiptBindingsEnv, testReceiptBinding)
			service, client := receiptTestService(receiptTestClaim())
			ctx := context.Background()
			config := receiptTestConfig()
			current := BuildInstanceDeployment(service.client, config, 1)
			if err := service.prepareOpenClawReceiptStorage(ctx, config, current); err != nil {
				t.Fatal(err)
			}
			pod := &current.Spec.Template.Spec
			switch name {
			case "instance_env":
				upsertEnvVar(&pod.Containers[0], "INSTANCE_ID", "999")
			case "owner_env":
				upsertEnvVar(&pod.Containers[0], "USER_ID", "999")
			case "fs_group":
				group := int64(1000)
				pod.SecurityContext = &corev1.PodSecurityContext{FSGroup: &group}
			case "annotation":
				delete(current.Spec.Template.Annotations, openClawReceiptAnnotation)
			case "uid":
				delete(current.Spec.Template.Annotations, openClawReceiptUIDAnnotation)
			case "volume":
				pod.Volumes[1].PersistentVolumeClaim.ClaimName = "old-receipts"
			case "subpath":
				pod.Containers[0].VolumeMounts[1].SubPath = "old-directory"
			case "mount_path":
				pod.Containers[0].VolumeMounts[1].MountPath = "/old-root"
			case "root":
				upsertEnvVar(&pod.Containers[0], openClawReceiptRootEnv, "/old-root")
			case "direct_directory":
				pod.Containers[0].Env = append(pod.Containers[0].Env, corev1.EnvVar{Name: "CLAWMANAGER_TASK_RECEIPT_DIR", Value: "/old-root"})
			case "sidecar":
				pod.Containers = append(pod.Containers, corev1.Container{Name: "sidecar", VolumeMounts: []corev1.VolumeMount{pod.Containers[0].VolumeMounts[1]}})
			case "init":
				pod.InitContainers = []corev1.Container{{Name: "setup", VolumeMounts: []corev1.VolumeMount{pod.Containers[0].VolumeMounts[1]}}}
			case "missing_mount":
				pod.Containers[0].VolumeMounts = pod.Containers[0].VolumeMounts[:1]
			}
			if _, err := client.AppsV1().Deployments(current.Namespace).Create(ctx, current, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
			client.ClearActions()
			if _, err := service.EnsureDeployment(ctx, config, 1); err == nil {
				t.Fatal("unverified storage transition accepted")
			}
			assertRuntimeReceiptNoWrites(t, client)
		})
	}
}

func TestReceiptStorageIntentWithoutAnnotationsBlocksScaleAndRemoval(t *testing.T) {
	for _, name := range []string{"volume", "mount_name", "mount_path", "init_mount", "environment"} {
		t.Run(name, func(t *testing.T) {
			service, client := receiptTestService()
			config := receiptTestConfig()
			current := BuildInstanceDeployment(service.client, config, 1)
			pod := &current.Spec.Template.Spec
			switch name {
			case "volume":
				pod.Volumes = append(pod.Volumes, corev1.Volume{Name: openClawReceiptVolume})
			case "mount_name":
				pod.Containers[0].VolumeMounts = append(pod.Containers[0].VolumeMounts, corev1.VolumeMount{Name: openClawReceiptVolume, MountPath: "/old-root"})
			case "mount_path":
				pod.Containers[0].VolumeMounts = append(pod.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "renamed", MountPath: openClawReceiptRoot + "/child"})
			case "init_mount":
				pod.InitContainers = []corev1.Container{{Name: "setup", VolumeMounts: []corev1.VolumeMount{{Name: "renamed", MountPath: openClawReceiptRoot}}}}
			case "environment":
				pod.Containers[0].Env = append(pod.Containers[0].Env, corev1.EnvVar{Name: openClawReceiptRootEnv, Value: "/old-root"})
			}
			ctx := context.Background()
			if _, err := client.AppsV1().Deployments(current.Namespace).Create(ctx, current, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
			client.ClearActions()
			if err := service.ScaleDeployment(ctx, 7, 42, 2); err == nil {
				t.Fatal("unannotated storage allowed multiple writers")
			}
			if err := validateOpenClawReceiptTransition(current, BuildInstanceDeployment(service.client, config, 1)); err == nil {
				t.Fatal("unannotated storage removed")
			}
			if err := rejectRuntimeReceiptClone(current); err == nil {
				t.Fatal("unannotated storage cloned")
			}
			assertRuntimeReceiptNoWrites(t, client)
		})
	}
}

func TestOpenClawReceiptRequiresBindingForInjectedStorage(t *testing.T) {
	t.Setenv(openClawReceiptBindingsEnv, "")
	service, client := receiptTestService()
	config := receiptTestConfig()
	config.ExtraEnv = map[string]string{openClawReceiptRootEnv: "/old-root"}
	if _, err := service.EnsureDeployment(context.Background(), config, 1); err == nil {
		t.Fatal("unbound receipt storage accepted")
	}
	assertRuntimeReceiptNoWrites(t, client)
}

func TestLegacyReceiptDeploymentIsNotDeleted(t *testing.T) {
	service, client := receiptTestService()
	ctx := context.Background()
	config := receiptTestConfig()
	for _, name := range []string{"a-legacy", "z-with-receipts"} {
		d := BuildInstanceDeployment(service.client, config, 1)
		d.Name = name
		if name == "z-with-receipts" {
			d.Spec.Template.Spec.Containers[0].Env = append(d.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: openClawReceiptRootEnv, Value: "/old-root"})
		}
		if _, err := client.AppsV1().Deployments(d.Namespace).Create(ctx, d, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	client.ClearActions()
	if err := service.deleteLegacyDeployments(ctx, service.client.GetNamespace(7), 42, InstanceDeploymentName(42)); err == nil {
		t.Fatal("legacy receipt deployment deleted")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "list" {
			t.Fatalf("partial cleanup before rejection: %v", action)
		}
	}
}

func TestLegacyDeploymentDeletionUsesObservedIdentity(t *testing.T) {
	service, client := receiptTestService()
	ctx := context.Background()
	d := BuildInstanceDeployment(service.client, receiptTestConfig(), 1)
	d.Name, d.UID, d.ResourceVersion = "old-deployment", "old-uid", "12"
	if _, err := client.AppsV1().Deployments(d.Namespace).Create(ctx, d, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	checked := false
	client.PrependReactor("delete", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		preconditions := action.(k8stesting.DeleteAction).GetDeleteOptions().Preconditions
		if preconditions == nil || preconditions.UID == nil || *preconditions.UID != d.UID || preconditions.ResourceVersion == nil || *preconditions.ResourceVersion != d.ResourceVersion {
			t.Fatal("delete not fenced by observed identity/version")
		}
		checked = true
		return false, nil, nil
	})
	if err := service.deleteLegacyDeployments(ctx, d.Namespace, 42, InstanceDeploymentName(42)); err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("missing delete")
	}
}

func TestOpenClawReceiptStartRevalidatesStorageButStopRemainsAvailable(t *testing.T) {
	for _, name := range []string{"binding_removed", "pvc_replaced", "wrong_owner", "lost", "rwo", "subpath", "annotation_removed"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(openClawReceiptBindingsEnv, testReceiptBinding)
			service, client := receiptTestService(receiptTestClaim())
			ctx := context.Background()
			config := receiptTestConfig()
			current, err := service.EnsureDeployment(ctx, config, 0)
			if err != nil {
				t.Fatal(err)
			}
			claim := receiptTestClaim()
			switch name {
			case "binding_removed":
				t.Setenv(openClawReceiptBindingsEnv, "")
			case "pvc_replaced":
				claim.UID = "new-volume"
			case "wrong_owner":
				claim.Labels["user-id"] = "8"
			case "lost":
				claim.Status.Phase = corev1.ClaimLost
			case "rwo":
				claim.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
			case "subpath":
				current.Spec.Template.Spec.Containers[0].VolumeMounts[1].SubPath = "old-data"
			case "annotation_removed":
				delete(current.Spec.Template.Annotations, openClawReceiptAnnotation)
			}
			if _, err := client.CoreV1().PersistentVolumeClaims(claim.Namespace).Update(ctx, claim, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := client.AppsV1().Deployments(current.Namespace).Update(ctx, current, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			client.ClearActions()
			if err := service.ScaleDeployment(ctx, 7, 42, 1); err == nil {
				t.Fatal("unsafe receipt writer resumed")
			}
			assertRuntimeReceiptNoWrites(t, client)
			if err := service.ScaleDeployment(ctx, 7, 42, 0); err != nil {
				t.Fatalf("must still allow stopping: %v", err)
			}
		})
	}
}
