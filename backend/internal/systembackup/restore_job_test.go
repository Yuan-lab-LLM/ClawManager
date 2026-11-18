package systembackup

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func dataJobFixture(t *testing.T, verify bool) (DataJobs, Attempt, DataJobRequest, DataOperationReceipt) {
	t.Helper()
	b, full := receiptFixture(t)
	phase := "restore"
	if verify {
		b.Operation = "verify-restored"
		phase = "verify"
		b.IntentSHA256, _ = RequestHash(RestoreVerificationIntent{RunID: b.RunID, NormalizedAt: full.StartedAt, HistoryParts: full.HistoryParts})
	}
	base := verifyFixtureRequest()
	base.Origin = b.Origin
	base.BackupID = b.BackupID
	base.BootstrapUID = "bootstrap-uid"
	base.DeadlineAt = time.Now().UTC().Add(time.Minute)
	base.MaxItems = 10000
	r := DataJobRequest{VerifyJobRequest: base, Binding: b, OperationSecret: "isolated-target-config", OperationSecretUID: "target-config-uid", Target: PVCIdentity{Name: "owned-target", UID: b.TargetUID}}
	raw, _ := json.Marshal(r)
	a := Attempt{ID: b.AttemptID, TaskID: b.RunID, Phase: phase, Request: raw}
	yes := true
	client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: r.Namespace, UID: types.UID(r.NamespaceUID)}}, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: r.BootstrapSecret, Namespace: r.Namespace, UID: types.UID(r.BootstrapUID)}, Immutable: &yes}, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: r.OperationSecret, Namespace: r.Namespace, UID: types.UID(r.OperationSecretUID)}, Immutable: &yes}, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: r.Target.Name, Namespace: r.Namespace, UID: types.UID(r.Target.UID)}})
	receipt, err := dataReceipt(b, full.SQL, full.StartedAt, 4, 1, 1, full.HistoryParts)
	if err != nil {
		t.Fatal(err)
	}
	return DataJobs{Client: client}, a, r, receipt
}

func completeDataJob(t *testing.T, j DataJobs, a Attempt, r DataJobRequest, receipt DataOperationReceipt) (*batchv1.Job, *corev1.Pod) {
	t.Helper()
	ctx := context.Background()
	job, err := j.Ensure(ctx, a, "", true)
	if err != nil {
		t.Fatal(err)
	}
	job.UID = "original-data-job"
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	job, err = j.Client.BatchV1().Jobs(r.Namespace).Update(ctx, job, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	raw, _ := json.Marshal(receipt)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "data-pod", Namespace: r.Namespace, Labels: job.Spec.Template.Labels, OwnerReferences: []metav1.OwnerReference{{Kind: "Job", UID: job.UID, Controller: &yes}}}, Spec: *job.Spec.Template.Spec.DeepCopy(), Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "data-operation", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, Message: string(raw)}}}}}}
	return job, pod
}

func TestDataJobsBindExecutionAndKeepIndependentVerifierReadOnly(t *testing.T) {
	for _, verify := range []bool{false, true} {
		j, a, r, receipt := dataJobFixture(t, verify)
		job, pod := completeDataJob(t, j, a, r, receipt)
		if pod.Spec.SecurityContext.FSGroup != nil || len(pod.Spec.Volumes) != 3 || len(pod.Spec.Containers[0].VolumeMounts) != 3 || pod.Spec.Containers[0].VolumeMounts[2].ReadOnly != verify || pod.Spec.Volumes[2].PersistentVolumeClaim.ReadOnly != verify {
			t.Fatal("target mount or identity permissions drift")
		}
		if verify && len(pod.Spec.Containers[0].SecurityContext.Capabilities.Add) != 1 {
			t.Fatal("verifier gained chown permission")
		}
		ctx := context.Background()
		if _, err := j.Client.CoreV1().Pods(r.Namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		got, err := j.Observe(ctx, a, string(job.UID))
		if err != nil || got.State != "succeeded" || got.Report.DataOperationBinding != r.Binding || got.Report.AcceptanceEligible {
			t.Fatal("bound process result rejected", got, err)
		}
		if err = j.Client.BatchV1().Jobs(r.Namespace).Delete(ctx, job.Name, metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err = j.Ensure(ctx, a, string(job.UID), false); err == nil {
			t.Fatal("lost restore job recreated into partial target")
		}
		if stopped, err := j.Stop(ctx, a, string(job.UID)); err != nil || stopped {
			t.Fatal("cleanup forgot still-owned Pod", stopped, err)
		}
		if err = j.Client.CoreV1().Pods(r.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
		if stopped, err := j.Stop(ctx, a, string(job.UID)); err != nil || !stopped {
			t.Fatal(stopped, err)
		}
	}
}

func TestDataJobsRejectPodOrReceiptDrift(t *testing.T) {
	for _, mutate := range []func(*corev1.Pod, *DataOperationReceipt){
		func(p *corev1.Pod, r *DataOperationReceipt) { p.Spec.Containers[0].VolumeMounts[2].ReadOnly = false },
		func(p *corev1.Pod, r *DataOperationReceipt) { p.Spec.Volumes[2].PersistentVolumeClaim.ReadOnly = false },
		func(p *corev1.Pod, r *DataOperationReceipt) {
			p.Spec.Containers[0].Args = append(p.Spec.Containers[0].Args, "--timeout", "12h")
		},
		func(p *corev1.Pod, r *DataOperationReceipt) { r.TargetUID = "another-target" },
		func(p *corev1.Pod, r *DataOperationReceipt) { r.AcceptanceEligible = true },
		func(p *corev1.Pod, r *DataOperationReceipt) { r.NormalizedAt = r.NormalizedAt.Add(time.Second) },
		func(p *corev1.Pod, r *DataOperationReceipt) { r.HistoryParts[1] = r.HistoryParts[0] },
	} {
		j, a, r, receipt := dataJobFixture(t, true)
		job, pod := completeDataJob(t, j, a, r, receipt)
		mutate(pod, &receipt)
		raw, _ := json.Marshal(receipt)
		pod.Status.ContainerStatuses[0].State.Terminated.Message = string(raw)
		if _, err := j.Client.CoreV1().Pods(r.Namespace).Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := j.Observe(context.Background(), a, string(job.UID)); err == nil {
			t.Fatal("mutated execution or proof adopted")
		}
	}
}
