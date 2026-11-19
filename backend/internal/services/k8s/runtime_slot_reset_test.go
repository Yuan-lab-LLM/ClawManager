package k8s

import (
	"clawreef/internal/runtimeeviction"
	"context"
	"encoding/json"
	"errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"strings"
	"testing"
	"time"
)

func TestPhysicalSlotResetHistoricalReceiptIsReadOnly(t *testing.T) {
	scope := runtimeeviction.SlotScope{Version: 1, OperationID: "reset-op", Namespace: "runtime", PodName: "old", PodUID: "old-uid", NodeName: "node", NodeUID: "node-uid", NodeBootID: "boot", ContainerName: "slot-0", ContainerID: "containerd://" + strings.Repeat("a", 64)}
	image := "registry/helper@sha256:" + strings.Repeat("b", 64)
	job, err := BuildRuntimeSlotResetJob(scope, image)
	if err != nil {
		t.Fatal(err)
	}
	job.UID = types.UID("job-uid")
	receipt := runtimeeviction.SlotReceipt{ScopeDigest: runtimeeviction.SlotScopeDigest(scope), OperationID: scope.OperationID, PodUID: scope.PodUID, ContainerName: scope.ContainerName, ContainerID: scope.ContainerID, NodeUID: scope.NodeUID, NodeBootID: scope.NodeBootID, FinishedAt: time.Now().UTC(), Confirmed: true}
	b, _ := json.Marshal(receipt)
	controller := true
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "helper-pod", Namespace: scope.Namespace, Labels: map[string]string{"job-name": job.Name}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID, Controller: &controller}}}, Spec: job.Spec.Template.Spec, Status: corev1.PodStatus{Phase: corev1.PodSucceeded, ContainerStatuses: []corev1.ContainerStatus{{Name: "recovery", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, Message: string(b)}}}}}}
	kube := fake.NewSimpleClientset(job, pod)
	svc := &runtimeDeploymentService{client: kube}
	got, err := svc.PhysicalSlotResetReceipt(context.Background(), scope, image)
	if err != nil || got == nil {
		t.Fatalf("lost historical exact receipt: %+v %v", got, err)
	}
	for _, action := range kube.Actions() {
		if action.GetVerb() != "get" && action.GetVerb() != "list" {
			t.Fatalf("historical lookup mutated resources: %s", action.GetVerb())
		}
	}
	// No original Pod or Node objects exist. A read of the exact completed Job
	// must still work without making a replacement into a new stop target.
}

func TestPhysicalSlotResetFailedBeforeContainerStartsCanRetry(t *testing.T) {
	scope := runtimeeviction.SlotScope{Version: 1, OperationID: "reset-failed", Namespace: "runtime", PodName: "old", PodUID: "old-uid", NodeName: "node", NodeUID: "node-uid", NodeBootID: "boot", ContainerName: "slot-0", ContainerID: "containerd://" + strings.Repeat("a", 64)}
	image := "registry/helper@sha256:" + strings.Repeat("b", 64)
	job, err := BuildRuntimeSlotResetJob(scope, image)
	if err != nil {
		t.Fatal(err)
	}
	job.UID = "failed-job-uid"
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "DeadlineExceeded"}}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: scope.NodeName, UID: types.UID(scope.NodeUID)}, Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{BootID: scope.NodeBootID}}}
	kube := fake.NewSimpleClientset(job, node)
	svc := &runtimeDeploymentService{client: kube}
	ctx := context.Background()
	if _, err = svc.ResetPhysicalSlot(ctx, scope, image); err == nil || errors.Is(err, ErrRuntimeFaultEvictionPending) {
		t.Fatalf("terminal helper failure was hidden: %v", err)
	}
	stored, err := kube.BatchV1().Jobs(scope.Namespace).Get(ctx, job.Name, metav1.GetOptions{})
	if err != nil || stored.Annotations["clawmanager.io/slot-reset-failure-observed"] != string(job.UID) {
		t.Fatalf("exact failure not checkpointed: %v", err)
	}
	if _, err = svc.ResetPhysicalSlot(ctx, scope, image); !errors.Is(err, ErrRuntimeFaultEvictionPending) {
		t.Fatalf("retry did not retire exact failed helper: %v", err)
	}
	found := false
	for _, action := range kube.Actions() {
		if action.GetVerb() == "delete" {
			found = true
			deleted := action.(interface{ GetDeleteOptions() metav1.DeleteOptions }).GetDeleteOptions()
			if deleted.Preconditions == nil || deleted.Preconditions.UID == nil || *deleted.Preconditions.UID != job.UID {
				t.Fatal("failed helper deletion lost UID precondition")
			}
		}
	}
	if !found {
		t.Fatal("failed helper retained until TTL instead of allowing retry")
	}
}
