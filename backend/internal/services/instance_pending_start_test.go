package services

import (
	"context"
	"encoding/json"
	"testing"

	"clawreef/internal/models"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type pendingStartInstanceRepo struct {
	*fakeRuntimeInstanceRepo
	rejectResume bool
}

func (r *pendingStartInstanceRepo) ResumeUnpublishedRuntimeStart(_ context.Context, expected models.Instance, _ models.InstanceRuntimeBinding) (bool, error) {
	if r.rejectResume {
		return false, nil
	}
	r.runtimeStates[expected.ID] = fakeRuntimeState{status: "creating", generation: expected.RuntimeGeneration}
	return true, nil
}

func TestExplicitStartResumesUnpublishedPendingReservation(t *testing.T) {
	f := newPendingSlotReplayFixture(t)
	f.seed(t, 1, "stopped")
	// The completed predecessor is in the old pool; the unpublished successor
	// already reserved a slot in the newly deployed pool.
	cm, _ := f.kube.CoreV1().ConfigMaps("runtime").Get(context.Background(), "lite-instance-7", metav1.GetOptions{})
	cm.Data["endpoint"], cm.Data["pod_uid"] = "https://10.0.0.3:19090", "previous-pod"
	var previous RuntimeAgentCreateGatewayRequest
	json.Unmarshal([]byte(cm.Data["request"]), &previous)
	previous.PodUID = "previous-pod"
	raw, _ := json.Marshal(previous)
	cm.Data["request"] = string(raw)
	if _, err := f.kube.CoreV1().ConfigMaps("runtime").Update(context.Background(), cm, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	f.instance.Status = "stopped"
	repo := &pendingStartInstanceRepo{fakeRuntimeInstanceRepo: newFakeRuntimeInstanceRepo()}
	repo.byID[7] = &f.instance
	s := &instanceService{instanceRepo: repo, bindingRepo: f.bindings, runtimePodRepo: f.pods, agentClient: f.client}
	configureLifecycleModels(t, s)
	if err := s.Start(7); err != nil {
		t.Fatalf("explicit retry attempted to delete an unpublished gateway: %v", err)
	}
	if got := repo.runtimeStates[7]; got.status != "creating" || got.generation != 2 {
		t.Fatalf("retry abandoned its existing generation: %+v", got)
	}
	f.assertReservationUnchanged(t)
	if len(f.requests) != 0 || f.envCalls != 0 {
		t.Fatal("API retry published credentials or launched a duplicate")
	}
	if errs := f.scheduler.reconcileCreatingInstance(context.Background(), f.instance); len(errs) != 0 {
		t.Fatal(errs)
	}
	if len(f.requests) != 1 || f.requests[0].Generation != 2 || f.binding.State != "running" {
		t.Fatal("scheduler did not resume the original pending reservation")
	}
}

func TestExplicitStartDoesNotResumeUnprovenPendingReservation(t *testing.T) {
	for _, kind := range []string{"old-active", "already-published", "orphan-secret", "wrong-workspace", "wrong-user", "wrong-runtime", "stale-target", "unready-target", "newer-stop", "missing-conditional-repository"} {
		t.Run(kind, func(t *testing.T) {
			f := newPendingSlotReplayFixture(t)
			gen, state := 1, "stopped"
			if kind == "old-active" {
				state = "running"
			}
			if kind == "already-published" {
				gen, state = 2, "running"
			}
			f.seed(t, gen, state)
			f.instance.Status = "stopped"
			switch kind {
			case "orphan-secret":
				if _, err := f.kube.CoreV1().Secrets("runtime").Create(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "lite-instance-7-g2"}}, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			case "wrong-workspace":
				f.binding.WorkspacePath += "/other"
			case "wrong-user":
				f.instance.UserID++
			case "wrong-runtime":
				f.binding.RuntimeType = "hermes"
			case "stale-target":
				pod, _ := f.kube.CoreV1().Pods("runtime").Get(context.Background(), "pool", metav1.GetOptions{})
				pod.UID = "replacement-pod"
				f.kube.CoreV1().Pods("runtime").Update(context.Background(), pod, metav1.UpdateOptions{})
			case "unready-target":
				f.pod.State = "unhealthy"
			}
			repo := &pendingStartInstanceRepo{fakeRuntimeInstanceRepo: newFakeRuntimeInstanceRepo(), rejectResume: kind == "newer-stop"}
			repo.byID[7] = &f.instance
			s := &instanceService{instanceRepo: repo, bindingRepo: f.bindings, runtimePodRepo: f.pods, agentClient: f.client}
			if kind == "missing-conditional-repository" {
				s.instanceRepo = repo.fakeRuntimeInstanceRepo
			}
			configureLifecycleModels(t, s)
			if kind == "already-published" {
				resumed, err := s.resumeUnpublishedV2Start(context.Background(), &f.instance, "openclaw")
				if err != nil || resumed || len(repo.runtimeStates) != 0 || len(f.requests) != 0 {
					t.Fatal("published immutable generation was treated as unpublished", err)
				}
				f.assertReservationUnchanged(t)
				return
			}
			if err := s.Start(7); err == nil {
				t.Fatal("unproven reservation resumed")
			}
			if len(repo.runtimeStates) != 0 || len(f.requests) != 0 || f.envCalls != 0 || f.bindings.bindings[7] == nil {
				t.Fatal("failed retry changed state or discarded the reservation")
			}
			f.assertReservationUnchanged(t)
		})
	}
}
