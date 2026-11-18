package k8s

import (
	"clawreef/internal/runtimeeviction"
	"context"
	"errors"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ktesting "k8s.io/client-go/testing"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/clientcmd"
)

func TestSlotResetPVCFinalizerIsOperationAndUIDScoped(t *testing.T) {
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "workspace", UID: "old", ResourceVersion: "123", Finalizers: []string{"storage.example/keep"}}}
	kube := fake.NewSimpleClientset(claim)
	ctx := t.Context()
	p := RuntimeSlotResetClaim{Namespace: "ns", Name: "workspace", UID: "old"}
	for _, op := range []string{"first", "second", "first"} {
		if err := PinRuntimeSlotResetClaim(ctx, kube, p, op); err != nil {
			t.Fatal(err)
		}
	}
	if err := ReleaseRuntimeSlotResetClaim(ctx, kube, p, "first"); err != nil {
		t.Fatal(err)
	}
	got, err := kube.CoreV1().PersistentVolumeClaims("ns").Get(ctx, "workspace", metav1.GetOptions{})
	if err != nil || len(got.Finalizers) != 2 || !slices.Contains(got.Finalizers, "storage.example/keep") || !slices.Contains(got.Finalizers, slotResetFinalizer("second")) {
		t.Fatalf("unexpected finalizers: %v, %v", got, err)
	}
	newUID := p
	newUID.UID = "wrong"
	if err = PinRuntimeSlotResetClaim(ctx, kube, newUID, "z"); err == nil {
		t.Fatal("pinned replacement")
	}
	if err = ReleaseRuntimeSlotResetClaim(ctx, kube, newUID, "second"); err != nil {
		t.Fatal(err)
	}
	now := metav1.Now()
	got.DeletionTimestamp = &now
	if _, err = kube.CoreV1().PersistentVolumeClaims("ns").Update(ctx, got, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = PinRuntimeSlotResetClaim(ctx, kube, p, "new-operation"); err == nil {
		t.Fatal("admitted new operation during deletion")
	}
	if err = PinRuntimeSlotResetClaim(ctx, kube, p, "second"); err != nil {
		t.Fatal("lost held pin", err)
	}
}

// Opt in only on an authorized test cluster. The run owns a generated namespace
// and an unprovisioned PVC; no production volume or filesystem is mounted.
func TestSlotResetPVCDeletionLive(t *testing.T) {
	configPath := os.Getenv("TEST_SLOT_RESET_PIN_KUBECONFIG")
	if configPath == "" {
		t.Skip("set TEST_SLOT_RESET_PIN_KUBECONFIG for a disposable namespace")
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", configPath)
	if err != nil {
		t.Fatal(err)
	}
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	ns, err := kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "cm-slot-pin-e2e-"}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = kube.CoreV1().Namespaces().Delete(context.Background(), ns.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &ns.UID}})
	})
	claims := kube.CoreV1().PersistentVolumeClaims(ns.Name)
	empty := ""
	spec := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "workspace", Namespace: ns.Name}, Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &empty, VolumeName: "nonexistent-slot-reset-test-volume", AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Mi")}}}}
	old, err := claims.Create(ctx, spec, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p := RuntimeSlotResetClaim{Namespace: old.Namespace, Name: old.Name, UID: string(old.UID)}
	t.Cleanup(func() { _ = ReleaseRuntimeSlotResetClaim(context.Background(), kube, p, "live") })
	if err = PinRuntimeSlotResetClaim(ctx, kube, p, "live"); err != nil {
		t.Fatal(err)
	}
	if err = claims.Delete(ctx, p.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &old.UID}}); err != nil {
		t.Fatal(err)
	}
	current, err := claims.Get(ctx, p.Name, metav1.GetOptions{})
	if err != nil || current.UID != old.UID || current.DeletionTimestamp == nil || !slices.Contains(current.Finalizers, slotResetFinalizer("live")) {
		t.Fatalf("original UID was not retained: %v", err)
	}
	if _, err = claims.Create(ctx, spec, metav1.CreateOptions{}); !apierrors.IsAlreadyExists(err) {
		t.Fatalf("name reused while held: %v", err)
	}
	if err = ReleaseRuntimeSlotResetClaim(ctx, kube, p, "live"); err != nil {
		t.Fatal(err)
	}
	if err = wait.PollUntilContextTimeout(ctx, 250*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		_, e := claims.Get(ctx, p.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(e) {
			return true, nil
		}
		return false, e
	}); err != nil {
		t.Fatal(err)
	}
	replacement, err := claims.Create(ctx, spec, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.UID == old.UID {
		t.Fatal("replacement retained old UID")
	}
	if err = PinRuntimeSlotResetClaim(ctx, kube, p, "live"); err == nil {
		t.Fatal("replacement was repinned")
	}
	if err = ReleaseRuntimeSlotResetClaim(ctx, kube, p, "live"); err != nil {
		t.Fatal(err)
	}
	current, err = claims.Get(ctx, p.Name, metav1.GetOptions{})
	if err != nil || current.UID != replacement.UID || slices.Contains(current.Finalizers, slotResetFinalizer("live")) {
		t.Fatal("replacement was mutated")
	}
	t.Logf("namespace=%s old=%s replacement=%s", ns.Name, old.UID, replacement.UID)
}

func TestSlotResetWriterJobQuiescence(t *testing.T) {
	s := runtimeeviction.Scope{Version: 1, OperationID: "reset", Namespace: "ns", PodName: "source", PodUID: "source-uid", NodeName: "node", NodeUID: "nodeuid", NodeBootID: "boot", ContainerName: "slot-1", ContainerID: "containerd://" + strings.Repeat("a", 64), RuntimeType: "opencode", GatewayID: "gw-1-3", WorkspaceRoot: "/workspaces", InstanceID: 1, UserID: 2, Generation: 3}
	source := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: s.PodName, Namespace: s.Namespace, UID: types.UID(s.PodUID)}, Spec: corev1.PodSpec{NodeName: s.NodeName, Containers: []corev1.Container{{Name: s.ContainerName, VolumeMounts: []corev1.VolumeMount{{Name: "ws", MountPath: s.WorkspaceRoot}}}}, Volumes: []corev1.Volume{{Name: "ws", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "workspace"}}}}}}
	image := "registry/helper@sha256:" + strings.Repeat("b", 64)
	for _, scenario := range []string{"active", "terminal-active-pod", "terminal-failed", "terminal-complete", "changed-scope", "missing-unconfirmed", "gc-confirmed", "gc-confirmed-active-pod"} {
		t.Run(scenario, func(t *testing.T) {
			job, err := buildRuntimeEvictionJob(s, source, image)
			if err != nil {
				t.Fatal(err)
			}
			job.UID = "job-uid"
			job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
			var receipt *runtimeeviction.Receipt
			var objects []runtime.Object
			if scenario == "active" {
				job.Status.Active = 1
				job.Status.Conditions = nil
			}
			if scenario == "terminal-failed" {
				job.Status.Conditions[0].Type = batchv1.JobFailed
			}
			if scenario == "changed-scope" {
				job.Annotations["clawmanager.io/recovery-scope"] = "changed"
			}
			if strings.HasPrefix(scenario, "gc-confirmed") {
				receipt = &runtimeeviction.Receipt{ScopeDigest: runtimeeviction.ScopeDigest(s), OperationID: s.OperationID, PodUID: s.PodUID, ContainerName: s.ContainerName, NodeUID: s.NodeUID, NodeBootID: s.NodeBootID, StoppedContainerIDs: []string{s.ContainerID}, LedgerDigest: strings.Repeat("c", 64), FinishedAt: time.Now().UTC(), Confirmed: true}
			} else if scenario != "missing-unconfirmed" {
				objects = append(objects, job)
			}
			if strings.Contains(scenario, "active-pod") {
				objects = append(objects, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "in-flight", Namespace: s.Namespace, Labels: map[string]string{"job-name": job.Name}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}})
			}
			client := fake.NewSimpleClientset(objects...)
			err = SlotResetWriterFinished(t.Context(), client, s, source, image, receipt)
			wantOK := scenario == "terminal-failed" || scenario == "terminal-complete" || scenario == "gc-confirmed"
			if (err == nil) != wantOK {
				t.Fatalf("quiescence %v", err)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" && action.GetVerb() != "list" {
					t.Fatalf("quiescence check mutated Job: %s", action.GetVerb())
				}
			}
		})
	}
}

func TestSlotResetClaimReplacedBetweenGetAndUpdate(t *testing.T) {
	p := RuntimeSlotResetClaim{Namespace: "ns", Name: "workspace", UID: "source"}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: p.Name, Namespace: p.Namespace, UID: types.UID(p.UID), ResourceVersion: "101"}}
	client := fake.NewSimpleClientset(claim)
	client.PrependReactor("update", "persistentvolumeclaims", func(action ktesting.Action) (bool, runtime.Object, error) {
		req := action.(ktesting.UpdateAction).GetObject().(*corev1.PersistentVolumeClaim)
		if req.UID != claim.UID || req.ResourceVersion != "101" {
			t.Fatal("update lost observed UID/resourceVersion")
		}
		replaced := claim.DeepCopy()
		replaced.UID = "replacement"
		replaced.ResourceVersion = "102"
		gvr := corev1.SchemeGroupVersion.WithResource("persistentvolumeclaims")
		if err := client.Tracker().Update(gvr, replaced, p.Namespace); err != nil {
			t.Fatal(err)
		}
		return true, nil, apierrors.NewConflict(gvr.GroupResource(), p.Name, errors.New("changed"))
	})
	if err := PinRuntimeSlotResetClaim(t.Context(), client, p, "reset"); err == nil {
		t.Fatal("adopted replacement")
	}
	got, err := client.CoreV1().PersistentVolumeClaims(p.Namespace).Get(t.Context(), p.Name, metav1.GetOptions{})
	if err != nil || got.UID != "replacement" || len(got.Finalizers) != 0 {
		t.Fatalf("replacement altered: %v, %v", got, err)
	}
}

func TestSlotResetTerminatingClaimStillRequiresOriginalPVAndOwnPin(t *testing.T) {
	ctx := t.Context()
	d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "source", Namespace: "tenant", RuntimeType: "openclaw", Image: "repo@sha256:" + strings.Repeat("a", 64), SlotCount: 2, AgentTokenSecretName: "agent", WorkspacePVCClaimName: "workspaces", WorkspaceMountPath: "/workspaces", PodUserNamespaces: true, NetworkIsolation: true})
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant", Name: "source"}, Spec: *d.Spec.Template.Spec.DeepCopy()}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "workspaces", Namespace: "tenant", UID: "claim", ResourceVersion: "1"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv", UID: "pv-uid"}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: "tenant", Name: "workspaces", UID: "claim"}, PersistentVolumeSource: corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "10.0.0.1", Path: "/business"}}}}
	client := fake.NewSimpleClientset(claim, pv)
	const root = "/workspaces"
	digest, err := RuntimeFaultStorageIdentity(ctx, client, pod, root, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	pin, err := RuntimeSlotResetWorkspaceClaim(ctx, client, pod, root, digest)
	if err != nil {
		t.Fatal(err)
	}
	if err = PinRuntimeSlotResetClaim(ctx, client, *pin, "reset"); err != nil {
		t.Fatal(err)
	}
	claims := client.CoreV1().PersistentVolumeClaims("tenant")
	claim, err = claims.Get(ctx, claim.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	now := metav1.Now()
	claim.DeletionTimestamp = &now
	if _, err = claims.Update(ctx, claim, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = RuntimeFaultStorageIdentity(ctx, client, pod, root, time.Time{}); err == nil {
		t.Fatal("ordinary fault eviction accepted a deleting PVC")
	}
	if err = VerifyRuntimeSlotResetStorage(ctx, client, pod, root, digest, pin, "reset"); err != nil {
		t.Fatal(err)
	}
	if err = VerifyRuntimeSlotResetStorage(ctx, client, pod, root, digest, pin, "neighbor"); err == nil {
		t.Fatal("borrowed another operation's pin")
	}
	pv = pv.DeepCopy()
	pv.UID = "replacement"
	if _, err = client.CoreV1().PersistentVolumes().Update(ctx, pv, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = VerifyRuntimeSlotResetStorage(ctx, client, pod, root, digest, pin, "reset"); err == nil {
		t.Fatal("accepted replacement backing PV")
	}
}
