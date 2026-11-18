package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestQueueDeletePersistsNativeIntentBeforeSlowCleanup(t *testing.T) {
	repo := newV2LifecycleInstanceRepo()
	repo.byID[77] = &models.Instance{ID: 77, UserID: 1, Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", Status: "creating", RuntimeGeneration: 1}
	entered, release := make(chan struct{}), make(chan struct{})
	service := &instanceService{instanceRepo: repo, bindingRepo: newFakeRuntimeBindingRepo(), agentClient: &fakeRuntimeAgentClient{}, resourceCleanup: func(context.Context, int) error {
		close(entered)
		<-release
		return errors.New("cleanup must remain pending")
	}}
	if err := service.QueueDelete(77); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("background cleanup did not begin")
	}
	if repo.byID[77].Status != "deleting" || len(repo.deleted) != 0 {
		close(release)
		t.Fatal("deletion intent was not durable before cleanup")
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for {
		service.deletionMu.Lock()
		_, active := service.deletionsInFlight[77]
		service.deletionMu.Unlock()
		if !active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background cleanup did not release its guard")
		}
		time.Sleep(time.Millisecond)
	}
	if repo.byID[77] == nil || repo.byID[77].Status != "deleting" || len(repo.deleted) != 0 {
		t.Fatal("cleanup failure erased the durable retry intent")
	}
}

func TestQueuedDeletionCannotBeOverwrittenByLifecycleActions(t *testing.T) {
	// Hold every worker before queue admission to exercise the post-202 window.
	for i := 0; i < cap(instanceDeletionWorkers); i++ {
		instanceDeletionWorkers <- struct{}{}
	}
	repo := newV2LifecycleInstanceRepo()
	repo.byID[78] = &models.Instance{ID: 78, UserID: 1, Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", Status: "stopped", RuntimeGeneration: 1}
	service := &instanceService{instanceRepo: repo, bindingRepo: newFakeRuntimeBindingRepo(), agentClient: &fakeRuntimeAgentClient{}, resourceCleanup: func(context.Context, int) error { return errors.New("retry cleanup later") }}
	t.Cleanup(func() {
		for i := 0; i < cap(instanceDeletionWorkers); i++ {
			<-instanceDeletionWorkers
		}
		deadline := time.Now().Add(time.Second)
		for {
			service.deletionMu.Lock()
			_, active := service.deletionsInFlight[78]
			service.deletionMu.Unlock()
			if !active {
				break
			}
			if time.Now().After(deadline) {
				t.Error("queued worker did not exit")
				break
			}
			time.Sleep(time.Millisecond)
		}
	})
	if err := service.QueueDelete(78); err != nil {
		t.Fatal(err)
	}
	for _, action := range []struct {
		name string
		run  func() error
	}{
		{"start", func() error { return service.Start(78) }},
		{"stop", func() error { return service.Stop(78) }},
		{"restart", func() error { return service.Restart(78) }},
		{"restart-environment", func() error { return service.RestartWithEnvironment(78, map[string]string{"LOG_LEVEL": "debug"}, nil) }},
		{"reset", func() error { return service.Reset(78) }},
	} {
		t.Run(action.name, func(t *testing.T) {
			if err := action.run(); !errors.Is(err, models.ErrInstanceStopping) {
				t.Errorf("queued deletion was not fenced: %v", err)
			}
			if repo.byID[78] == nil || repo.byID[78].Status != "deleting" || repo.byID[78].RuntimeGeneration != 1 {
				t.Fatalf("accepted deletion intent changed: %+v", repo.byID[78])
			}
		})
	}
}

func waitQueuedDeletion(t *testing.T, service *instanceService, id int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		service.deletionMu.Lock()
		_, active := service.deletionsInFlight[id]
		service.deletionMu.Unlock()
		if !active {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("background deletion did not finish its attempt")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestQueueDeleteIntentSupersedesBusyLifecycle(t *testing.T) {
	id := 91780
	repo := newV2LifecycleInstanceRepo()
	repo.byID[id] = &models.Instance{ID: id, UserID: 1, Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", Status: "stopped", RuntimeGeneration: 2}
	service := &instanceService{instanceRepo: repo, bindingRepo: newFakeRuntimeBindingRepo(), agentClient: &fakeRuntimeAgentClient{}, resourceCleanup: func(context.Context, int) error { return errors.New("cleanup remains pending") }}
	release, err := service.acquireLifecycleOperation(id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { release(); waitQueuedDeletion(t, service, id) }()
	if err := service.QueueDelete(id); err != nil {
		t.Fatalf("existing stop blocked explicit deletion intent: %v", err)
	}
	if repo.byID[id].Status != "deleting" || repo.byID[id].RuntimeGeneration != 2 {
		t.Fatalf("durable deleting intent missing or changed generation: %+v", repo.byID[id])
	}
	if len(repo.deleted) != 0 {
		t.Fatal("cleanup bypassed the held lifecycle lock")
	}
}

type deletionIntentNotificationRepo struct {
	*v2LifecycleInstanceRepo
	marked chan struct{}
}

func (r *deletionIntentNotificationRepo) Update(instance *models.Instance) error {
	err := r.v2LifecycleInstanceRepo.Update(instance)
	if err == nil && instance.Status == "deleting" {
		close(r.marked)
	}
	return err
}

func TestInternalDeletePersistsIntentButWaitsForCleanupProof(t *testing.T) {
	id := 91781
	repo := &deletionIntentNotificationRepo{v2LifecycleInstanceRepo: newV2LifecycleInstanceRepo(), marked: make(chan struct{})}
	repo.byID[id] = &models.Instance{ID: id, UserID: 1, Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", Status: "stopped", RuntimeGeneration: 2}
	entered, unblock := make(chan struct{}), make(chan struct{})
	service := &instanceService{instanceRepo: repo, bindingRepo: newFakeRuntimeBindingRepo(), agentClient: &fakeRuntimeAgentClient{}, resourceCleanup: func(context.Context, int) error {
		close(entered)
		<-unblock
		return errors.New("cleanup proof pending")
	}}
	held, err := service.acquireLifecycleOperation(id)
	if err != nil {
		t.Fatal(err)
	}
	release, finishCleanup := sync.OnceFunc(held), sync.OnceFunc(func() { close(unblock) })
	done := make(chan error, 1)
	go func() { done <- service.Delete(id); close(done) }()
	t.Cleanup(func() { release(); finishCleanup(); <-done; waitQueuedDeletion(t, service, id) })
	select {
	case <-repo.marked:
	case <-time.After(time.Second):
		t.Fatal("synchronous delete did not persist intent before waiting on stop lock")
	}
	select {
	case <-done:
		t.Fatal("internal delete returned before acquiring lifecycle lock/proof")
	default:
	}
	release()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not start after old stop released lock")
	}
	select {
	case <-done:
		t.Fatal("internal delete returned before cleanup result")
	default:
	}
	finishCleanup()
	if err := <-done; err == nil || repo.byID[id] == nil || repo.byID[id].Status != "deleting" {
		t.Fatalf("internal caller lost cleanup failure or retry intent: %v", err)
	}
}

type forceDeletionAgent struct {
	*fakeRuntimeAgentClient
	forced  int
	failure error
}

func (a *forceDeletionAgent) ForceDeleteGateway(context.Context, string, string) error {
	a.forced++
	return a.failure
}

func TestDeletingGatewayUsesExplicitForceAndRetainsUnprovenBinding(t *testing.T) {
	for _, status := range []string{"deleting", "stopped"} {
		for _, fail := range []bool{false, true} {
			t.Run(status+map[bool]string{false: "/confirmed", true: "/unconfirmed"}[fail], func(t *testing.T) {
				repo := newV2LifecycleInstanceRepo()
				instance := &models.Instance{ID: 91782, UserID: 1, Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", Status: status, RuntimeGeneration: 2}
				repo.byID[instance.ID] = instance
				bindings := newFakeRuntimeBindingRepo()
				bindings.bindings[instance.ID] = &models.InstanceRuntimeBinding{InstanceID: instance.ID, RuntimePodID: 17, GatewayID: "gw-91782-2", Generation: 2}
				endpoint := "https://10.0.0.2:19090"
				agent := &forceDeletionAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}}
				if fail {
					agent.failure = errors.New("termination proof not confirmed")
				}
				service := &instanceService{instanceRepo: repo, bindingRepo: bindings, runtimePodRepo: &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{17: {ID: 17, AgentEndpoint: &endpoint}}}, agentClient: agent}
				err := service.cleanupV2GatewayBinding(context.Background(), instance)
				if status == "deleting" {
					if agent.forced != 1 || len(agent.deleteRequests) != 0 || (err != nil) != fail {
						t.Fatalf("explicit force dispatch: forced=%d ordinary=%d err=%v", agent.forced, len(agent.deleteRequests), err)
					}
					if fail && bindings.bindings[instance.ID] == nil {
						t.Fatal("unconfirmed force deleted source binding")
					}
				} else if agent.forced != 0 || len(agent.deleteRequests) != 1 {
					t.Fatal("ordinary stop was escalated to force")
				}
			})
		}
	}
}

func TestQueueDeleteRetainsTeamAuthorizationBeforeIntent(t *testing.T) {
	repo := newV2LifecycleInstanceRepo()
	repo.byID[91783] = &models.Instance{ID: 91783, UserID: 1, Status: "running"}
	service := &instanceService{instanceRepo: repo, ownerTeamMutationGuard: func(int, string, int) error { return errors.New("Team ownership guard") }}
	if err := service.QueueDelete(91783); err == nil || repo.byID[91783].Status != "running" {
		t.Fatalf("deletion bypassed Team authorization or persisted rejected intent: %v", err)
	}
}

func TestDeletingStrictGatewayWithoutForceCapabilityFailsClosed(t *testing.T) {
	repo := newV2LifecycleInstanceRepo()
	bindings := newFakeRuntimeBindingRepo()
	bindings.bindings[91784] = &models.InstanceRuntimeBinding{InstanceID: 91784, RuntimePodID: 17, GatewayID: "gw-91784-2", Generation: 2}
	endpoint := "https://10.0.0.2:19090"
	agent := &strictStopRuntimeAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, strict: true}
	service := &instanceService{instanceRepo: repo, bindingRepo: bindings, runtimePodRepo: &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{17: {ID: 17, AgentEndpoint: &endpoint}}}, agentClient: agent}
	err := service.cleanupV2GatewayBinding(context.Background(), &models.Instance{ID: 91784, Status: "deleting"})
	if err == nil || bindings.bindings[91784] == nil || len(agent.deleteRequests) != 0 {
		t.Fatalf("strict adapter downgraded explicit force: err=%v ordinary=%d", err, len(agent.deleteRequests))
	}
}

func TestTeamDeleteRetainsSharedResourcesUntilNativeProof(t *testing.T) {
	instanceID := 91785
	instances := newV2LifecycleInstanceRepo()
	instances.byID[instanceID] = &models.Instance{ID: instanceID, UserID: 1, Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", Status: "running", RuntimeGeneration: 2}
	bindings := newFakeRuntimeBindingRepo()
	bindings.bindings[instanceID] = &models.InstanceRuntimeBinding{InstanceID: instanceID, RuntimePodID: 17, GatewayID: "gw-91785-2", Generation: 2}
	endpoint := "https://10.0.0.2:19090"
	agent := &forceDeletionAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, failure: errors.New("termination proof pending")}
	lifecycle := &instanceService{instanceRepo: instances, bindingRepo: bindings, runtimePodRepo: &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{17: {ID: 17, AgentEndpoint: &endpoint}}}, agentClient: agent}
	team := &models.Team{ID: 91785, UserID: 1, Status: models.TeamStatusRunning, ResourceScope: models.TeamScopeCM}
	member := &models.TeamMember{ID: 91785, TeamID: team.ID, UserID: 1, MemberKey: "worker", Role: "worker", Status: models.TeamMemberStatusBusy, InstanceID: &instanceID}
	repo := &teamRepositoryStub{teamsByID: map[int]*models.Team{team.ID: team}, membersByID: map[int]*models.TeamMember{member.ID: member}}
	service := &teamService{repo: repo, instanceService: lifecycle}
	// Nil shared-resource services also prove the failure cannot reach removal.
	if err := service.DeleteTeam(1, team.ID); err == nil || agent.forced != 1 {
		t.Fatalf("Team deletion ignored native proof failure: forced=%d err=%v", agent.forced, err)
	}
	if team.Status != models.TeamStatusDeleting || repo.updatedMember == nil || repo.updatedMember.InstanceID == nil || *repo.updatedMember.InstanceID != instanceID || repo.updatedMember.Status != models.TeamMemberStatusDeleting || instances.byID[instanceID] == nil || bindings.bindings[instanceID] == nil {
		t.Fatal("Team cleared its member/shared state before confirmed native deletion")
	}
}
