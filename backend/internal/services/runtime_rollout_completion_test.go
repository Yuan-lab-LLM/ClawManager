package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
)

type completionDeployment struct {
	checkpointDeployment
	inventoryErr error
	webReady     bool
}

func TestPoolCompletionSafetyMatrix(t *testing.T) {
	for _, scenario := range []string{"old-live", "deployment-not-ready", "web-not-ready", "missing-agent", "missing-heartbeat", "stale-heartbeat", "wrong-uid", "agent-draining", "wrong-agent-image", "wrong-pod-image", "pod-not-ready", "no-live-pods", "missing-replica", "deployment-replaced", "deployment-deleted", "inventory-error", "pod-list-error", "legacy-agent", "legacy-agent-ip-mismatch", "unrelated-pool"} {
		t.Run(scenario, func(t *testing.T) {
			s, repo, dep, r := completionFixture(t)
			agents := s.podRepo.(*fakeRuntimePodRepo).pods
			wantFinished, wantFailed, wantErr := false, false, false
			switch scenario {
			case "old-live":
				old := dep.pods[0]
				old.PodName, old.PodUID, old.ImageRef = "old", "old-uid", "repo:old"
				dep.pods = append(dep.pods, old)
			case "deployment-not-ready":
				dep.targets[0].Ready = false
			case "web-not-ready":
				dep.webReady = false
			case "missing-agent":
				delete(agents, 310)
			case "missing-heartbeat":
				agents[310].LastSeenAt = nil
			case "stale-heartbeat":
				stale := time.Now().Add(-7 * time.Minute)
				agents[310].LastSeenAt = &stale
			case "wrong-uid":
				agents[310].PodUID = ptrString("another-uid")
			case "agent-draining":
				agents[310].Draining = true
			case "wrong-agent-image":
				agents[310].ImageRef = "repo:old"
			case "wrong-pod-image":
				dep.pods[0].ImageRef = "repo:old"
			case "pod-not-ready":
				dep.pods[0].State = "pending"
			case "no-live-pods":
				dep.pods = nil
			case "missing-replica":
				dep.pods[0].DesiredReplicas = 2
			case "deployment-replaced":
				dep.targets[0].UID = "replacement"
				wantFailed = true
			case "deployment-deleted":
				dep.targets = nil
				wantFailed = true
			case "inventory-error":
				dep.inventoryErr = errors.New("API unavailable")
				wantErr = true
			case "pod-list-error":
				dep.listErr = errors.New("Pod inventory unavailable")
				wantErr = true
			case "legacy-agent":
				agents[310].PodUID = nil
				wantFinished = true
			case "legacy-agent-ip-mismatch":
				agents[310].PodUID = nil
				agents[310].PodIP = ptrString("10.1.2.99")
			case "unrelated-pool":
				other := *agents[306]
				other.ID = 999
				other.DeploymentName = "unrelated"
				agents[999] = &other
				dep.targets = append(dep.targets, k8s.RuntimeRolloutTarget{Namespace: "tenant", Name: "unrelated", UID: "other", Image: "repo:old"})
				wantFinished = true
			}
			err := s.finishRolloutIfReady(context.Background(), *r)
			if (err != nil) != wantErr {
				t.Fatalf("error=%v wantErr=%v", err, wantErr)
			}
			wantStatus := ""
			if wantFinished {
				wantStatus = "finished"
			}
			if wantFailed {
				wantStatus = "error"
			}
			if wantStatus == "" && len(repo.statuses) != 0 {
				t.Fatalf("premature terminal state: %+v", repo.statuses)
			}
			if wantStatus != "" && (len(repo.statuses) != 1 || repo.statuses[0].status != wantStatus) {
				t.Fatalf("statuses=%+v want=%s", repo.statuses, wantStatus)
			}
			if len(dep.rolloutImageCalls) != 0 || dep.scaleCalls != 0 {
				t.Fatal("verification mutated runtime")
			}
		})
	}
}

func TestPoolCompletionDeadlineSurvivesResume(t *testing.T) {
	s, repo, dep, r := completionFixture(t)
	delete(s.podRepo.(*fakeRuntimePodRepo).pods, 310)
	ctx := context.Background()
	if err := s.finishRolloutIfReady(ctx, *r); err != nil {
		t.Fatal(err)
	}
	p, _ := LoadRuntimePoolProgress(ctx, repo, r.ID)
	if p.VerificationStarted.IsZero() || len(repo.statuses) != 0 {
		t.Fatal("old checkpoint did not acquire independent final-stage deadline")
	}
	first := p.VerificationStarted
	s = NewRuntimeScheduler(s.instanceRepo, s.podRepo, s.bindingRepo, repo, s.agentClient, nil, nil, dep, time.Second, WithRuntimeSchedulerNamespace("tenant"))
	if err := s.finishRolloutIfReady(ctx, *r); err != nil {
		t.Fatal(err)
	}
	p, _ = LoadRuntimePoolProgress(ctx, repo, r.ID)
	if !p.VerificationStarted.Equal(first) {
		t.Fatal("restart reset verification deadline")
	}
	p.VerificationStarted = time.Now().Add(-16 * time.Minute)
	if err := s.savePoolProgress(ctx, r.ID, p); err != nil {
		t.Fatal(err)
	}
	if err := s.finishRolloutIfReady(ctx, *r); err != nil {
		t.Fatal(err)
	}
	if len(repo.statuses) != 1 || repo.statuses[0].status != "error" || !strings.Contains(*repo.statuses[0].message, "has not registered") {
		t.Fatalf("missing truthful bounded failure: %+v", repo.statuses)
	}
	if len(dep.rolloutImageCalls) != 0 {
		t.Fatal("resume resubmitted image")
	}
}

func TestPoolCompletionQueryFailureDoesNotClaimStartupFailure(t *testing.T) {
	s, repo, dep, r := completionFixture(t)
	p, _ := LoadRuntimePoolProgress(context.Background(), repo, r.ID)
	p.VerificationStarted = time.Now().Add(-16 * time.Minute)
	if err := s.savePoolProgress(context.Background(), r.ID, p); err != nil {
		t.Fatal(err)
	}
	dep.inventoryErr = errors.New("Kubernetes temporarily unavailable")
	if err := s.finishRolloutIfReady(context.Background(), *r); err == nil {
		t.Fatal("query error swallowed")
	}
	if len(repo.statuses) != 1 || repo.statuses[0].status != "error" || !strings.Contains(*repo.statuses[0].message, "control-plane observation failed") || strings.Contains(*repo.statuses[0].message, "startup logs") {
		t.Fatalf("incorrect failure: %+v", repo.statuses)
	}
}

func TestPoolCompletionMultipleReplicasAndFrozenPools(t *testing.T) {
	s, repo, dep, r := completionFixture(t)
	ctx := context.Background()
	p, _ := LoadRuntimePoolProgress(ctx, repo, r.ID)
	second := p.Targets[0]
	second.Name, second.UID = "pool-b", "pool-b-uid"
	p.Targets = append(p.Targets, second)
	p.Completed = 2
	if err := s.savePoolProgress(ctx, r.ID, p); err != nil {
		t.Fatal(err)
	}
	second.Image, second.Ready = r.TargetImageRef, true
	dep.targets = append(dep.targets, second)
	agents := s.podRepo.(*fakeRuntimePodRepo).pods
	for i := 1; i <= 2; i++ {
		pod := dep.pods[0]
		agent := *agents[310]
		if i == 1 {
			dep.pods[0].DesiredReplicas = 2
			pod.DesiredReplicas = 2
		} else {
			pod.DeploymentName = "pool-b"
			pod.DesiredReplicas = 1
			agent.DeploymentName = "pool-b"
		}
		pod.PodName, pod.PodUID = fmt.Sprintf("new-%d", i), fmt.Sprintf("uid-%d", i)
		agent.ID, agent.PodName, agent.PodUID = int64(310+i), pod.PodName, ptrString(pod.PodUID)
		dep.pods = append(dep.pods, pod)
		agents[agent.ID] = &agent
	}
	missing := agents[312]
	delete(agents, 312)
	if err := s.finishRolloutIfReady(ctx, *r); err != nil {
		t.Fatal(err)
	}
	if len(repo.statuses) != 0 {
		t.Fatal("completed without every pool's Agent")
	}
	agents[312] = missing
	if err := s.finishRolloutIfReady(ctx, *r); err != nil {
		t.Fatal(err)
	}
	if len(repo.statuses) != 1 || repo.statuses[0].status != "finished" {
		t.Fatalf("multi-pool completion failed: %+v", repo.statuses)
	}
}

func TestLiveCompletionLegacyRuntimeTypes(t *testing.T) {
	for _, kind := range []string{RuntimeTypeHermes, RuntimeTypeOpenCode, RuntimeTypeDeepSeekHarness, RuntimeTypeOpenClaw} {
		t.Run(kind, func(t *testing.T) {
			s, repo, dep, r := completionFixture(t)
			repo.raw = ""
			r.RuntimeType = kind
			r.Phase = "requested"
			if kind == RuntimeTypeOpenClaw {
				// New ordinary rollouts freeze this scope. Historical NULL-scope
				// compatibility is covered by the real-MySQL placement tests.
				r.SourceImagesJSON = ptrString(`{"tenant/hermes-runtime":"repo:old"}`)
			}
			for _, p := range s.podRepo.(*fakeRuntimePodRepo).pods {
				p.RuntimeType = kind
			}
			dep.pods[0].RuntimeType = kind
			if err := s.finishRolloutIfReady(context.Background(), *r); err != nil {
				t.Fatal(err)
			}
			if len(repo.statuses) != 1 || repo.statuses[0].status != "finished" {
				t.Fatalf("legacy %s blocked by history: %+v", kind, repo.statuses)
			}
		})
	}
}

func (d *completionDeployment) RolloutTargets(ctx context.Context, ns, kind string) ([]k8s.RuntimeRolloutTarget, error) {
	return d.targets, d.inventoryErr
}
func (d *completionDeployment) ListDeploymentPods(ctx context.Context, refs []k8s.RuntimeDeploymentRef) ([]k8s.RuntimeDeploymentPod, error) {
	if d.listErr != nil {
		return nil, d.listErr
	}
	return d.fakeRuntimeDeploymentService.ListDeploymentPods(ctx, refs)
}
func (d *completionDeployment) HermesWebEnvironment(context.Context, string, string) (map[string]string, error) {
	return nil, nil
}
func (d *completionDeployment) RolloutHermesWebImage(ctx context.Context, ns, name, image string, unavailable, surge int) error {
	return d.RolloutImage(ctx, ns, name, image, "", unavailable, surge)
}
func (d *completionDeployment) HermesWebRolloutReady(context.Context, string, string) (bool, error) {
	return d.webReady, nil
}

func completionFixture(t *testing.T) (*RuntimeScheduler, *checkpointRepo, *completionDeployment, *models.RuntimeRollout) {
	t.Helper()
	now, start := time.Now().UTC(), time.Now().UTC().Add(-16*time.Minute)
	r := &models.RuntimeRollout{ID: 126, RuntimeType: RuntimeTypeHermes, TargetImageRef: "repo:new", Status: "running", Phase: k8s.HermesWebRolloutPhase, StartedAt: &start}
	p := RuntimePoolProgress{Version: 1, Targets: []k8s.RuntimeRolloutTarget{{Namespace: "tenant", Name: "hermes-runtime", UID: "deployment-uid", Image: "repo:old"}}, Completed: 1}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	repo := &checkpointRepo{raw: string(raw), fakeRuntimeRolloutRepo: fakeRuntimeRolloutRepo{rollouts: map[int64]*models.RuntimeRollout{126: r}}}
	dep := &completionDeployment{webReady: true, checkpointDeployment: checkpointDeployment{
		targets:                      []k8s.RuntimeRolloutTarget{{Namespace: "tenant", Name: "hermes-runtime", UID: "deployment-uid", Image: "repo:new", Ready: true}},
		fakeRuntimeDeploymentService: fakeRuntimeDeploymentService{pods: []k8s.RuntimeDeploymentPod{{RuntimeType: RuntimeTypeHermes, Namespace: "tenant", DeploymentName: "hermes-runtime", PodName: "new", PodUID: "new-uid", PodIP: ptrString("10.1.2.3"), State: "ready", ImageRef: "repo:new", DesiredReplicas: 1}}},
	}}
	pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{
		306: {ID: 306, RuntimeType: RuntimeTypeHermes, Namespace: "tenant", DeploymentName: "hermes-runtime", PodName: "old", PodUID: ptrString("old-uid"), ImageRef: "repo:old", State: "draining", Draining: true, LastSeenAt: &start},
		310: {ID: 310, RuntimeType: RuntimeTypeHermes, Namespace: "tenant", DeploymentName: "hermes-runtime", PodName: "new", PodUID: ptrString("new-uid"), PodIP: ptrString("10.1.2.3"), AgentEndpoint: ptrString("http://10.1.2.3:19090"), ImageRef: "repo:new", State: "ready", LastSeenAt: &now},
	}}
	s := NewRuntimeScheduler(newFakeRuntimeInstanceRepo(), pods, newFakeRuntimeBindingRepo(), repo, &fakeRuntimeAgentClient{}, nil, nil, dep, time.Second, WithRuntimeSchedulerNamespace("tenant"), WithRuntimeSchedulerHeartbeatTimeout(6*time.Minute))
	return s, repo, dep, r
}

func TestPoolCompletionIgnoresRemovedHistoricalRegistration(t *testing.T) {
	s, repo, dep, r := completionFixture(t)
	if err := s.finishRolloutIfReady(context.Background(), *r); err != nil {
		t.Fatal(err)
	}
	if len(repo.statuses) != 1 || repo.statuses[0].status != "finished" {
		t.Fatalf("healthy replacement blocked by removed source: %+v", repo.statuses)
	}
	if len(dep.rolloutImageCalls) != 0 || dep.scaleCalls != 0 {
		t.Fatal("completion mutated runtime")
	}
	if len(s.podRepo.(*fakeRuntimePodRepo).pods) != 2 {
		t.Fatal("completion deleted historical registration")
	}
}
