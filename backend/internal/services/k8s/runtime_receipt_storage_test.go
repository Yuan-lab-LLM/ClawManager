package k8s

import (
	"context"
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

const testRuntimeReceiptBinding = `[{"namespace":"runtime-system","deployment":"openclaw-runtime","claim_name":"pool-receipts"}]`

func runtimeReceiptTestSpec() RuntimeDeploymentSpec {
	return RuntimeDeploymentSpec{Name: "openclaw-runtime", Namespace: "runtime-system", RuntimeType: "openclaw", Image: "openclaw:old", Replicas: 1, WorkspacePVCClaimName: "workspaces"}
}

func runtimeReceiptTestClaim() *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-receipts", Namespace: "runtime-system", UID: "pool-receipts-uid", Labels: map[string]string{"clawmanager.io/storage-purpose": "task-receipts"}, Annotations: map[string]string{"clawmanager.io/runtime-deployment": "openclaw-runtime"}},
		Spec:       corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOncePod}},
	}
}

func assertRuntimeReceiptNoWrites(t *testing.T, client *fake.Clientset) {
	t.Helper()
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatalf("unexpected write: %s %s", action.GetVerb(), action.GetResource().Resource)
		}
	}
}

func TestRuntimeReceiptBindings(t *testing.T) {
	d := BuildRuntimeDeployment(runtimeReceiptTestSpec())
	for _, raw := range []string{
		"bad-json", testRuntimeReceiptBinding + "{}",
		`[{"namespace":"runtime-system","deployment":"openclaw-runtime","claim_name":"pool-receipts","unknown":true}]`,
		`[{"namespace":"../invalid","deployment":"pool","claim_name":"r"}]`,
		`[{"namespace":"runtime-system","deployment":"pool","claim_name":"r"},{"namespace":"runtime-system","deployment":"pool","claim_name":"s"}]`,
		`[{"namespace":"runtime-system","deployment":"pool","claim_name":"r"},{"namespace":"runtime-system","deployment":"pool2","claim_name":"r"}]`,
		strings.Repeat(" ", 65537) + "[]",
	} {
		if _, err := selectRuntimeReceiptBinding(raw, d); err == nil {
			t.Fatal("invalid binding accepted")
		}
	}
	b, err := selectRuntimeReceiptBinding(testRuntimeReceiptBinding, d)
	if err != nil || b == nil || b.ClaimName != "pool-receipts" {
		t.Fatalf("selection: %v %v", b, err)
	}
	d.Namespace = "another"
	if b, err := selectRuntimeReceiptBinding(testRuntimeReceiptBinding, d); b != nil || err != nil {
		t.Fatal("cross-namespace binding")
	}
	d.Labels["clawmanager.io/runtime-type"] = "hermes"
	if _, err := selectRuntimeReceiptBinding("invalid", d); err != nil {
		t.Fatal("non-OpenClaw affected")
	}
}

func TestRuntimeReceiptDisabledLeavesManifestUnchanged(t *testing.T) {
	t.Setenv(runtimeReceiptBindingsEnv, "")
	client := fake.NewSimpleClientset()
	s := &runtimeDeploymentService{client: client}
	d := BuildRuntimeDeployment(runtimeReceiptTestSpec())
	before := d.DeepCopy()
	if err := s.prepareRuntimeReceiptStorage(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, d) || len(client.Actions()) != 0 {
		t.Fatal("disabled feature changed deployment or read PVC")
	}
}

func assertRuntimeReceiptDeployment(t *testing.T, d *appsv1.Deployment) {
	t.Helper()
	if d.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType || d.Spec.Strategy.RollingUpdate != nil {
		t.Fatal("expected single-writer Recreate")
	}
	if d.Spec.Template.Annotations[openClawReceiptAnnotation] != "pool-receipts" || d.Spec.Template.Annotations[openClawReceiptUIDAnnotation] != "pool-receipts-uid" {
		t.Fatal("missing binding identity")
	}
	if len(d.Spec.Template.Spec.Volumes) != 2 {
		t.Fatal("receipt volume missing or duplicated")
	}
	c := d.Spec.Template.Spec.Containers[0]
	if len(c.VolumeMounts) != 2 || c.VolumeMounts[1].MountPath != openClawReceiptRoot {
		t.Fatal("receipt mount missing or duplicated")
	}
	count := 0
	for _, env := range c.Env {
		if env.Name == openClawReceiptRootEnv {
			count++
			if env.Value != openClawReceiptRoot {
				t.Fatal("incorrect root")
			}
		}
		if env.Name == "CLAWMANAGER_OPENCLAW_TASK_PROTOCOL" {
			t.Fatal("must not enable protocol")
		}
	}
	if count != 1 {
		t.Fatal("root missing or duplicated")
	}
}

func TestRuntimeReceiptEnsureAndRealRolloutPath(t *testing.T) {
	t.Setenv(runtimeReceiptBindingsEnv, testRuntimeReceiptBinding)
	ctx := context.Background()
	for _, createViaEnsure := range []bool{true, false} {
		objects := []runtime.Object{runtimeReceiptTestClaim()}
		if !createViaEnsure {
			objects = append(objects, BuildRuntimeDeployment(runtimeReceiptTestSpec()))
		}
		client := fake.NewSimpleClientset(objects...)
		s := &runtimeDeploymentService{client: client}
		spec := runtimeReceiptTestSpec()
		if createViaEnsure {
			if err := s.Ensure(ctx, spec); err != nil {
				t.Fatal(err)
			}
		}
		// Existing static YAML pools enter through RolloutImage in production.
		for i := 0; i < 2; i++ {
			if err := s.RolloutImage(ctx, spec.Namespace, spec.Name, "openclaw:new", "", 0, 1); err != nil {
				t.Fatal(err)
			}
			d, err := client.AppsV1().Deployments(spec.Namespace).Get(ctx, spec.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			assertRuntimeReceiptDeployment(t, d)
		}
		if err := s.Ensure(ctx, spec); err != nil {
			t.Fatal(err)
		}
		d, _ := client.AppsV1().Deployments(spec.Namespace).Get(ctx, spec.Name, metav1.GetOptions{})
		assertRuntimeReceiptDeployment(t, d)
		for _, a := range client.Actions() {
			if a.GetResource().Resource == "persistentvolumeclaims" && a.GetVerb() != "get" {
				t.Fatal("must not mutate PVC")
			}
		}
	}
}

func TestRuntimeReceiptInvalidStorageNoWrites(t *testing.T) {
	for _, name := range []string{"missing", "owner", "purpose", "rwo", "lost", "block", "terminating", "uid", "replicas", "fsgroup", "workspace", "overlap", "wrong_volume", "subpath", "sidecar", "wrong_root", "direct_dir", "duplicate_root", "no_runtime"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(runtimeReceiptBindingsEnv, testRuntimeReceiptBinding)
			d := BuildRuntimeDeployment(runtimeReceiptTestSpec())
			p := &d.Spec.Template.Spec
			claim := runtimeReceiptTestClaim()
			switch name {
			case "owner":
				claim.Annotations["clawmanager.io/runtime-deployment"] = "another"
			case "purpose":
				claim.Labels["clawmanager.io/storage-purpose"] = "workspaces"
			case "rwo":
				claim.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
			case "lost":
				claim.Status.Phase = corev1.ClaimLost
			case "block":
				v := corev1.PersistentVolumeBlock
				claim.Spec.VolumeMode = &v
			case "terminating":
				now := metav1.Now()
				claim.DeletionTimestamp = &now
			case "uid":
				claim.UID = ""
			case "replicas":
				*d.Spec.Replicas = 2
			case "fsgroup":
				group := int64(1000)
				p.SecurityContext = &corev1.PodSecurityContext{FSGroup: &group}
			case "workspace":
				p.Volumes[0].PersistentVolumeClaim.ClaimName = "pool-receipts"
			case "overlap":
				p.Containers[0].VolumeMounts[0].MountPath = "/var/lib"
			case "wrong_volume":
				p.Volumes = append(p.Volumes, corev1.Volume{Name: openClawReceiptVolume, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
			case "subpath":
				p.Containers[0].VolumeMounts = append(p.Containers[0].VolumeMounts, corev1.VolumeMount{Name: openClawReceiptVolume, MountPath: openClawReceiptRoot, SubPath: "unsafe"})
			case "sidecar":
				p.Containers = append(p.Containers, corev1.Container{Name: "sidecar", VolumeMounts: []corev1.VolumeMount{{Name: openClawReceiptVolume, MountPath: openClawReceiptRoot}}})
			case "wrong_root":
				p.Containers[0].Env = append(p.Containers[0].Env, corev1.EnvVar{Name: openClawReceiptRootEnv, Value: "/workspaces"})
			case "direct_dir":
				p.Containers[0].Env = append(p.Containers[0].Env, corev1.EnvVar{Name: "CLAWMANAGER_TASK_RECEIPT_DIR", Value: "/tmp/receipt"})
			case "duplicate_root":
				p.Containers[0].Env = append(p.Containers[0].Env, corev1.EnvVar{Name: openClawReceiptRootEnv, Value: openClawReceiptRoot}, corev1.EnvVar{Name: openClawReceiptRootEnv, Value: openClawReceiptRoot})
			case "no_runtime":
				p.Containers[0].Name = "other"
			}
			objects := []runtime.Object{d}
			if name != "missing" {
				objects = append(objects, claim)
			}
			client := fake.NewSimpleClientset(objects...)
			s := &runtimeDeploymentService{client: client}
			if err := s.RolloutImage(context.Background(), d.Namespace, d.Name, "new", "", 0, 1); err == nil {
				t.Fatal("expected failure")
			}
			assertRuntimeReceiptNoWrites(t, client)
		})
	}
}

func TestRuntimeReceiptRemovalReplacementAndScale(t *testing.T) {
	t.Setenv(runtimeReceiptBindingsEnv, testRuntimeReceiptBinding)
	ctx := context.Background()
	spec := runtimeReceiptTestSpec()
	client := fake.NewSimpleClientset(runtimeReceiptTestClaim())
	s := &runtimeDeploymentService{client: client}
	if err := s.Ensure(ctx, spec); err != nil {
		t.Fatal(err)
	}
	client.ClearActions()
	if err := s.Scale(ctx, spec.Namespace, spec.Name, 2); err == nil {
		t.Fatal("multiple writers allowed")
	}
	assertRuntimeReceiptNoWrites(t, client)
	if err := s.Scale(ctx, spec.Namespace, spec.Name, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Scale(ctx, spec.Namespace, spec.Name, 1); err != nil {
		t.Fatal(err)
	}
	t.Setenv(runtimeReceiptBindingsEnv, "")
	client.ClearActions()
	if err := s.Ensure(ctx, spec); err == nil {
		t.Fatal("removal allowed")
	}
	if err := s.RolloutImage(ctx, spec.Namespace, spec.Name, "new", "", 0, 1); err == nil {
		t.Fatal("rollout without binding allowed")
	}
	assertRuntimeReceiptNoWrites(t, client)
	// Emergency stop remains available even when the binding is invalid.
	if err := s.Scale(ctx, spec.Namespace, spec.Name, 0); err != nil {
		t.Fatal(err)
	}
	t.Setenv(runtimeReceiptBindingsEnv, testRuntimeReceiptBinding)
	claim := runtimeReceiptTestClaim()
	claim.UID = "replaced"
	if _, err := client.CoreV1().PersistentVolumeClaims(claim.Namespace).Update(ctx, claim, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	client.ClearActions()
	if err := s.Ensure(ctx, spec); err == nil {
		t.Fatal("recreated PVC accepted by Ensure")
	}
	if err := s.RolloutImage(ctx, spec.Namespace, spec.Name, "new", "", 0, 1); err == nil {
		t.Fatal("recreated PVC accepted by rollout")
	}
	assertRuntimeReceiptNoWrites(t, client)
}

func TestRuntimeReceiptRejectsUpgradeAndLabCloning(t *testing.T) {
	ctx := context.Background()
	spec := runtimeReceiptTestSpec()
	image := "openclaw@sha256:" + strings.Repeat("a", 64)
	for _, planned := range []bool{true, false} {
		t.Run(map[bool]string{true: "planned", false: "mounted"}[planned], func(t *testing.T) {
			t.Setenv(runtimeReceiptBindingsEnv, testRuntimeReceiptBinding)
			d := BuildRuntimeDeployment(spec)
			if !planned {
				t.Setenv(runtimeReceiptBindingsEnv, "")
				d.Spec.Template.Spec.Containers[0].Env = append(d.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: openClawReceiptRootEnv, Value: openClawReceiptRoot})
			}
			client := fake.NewSimpleClientset(d)
			s := &runtimeDeploymentService{client: client}
			if err := s.EnsureUpgradePool(ctx, spec.Namespace, spec.Name, "target", image, "u1"); err == nil {
				t.Fatal("upgrade clone accepted")
			}
			if err := s.EnsureUpgradeLabPool(ctx, spec.Namespace, spec.Name, "lab", image, "l1", 1); err == nil {
				t.Fatal("lab clone accepted")
			}
			if err := s.RolloutImage(ctx, spec.Namespace, spec.Name, image, "u1", 0, 1); err == nil {
				t.Fatal("managed surge rollout accepted")
			}
			assertRuntimeReceiptNoWrites(t, client)
		})
	}
}

func TestRuntimeReceiptExistingTargetsCannotBypassCloneGuard(t *testing.T) {
	t.Setenv(runtimeReceiptBindingsEnv, testRuntimeReceiptBinding)
	ctx := context.Background()
	spec := runtimeReceiptTestSpec()
	image := "openclaw@sha256:" + strings.Repeat("a", 64)
	source := BuildRuntimeDeployment(spec)
	spec.Name, spec.Image = "target", image
	target := BuildRuntimeDeployment(spec)
	target.Labels[runtimePoolRoleLabel] = "upgrade-target"
	target.Labels[runtimeUpgradeIDLabel] = "u1"
	target.Labels[runtimeSourceLabel] = source.Name
	spec.Name = "lab"
	lab := BuildRuntimeDeployment(spec)
	lab.Labels[upgradeLabPurposeLabel] = upgradeLabPurposeValue
	lab.Labels[upgradeLabRunLabel] = "l1"
	client := fake.NewSimpleClientset(source, target, lab)
	s := &runtimeDeploymentService{client: client}
	if err := s.EnsureUpgradePool(ctx, source.Namespace, source.Name, target.Name, image, "u1"); err == nil {
		t.Fatal("existing upgrade target bypassed guard")
	}
	if err := s.EnsureUpgradeLabPool(ctx, source.Namespace, source.Name, lab.Name, image, "l1", 1); err == nil {
		t.Fatal("existing lab bypassed guard")
	}
	if err := s.SetUpgradePoolActive(ctx, source.Namespace, source.Name, target.Name, "u1", true); err == nil {
		t.Fatal("existing target activated despite source storage binding")
	}
	assertRuntimeReceiptNoWrites(t, client)
}

func TestRuntimeReceiptReconciliationRejectsUnannotatedMigration(t *testing.T) {
	t.Setenv(runtimeReceiptBindingsEnv, testRuntimeReceiptBinding)
	for _, unsafe := range []string{"claim", "root", "directory", "subpath"} {
		t.Run(unsafe, func(t *testing.T) {
			spec := runtimeReceiptTestSpec()
			current := BuildRuntimeDeployment(spec)
			switch unsafe {
			case "claim":
				current.Spec.Template.Spec.Volumes = append(current.Spec.Template.Spec.Volumes, corev1.Volume{Name: openClawReceiptVolume, VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "old-receipts"}}})
			case "root":
				current.Spec.Template.Spec.Containers[0].Env = append(current.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: openClawReceiptRootEnv, Value: "/old-root"})
			case "directory":
				current.Spec.Template.Spec.Containers[0].Env = append(current.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "CLAWMANAGER_TASK_RECEIPT_DIR", Value: "/old-dir"})
			case "subpath":
				current.Spec.Template.Spec.Volumes = append(current.Spec.Template.Spec.Volumes, corev1.Volume{Name: openClawReceiptVolume, VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "pool-receipts"}}})
				current.Spec.Template.Spec.Containers[0].VolumeMounts = append(current.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: openClawReceiptVolume, MountPath: openClawReceiptRoot, SubPath: "old-directory"})
			}
			client := fake.NewSimpleClientset(current, runtimeReceiptTestClaim())
			s := &runtimeDeploymentService{client: client}
			if err := s.Ensure(context.Background(), spec); err == nil {
				t.Fatal("unannotated old storage silently replaced")
			}
			assertRuntimeReceiptNoWrites(t, client)
		})
	}
}

func TestRuntimeReceiptExistingTargetWithDeletedLegacySource(t *testing.T) {
	ctx := context.Background()
	image := "openclaw@sha256:" + strings.Repeat("b", 64)
	spec := runtimeReceiptTestSpec()
	sourceName := spec.Name
	spec.Name, spec.Image = "target", image
	target := BuildRuntimeDeployment(spec)
	target.Labels[runtimePoolRoleLabel] = "upgrade-target"
	target.Labels[runtimeUpgradeIDLabel] = "u1"
	target.Labels[runtimeSourceLabel] = sourceName
	spec.Name = "lab"
	lab := BuildRuntimeDeployment(spec)
	lab.Labels[upgradeLabPurposeLabel] = upgradeLabPurposeValue
	lab.Labels[upgradeLabRunLabel] = "l1"
	for _, configured := range []bool{false, true} {
		raw := ""
		if configured {
			raw = testRuntimeReceiptBinding
		}
		t.Setenv(runtimeReceiptBindingsEnv, raw)
		client := fake.NewSimpleClientset(target.DeepCopy(), lab.DeepCopy())
		s := &runtimeDeploymentService{client: client}
		for _, err := range []error{
			s.EnsureUpgradePool(ctx, spec.Namespace, sourceName, target.Name, image, "u1"),
			s.EnsureUpgradeLabPool(ctx, spec.Namespace, sourceName, lab.Name, image, "l1", 1),
		} {
			if (err != nil) != configured {
				t.Fatalf("configured=%v, error=%v", configured, err)
			}
		}
		assertRuntimeReceiptNoWrites(t, client)
	}
}
