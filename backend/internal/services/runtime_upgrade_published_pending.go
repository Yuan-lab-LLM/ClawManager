package services

import (
	"context"
	"encoding/json"
	"fmt"

	"clawreef/internal/models"
)

type publishedUpgradeReservationVerifier interface {
	verifyPublishedUpgradeReservation(context.Context, runtimeUpgradeCandidate, models.InstanceRuntimeBinding) (string, error)
}

// This is identity evidence only, never an execution/termination observation.
// A published successor still needs the ordinary native Delete/cancel receipt.
func (c *slotRuntimeAgentClient) verifyPublishedUpgradeReservation(ctx context.Context, want runtimeUpgradeCandidate, b models.InstanceRuntimeBinding) (string, error) {
	if c.assignments == nil || want.RuntimePodID == nil || b.RuntimePodID != *want.RuntimePodID || b.InstanceID != want.InstanceID || b.Generation != want.Generation || b.State != "creating" || b.GatewayPID != nil || b.GatewayID != fmt.Sprintf("pending-%d-%d", want.InstanceID, want.Generation) || b.WorkspacePath != want.WorkspacePath || want.SourcePodUID == "" {
		return "", ErrUpgradeSourceState
	}
	id, err := c.assignments.ResolvePendingGatewayID(ctx, want.AgentEndpoint, b.GatewayID)
	if err != nil {
		return "", err
	}
	a, state, _, _, err := c.assignments.ReadContainerStop(ctx, want.AgentEndpoint, id)
	if err != nil {
		return "", err
	}
	var original RuntimeAgentCreateGatewayRequest
	if a.InstanceID != want.InstanceID || a.UserID != want.UserID || a.Generation != want.Generation || a.RuntimeType != "openclaw" || a.PodUID != want.SourcePodUID || json.Unmarshal(a.Request, &original) != nil || original.InstanceID != a.InstanceID || original.UserID != a.UserID || original.Generation != a.Generation || original.AgentType != a.RuntimeType || original.PodUID != a.PodUID || original.GatewayPort != b.GatewayPort || original.WorkspacePath != b.WorkspacePath {
		return "", ErrUpgradeSourceState
	}
	_, err = c.containerStopPod(ctx, want.AgentEndpoint, want.SourcePodUID)
	return state, err
}

func (s *RuntimeUpgradeService) retireConfirmedPublishedUpgradeReservation(ctx context.Context, c runtimeUpgradeCandidate, b models.InstanceRuntimeBinding, claim string) (bool, error) {
	verifier, ok := s.agent.(publishedUpgradeReservationVerifier)
	if !ok {
		return false, ErrUpgradeSourceState
	}
	var authorized int
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_upgrade_items ui JOIN runtime_rollouts r ON r.id=ui.rollout_id WHERE ui.instance_id=? AND ui.source_generation=? AND BINARY ui.operation_token=BINARY ? AND ui.operation_deadline_at>UTC_TIMESTAMP(6) AND ui.operation_phase='session_migration' AND r.status='running' AND r.recovery_key IS NOT NULL AND JSON_UNQUOTE(JSON_EXTRACT(r.preflight_json,'$.recovery_mode')) IN ('retry','rebuild')`, c.InstanceID, c.Generation, claim)
	if err != nil {
		return false, err
	}
	if err = row.Scan(&authorized); err != nil {
		return false, err
	}
	if authorized != 1 {
		return false, ErrUpgradeSourceState
	}
	if _, err = verifier.verifyPublishedUpgradeReservation(ctx, c, b); err != nil {
		return false, err
	}
	// Run native teardown outside SQL admission locks. It persists the real
	// writer fence/receipt before the exact reservation can be retired below.
	if err = s.agent.DeleteGateway(ctx, c.AgentEndpoint, c.GatewayID); err != nil {
		return false, err
	}
	store := s.bindings.(interface {
		RetireUnpublishedUpgradeReservation(context.Context, models.InstanceRuntimeBinding, string, func(context.Context, int, string) error) (bool, error)
	})
	return store.RetireUnpublishedUpgradeReservation(ctx, b, claim, func(ctx context.Context, user int, workspace string) error {
		if user != c.UserID || workspace != c.WorkspacePath {
			return ErrUpgradeSourceState
		}
		state, err := verifier.verifyPublishedUpgradeReservation(ctx, c, b)
		if err != nil {
			return err
		}
		if state != "stopped" {
			return ErrUpgradeSourceState
		}
		return nil
	})
}
