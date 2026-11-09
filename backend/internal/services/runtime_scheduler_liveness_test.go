package services

import (
	"clawreef/internal/models"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type timestampPendingBindingRepo struct {
	*fakeRuntimeBindingRepo
	writes int
}

func (r *timestampPendingBindingRepo) UpdateState(ctx context.Context, id, generation int, state string, message *string) error {
	if err := r.fakeRuntimeBindingRepo.UpdateState(ctx, id, generation, state, message); err != nil {
		return err
	}
	r.bindings[id].UpdatedAt = time.Now().UTC()
	r.writes++
	return nil
}

func TestPendingFirstPublicationResumesOriginalReservation(t *testing.T) {
	f := newPendingSlotReplayFixture(t)
	f.instance.RuntimeGeneration = 1
	f.binding.Generation = 1
	f.binding.GatewayID = "pending-7-1"
	if errs := f.scheduler.reconcileCreatingInstance(context.Background(), f.instance); len(errs) > 0 {
		t.Fatal(errs)
	}
	if len(f.requests) != 1 || f.requests[0].Generation != 1 || f.requests[0].GatewayPort != f.binding.GatewayPort {
		t.Fatalf("first publication did not resume reservation: %+v", f.requests)
	}
	if f.pods.claims[9] != 0 || f.pods.releases[9] != 0 {
		t.Fatal("first publication changed reserved capacity")
	}
}

func TestPendingPublishedReservationRetainsTimeoutOrigin(t *testing.T) {
	f := newPendingSlotReplayFixture(t)
	f.seed(t, 2, "running")
	origin := time.Now().UTC().Add(-time.Hour)
	f.binding.UpdatedAt = origin
	bindings := &timestampPendingBindingRepo{fakeRuntimeBindingRepo: f.bindings}
	f.scheduler.bindingRepo = bindings
	for range 3 {
		if errs := f.scheduler.reconcileCreatingInstance(context.Background(), f.instance); len(errs) > 0 {
			t.Fatal(errs)
		}
	}
	if !f.binding.UpdatedAt.Equal(origin) || bindings.writes != 0 {
		t.Fatalf("reconciliation postponed timeout: origin=%v updated=%v writes=%d", origin, f.binding.UpdatedAt, bindings.writes)
	}
	if len(f.requests) != 0 || f.envCalls != 0 {
		t.Fatal("published credentials regenerated")
	}
}

type pagedSchedulerInstanceRepo struct {
	*fakeRuntimeInstanceRepo
	calls []int
}

func (r *pagedSchedulerInstanceRepo) GetV2SchedulerPage(_ context.Context, creating bool, afterID, throughID, limit int) ([]models.Instance, int, error) {
	if creating {
		return nil, 1001, nil
	}
	r.calls = append(r.calls, afterID)
	if throughID == 0 {
		throughID = 1001
	}
	var rows []models.Instance
	for id := afterID + 1; id <= throughID && len(rows) < limit; id++ {
		rows = append(rows, models.Instance{ID: id})
	}
	return rows, throughID, nil
}
func TestSchedulerRotatesDesiredSnapshotBeyondFirstBatch(t *testing.T) {
	repo := &pagedSchedulerInstanceRepo{fakeRuntimeInstanceRepo: newFakeRuntimeInstanceRepo()}
	s := NewRuntimeScheduler(repo, &fakeRuntimePodRepo{}, newFakeRuntimeBindingRepo(), nil, nil, nil, nil, nil, time.Second, WithRuntimeSchedulerHeartbeatTimeout(0))
	for range 3 {
		if err := s.reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(repo.calls, []int{0, 1000, 0}) {
		t.Fatalf("bounded scan cursors=%v; later instances starve", repo.calls)
	}
}

func TestPhysicalExhaustionRequestsActualScaleOut(t *testing.T) {
	for _, path := range []string{"backlog", "assignment"} {
		t.Run(path, func(t *testing.T) {
			endpoint := "https://10.0.0.2:19090"
			instance := models.Instance{ID: 17, UserID: 1, Type: RuntimeTypeHermes, RuntimeType: RuntimeBackendGateway, InstanceMode: InstanceModeLite, Status: "creating", RuntimeGeneration: 1, WorkspacePath: ptrString("/workspace")}
			pod := models.RuntimePod{ID: 9, RuntimeType: RuntimeTypeHermes, AgentEndpoint: &endpoint, State: "ready", Capacity: 1}
			pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{9: &pod}, schedulable: []models.RuntimePod{pod}}
			deployments := &fakeRuntimeDeploymentService{}
			agent := &occupiedPortsAgent{strictSchedulerAgent: &strictSchedulerAgent{&fakeRuntimeAgentClient{}}, ports: map[int]struct{}{20000: {}}}
			s := newLiveFixtureRuntimeScheduler(newFakeRuntimeInstanceRepo(), pods, newFakeRuntimeBindingRepo(), nil, agent, nil, nil, deployments, time.Second, WithRuntimeSchedulerWorkspaceRoot(t.TempDir()))
			if path == "backlog" {
				if err := s.scaleOutForPendingBacklog(context.Background(), []models.Instance{instance}); err != nil {
					t.Fatal(err)
				}
			} else if err := s.assignInstance(context.Background(), instance); !errors.Is(err, errRuntimeScaleOutPending) {
				t.Fatal(err)
			}
			if deployments.scaleCalls != 1 {
				t.Fatalf("exhausted physical capacity reported pending without scale: calls=%d", deployments.scaleCalls)
			}
		})
	}
}

type failoverIntentInstanceRepo struct {
	*fakeRuntimeInstanceRepo
	intents    map[int]models.RuntimeFailoverIntent
	advanceErr error
}

func (r *failoverIntentInstanceRepo) BeginRuntimeFailover(_ context.Context, state models.RuntimeFailoverIntent) (*models.RuntimeFailoverIntent, error) {
	if prior, ok := r.intents[state.InstanceID]; ok {
		return &prior, nil
	}
	r.intents[state.InstanceID] = state
	return &state, nil
}
func (r *failoverIntentInstanceRepo) ListRuntimeFailovers(context.Context, int) ([]models.RuntimeFailoverIntent, error) {
	var rows []models.RuntimeFailoverIntent
	for _, state := range r.intents {
		if state.Attempts <= 5 && (state.SourceStopped || state.Attempts < 5) {
			rows = append(rows, state)
		}
	}
	return rows, nil
}
func (r *failoverIntentInstanceRepo) SaveRuntimeFailover(_ context.Context, state models.RuntimeFailoverIntent) error {
	r.intents[state.InstanceID] = state
	return nil
}
func (r *failoverIntentInstanceRepo) AdvanceRuntimeFailover(_ context.Context, state models.RuntimeFailoverIntent) (bool, error) {
	if r.advanceErr != nil {
		return false, r.advanceErr
	}
	instance := r.byID[state.InstanceID]
	instance.Status = "creating"
	instance.RuntimeGeneration++
	delete(r.intents, state.InstanceID)
	return true, nil
}
func TestFailoverPersistsSourceAndResumesAfterTransientStopFailure(t *testing.T) {
	for _, scenario := range []string{"retry", "source-replaced", "owner-changed", "stopped", "advance-failure", "manual-exhausted", "exhausted", "stale-cursor"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			endpoint, uid := "https://10.0.0.2:19090", "source-uid"
			repo := &failoverIntentInstanceRepo{fakeRuntimeInstanceRepo: newFakeRuntimeInstanceRepo(), intents: map[int]models.RuntimeFailoverIntent{}}
			repo.byID[17] = &models.Instance{ID: 17, UserID: 1, Type: RuntimeTypeHermes, Status: "running", RuntimeGeneration: 3, RuntimeType: RuntimeBackendGateway, InstanceMode: InstanceModeLite, WorkspacePath: ptrString("/workspace")}
			pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{9: {ID: 9, RuntimeType: RuntimeTypeHermes, AgentEndpoint: &endpoint, PodUID: &uid, State: "ready", Capacity: 1, UsedSlots: 1}}}
			bindings := newFakeRuntimeBindingRepo()
			bindings.bindings[17] = &models.InstanceRuntimeBinding{InstanceID: 17, RuntimePodID: 9, RuntimeType: RuntimeTypeHermes, GatewayID: "gw-17-3", GatewayPort: 20000, Generation: 3, State: "running", WorkspacePath: "/workspace"}
			agent := &strictSchedulerAgent{&fakeRuntimeAgentClient{deleteErr: context.DeadlineExceeded}}
			s := NewRuntimeScheduler(repo, pods, bindings, nil, agent, nil, nil, &fakeRuntimeDeploymentService{}, time.Second)
			if err := s.FailoverPod(ctx, 9, "manual failover"); err == nil {
				t.Fatal("expected injected stop failure")
			}
			state, ok := repo.intents[17]
			if !ok || state.SourcePodUID != uid || state.SourceEndpoint != endpoint || state.GatewayID != "gw-17-3" {
				t.Fatalf("source failover intent was lost: %+v", repo.intents)
			}
			if bindings.bindings[17] == nil || repo.byID[17].RuntimeGeneration != 3 {
				t.Fatal("unconfirmed stop released writer")
			}
			agent.deleteErr = nil
			state.NextRetryAt = nil
			repo.intents[17] = state
			switch scenario {
			case "source-replaced":
				other := "new-uid"
				pods.pods[9].PodUID = &other
			case "owner-changed":
				repo.byID[17].UserID++
			case "stopped":
				repo.byID[17].Status = "stopped"
			case "advance-failure":
				repo.advanceErr = errors.New("commit interrupted")
			case "manual-exhausted", "exhausted":
				state.Attempts = 5
				repo.intents[17] = state
			}
			// A new scheduler has only the persisted journal. The pod is already unhealthy.
			s = NewRuntimeScheduler(repo, pods, bindings, nil, agent, nil, nil, &fakeRuntimeDeploymentService{}, time.Second)
			if scenario == "stale-cursor" {
				checkpoint := state
				checkpoint.SourceStopped = true
				repo.intents[17] = checkpoint
				if err := s.resumeOwnedRuntimeFailover(ctx, repo, state); err != nil {
					t.Fatal(err)
				}
				if len(agent.deleteRequests) != 1 || repo.byID[17].RuntimeGeneration != 4 {
					t.Fatal("stale queue snapshot repeated a confirmed stop")
				}
				return
			}
			_ = s.failoverStalePods(ctx)
			if scenario == "exhausted" {
				if len(agent.deleteRequests) != 1 || repo.byID[17].RuntimeGeneration != 3 {
					t.Fatal("exhausted automatic stop was retried")
				}
				return
			}
			if scenario == "manual-exhausted" {
				handled, err := s.RetryOwnedRuntimeFailoverLocked(ctx, *repo.byID[17])
				if !handled || err != nil {
					t.Fatalf("explicit retry did not rearm original failover: %v %v", handled, err)
				}
			}
			if scenario == "source-replaced" || scenario == "owner-changed" || scenario == "stopped" {
				if len(agent.deleteRequests) != 1 || repo.byID[17].RuntimeGeneration != 3 {
					t.Fatal("changed source/owner/desired state granted stop authority")
				}
				return
			}
			if scenario == "advance-failure" {
				if !repo.intents[17].SourceStopped {
					t.Fatal("confirmed stop checkpoint was lost")
				}
				repo.advanceErr = nil
				_ = s.failoverStalePods(ctx)
			}
			if repo.byID[17].RuntimeGeneration != 4 || repo.byID[17].Status != "creating" || len(repo.intents) != 0 {
				t.Fatalf("persisted failover did not finish: instance=%+v intent=%+v", repo.byID[17], repo.intents)
			}
			if len(agent.deleteRequests) != 2 {
				t.Fatalf("confirmed stop repeated during DB continuation: %v", agent.deleteRequests)
			}
		})
	}
}

func (r *failoverIntentInstanceRepo) GetRuntimeFailover(_ context.Context, id int) (*models.RuntimeFailoverIntent, error) {
	state, ok := r.intents[id]
	if !ok {
		return nil, nil
	}
	return &state, nil
}
