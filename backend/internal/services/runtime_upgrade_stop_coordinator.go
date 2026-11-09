package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// Internal phase coordinator only. Not registered as an API or background job.
// It neither creates a drain/reservation nor permits backup, replacement or
// fence release. Those require stronger writer isolation and recovery support.
type RuntimeUpgradeStopCoordinator struct {
	upgrades  repository.RuntimeUpgradeStopRepository
	instances repository.InstanceRepository
	bindings  repository.InstanceRuntimeBindingRepository
	pods      repository.RuntimePodRepository
	agent     RuntimeAgentStopClient
}

func NewRuntimeUpgradeStopCoordinator(upgrades repository.RuntimeUpgradeStopRepository, instances repository.InstanceRepository, bindings repository.InstanceRuntimeBindingRepository, pods repository.RuntimePodRepository, agent RuntimeAgentStopClient) *RuntimeUpgradeStopCoordinator {
	return &RuntimeUpgradeStopCoordinator{upgrades, instances, bindings, pods, agent}
}

// StopReserved retries only a previously pinned target. Timeouts leave the
// durable intent in place; a controller restart must not guess another target.
func (c *RuntimeUpgradeStopCoordinator) StopReserved(ctx context.Context, id, owner, generation int, operation string) (*models.RuntimeUpgradeStop, error) {
	if id <= 0 || owner <= 0 || generation < 0 || !models.ValidRuntimeOperation(operation) {
		return nil, models.ErrRuntimeUpgradeConflict
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	upgrade, err := c.upgrades.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if upgrade == nil || upgrade.OwnerID != owner || upgrade.Generation != generation || upgrade.Operation != operation ||
		(upgrade.State != "reserved" && upgrade.State != "stop_intent" && upgrade.State != "process_group_confirmed") {
		return nil, models.ErrRuntimeUpgradeConflict
	}
	uncertain := func(cause error) (*models.RuntimeUpgradeStop, error) {
		if upgrade.State == "reserved" {
			return nil, cause
		}
		// Persist uncertainty even if the caller disconnected. This never sends
		// a new stop or releases a fence, and is bounded independently.
		recoveryCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer done()
		err := c.upgrades.MarkRecoveryRequired(recoveryCtx, id, owner, generation, operation)
		return nil, errors.Join(cause, err)
	}
	prior, err := c.upgrades.GetStop(ctx, id)
	if err != nil {
		return nil, err
	}
	if upgrade.State != "reserved" && prior == nil {
		return uncertain(models.ErrRuntimeUpgradeConflict)
	}
	instance, err := c.instances.GetByID(id)
	if err != nil {
		return nil, err
	}
	if instance == nil || instance.UserID != owner || instance.RuntimeGeneration != generation || instance.Type != "opencode" || instance.Status != "running" {
		return uncertain(models.ErrRuntimeUpgradeConflict)
	}
	binding, err := c.bindings.GetByInstanceID(ctx, id)
	if err != nil {
		return nil, err
	}
	if binding == nil || binding.InstanceID != id || binding.Generation != generation || binding.State != "running" || binding.GatewayPID == nil {
		return uncertain(models.ErrRuntimeUpgradeConflict)
	}
	pod, err := c.pods.GetByID(ctx, binding.RuntimePodID)
	if err != nil {
		return nil, err
	}
	if pod == nil || pod.ID != binding.RuntimePodID || pod.PodUID == nil || pod.AgentEndpoint == nil {
		return uncertain(models.ErrRuntimeUpgradeConflict)
	}
	target := models.RuntimeUpgradeStop{InstanceID: id, OwnerID: owner, Generation: generation, Operation: operation,
		RuntimePodID: pod.ID, PodUID: *pod.PodUID, AgentEndpoint: *pod.AgentEndpoint, GatewayID: binding.GatewayID, PID: *binding.GatewayPID}
	// Do not contact a changed endpoint on retry, even for capabilities.
	if prior != nil {
		target.AgentEpoch = prior.AgentEpoch
		if !target.SameTarget(*prior) {
			return uncertain(models.ErrRuntimeUpgradeConflict)
		}
	}
	caps, err := c.capabilities(ctx, target.AgentEndpoint)
	if err != nil {
		return nil, err
	}
	if !validUpgradeStopCapabilities(caps) || caps.PodUID != target.PodUID || (prior != nil && caps.AgentEpoch != prior.AgentEpoch) {
		return uncertain(ErrRuntimeStopUnconfirmed)
	}
	target.AgentEpoch = caps.AgentEpoch
	if !target.ValidTarget() {
		return uncertain(models.ErrRuntimeUpgradeConflict)
	}
	// Transaction rechecks current placement, fresh drain (first attempt) and
	// operation state. Failure, including ambiguous commit, must not send HTTP.
	if err := c.upgrades.PrepareStop(ctx, target); err != nil {
		return nil, err
	}
	upgrade.State = "stop_intent"
	req := RuntimeAgentStopRequest{InstanceID: id, UserID: owner, Generation: generation, GatewayID: target.GatewayID,
		OperationID: operation, PodUID: target.PodUID, AgentEpoch: target.AgentEpoch, TimeoutSeconds: 20, ExpectedPID: target.PID}
	stopCtx, stopCancel := context.WithTimeout(ctx, 25*time.Second)
	receipt, err := c.agent.ConfirmGatewayStop(stopCtx, target.AgentEndpoint, req)
	stopCancel()
	if err != nil {
		// A timeout or lost response can mean the stop happened. Keep the
		// immutable intent so the exact same Agent operation can be queried again.
		if errors.Is(err, ErrRuntimeStopReceiptMismatch) {
			return uncertain(err)
		}
		return nil, fmt.Errorf("%w: %w", ErrRuntimeStopUnconfirmed, err)
	}
	if !validRuntimeStopReceipt(req, receipt) || receipt.PID != target.PID {
		return uncertain(ErrRuntimeStopUnconfirmed)
	}
	// Detect a restart while the stop response was in flight. A recorded receipt
	// remains historical evidence, never authorization to start copying data.
	caps, err = c.capabilities(ctx, target.AgentEndpoint)
	if err != nil {
		return nil, err
	}
	if !validUpgradeStopCapabilities(caps) || caps.PodUID != target.PodUID || caps.AgentEpoch != target.AgentEpoch {
		return uncertain(ErrRuntimeStopUnconfirmed)
	}
	target.ConfirmationScope, target.ConfirmedAt = receipt.ConfirmationScope, &receipt.ConfirmedAt
	if err := c.upgrades.RecordStopConfirmation(ctx, target); err != nil {
		return nil, err
	}
	stored, err := c.upgrades.GetStop(ctx, id)
	if err != nil {
		return nil, err
	}
	if stored == nil || !stored.SameTarget(target) || stored.ConfirmedAt == nil || stored.ConfirmationScope != "process_group" {
		return nil, ErrRuntimeStopUnconfirmed
	}
	return stored, nil
}

func (c *RuntimeUpgradeStopCoordinator) capabilities(ctx context.Context, endpoint string) (*RuntimeAgentStopCapabilities, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.agent.StopCapabilities(ctx, endpoint)
}

func validUpgradeStopCapabilities(caps *RuntimeAgentStopCapabilities) bool {
	return caps != nil && caps.Protocol == 1 && caps.GracefulStop && caps.ExpectedPIDGuard && caps.PodUID != "" && stopTokenValid(caps.AgentEpoch) && caps.ConfirmationScope == "process_group"
}
