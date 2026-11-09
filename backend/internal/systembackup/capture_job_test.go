package systembackup

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func captureFixtureRequest() CaptureJobRequest {
	base := verifyFixtureRequest()
	base.DeadlineAt = time.Now().UTC().Add(time.Minute)
	base.BackupID = "sbk_" + strings.Repeat("3", 32)
	return CaptureJobRequest{VerifyJobRequest: base, Phase: "capture", Gate: GateOwnership{Scope: "installation", Owner: base.BackupID, Generation: 1, Token: 2}, BootstrapUID: "bootstrap-uid", OperationSecret: "capture-source", OperationSecretUID: "source-uid", Stage: PVCIdentity{Name: "capture-stage", UID: "stage-uid"}, SourceVolumes: []CaptureSourceVolume{{PVCIdentity: PVCIdentity{Name: "source-files", UID: "workspace-uid"}, ID: "workspaces"}}, MaxItems: 10000}
}
func TestCaptureJobsBindUIDsAndNeverRecreateALostExecution(t *testing.T) {
	r := captureFixtureRequest()
	raw, _ := json.Marshal(r)
	a := Attempt{ID: "sat_" + strings.Repeat("2", 32), TaskID: r.BackupID, Phase: "capture", Request: raw}
	yes := true
	client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: r.Namespace, UID: types.UID(r.NamespaceUID)}}, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: r.BootstrapSecret, Namespace: r.Namespace, UID: types.UID(r.BootstrapUID)}, Immutable: &yes}, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: r.OperationSecret, Namespace: r.Namespace, UID: types.UID(r.OperationSecretUID)}, Immutable: &yes}, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: r.Stage.Name, Namespace: r.Namespace, UID: types.UID(r.Stage.UID)}}, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: r.SourceVolumes[0].Name, Namespace: r.Namespace, UID: types.UID(r.SourceVolumes[0].UID)}})
	j := CaptureJobs{Client: client}
	ctx := context.Background()
	job, err := j.Ensure(ctx, a, "", true)
	if err != nil {
		t.Fatal(err)
	}
	job.UID = "job-uid"
	if _, err = client.BatchV1().Jobs(r.Namespace).Update(ctx, job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = j.Ensure(ctx, a, "different-uid", false); err == nil {
		t.Fatal("replaced execution adopted")
	}
	if _, err = j.Ensure(ctx, a, "job-uid", false); err != nil {
		t.Fatal(err)
	}
	if err = client.BatchV1().Jobs(r.Namespace).Delete(ctx, job.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = j.Ensure(ctx, a, "job-uid", false); err == nil {
		t.Fatal("lost capture Job recreated")
	}
	jobs, _ := client.BatchV1().Jobs(r.Namespace).List(ctx, metav1.ListOptions{})
	if len(jobs.Items) != 0 {
		t.Fatal("duplicate source read started")
	}
}
func TestCaptureJobRejectsWritableSourceAndCredentialDrift(t *testing.T) {
	r := captureFixtureRequest()
	raw, _ := json.Marshal(r)
	a := Attempt{ID: "sat_" + strings.Repeat("2", 32), TaskID: r.BackupID, Phase: "capture", Request: raw}
	want, _, err := captureJob(a)
	if err != nil {
		t.Fatal(err)
	}
	if want.Spec.Template.Spec.SecurityContext.FSGroup != nil {
		t.Fatal("capture may change source volume groups")
	}
	bad := want.DeepCopy()
	bad.Spec.Template.Spec.Containers[0].VolumeMounts[3].ReadOnly = false
	if matchesCaptureJob(want, bad) {
		t.Fatal("writable source volume adopted")
	}
	bad = want.DeepCopy()
	bad.Spec.Template.Labels["backup.clawmanager.io/role"] = "unrestricted"
	if matchesCaptureJob(want, bad) {
		t.Fatal("changed isolation selector adopted")
	}
	r.SourceVolumes[0].UID = r.Stage.UID
	raw, _ = json.Marshal(r)
	a.Request = raw
	if _, _, err = captureJob(a); err == nil {
		t.Fatal("source volume reused as writable staging")
	}
}
