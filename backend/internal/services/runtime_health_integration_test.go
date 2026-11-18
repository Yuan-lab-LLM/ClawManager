package services

import (
	"context"
	"errors"
	"testing"

	"clawreef/internal/models"
)

func TestRuntimeRecoveryPlacementAvoidsFailedSourceSlot(t *testing.T) {
	agent := &occupiedPortsAgent{strictSchedulerAgent: &strictSchedulerAgent{&fakeRuntimeAgentClient{}}, ports: map[int]struct{}{}}
	s := &RuntimeScheduler{bindingRepo: newFakeRuntimeBindingRepo(), agentClient: agent, gatewayPortStart: 20000, gatewayPortEnd: 20001}
	ctx := context.WithValue(context.Background(), runtimeRecoverySourceKey{}, runtimeRecoveryPlacementSource{podID: 132, gatewayPort: 20000})
	binding, err := s.reserveGatewayPort(ctx, models.Instance{ID: 161, RuntimeGeneration: 3}, RuntimeTypeHermes, models.RuntimePod{ID: 132, Capacity: 2}, "/workspace")
	if err != nil || binding.GatewayPort != 20001 {
		t.Fatalf("recovery reused failed source slot: %+v %v", binding, err)
	}
}

func TestRuntimeSlotAuditUncertaintyKeepsCreatingIntent(t *testing.T) {
	agent := &occupiedPortsAgent{strictSchedulerAgent: &strictSchedulerAgent{&fakeRuntimeAgentClient{}}, err: errors.New("slot proof unavailable")}
	s := &RuntimeScheduler{bindingRepo: newFakeRuntimeBindingRepo(), agentClient: agent, gatewayPortStart: 20000, gatewayPortEnd: 20001}
	_, err := s.reserveGatewayPort(context.Background(), models.Instance{ID: 161, RuntimeGeneration: 3}, RuntimeTypeHermes, models.RuntimePod{ID: 132, Capacity: 2}, "/workspace")
	if !isRuntimeAssignmentPending(err) {
		t.Fatalf("temporary audit failure became terminal startup error: %v", err)
	}
}

type runtimeLayoutProofAgent struct {
	*occupiedPortsAgent
}

func (*runtimeLayoutProofAgent) physicalGatewayPorts(context.Context, models.RuntimePod) (map[int]struct{}, error) {
	return map[int]struct{}{20000: {}, 20001: {}}, nil
}

func TestRuntimeSlotAuditRejectsDifferentAllocatorPortLayout(t *testing.T) {
	agent := &runtimeLayoutProofAgent{&occupiedPortsAgent{strictSchedulerAgent: &strictSchedulerAgent{&fakeRuntimeAgentClient{}}, ports: map[int]struct{}{}}}
	s := &RuntimeScheduler{bindingRepo: newFakeRuntimeBindingRepo(), agentClient: agent, gatewayPortStart: 20010, gatewayPortEnd: 20011}
	_, err := s.reserveGatewayPort(context.Background(), models.Instance{ID: 161, RuntimeGeneration: 3}, RuntimeTypeHermes, models.RuntimePod{ID: 132, Capacity: 2}, "/workspace")
	if !isRuntimeAssignmentPending(err) {
		t.Fatalf("allocator admitted a port absent from the physical slot layout: %v", err)
	}
}
