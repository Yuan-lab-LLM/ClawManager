package systembackup

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func verifyFixtureRequest() VerifyJobRequest {
	return VerifyJobRequest{Namespace: "restore-fixture", NamespaceUID: "namespace-uid", Image: "registry.invalid/backup@sha256:" + strings.Repeat("a", 64), ServiceAccount: "verifier", BootstrapSecret: "verifier-bootstrap", Origin: "verify-installation", BackupID: "sbk_" + strings.Repeat("1", 32), MaxBytes: 1 << 20, DeadlineSeconds: 60}
}

func TestVerifyJobRejectsChangedIsolationAndResourceBudget(t *testing.T) {
	raw, _ := json.Marshal(verifyFixtureRequest())
	a := Attempt{ID: "sat_" + strings.Repeat("2", 32), TaskID: "sav_" + strings.Repeat("3", 32), Request: raw}
	want, _, e := verifyJob(a)
	if e != nil {
		t.Fatal(e)
	}
	for name, mutate := range map[string]func(*batchv1.Job){
		"network-policy-label": func(j *batchv1.Job) { delete(j.Spec.Template.Labels, "backup.clawmanager.io/attempt") },
		"task-label":           func(j *batchv1.Job) { j.Labels["backup.clawmanager.io/task"] = "other" },
		"parallelism":          func(j *batchv1.Job) { v := int32(2); j.Spec.Parallelism = &v },
		"completions":          func(j *batchv1.Job) { v := int32(2); j.Spec.Completions = &v },
		"resource-limits":      func(j *batchv1.Job) { j.Spec.Template.Spec.Containers[0].Resources.Limits = nil },
		"lifecycle-exec": func(j *batchv1.Job) {
			j.Spec.Template.Spec.Containers[0].Lifecycle = &corev1.Lifecycle{PostStart: &corev1.LifecycleHandler{Exec: &corev1.ExecAction{Command: []string{"/bin/sh"}}}}
		},
		"probe-exec": func(j *batchv1.Job) {
			j.Spec.Template.Spec.Containers[0].LivenessProbe = &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"/bin/sh"}}}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := want.DeepCopy()
			mutate(got)
			if matchesVerifyJob(want, got) {
				t.Fatal("changed frozen execution adopted")
			}
		})
	}
}
func TestVerifyJobAdoptionCollisionAndBoundResult(t *testing.T) {
	ctx := context.Background()
	request := verifyFixtureRequest()
	raw, _ := json.Marshal(request)
	a := Attempt{ID: "sat_" + strings.Repeat("2", 32), TaskID: "sav_" + strings.Repeat("3", 32), Request: raw}
	client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: request.Namespace, UID: types.UID(request.NamespaceUID)}})
	j := VerifyJobs{Client: client}
	created, e := j.Ensure(ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	created.UID = "job-uid"
	created.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	if _, e = client.BatchV1().Jobs(request.Namespace).Update(ctx, created, metav1.UpdateOptions{}); e != nil {
		t.Fatal(e)
	}
	if _, e = j.Ensure(ctx, a); e != nil {
		t.Fatal("lost-response adoption", e)
	}
	list, _ := client.BatchV1().Jobs(request.Namespace).List(ctx, metav1.ListOptions{})
	if len(list.Items) != 1 {
		t.Fatal("duplicate execution")
	}
	if _, e = j.Observe(ctx, a); e == nil {
		t.Fatal("exit status accepted without bounded report")
	}
	yes := true
	report := IntegrityReport{BackupID: request.BackupID, Origin: request.Origin, Purpose: "functional_test", Consistency: "best_effort", IntegrityVerified: true, PartsVerified: 6}
	message, _ := json.Marshal(report)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "verify-pod", Namespace: request.Namespace, Labels: created.Labels, OwnerReferences: []metav1.OwnerReference{{UID: created.UID, Kind: "Job", Controller: &yes}}}, Spec: *created.Spec.Template.Spec.DeepCopy(), Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "verify", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, Message: string(message)}}}}}}
	if _, e = client.CoreV1().Pods(request.Namespace).Create(ctx, pod, metav1.CreateOptions{}); e != nil {
		t.Fatal(e)
	}
	result, e := j.Observe(ctx, a)
	if e != nil || result.State != "succeeded" || result.Report.AcceptanceEligible {
		t.Fatal(result, e)
	}
	pod.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "UNREGISTERED_SOURCE", Value: "forbidden"}}
	client.CoreV1().Pods(request.Namespace).Update(ctx, pod, metav1.UpdateOptions{})
	if _, e = j.Observe(ctx, a); e == nil {
		t.Fatal("report accepted from Pod with substituted runtime configuration")
	}
	pod.Spec = *created.Spec.Template.Spec.DeepCopy()
	client.CoreV1().Pods(request.Namespace).Update(ctx, pod, metav1.UpdateOptions{})
	// A matching annotation does not authorize a substituted command/image.
	created.Spec.Template.Spec.Containers[0].Command = []string{"/bin/sh"}
	client.BatchV1().Jobs(request.Namespace).Update(ctx, created, metav1.UpdateOptions{})
	if _, e = j.Ensure(ctx, a); e == nil {
		t.Fatal("mutated Job adopted")
	}
	if _, e = j.Stop(ctx, a); e == nil {
		t.Fatal("foreign Job deleted")
	}
}

type observedJobs struct {
	ids          []string
	stopIDs      []string
	stopAttempts []Attempt
	result       VerifyJobResult
	stop         bool
}

func TestVerificationCleanupRejectsReplacedJobUID(t *testing.T) {
	ctx := context.Background()
	raw, _ := json.Marshal(verifyFixtureRequest())
	result, _ := json.Marshal(VerifyJobResult{State: "succeeded", JobUID: "original-job-uid"})
	a := Attempt{ID: "sat_" + strings.Repeat("2", 32), TaskID: "sav_" + strings.Repeat("3", 32), Status: "succeeded", Request: raw, Result: result}
	job, request, e := verifyJob(a)
	if e != nil {
		t.Fatal(e)
	}
	job.UID = "replacement-job-uid"
	client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: request.Namespace, UID: types.UID(request.NamespaceUID)}}, job)
	if _, e := (VerifyJobs{Client: client}).Stop(ctx, a); e == nil {
		t.Fatal("cleanup deleted a replacement Job despite the persisted original UID")
	}
	if _, e := client.BatchV1().Jobs(request.Namespace).Get(ctx, job.Name, metav1.GetOptions{}); e != nil {
		t.Fatal("replacement Job was deleted", e)
	}
	job.UID = "original-job-uid"
	if _, e = client.BatchV1().Jobs(request.Namespace).Update(ctx, job, metav1.UpdateOptions{}); e != nil {
		t.Fatal(e)
	}
	if _, e = (VerifyJobs{Client: client}).Stop(ctx, a); e != nil {
		t.Fatal("matching original Job could not be cleaned", e)
	}
}

func TestVerificationCleanupWaitsForOriginalPodsAfterJobDisappears(t *testing.T) {
	ctx := context.Background()
	r := verifyFixtureRequest()
	raw, _ := json.Marshal(r)
	a := Attempt{ID: "sat_" + strings.Repeat("b", 32), TaskID: "sav_" + strings.Repeat("c", 32), Phase: "verify", Status: "succeeded", Request: raw}
	a.Result, _ = json.Marshal(VerifyJobResult{State: "succeeded", JobUID: "original-verify"})
	yes := true
	client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: r.Namespace, UID: types.UID(r.NamespaceUID)}}, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "remaining-verifier", Namespace: r.Namespace, Labels: map[string]string{"backup.clawmanager.io/attempt": a.ID}, OwnerReferences: []metav1.OwnerReference{{Kind: "Job", Name: "backup-verify-" + a.ID[4:], UID: "original-verify", Controller: &yes}}}})
	jobs := VerifyJobs{Client: client}
	if done, err := jobs.Stop(ctx, a); err != nil || done {
		t.Fatal("Job absence was mistaken for stopped Pods", done, err)
	}
	if err := client.CoreV1().Pods(r.Namespace).Delete(ctx, "remaining-verifier", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if done, err := jobs.Stop(ctx, a); err != nil || !done {
		t.Fatal("completed owned cleanup did not converge", done, err)
	}
}

func TestVerificationSuccessWaitsForOwnedRunnerCleanup(t *testing.T) {
	ctx := context.Background()
	s := Store{DB: controlDB(t), InstallationID: "verify-installation"}
	if e := s.Bootstrap(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig); e != nil {
		t.Fatal(e)
	}
	task, _, e := s.Create(ctx, 1, "verify-cleanup-test", CreateTaskRequest{Type: ArtifactVerify, Purpose: "functional_test", SourceID: verifyFixtureRequest().BackupID}, permitTask)
	if e != nil {
		t.Fatal(e)
	}
	jobs := &observedJobs{result: VerifyJobResult{State: "succeeded", JobUID: "uid", Report: &IntegrityReport{BackupID: task.SourceID, Origin: s.InstallationID, IntegrityVerified: true, PartsVerified: 6}}}
	c := Controller{Store: s, Owner: "cleanup-controller", PollInterval: time.Second, Verifier: &VerifyTaskRunner{Jobs: jobs, Template: verifyFixtureRequest()}}
	if e = c.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	pending, e := s.Get(ctx, task.ID)
	if e != nil || pending.Status != "running" {
		t.Fatal("success reported before physical cleanup", pending.Status, e)
	}
	attempt, e := s.GetAttempt(ctx, task.ID, "verify")
	if e != nil || attempt.Status != "succeeded" {
		t.Fatal("result not durable before cleanup", e)
	}
	if len(jobs.stopAttempts) != 1 || jobs.stopAttempts[0].Status != "succeeded" || string(jobs.stopAttempts[0].Result) != string(attempt.Result) {
		t.Fatal("first cleanup did not receive the persisted Job identity")
	}
	// Subsequent ticks use the durable result, never start another verifier.
	jobs.result = VerifyJobResult{State: "failed"}
	jobs.stop = true
	if e = c.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	done, e := s.Get(ctx, task.ID)
	if e != nil || done.Status != "succeeded" || len(jobs.ids) != 1 {
		t.Fatal("cleanup did not settle original result", done.Status, e)
	}
	if _, e = s.VerificationReport(ctx, task.ID); e != nil {
		t.Fatal("cleanup lost report", e)
	}
}

func (j *observedJobs) Observe(_ context.Context, a Attempt) (VerifyJobResult, error) {
	j.ids = append(j.ids, a.ID)
	return j.result, nil
}
func (j *observedJobs) Stop(_ context.Context, a Attempt) (bool, error) {
	j.stopIDs = append(j.stopIDs, a.ID)
	j.stopAttempts = append(j.stopAttempts, a)
	return j.stop, nil
}
func TestDurableVerificationControllerAdoptsAttemptAfterLeaderLoss(t *testing.T) {
	ctx := context.Background()
	s := Store{DB: controlDB(t), InstallationID: "verify-installation"}
	if e := s.Bootstrap(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig); e != nil {
		t.Fatal(e)
	}
	task, _, e := s.Create(ctx, 1, "verify-adoption-test", CreateTaskRequest{Type: ArtifactVerify, Purpose: "functional_test", SourceID: verifyFixtureRequest().BackupID}, permitTask)
	if e != nil {
		t.Fatal(e)
	}
	changed := testConfig()
	changed.MaxArtifactBytes = 8 << 30
	if _, e = s.SaveConfig(ctx, 1, 1, changed, permitConfig); e != nil {
		t.Fatal(e)
	}
	jobs := &observedJobs{stop: true, result: VerifyJobResult{State: "running", JobUID: "uid"}}
	runner := &VerifyTaskRunner{Jobs: jobs, Template: verifyFixtureRequest()}
	c := Controller{Store: s, Owner: "first-controller", PollInterval: time.Second, Verifier: runner}
	if e = c.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	a, e := s.GetAttempt(ctx, task.ID, "verify")
	if e != nil {
		t.Fatal(e)
	}
	var wire map[string]json.RawMessage
	if json.Unmarshal(a.Request, &wire) != nil || wire["deadline_at"] == nil {
		t.Fatal("attempt does not bind the absolute task deadline")
	}
	var frozen VerifyJobRequest
	if json.Unmarshal(a.Request, &frozen) != nil || frozen.MaxBytes != testConfig().MaxArtifactBytes || !frozen.DeadlineAt.Equal(task.DeadlineAt) {
		t.Fatal("queued task used changed configuration")
	}
	if len(jobs.ids) != 1 || jobs.ids[0] != a.ID {
		t.Fatal("missing frozen intent")
	}
	first, e := s.AcquireLeadership(ctx, c.Owner, 30*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	oldTask, _ := s.Get(ctx, task.ID)
	if _, e = s.DB.Exec("UPDATE system_backup_controller_leases SET expires_at=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6))"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("UPDATE system_backup_tasks SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE public_id=?", task.ID); e != nil {
		t.Fatal(e)
	}
	c.Owner = "replacement-controller"
	jobs.result = VerifyJobResult{State: "succeeded", JobUID: "uid", Report: &IntegrityReport{BackupID: task.SourceID, Origin: s.InstallationID, IntegrityVerified: true, PartsVerified: 6}}
	if e = c.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	finished, e := s.Get(ctx, task.ID)
	if e != nil || finished.Status != "succeeded" || finished.Eligible || !finished.DeadlineAt.Equal(task.DeadlineAt) {
		t.Fatal(finished, e)
	}
	if len(jobs.ids) != 2 || jobs.ids[0] != jobs.ids[1] {
		t.Fatal("takeover launched a second attempt")
	}
	report, e := s.VerificationReport(ctx, task.ID)
	if e != nil || report.BackupID != task.SourceID || !report.IntegrityVerified || report.AcceptanceEligible {
		t.Fatal("safe report unavailable", report, e)
	}
	if e = s.FinishAttempt(ctx, first, oldTask, a, "failed", map[string]string{"reason": "stale"}); !errors.Is(e, ErrFence) {
		t.Fatal("stale leader changed result", e)
	}
	var count int
	if e = s.DB.QueryRow("SELECT COUNT(*) FROM system_backup_attempts WHERE task_id=?", task.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	// Running cancellation waits for physical Job deletion confirmation.
	second, _, e := s.Create(ctx, 1, "verify-cancel-test", CreateTaskRequest{Type: ArtifactVerify, Purpose: "functional_test", SourceID: task.SourceID}, permitTask)
	if e != nil {
		t.Fatal(e)
	}
	jobs.result = VerifyJobResult{State: "running"}
	jobs.stop = false
	if e = c.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	second, _ = s.Get(ctx, second.ID)
	if _, e = s.Cancel(ctx, second.ID, second.Version, 1); e != nil {
		t.Fatal(e)
	}
	if e = c.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	second, _ = s.Get(ctx, second.ID)
	if second.Status != "canceling" {
		t.Fatal("canceled before Job stopped")
	}
	jobs.stop = true
	if e = c.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	second, _ = s.Get(ctx, second.ID)
	if second.Status != "canceled" {
		t.Fatal(second)
	}
}
