package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"clawreef/internal/models"
)

type recoveryTestRepo struct {
	*fakeRuntimeInstanceRepo
	lifecycle  sync.Mutex
	state      *models.InstanceRuntimeRecovery
	beforeSave func()
	advanceErr error
}

func (r *recoveryTestRepo) AcquireLifecycleLock(context.Context, int) (func(), error) {
	r.lifecycle.Lock()
	return r.lifecycle.Unlock, nil
}
func (r *recoveryTestRepo) GetRuntimeRecovery(context.Context, int) (*models.InstanceRuntimeRecovery, error) {
	if r.state == nil {
		return nil, nil
	}
	copy := *r.state
	return &copy, nil
}
func (r *recoveryTestRepo) SaveRuntimeRecovery(_ context.Context, i models.Instance, s models.InstanceRuntimeRecovery) (bool, error) {
	if r.beforeSave != nil {
		r.beforeSave()
		r.beforeSave = nil
	}
	current, _ := r.GetByID(i.ID)
	if current == nil || current.Status != i.Status || current.RuntimeGeneration != i.RuntimeGeneration {
		return false, nil
	}
	r.state = &s
	return true, nil
}
func (r *recoveryTestRepo) AdvanceRuntimeRecovery(_ context.Context, i models.Instance) (bool, error) {
	if r.advanceErr != nil {
		err := r.advanceErr
		r.advanceErr = nil
		return false, err
	}
	current, _ := r.GetByID(i.ID)
	if current == nil || current.Status != i.Status || current.RuntimeGeneration != i.RuntimeGeneration {
		return false, nil
	}
	copy := *current
	copy.Status = "creating"
	copy.RuntimeGeneration++
	if r.state != nil {
		r.state.Phase = "placing"
		r.state.Generation = copy.RuntimeGeneration
	}
	copy.RuntimeErrorMessage = nil
	copy.RuntimeErrorCode = nil
	r.byID[i.ID] = &copy
	return true, nil
}
func (r *recoveryTestRepo) ResetRuntimeRecovery(context.Context, int) error {
	r.state = nil
	return nil
}

func recoveryFixture() (*RuntimeScheduler, *recoveryTestRepo, *fakeRuntimeBindingRepo, *fakeRuntimeAgentClient, models.Instance, time.Time) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	path, endpoint, uid, message := "/workspaces/openclaw/user-3/instance-7", "https://runtime", "pod-uid", "gateway process exited unexpectedly: exit status 137"
	i := models.Instance{ID: 7, UserID: 3, Type: RuntimeTypeOpenClaw, RuntimeType: RuntimeBackendGateway, InstanceMode: InstanceModeLite, Status: "error", RuntimeGeneration: 2, WorkspacePath: &path, RuntimeErrorMessage: &message}
	r := &recoveryTestRepo{fakeRuntimeInstanceRepo: newFakeRuntimeInstanceRepo()}
	r.byID[7] = &i
	b := newFakeRuntimeBindingRepo()
	b.bindings[7] = &models.InstanceRuntimeBinding{InstanceID: 7, RuntimePodID: 9, Generation: 2, GatewayID: "gw-7-2", GatewayPort: 19000, State: "error", ErrorMessage: &message}
	a := &fakeRuntimeAgentClient{}
	s := &RuntimeScheduler{instanceRepo: r, bindingRepo: b, podRepo: &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{9: {ID: 9, State: "ready", PodUID: &uid, AgentEndpoint: &endpoint}}}, agentClient: a}
	return s, r, b, a, i, now
}

func TestRuntimeRecoveryCrashWaitsThenConfirmsStopBeforeGenerationAdvance(t *testing.T) {
	s, r, b, a, i, now := recoveryFixture()
	handled, errs := s.reconcileInstanceRecoveryLocked(context.Background(), i, now)
	if !handled || len(errs) > 0 || len(a.deleteRequests) != 0 || r.state.NextRetryAt == nil {
		t.Fatalf("first pass did not wait: %v %v", handled, errs)
	}
	// No eligible replacement is needed to verify fencing: normal assignment may report pending capacity.
	_, _ = s.reconcileInstanceRecoveryLocked(context.Background(), i, now.Add(30*time.Second))
	current, _ := r.GetByID(i.ID)
	if len(a.deleteRequests) != 1 || b.bindings[i.ID] != nil || current.RuntimeGeneration != 3 || r.state.Attempts != 1 {
		t.Fatalf("confirmed exit did not advance safely: %+v %+v", current, r.state)
	}
}

func TestRuntimeRecoveryUnconfirmedStopRetainsWriterAndExhaustsAcrossSchedulers(t *testing.T) {
	s, r, b, a, i, now := recoveryFixture()
	a.deleteErr = errors.New("worker unreachable")
	for attempt, delay := range []time.Duration{30 * time.Second, 120 * time.Second, 300 * time.Second} {
		s.reconcileInstanceRecoveryLocked(context.Background(), i, now)
		now = now.Add(delay)
		_, errs := s.reconcileInstanceRecoveryLocked(context.Background(), i, now)
		if r.state.Attempts < 3 && (r.state.NextRetryAt == nil || !r.state.NextRetryAt.Equal(now.Add(runtimeRecoveryBackoff[r.state.Attempts]))) {
			t.Fatal("next retry not anchored to durable attempt time")
		}
		if len(errs) == 0 || b.bindings[i.ID] == nil || r.state.Attempts != attempt+1 {
			t.Fatal("uncertain stop released writer or lost attempt")
		}
		restored := *s
		s = &restored
	}
	s.reconcileInstanceRecoveryLocked(context.Background(), i, now.Add(time.Hour))
	if len(a.deleteRequests) != 3 || r.state.NextRetryAt != nil {
		t.Fatal("manager restart replenished exhausted budget")
	}
}

func TestRuntimeRecoveryUserStopCommittedBeforeCASWins(t *testing.T) {
	s, r, b, a, i, now := recoveryFixture()
	s.reconcileInstanceRecoveryLocked(context.Background(), i, now)
	r.beforeSave = func() { copy := i; copy.Status = "stopped"; r.byID[i.ID] = &copy }
	s.reconcileInstanceRecoveryLocked(context.Background(), i, now.Add(30*time.Second))
	if len(a.deleteRequests) != 0 || b.bindings[i.ID] == nil {
		t.Fatal("recovery ignored user stop before recovery intent CAS")
	}
}

func TestRuntimeRecoveryStoppedStaleAndUnrelatedRunningAreUntouched(t *testing.T) {
	for _, status := range []string{"stopped", "sleeping", "deleting", "running"} {
		t.Run(status, func(t *testing.T) {
			s, r, b, a, i, now := recoveryFixture()
			i.Status = status
			r.byID[i.ID] = &i
			if status == "running" {
				b.bindings[i.ID].State = "running"
				old := now.Add(-time.Hour)
				b.bindings[i.ID].LastHealthAt = &old
			}
			s.reconcileInstanceRecoveryLocked(context.Background(), i, now)
			s.reconcileInstanceRecoveryLocked(context.Background(), i, now.Add(time.Hour))
			if len(a.deleteRequests) != 0 || r.state != nil {
				t.Fatal("stop intent or stale heartbeat authorized recovery")
			}
		})
	}
}

func TestRuntimeRecoveryDesiredPassReReadsUserStopAndHonorsLifecycleLock(t *testing.T) {
	s, r, _, a, i, _ := recoveryFixture()
	stopped := i
	stopped.Status = "stopped"
	r.byID[i.ID] = &stopped
	if errs := s.reconcileDesiredInstance(context.Background(), i); len(errs) > 0 {
		t.Fatal(errs)
	}
	if r.state != nil || len(a.deleteRequests) > 0 {
		t.Fatal("stale desired snapshot overrode stop")
	}
	r.byID[i.ID] = &i
	release, err := acquireInstanceLifecycleOperation(context.Background(), r, i.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if errs := s.reconcileDesiredInstance(context.Background(), i); len(errs) > 0 {
		t.Fatal(errs)
	}
	if r.state != nil || len(a.deleteRequests) > 0 {
		t.Fatal("recovery overlapped user lifecycle operation")
	}
}

func TestRuntimeRecoveryMissingSourceProofNeverReleasesWriter(t *testing.T) {
	for _, missing := range []string{"pod", "uid", "endpoint", "404"} {
		t.Run(missing, func(t *testing.T) {
			s, r, b, a, i, now := recoveryFixture()
			s.reconcileInstanceRecoveryLocked(context.Background(), i, now)
			pod := s.podRepo.(*fakeRuntimePodRepo).pods[9]
			switch missing {
			case "pod":
				delete(s.podRepo.(*fakeRuntimePodRepo).pods, 9)
			case "uid":
				pod.PodUID = nil
			case "endpoint":
				pod.AgentEndpoint = nil
			case "404":
				a.deleteErr = ErrRuntimeAgentNotFound
			}
			_, errs := s.reconcileInstanceRecoveryLocked(context.Background(), i, now.Add(30*time.Second))
			current, _ := r.GetByID(i.ID)
			if len(errs) == 0 || b.bindings[i.ID] == nil || current.RuntimeGeneration != 2 {
				t.Fatal("missing source proof freed writer")
			}
		})
	}
}

type recoveryGatewayStateAgent struct {
	*fakeRuntimeAgentClient
	state    *RuntimeAgentGatewayState
	stateErr error
}

func (a *recoveryGatewayStateAgent) GatewayState(context.Context, string, string) (*RuntimeAgentGatewayState, error) {
	return a.state, a.stateErr
}

func TestRuntimeRecoveryFreshSourceStateRejectsChangedOwnerOrHealthyWriter(t *testing.T) {
	for _, scenario := range []string{"owner", "generation", "gateway", "healthy", "unreadable"} {
		t.Run(scenario, func(t *testing.T) {
			s, r, b, base, i, now := recoveryFixture()
			snapshot := &RuntimeAgentGatewayState{InstanceID: i.ID, Generation: i.RuntimeGeneration, GatewayID: "gw-7-2", State: "error"}
			agent := &recoveryGatewayStateAgent{fakeRuntimeAgentClient: base, state: snapshot}
			s.agentClient = agent
			switch scenario {
			case "owner":
				snapshot.InstanceID++
			case "generation":
				snapshot.Generation++
			case "gateway":
				snapshot.GatewayID = "gw-8-2"
			case "healthy":
				snapshot.State = "running"
			case "unreadable":
				agent.stateErr = errors.New("unreachable")
			}
			s.reconcileInstanceRecoveryLocked(context.Background(), i, now)
			s.reconcileInstanceRecoveryLocked(context.Background(), i, now.Add(30*time.Second))
			current, _ := r.GetByID(i.ID)
			if len(base.deleteRequests) != 0 || b.bindings[i.ID] == nil || current.RuntimeGeneration != 2 {
				t.Fatal("fresh state mismatch authorized destructive recovery")
			}
		})
	}
}

func TestRuntimeRecoveryOpaqueStartupFailureDoesNotEnterLegacyRetryLoop(t *testing.T) {
	s, r, b, a, i, now := recoveryFixture()
	code, message := "GATEWAY_START_FAILED", "gateway_start_failed"
	i.RuntimeErrorCode = &code
	i.RuntimeErrorMessage = &message
	r.byID[i.ID] = &i
	delete(b.bindings, i.ID)
	handled, errs := s.reconcileInstanceRecoveryLocked(context.Background(), i, now)
	if !handled || len(errs) > 0 || r.state != nil || len(a.deleteRequests) > 0 {
		t.Fatal("opaque startup error re-entered automatic legacy retry")
	}
}

func TestRuntimeRecoveryFreshPermanentNativeFailureOverridesStaleSQLCrash(t *testing.T) {
	for _, field := range []string{"error_message", "error_code"} {
		t.Run(field, func(t *testing.T) {
			s, r, b, a, i, now := recoveryFixture()
			wire := map[string]any{"gateway_id": "gw-7-2", "instance_id": 7, "generation": 2, "state": "error", field: "missing_llm_credentials: secret-test-credential"}
			if field == "error_code" {
				wire[field] = "INVALID_CREDENTIALS"
			}
			encoded, _ := json.Marshal(wire)
			var fresh RuntimeAgentGatewayState
			if err := json.Unmarshal(encoded, &fresh); err != nil {
				t.Fatal(err)
			}
			s.agentClient = &recoveryGatewayStateAgent{fakeRuntimeAgentClient: a, state: &fresh}
			s.reconcileInstanceRecoveryLocked(context.Background(), i, now)
			_, errs := s.reconcileInstanceRecoveryLocked(context.Background(), i, now.Add(30*time.Second))
			s.reconcileInstanceRecoveryLocked(context.Background(), i, now.Add(time.Hour))
			current, _ := r.GetByID(i.ID)
			if len(a.deleteRequests) != 0 || b.bindings[i.ID] == nil || current.RuntimeGeneration != 2 {
				t.Fatal("stale SQL crash reset current permanent failure")
			}
			if r.state.Attempts != 1 || r.state.NextRetryAt != nil {
				t.Fatal("fresh permanent failure did not halt bounded recovery")
			}
			inspected, _ := json.Marshal(r.state)
			stateJSON, _ := json.Marshal(fresh)
			if strings.Contains(string(inspected), "secret-test-credential") || strings.Contains(string(stateJSON), "secret-test-credential") {
				t.Fatal("fresh runtime credentials escaped into inspection")
			}
			for _, err := range errs {
				if strings.Contains(err.Error(), "secret-test-credential") {
					t.Fatal("runtime credentials escaped into recovery error")
				}
			}
		})
	}
}

func TestRuntimeRecoveryFreshOpaqueFailureCannotBorrowStaleCrashAuthority(t *testing.T) {
	s, r, b, a, i, now := recoveryFixture()
	var fresh RuntimeAgentGatewayState
	if err := json.Unmarshal([]byte(`{"gateway_id":"gw-7-2","instance_id":7,"generation":2,"state":"error","error_message":"gateway_start_failed"}`), &fresh); err != nil {
		t.Fatal(err)
	}
	s.agentClient = &recoveryGatewayStateAgent{fakeRuntimeAgentClient: a, state: &fresh}
	s.reconcileInstanceRecoveryLocked(context.Background(), i, now)
	s.reconcileInstanceRecoveryLocked(context.Background(), i, now.Add(30*time.Second))
	current, _ := r.GetByID(i.ID)
	if len(a.deleteRequests) != 0 || b.bindings[i.ID] == nil || current.RuntimeGeneration != 2 || r.state.NextRetryAt != nil {
		t.Fatal("opaque current failure borrowed stale crash permission")
	}
}

func TestRuntimeRecoveryFreshNativeCrashRetainsBoundedBudget(t *testing.T) {
	s, r, _, a, i, now := recoveryFixture()
	var fresh RuntimeAgentGatewayState
	if err := json.Unmarshal([]byte(`{"gateway_id":"gw-7-2","instance_id":7,"generation":2,"state":"error","error_message":"gateway_process_exited"}`), &fresh); err != nil {
		t.Fatal(err)
	}
	s.agentClient = &recoveryGatewayStateAgent{fakeRuntimeAgentClient: a, state: &fresh}
	s.reconcileInstanceRecoveryLocked(context.Background(), i, now)
	s.reconcileInstanceRecoveryLocked(context.Background(), i, now.Add(30*time.Second))
	current, _ := r.GetByID(i.ID)
	if len(a.deleteRequests) != 1 || current.RuntimeGeneration != 3 || r.state.Attempts != 1 || r.state.NextRetryAt == nil {
		t.Fatal("fresh crash was blocked or budget was reset")
	}
}

func TestRuntimeRecoveryCompletedStopResumesAfterFinalAttemptAdvanceFailure(t *testing.T) {
	s, r, b, a, i, now := recoveryFixture()
	delete(b.bindings, i.ID)
	r.state = &models.InstanceRuntimeRecovery{InstanceID: i.ID, Generation: i.RuntimeGeneration, Attempts: 3, Phase: "source_stopped", Reason: "gateway_process_exited", UpdatedAt: now}
	r.advanceErr = errors.New("temporary SQL failure after confirmed stop")
	_, errs := s.reconcileInstanceRecoveryLocked(context.Background(), i, now)
	if len(errs) == 0 {
		t.Fatal("confirmed final stop did not attempt generation advance")
	}
	_, _ = s.reconcileInstanceRecoveryLocked(context.Background(), i, now.Add(time.Hour))
	current, _ := r.GetByID(i.ID)
	if current.RuntimeGeneration != i.RuntimeGeneration+1 || r.state.Attempts != 3 || len(a.deleteRequests) != 0 {
		t.Fatalf("durable stop could not resume without another attempt: instance=%+v state=%+v", current, r.state)
	}
}
func TestRuntimeRecoveryStopCheckpointCannotAuthorizeDifferentGeneration(t *testing.T) {
	s, r, b, a, i, now := recoveryFixture()
	delete(b.bindings, i.ID)
	r.state = &models.InstanceRuntimeRecovery{InstanceID: i.ID, Generation: i.RuntimeGeneration - 1, Attempts: 3, Phase: "source_stopped", UpdatedAt: now}
	_, _ = s.reconcileInstanceRecoveryLocked(context.Background(), i, now)
	current, _ := r.GetByID(i.ID)
	if current.RuntimeGeneration != i.RuntimeGeneration || len(a.deleteRequests) != 0 {
		t.Fatal("old stop receipt authorized current generation")
	}
}

func (r *recoveryTestRepo) ResumeRuntimeRecoveryPlacement(ctx context.Context, i models.Instance) (bool, error) {
	current, _ := r.GetByID(i.ID)
	if current == nil || current.Status != i.Status || current.RuntimeGeneration != i.RuntimeGeneration || r.state == nil || r.state.Phase != "placing" || r.state.Generation != i.RuntimeGeneration {
		return false, nil
	}
	copy := *current
	copy.Status = "creating"
	r.byID[i.ID] = &copy
	return true, nil
}
