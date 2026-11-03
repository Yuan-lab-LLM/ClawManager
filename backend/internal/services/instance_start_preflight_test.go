package services

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestRestartMissingModelsPreservesRunningInstance(t *testing.T) {
	t.Setenv("CLAWMANAGER_LLM_GATEWAY_BASE_URL", "http://manager.test")
	for _, runtimeType := range []string{"openclaw", "hermes", "opencode", "deepseek-harness"} {
		for _, edit := range []bool{false, true} {
			t.Run(runtimeType+map[bool]string{false: "/restart", true: "/environment"}[edit], func(t *testing.T) {
				repo := newV2LifecycleInstanceRepo()
				original := models.Instance{ID: 89, UserID: 45, Type: runtimeType, RuntimeType: "gateway", InstanceMode: "lite", Status: "running", RuntimeGeneration: 8}
				repo.byID[89] = &original
				before := original
				bindings := newFakeRuntimeBindingRepo()
				bindings.bindings[89] = &models.InstanceRuntimeBinding{InstanceID: 89, GatewayID: "gw-89-8", State: "running", Generation: 8}
				s := &instanceService{instanceRepo: repo, bindingRepo: bindings, llmModelRepo: &stubLLMModelRepository{}}
				var err error
				if edit {
					err = s.RestartWithEnvironment(89, map[string]string{"LOG_LEVEL": "debug"}, nil)
				} else {
					err = s.Restart(89)
				}
				if !errors.Is(err, ErrModelConfigurationRequired) {
					t.Fatalf("error = %v, want model configuration failure", err)
				}
				if !reflect.DeepEqual(before, *repo.byID[89]) || len(repo.runtimeStates) != 0 || bindings.bindings[89] == nil || bindings.deleteAndReleaseCalls[89] != 0 {
					t.Fatal("failed preflight mutated instance, generation, environment or binding")
				}
			})
		}
	}
}

func TestStartMissingModelsDoesNotCleanupExistingBinding(t *testing.T) {
	t.Setenv("CLAWMANAGER_LLM_GATEWAY_BASE_URL", "http://manager.test")
	repo := newV2LifecycleInstanceRepo()
	repo.byID[89] = &models.Instance{ID: 89, UserID: 45, Type: "openclaw", RuntimeType: "gateway", Status: "error", RuntimeGeneration: 8}
	bindings := newFakeRuntimeBindingRepo()
	bindings.bindings[89] = &models.InstanceRuntimeBinding{InstanceID: 89, GatewayID: "gw-89-8", State: "failed", Generation: 8}
	s := &instanceService{instanceRepo: repo, bindingRepo: bindings, llmModelRepo: &stubLLMModelRepository{}}
	if err := s.Start(89); !errors.Is(err, ErrModelConfigurationRequired) {
		t.Fatalf("error = %v", err)
	}
	if bindings.bindings[89] == nil || len(repo.runtimeStates) != 0 {
		t.Fatal("Start mutated existing binding/state before validating configuration")
	}
}

type forbidPoolLookup struct{ fakeRuntimePodRepo }

func (*forbidPoolLookup) ListSchedulable(context.Context, string) ([]models.RuntimePod, error) {
	panic("configuration failure must not query capacity")
}

func TestAssignmentMissingModelsDoesNotQueryCapacity(t *testing.T) {
	s := &RuntimeScheduler{podRepo: &forbidPoolLookup{}, envBuilder: func(*models.Instance) (map[string]string, error) {
		return nil, ErrModelConfigurationRequired
	}}
	workspace := "/workspaces/openclaw/user-45/instance-89"
	err := s.assignInstance(context.Background(), models.Instance{ID: 89, Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", Status: "creating", WorkspacePath: &workspace})
	if !errors.Is(err, ErrModelConfigurationRequired) {
		t.Fatalf("error = %v", err)
	}
}

func configureLifecycleModels(t *testing.T, s *instanceService) {
	t.Helper()
	t.Setenv("CLAWMANAGER_LLM_GATEWAY_BASE_URL", "http://manager.test")
	s.llmModelRepo = &stubLLMModelRepository{active: []models.LLMModel{{DisplayName: "test-model"}}}
}

type deniedRestartFenceRepo struct {
	*v2LifecycleInstanceRepo
	calls int
}

func (r *deniedRestartFenceRepo) FenceRuntimeRestart(context.Context, int, int) (bool, error) {
	r.calls++
	return false, nil
}

func TestRestartConcurrentChangeDoesNotStopWriter(t *testing.T) {
	repo := &deniedRestartFenceRepo{v2LifecycleInstanceRepo: newV2LifecycleInstanceRepo()}
	original := models.Instance{ID: 89, UserID: 45, Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", Status: "running", RuntimeGeneration: 8}
	repo.byID[89] = &original
	before := original
	s := &instanceService{instanceRepo: repo}
	configureLifecycleModels(t, s)
	if err := s.Restart(89); err == nil {
		t.Fatal("unclaimed generation restarted")
	}
	if repo.calls != 1 || !reflect.DeepEqual(before, original) || len(repo.runtimeStates) != 0 {
		t.Fatal("concurrent restart mutated running state")
	}
}

func TestModelConfigurationRetriesAreBoundedAndGenerationScoped(t *testing.T) {
	repo := newV2LifecycleInstanceRepo()
	instance := models.Instance{ID: 89, Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", Status: "error", RuntimeGeneration: 8}
	repo.byID[89] = &instance
	s := &RuntimeScheduler{instanceRepo: repo}
	var errs []error
	s.markInstanceError(context.Background(), instance, ErrModelConfigurationRequired, &errs)
	if len(errs) != 0 || !s.configurationRetryPending(instance) {
		t.Fatal("missing retry cooldown", errs)
	}
	next := instance
	next.RuntimeGeneration++
	if s.configurationRetryPending(next) {
		t.Fatal("old failure blocks explicit new generation")
	}
	code, message := "MODEL_CONFIGURATION_REQUIRED", ErrModelConfigurationRequired.Error()
	instance.RuntimeErrorCode, instance.RuntimeErrorMessage = &code, &message
	instance.UpdatedAt = time.Now()
	if isRecoverableRuntimeSchedulingError(instance) {
		t.Fatal("retries immediately")
	}
	instance.UpdatedAt = time.Now().Add(-31 * time.Second)
	if !isRecoverableRuntimeSchedulingError(instance) {
		t.Fatal("never rechecks restored configuration")
	}
	s.configurationRetryAt["89/8"] = time.Now().Add(-time.Second)
	if s.configurationRetryPending(instance) {
		t.Fatal("expired retry cooldown did not release")
	}
}
