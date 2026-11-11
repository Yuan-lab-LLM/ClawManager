package services

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
)

type exhaustedPoolWakeProbe struct {
	*fakeRuntimeDeploymentService
	calls int
	err   error
}

func (p *exhaustedPoolWakeProbe) IdleRuntimePoolOperations(context.Context, string, string) ([]string, error) {
	p.calls++
	return nil, p.err
}
func (p *exhaustedPoolWakeProbe) WakeIdleRuntimePool(context.Context, k8s.RuntimePodScaleTarget, *k8s.RuntimePodScaleSnapshot) error {
	return errors.New("unexpected wake without a journal")
}

func TestRuntimeSchedulerChecksSleepingCapacityAfterLiveSlotsAreUnusable(t *testing.T) {
	for _, kind := range []string{RuntimeTypeOpenClaw, RuntimeTypeHermes, RuntimeTypeOpenCode, RuntimeTypeDeepSeekHarness} {
		for _, reason := range []string{"reserved slots", "unreadable slots", "uncertain create"} {
			t.Run(kind+"/"+reason, func(t *testing.T) {
				now := time.Now()
				uid, ip, endpoint := "uid-132", "10.0.0.132", "https://10.0.0.132:19090"
				p := models.RuntimePod{ID: 132, RuntimeType: kind, Namespace: "clawmanager-system", PodName: "old-pod", DeploymentName: kind + "-runtime", PodUID: &uid, PodIP: &ip, AgentEndpoint: &endpoint, ImageRef: "repo/" + kind + ":test", State: "ready", Capacity: 8, LastSeenAt: &now}
				pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{132: &p}, schedulable: []models.RuntimePod{p}}
				bindings := newFakeRuntimeBindingRepo()
				if reason == "reserved slots" {
					for i := 0; i < 8; i++ {
						bindings.bindings[100+i] = &models.InstanceRuntimeBinding{InstanceID: 100 + i, RuntimePodID: 132, GatewayPort: RuntimeGatewayPortStart + i*RuntimeGatewayPortBlockSize(kind), State: "error", Generation: 1}
					}
				}
				fake := &fakeRuntimeAgentClient{}
				if reason == "uncertain create" {
					fake.createErr = context.DeadlineExceeded
				}
				agent := &occupiedPortsAgent{strictSchedulerAgent: &strictSchedulerAgent{fake}}
				if reason == "unreadable slots" {
					agent.err = errors.New("old worker inspection unavailable")
				}
				marker := fmt.Errorf("sleep proof lookup attempted")
				deployment := &exhaustedPoolWakeProbe{fakeRuntimeDeploymentService: &fakeRuntimeDeploymentService{pods: []k8s.RuntimeDeploymentPod{{RuntimeType: kind, Namespace: p.Namespace, PodName: p.PodName, DeploymentName: p.DeploymentName, PodUID: uid, PodIP: &ip, ImageRef: p.ImageRef, State: "ready", DesiredReplicas: 1}}}, err: marker}
				s := NewRuntimeScheduler(newFakeRuntimeInstanceRepo(), pods, bindings, &fakeRuntimeRolloutRepo{}, agent, nil, nil, deployment, time.Second, WithRuntimeSchedulerWorkspaceRoot(canonicalCompatibilityTestDir(t)), WithRuntimeSchedulerGatewayEnvBuilder(func(*models.Instance) (map[string]string, error) { return map[string]string{}, nil }))
				s.management = &RuntimeManagementService{}
				instance := models.Instance{ID: 161, UserID: 1, Type: kind, RuntimeType: RuntimeBackendGateway, InstanceMode: InstanceModeLite, Status: "creating", RuntimeGeneration: 15, WorkspacePath: stringPtrOrNil("/workspace")}
				err := s.assignInstance(context.Background(), instance)
				if reason == "uncertain create" {
					if deployment.calls != 0 || !isRuntimeAssignmentPending(err) || bindings.bindings[161] == nil || len(fake.createRequests) != 1 || pods.releases[132] != 0 {
						t.Fatalf("uncertain publication escaped its reservation: wake=%d err=%v binding=%+v creates=%d releases=%v", deployment.calls, err, bindings.bindings[161], len(fake.createRequests), pods.releases)
					}
					return
				}
				if deployment.calls != 1 || err == nil || !isRuntimeAssignmentPending(err) {
					t.Fatalf("unusable live pool prevented cold start: wake checks=%d err=%v", deployment.calls, err)
				}
				if len(fake.createRequests) != 0 || bindings.bindings[161] != nil || pods.releases[132] != 1 {
					t.Fatalf("old writer/reservation changed: creates=%d binding=%+v releases=%v", len(fake.createRequests), bindings.bindings[161], pods.releases)
				}
			})
		}
	}
}
