package services

import (
	"context"
	"fmt"

	"clawreef/internal/models"
)

func (s *RuntimeUpgradeService) retireUnpublishedUpgradeReservation(ctx context.Context, c runtimeUpgradeCandidate) (bool, error) {
	claim, _ := ctx.Value(upgradeClaimKey{}).(string)
	store, ok := s.bindings.(interface {
		RetireUnpublishedUpgradeReservation(context.Context, models.InstanceRuntimeBinding, string, func(context.Context, int, string) error) (bool, error)
	})
	reader, hasReader := s.deployments.(teamStoppedSourceReader)
	if !ok || !hasReader || claim == "" || c.RuntimePodID == nil {
		return false, ErrUpgradeSourceState
	}
	b, err := s.bindings.GetByInstanceID(ctx, c.InstanceID)
	if err != nil {
		return false, err
	}
	if b == nil || b.RuntimePodID != *c.RuntimePodID || b.Generation != c.Generation || b.GatewayID != c.GatewayID || b.WorkspacePath != c.WorkspacePath {
		return false, ErrUpgradeSourceState
	}
	if verifier, published := s.agent.(publishedUpgradeReservationVerifier); published {
		if _, verified := verifier.verifyPublishedUpgradeReservation(ctx, c, *b); verified == nil {
			return s.retireConfirmedPublishedUpgradeReservation(ctx, c, *b, claim)
		}
	}
	return store.RetireUnpublishedUpgradeReservation(ctx, *b, claim, func(ctx context.Context, user int, workspace string) error {
		proof, err := reader.ConfirmedTeamStoppedSource(ctx, c.InstanceID, user, c.Generation, workspace)
		if err != nil {
			return err
		}
		if proof.InstanceID != c.InstanceID || proof.UserID != user || proof.Generation >= c.Generation || proof.Generation < 1 || proof.Phase != "completed" || proof.WorkspacePath != workspace || proof.StorageIdentity == "" || proof.PodUID == "" || proof.RuntimePodID <= 0 || proof.GatewayID == "" || proof.Endpoint == "" {
			return fmt.Errorf("%w: unpublished reservation has no finalized source", ErrUpgradeSourceState)
		}
		return nil
	})
}
