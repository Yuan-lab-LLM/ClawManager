package services

import (
	"context"
	"encoding/json"
	"testing"

	"clawreef/internal/models"
	k8sservice "clawreef/internal/services/k8s"
	"k8s.io/client-go/kubernetes/fake"
)

type cleanupHistoryRepo struct {
	*v2LifecycleInstanceRepo
	sources []models.RuntimeCleanupSource
}

func (r *cleanupHistoryRepo) RuntimeCleanupHistory(context.Context, *models.Instance) ([]models.RuntimeCleanupSource, error) {
	return r.sources, nil
}

func TestDeleteUnboundGatewayRequiresProofBeforeResourceCleanup(t *testing.T) {
	for _, scenario := range []string{"never-published", "live-assignment", "stopped-assignment", "recovered-successor", "unproven-migration"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			kube := fake.NewSimpleClientset()
			assignments := k8sservice.NewRuntimeAssignmentService(kube, "runtime")
			base := newV2LifecycleInstanceRepo()
			repo := &cleanupHistoryRepo{v2LifecycleInstanceRepo: base}
			workspace := "/workspaces/openclaw/user-4/instance-7"
			instance := &models.Instance{ID: 7, UserID: 4, Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", Status: "creating", RuntimeGeneration: 1, WorkspacePath: &workspace}
			base.byID[7] = instance
			if scenario != "never-published" {
				source := k8sservice.RuntimeAssignment{InstanceID: 7, UserID: 4, Generation: 1, RuntimeType: "openclaw", Endpoint: "http://old:19091", GatewayID: "gw-7-1", PodUID: "old", Request: json.RawMessage(`{"workspace_path":"/workspaces/openclaw/user-4/instance-7","gateway_port":20000}`)}
				if err := assignments.EnsureDesired(ctx, source); err != nil {
					t.Fatal(err)
				}
				if scenario != "live-assignment" {
					if err := assignments.MarkDraining(ctx, source.Endpoint, source.GatewayID); err != nil {
						t.Fatal(err)
					}
					if err := assignments.ConfirmStopped(ctx, source.Endpoint, source.GatewayID); err != nil {
						t.Fatal(err)
					}
				}
			}
			if scenario == "unproven-migration" {
				repo.sources = []models.RuntimeCleanupSource{{Generation: 1, RuntimePodID: 99, PodUID: "different-old-pod", GatewayID: "gw-7-1", WorkspacePath: workspace}}
			}
			if scenario == "recovered-successor" {
				instance.RuntimeGeneration = 2
				repo.sources = []models.RuntimeCleanupSource{{Generation: 1, RuntimePodID: 99, PodUID: "old", Endpoint: "http://old:19091", GatewayID: "gw-7-1", WorkspacePath: workspace}}
			}
			cleaned := false
			service := &instanceService{instanceRepo: repo, bindingRepo: newFakeRuntimeBindingRepo(), agentClient: &slotRuntimeAgentClient{assignments: assignments}, resourceCleanup: func(context.Context, int) error { cleaned = true; return nil }}
			err := service.deleteV2Instance(ctx, instance)
			blocked := scenario == "live-assignment" || scenario == "unproven-migration"
			if blocked {
				if err == nil || len(base.deleted) != 0 || base.byID[7] == nil || base.byID[7].Status != "deleting" {
					t.Fatalf("unproven source record deleted: err=%v deleted=%v", err, base.deleted)
				}
				if cleaned {
					t.Fatal("resource cleanup ran before missing-binding source was proved stopped")
				}
			} else if err != nil || len(base.deleted) != 1 {
				t.Fatalf("proved cleanup blocked: %v", err)
			}
		})
	}
}

func TestCleanupUnboundStrictAdapterWithoutProofFailsClosed(t *testing.T) {
	for _, strict := range []bool{false, true} {
		for _, bound := range []bool{false, true} {
			bindings := newFakeRuntimeBindingRepo()
			if bound {
				bindings.bindings[7] = &models.InstanceRuntimeBinding{InstanceID: 7, RuntimePodID: 9, Generation: 1, GatewayID: "gw-7-1"}
			}
			s := &instanceService{bindingRepo: bindings, agentClient: &unprovedCleanupAgent{RuntimeAgentClient: &fakeRuntimeAgentClient{}, strict: strict}}
			if err := s.cleanupV2GatewayBinding(context.Background(), &models.Instance{ID: 7}); err == nil {
				t.Fatalf("unknown adapter strict=%v bound=%v bypassed cleanup proof", strict, bound)
			}
			if bound && bindings.bindings[7] == nil {
				t.Fatal("unsupported adapter discarded source binding before discovering missing proof capability")
			}
		}
	}
}

// These test-only adapters explicitly certify fixtures with no publications.
// Production clients without the capability are rejected; no legacy fallback.
func (a *fakeRuntimeAgentClient) VerifyUnboundGatewayCleanup(context.Context, *models.Instance, []models.RuntimeCleanupSource) error {
	return nil
}
func (r *v2LifecycleInstanceRepo) RuntimeCleanupHistory(context.Context, *models.Instance) ([]models.RuntimeCleanupSource, error) {
	return nil, nil
}

type unprovedCleanupAgent struct {
	RuntimeAgentClient
	strict bool
}

func (a *unprovedCleanupAgent) RequiresConfirmedGatewayStop() bool { return a.strict }
