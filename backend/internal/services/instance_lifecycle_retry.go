package services

import (
	"clawreef/internal/models"
	"fmt"
	"strings"
)

// ReconcileResetRetry never resets an already-promoted replacement. A lost
// create response is resolved through the durable provisioning operation ID.
func (s *instanceService) ReconcileResetRetry(sourceID int, previousOperationID string) (*models.Instance, error) {
	repo, ok := s.instanceRepo.(interface {
		GetByProvisioningOperationID(string) (*models.Instance, error)
	})
	if !ok {
		return nil, fmt.Errorf("durable reset reconciliation unavailable")
	}
	replacement, err := repo.GetByProvisioningOperationID("reset_" + previousOperationID)
	if err != nil {
		return nil, err
	}
	source, err := s.instanceRepo.GetByID(sourceID)
	if err != nil {
		return nil, err
	}
	if replacement == nil {
		if source == nil {
			return nil, fmt.Errorf("source and replacement are absent; refusing to reset")
		}
		if source.Owner != nil && *source.Owner == factoryResetQuarantineOwner && source.Name == factoryResetQuarantineName(sourceID) {
			return nil, fmt.Errorf("source was cut over but replacement is missing")
		}
		return nil, nil
	}
	if replacement.ID == sourceID || (source != nil && replacement.UserID != source.UserID) {
		return nil, fmt.Errorf("replacement identity mismatch")
	}
	staging := replacement.Owner != nil && *replacement.Owner == factoryResetStagingOwner
	if !staging {
		// Only a proven cutover can be completed/cleaned; never treat a quarantined
		// failed staging instance as the user's promoted replacement.
		if strings.HasPrefix(replacement.Name, "reset-") {
			return nil, fmt.Errorf("failed replacement cleanup is incomplete")
		}
		if source != nil && !(source.Owner != nil && *source.Owner == factoryResetQuarantineOwner && source.Name == factoryResetQuarantineName(sourceID)) {
			return nil, fmt.Errorf("unconfirmed replacement cutover")
		}
		return replacement, nil
	}
	if source == nil {
		return nil, fmt.Errorf("source missing while replacement is staged")
	}
	if replacement.Status == "running" {
		return replacement, nil
	}
	if replacement.Status != "error" && replacement.Status != "stopped" {
		return nil, fmt.Errorf("replacement is still in progress; refusing duplicate creation")
	}
	if err := s.DiscardResetReplacement(replacement.ID); err != nil {
		return nil, err
	}
	left, err := s.instanceRepo.GetByID(replacement.ID)
	if err != nil {
		return nil, err
	}
	if left != nil {
		return nil, fmt.Errorf("replacement cleanup has not completed")
	}
	return nil, nil
}
