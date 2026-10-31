package systembackup

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func durableDataFixture(t *testing.T) (Store, Leadership, Task, DataJobs, Attempt, DataJobRequest, DataOperationReceipt) {
	t.Helper()
	ctx := context.Background()
	j, _, r, receipt := dataJobFixture(t, false)
	s := Store{controlDB(t), r.Origin}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig); err != nil {
		t.Fatal(err)
	}
	task, _, err := s.Create(ctx, 1, "durable-data-job-0001", CreateTaskRequest{Type: Drill, Purpose: "functional_test", SourceID: r.BackupID}, permitTask)
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.AcquireLeadership(ctx, "data-controller-first", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	task, err = s.ClaimLedTask(ctx, l, task.ID, task.Version, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for _, next := range []string{"preflighting", "provisioning", "restoring"} {
		task, err = s.Advance(ctx, task, l.Owner, next, "none")
		if err != nil {
			t.Fatal(err)
		}
	}
	r.Binding.RunID = task.ID
	r.Binding.AttemptID = ""
	r.DeadlineAt = task.DeadlineAt
	a, err := s.PrepareAttempt(ctx, l, task, "restore", r)
	if err != nil {
		t.Fatal(err)
	}
	_, r, err = dataJob(a)
	if err != nil {
		t.Fatal(err)
	}
	receipt.DataOperationBinding = r.Binding
	return s, l, task, j, a, r, receipt
}

func replaceDataLeader(t *testing.T, s Store, task Task) (Leadership, Task) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.DB.Exec("UPDATE system_backup_controller_leases SET expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE scope=?", s.InstallationID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("UPDATE system_backup_tasks SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE public_id=?", task.ID); err != nil {
		t.Fatal(err)
	}
	l, err := s.AcquireLeadership(ctx, "data-controller-second", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	task, err = s.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	task, err = s.ClaimLedTask(ctx, l, task.ID, task.Version, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return l, task
}

func TestDataExecutionAdoptsLostCreateAndBuildsIntentOnlyFromStoppedOriginalJob(t *testing.T) {
	s, l, task, j, a, r, receipt := durableDataFixture(t)
	ctx := context.Background()
	client := j.Client.(*fake.Clientset)
	creates := 0
	client.PrependReactor("create", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		creates++
		job := action.(ktesting.CreateAction).GetObject().(*batchv1.Job).DeepCopy()
		job.UID = "original-data-job"
		if err := client.Tracker().Create(schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, job, r.Namespace); err != nil {
			t.Fatal(err)
		}
		return true, nil, errors.New("lost create response")
	})
	if _, err := j.EnsureExecution(ctx, s, l, task, a); err == nil {
		t.Fatal("fixture failed to lose response")
	}
	b, err := s.DataExecution(ctx, a.ID)
	if err != nil || !b.CreateSent || b.JobUID != "" {
		t.Fatal("send intent was not durable", b, err)
	}
	oldLeader, oldTask := l, task
	l, task = replaceDataLeader(t, s, task)
	if _, err = j.EnsureExecution(ctx, s, oldLeader, oldTask, a); !errors.Is(err, ErrFence) {
		t.Fatal("old leader bound original execution", err)
	}
	b, err = j.EnsureExecution(ctx, s, l, task, a)
	if err != nil || b.JobUID != "original-data-job" || creates != 1 {
		t.Fatal("replacement repeated restore creation", b, creates, err)
	}
	job, pod := completeDataJob(t, j, a, r, receipt)
	if _, err = client.CoreV1().Pods(r.Namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	result, err := j.Observe(ctx, a, string(job.UID))
	if err != nil || result.State != "succeeded" {
		t.Fatal(result, err)
	}
	if err = s.FinishAttempt(ctx, l, task, a, "succeeded", result); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.RestoreIntentFromExecution(ctx, task.ID); err == nil {
		t.Fatal("verification intent exposed while restore writer Pod still exists")
	}
	if stopped, err := j.StopExecution(ctx, s, l, task, a); err != nil || stopped {
		t.Fatal(stopped, err)
	}
	if stopped, err := j.StopExecution(ctx, s, l, task, a); err != nil || stopped {
		t.Fatal("deleted Job ignored lingering writer Pod", stopped, err)
	}
	if err = client.CoreV1().Pods(r.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if stopped, err := j.StopExecution(ctx, s, l, task, a); err != nil || !stopped {
		t.Fatal(stopped, err)
	}
	intent, sqlHash, err := s.RestoreIntentFromExecution(ctx, task.ID)
	if err != nil || intent.RunID != task.ID || !intent.NormalizedAt.Equal(receipt.NormalizedAt) || len(intent.HistoryParts) != 2 || sqlHash != receipt.SQLSHA256 {
		t.Fatal("lost immutable restore verification input", intent, err)
	}
	// A target report is neither supplied nor opened by this path. Mutating
	// the caller's receipt after persistence cannot change the trusted intent.
	receipt.NormalizedAt = receipt.NormalizedAt.Add(time.Hour)
	again, _, err := s.RestoreIntentFromExecution(ctx, task.ID)
	if err != nil || !again.NormalizedAt.Equal(intent.NormalizedAt) {
		t.Fatal("mutable report altered verifier intent", err)
	}
}

func TestDataExecutionUnknownCreateNeverRecreatesOrClaimsCleanup(t *testing.T) {
	s, l, task, j, a, _, _ := durableDataFixture(t)
	ctx := context.Background()
	client := j.Client.(*fake.Clientset)
	creates := 0
	client.PrependReactor("create", "jobs", func(ktesting.Action) (bool, runtime.Object, error) {
		creates++
		return true, nil, errors.New("transport unavailable")
	})
	if _, err := j.EnsureExecution(ctx, s, l, task, a); err == nil {
		t.Fatal("fixture should fail")
	}
	for i := 0; i < 2; i++ {
		if _, err := j.EnsureExecution(ctx, s, l, task, a); err == nil {
			t.Fatal("unknown restore result restarted")
		}
	}
	if creates != 1 {
		t.Fatal("unknown create repeated", creates)
	}
	if stopped, err := j.StopExecution(ctx, s, l, task, a); err == nil || stopped {
		t.Fatal("unbound absence presented as cleanup proof", stopped, err)
	}
	old, err := s.GetAttempt(ctx, task.ID, "restore")
	if err != nil {
		t.Fatal(err)
	}
	var request DataJobRequest
	if json.Unmarshal(old.Request, &request) != nil {
		t.Fatal("bad fixture request")
	}
	request.Target.UID = "changed-target"
	request.Binding.TargetUID = request.Target.UID
	if _, err = s.PrepareAttempt(ctx, l, task, "restore", request); err == nil {
		t.Fatal("frozen target identity changed")
	}
}
