package k8s

import (
	"clawreef/internal/runtimeeviction"
	"context"
	"errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"strings"
	"testing"
)

func TestRecoveryJobPinnedScope(t *testing.T) {
	t.Setenv("CLAWMANAGER_RUNTIME_RECOVERY_IMAGE", "helper@sha256:abc")
	s := runtimeeviction.Scope{Version: 1, OperationID: "op", Namespace: "ns", PodName: "pod", PodUID: "uid", NodeName: "node", NodeUID: "nodeuid", NodeBootID: "boot", ContainerName: "slot-1", ContainerID: "containerd://cid", RuntimeType: "opencode", GatewayID: "gw-1-3", WorkspaceRoot: "/workspaces", InstanceID: 1, UserID: 2, Generation: 3}
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns", UID: types.UID("uid")}, Spec: corev1.PodSpec{NodeName: "node", Containers: []corev1.Container{{Name: "slot-1", VolumeMounts: []corev1.VolumeMount{{Name: "ws", MountPath: "/workspaces"}}}}, Volumes: []corev1.Volume{{Name: "ws", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "workspace"}}}}}}
	job, e := buildRuntimeEvictionJob(s, p, "helper@sha256:abc")
	if e != nil {
		t.Fatal(e)
	}
	if job.Spec.Template.Spec.NodeName != "node" || len(job.Spec.Template.Spec.Containers) != 1 || *job.Spec.Template.Spec.AutomountServiceAccountToken {
		t.Fatal("unsafe helper")
	}

	actual := job.Spec.Template.Spec.DeepCopy()
	actual.Containers[0].ImagePullPolicy = corev1.PullIfNotPresent
	proc := corev1.DefaultProcMount
	actual.Containers[0].SecurityContext.ProcMount = &proc
	if !trustedRecoveryTemplate(*actual, job.Spec.Template.Spec) {
		t.Fatal("safe API defaults rejected")
	}
	actual.Containers[0].Lifecycle = &corev1.Lifecycle{PostStart: &corev1.LifecycleHandler{Exec: &corev1.ExecAction{Command: []string{"touch", "/workspaces/bad"}}}}
	if trustedRecoveryTemplate(*actual, job.Spec.Template.Spec) {
		t.Fatal("injected lifecycle accepted")
	}
	p.Annotations = map[string]string{RuntimeFaultHelperImageAnnotation: "helper@sha256:abc"}
	client := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node", UID: "nodeuid"}, Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{BootID: "boot"}}})
	svc := &runtimeDeploymentService{client: client}
	if _, e = svc.RecoverRuntimeEviction(context.Background(), s, p); e == nil {
		t.Fatal("pending job reported success")
	}
	t.Setenv("CLAWMANAGER_RUNTIME_RECOVERY_IMAGE", "helper@sha256:upgraded")
	if _, e = svc.RecoverRuntimeEviction(context.Background(), s, p); !errors.Is(e, ErrRuntimeFaultEvictionPending) {
		t.Fatalf("pinned helper image changed after app upgrade: %v", e)
	}
	job.Spec.Template.Spec.NodeName = "other"
	client.BatchV1().Jobs("ns").Update(context.Background(), job, metav1.UpdateOptions{})
	if _, e = svc.RecoverRuntimeEviction(context.Background(), s, p); e == nil {
		t.Fatal("job identity conflict accepted")
	}

	job.Spec.Template.Spec.NodeName = s.NodeName
	job.UID = "failed-job-uid"
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "DeadlineExceeded"}}
	client.BatchV1().Jobs("ns").Update(context.Background(), job, metav1.UpdateOptions{})
	controller := true
	failedPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "failed-helper", Namespace: "ns", UID: "failed-helper-uid", Labels: map[string]string{"job-name": job.Name}, OwnerReferences: []metav1.OwnerReference{{Kind: "Job", Name: job.Name, UID: job.UID, Controller: &controller}}}, Spec: *job.Spec.Template.Spec.DeepCopy(), Status: corev1.PodStatus{Phase: corev1.PodFailed, ContainerStatuses: []corev1.ContainerStatus{{Name: "recovery", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error", Message: string(runtimeeviction.FailureJSON(errors.New("ledger owner or generation changed")))}}}}}}
	client.CoreV1().Pods("ns").Create(context.Background(), failedPod, metav1.CreateOptions{})
	if _, e = svc.RecoverRuntimeEviction(context.Background(), s, p); e == nil || errors.Is(e, ErrRuntimeFaultEvictionPending) {
		t.Fatalf("first terminal failure must request Retry: %v", e)
	}
	if !strings.Contains(e.Error(), "ledger") {
		t.Fatalf("missing actionable diagnostic: %v", e)
	}
	if _, e = svc.RecoverRuntimeEviction(context.Background(), s, p); !errors.Is(e, ErrRuntimeFaultEvictionPending) {
		t.Fatalf("manual Retry did not recreate failed helper: %v", e)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "delete" && action.GetResource().Resource == "jobs" {
			deletion := action.(interface{ GetDeleteOptions() metav1.DeleteOptions }).GetDeleteOptions()
			if deletion.Preconditions == nil || deletion.Preconditions.UID == nil || *deletion.Preconditions.UID != "failed-job-uid" {
				t.Fatal("delete lacks exact UID precondition")
			}
		}
	}
}

func TestRecoveryJobUsesVerifiedStorageIdentity(t *testing.T) {
	s := runtimeeviction.Scope{Version: 1, OperationID: "op", Namespace: "ns", PodName: "pod", PodUID: "uid", NodeName: "node", NodeUID: "nodeuid", NodeBootID: "boot", ContainerName: "slot-0", RuntimeType: "openclaw", GatewayID: "gw-1-1", WorkspaceRoot: "/workspaces", InstanceID: 1, UserID: 1, Generation: 1}
	for _, profile := range []string{"", "nfs-rootless-v1", "unknown"} {
		t.Run(profile, func(t *testing.T) {
			p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns", UID: "uid", Annotations: map[string]string{"clawmanager.io/fault-eviction-sandbox-profile": profile}}, Spec: corev1.PodSpec{NodeName: "node", Containers: []corev1.Container{{Name: "slot-0", VolumeMounts: []corev1.VolumeMount{{Name: "ws", MountPath: "/workspaces"}}}}, Volumes: []corev1.Volume{{Name: "ws", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}}}
			job, err := buildRuntimeEvictionJob(s, p, "helper@sha256:abc")
			if profile == "unknown" {
				if err == nil {
					t.Fatal("unknown storage identity accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			security := job.Spec.Template.Spec.Containers[0].SecurityContext
			want := int64(0)
			if profile == "nfs-rootless-v1" {
				want = 10001
			}
			if security.RunAsUser == nil || *security.RunAsUser != want {
				t.Fatalf("helper UID=%v, want %d", security.RunAsUser, want)
			}
			if profile != "" && (security.RunAsGroup == nil || *security.RunAsGroup != 0) {
				t.Fatal("helper lost CRI socket group")
			}
			if len(security.Capabilities.Add) != 0 || len(security.Capabilities.Drop) != 1 || security.Capabilities.Drop[0] != "ALL" {
				t.Fatal("helper gained capabilities")
			}
			if profile == RuntimeNFSRootlessProfile {
				old := job.Spec.Template.Spec.DeepCopy()
				root := int64(0)
				old.Containers[0].SecurityContext.RunAsUser = &root
				if trustedRecoveryTemplate(*old, job.Spec.Template.Spec) {
					t.Fatal("old root helper template accepted for rootless storage")
				}
			}
		})
	}
}
