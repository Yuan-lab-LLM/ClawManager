package services

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/workbench"
)

type workbenchTestInstances struct {
	instance *models.Instance
	mu       *sync.RWMutex
}

func (r workbenchTestInstances) GetByID(int) (*models.Instance, error) {
	if r.mu != nil {
		r.mu.RLock()
		defer r.mu.RUnlock()
	}
	if r.instance == nil {
		return nil, nil
	}
	snapshot := *r.instance
	return &snapshot, nil
}

type workbenchTestBindings struct {
	binding *models.InstanceRuntimeBinding
}

func (r workbenchTestBindings) GetRunningByInstanceID(context.Context, int) (*models.InstanceRuntimeBinding, error) {
	return r.binding, nil
}

type workbenchTestPods struct{ pod *models.RuntimePod }

func (r workbenchTestPods) GetByID(context.Context, int64) (*models.RuntimePod, error) {
	return r.pod, nil
}

type workbenchTestAgent struct{ requests []WorkbenchRequest }

func (a *workbenchTestAgent) Workbench(ctx context.Context, endpoint string, req WorkbenchRequest) (json.RawMessage, error) {
	a.requests = append(a.requests, req)
	return json.RawMessage(`{"git":true,"terminal":true,"preview":true}`), nil
}
func TestWorkbenchResolvesOwnerAndCurrentGenerationBeforeExecution(t *testing.T) {
	endpoint := "https://runtime.example:8080"
	instance := &models.Instance{ID: 42, UserID: 7, Status: "running", RuntimeGeneration: 3}
	binding := &models.InstanceRuntimeBinding{InstanceID: 42, RuntimePodID: 9, Generation: 3, GatewayID: "gw-42-3", GatewayPort: 3000, State: "running", WorkspacePath: "/workspace/7/42"}
	agent := &workbenchTestAgent{}
	service := NewInstanceWorkbenchService(workbenchTestInstances{instance: instance}, workbenchTestBindings{binding}, workbenchTestPods{&models.RuntimePod{ID: 9, State: "ready", AgentEndpoint: &endpoint}}, agent, nil)
	ctx := context.Background()
	if _, err := service.Execute(ctx, 8, false, 42, 3, "git", json.RawMessage(`{"operation":"projects"}`)); !errors.Is(err, ErrWorkbenchDenied) {
		t.Fatalf("unrelated owner allowed: %v", err)
	}
	if _, err := service.Execute(ctx, 7, false, 42, 2, "git", nil); !errors.Is(err, ErrWorkbenchStale) {
		t.Fatalf("stale request allowed: %v", err)
	}
	instance.Status = "sleeping"
	if _, err := service.Execute(ctx, 7, false, 42, 3, "git", nil); !errors.Is(err, ErrWorkbenchUnavailable) {
		t.Fatalf("sleeping instance allowed: %v", err)
	}
	instance.Status = "running"
	binding.Generation = 2
	if _, err := service.Execute(ctx, 7, false, 42, 3, "git", nil); !errors.Is(err, ErrWorkbenchStale) {
		t.Fatalf("stale binding allowed: %v", err)
	}
	binding.Generation = 3
	if len(agent.requests) != 0 {
		t.Fatal("denied request reached business executor")
	}
	if _, err := service.Execute(ctx, 7, false, 42, 3, "git", json.RawMessage(`{"operation":"projects"}`)); err != nil {
		t.Fatal(err)
	}
	if len(agent.requests) != 1 || agent.requests[0].PreviewScope != (workbench.PreviewScope{InstanceID: 42, UserID: 7, Generation: 3, GatewayID: "gw-42-3"}) {
		t.Fatalf("scope not derived from live binding: %+v", agent.requests)
	}
	if _, err := service.Execute(ctx, 99, true, 42, 3, "terminal.list", nil); err != nil {
		t.Fatalf("admin cannot use owner workspace: %v", err)
	}
	if agent.requests[1].UserID != 7 {
		t.Fatal("admin identity substituted for business owner")
	}
}

func TestWorkbenchLegacyRuntimeRequestsUpgradeWithoutHostExecution(t *testing.T) {
	instance := &models.Instance{ID: 42, UserID: 7, Status: "running"}
	agent := &workbenchTestAgent{}
	service := NewInstanceWorkbenchService(workbenchTestInstances{instance: instance}, workbenchTestBindings{}, workbenchTestPods{}, agent, nil)
	if _, err := service.Capabilities(context.Background(), 7, false, 42); !errors.Is(err, ErrRuntimeAgentUnsupported) {
		t.Fatalf("legacy runtime did not request upgrade: %v", err)
	}
	if len(agent.requests) != 0 {
		t.Fatal("legacy runtime executed a host command")
	}
}
