package systembackup

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func uploadFixtureRequest() UploadJobRequest {
	c := captureFixtureRequest()
	return UploadJobRequest{VerifyJobRequest: c.VerifyJobRequest, BootstrapUID: c.BootstrapUID, OperationSecret: c.OperationSecret, OperationSecretUID: c.OperationSecretUID, OrdinaryStage: c.Stage, SecretStage: PVCIdentity{Name: "secrets-stage", UID: "secret-stage-uid"}}
}

func TestUploadJobFreezesReadOnlySealedStagesAndOwnedCompletion(t *testing.T) {
	ctx := context.Background()
	r := uploadFixtureRequest()
	raw, _ := json.Marshal(r)
	a := Attempt{ID: "sat_" + strings.Repeat("a", 32), TaskID: r.BackupID, Phase: "upload", Request: raw}
	yes := true
	client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: r.Namespace, UID: types.UID(r.NamespaceUID)}}, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: r.BootstrapSecret, Namespace: r.Namespace, UID: types.UID(r.BootstrapUID)}, Immutable: &yes}, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: r.OperationSecret, Namespace: r.Namespace, UID: types.UID(r.OperationSecretUID)}, Immutable: &yes}, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: r.OrdinaryStage.Name, Namespace: r.Namespace, UID: types.UID(r.OrdinaryStage.UID)}}, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: r.SecretStage.Name, Namespace: r.Namespace, UID: types.UID(r.SecretStage.UID)}})
	j := UploadJobs{Client: client}
	job, err := j.Ensure(ctx, a, "", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range job.Spec.Template.Spec.Containers[0].VolumeMounts {
		if !m.ReadOnly {
			t.Fatal("upload stage is writable")
		}
	}
	bad := job.DeepCopy()
	bad.Spec.Template.Spec.Containers[0].VolumeMounts[2].ReadOnly = false
	if matchesCaptureJob(job, bad) {
		t.Fatal("writable stage adopted")
	}
	job.UID = "original-upload"
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	if _, err = client.BatchV1().Jobs(r.Namespace).Update(ctx, job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	request := PartUploadRequest{Origin: r.Origin, BackupID: r.BackupID, AttemptID: a.ID, OrdinaryStageUID: r.OrdinaryStage.UID, SecretStageUID: r.SecretStage.UID}
	report := PartUploadReport{Schema: "system-backup-upload.v1", PartUploadRequest: request, Parts: 6, Index: ObjectReference{Key: request.indexKey(), VersionID: "immutable-v1", Bytes: 100, SHA256: strings.Repeat("a", 64)}}
	result, _ := json.Marshal(report)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "uploader-pod", Namespace: r.Namespace, Labels: job.Spec.Template.Labels, OwnerReferences: []metav1.OwnerReference{{Kind: "Job", UID: job.UID, Controller: &yes}}}, Spec: *job.Spec.Template.Spec.DeepCopy(), Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "uploader", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, Message: string(result)}}}}}}
	if _, err = client.CoreV1().Pods(r.Namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	observed, err := j.Observe(ctx, a, "original-upload")
	if err != nil || observed.State != "succeeded" || observed.Report == nil {
		t.Fatal(observed, err)
	}
	pod.Spec.Containers[0].VolumeMounts[2].ReadOnly = false
	if _, err = client.CoreV1().Pods(r.Namespace).Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = j.Observe(ctx, a, "original-upload"); PublicError(err) != "isolation_invalid" {
		t.Fatal("actual writable execution accepted", err)
	}
	if _, err = j.Stop(ctx, a, "replacement-upload"); PublicError(err) != "cleanup_unsafe_target" {
		t.Fatal("replacement Job deleted", err)
	}
	if err = client.BatchV1().Jobs(r.Namespace).Delete(ctx, job.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if done, err := j.Stop(ctx, a, "original-upload"); err != nil || done {
		t.Fatal("owned Pod was still running", done, err)
	}
	if _, err = j.Ensure(ctx, a, "original-upload", false); err == nil {
		t.Fatal("missing bound Job recreated")
	}
}
