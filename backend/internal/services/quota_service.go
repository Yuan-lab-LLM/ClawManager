package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// QuotaService defines the interface for quota operations
type QuotaService interface {
	GetUserQuota(userID int) (*models.UserQuota, error)
	UpdateUserQuota(userID int, quota *models.UserQuota) error
	CreateDefaultQuota(userID int) (*models.UserQuota, error)
	CheckUserQuota(userID int, requiredCPU float64, requiredMemory, requiredStorage int) error
}

// quotaService implements QuotaService
type quotaService struct {
	quotaRepo repository.QuotaRepository
}

// NewQuotaService creates a new quota service
func NewQuotaService(quotaRepo repository.QuotaRepository) QuotaService {
	return &quotaService{
		quotaRepo: quotaRepo,
	}
}

// SyncDefaultQuotaForRole upgrades a user who still has the ordinary default
// quota when the directory assigns the administrator role. Custom quotas are
// intentionally left untouched.
func SyncDefaultQuotaForRole(quotaRepo repository.QuotaRepository, userID int, role string) error {
	if quotaRepo == nil || !isAdministratorRole(role) {
		return nil
	}

	quota, err := quotaRepo.GetByUserID(userID)
	if err != nil {
		return fmt.Errorf("failed to get quota for role sync: %w", err)
	}
	created := quota == nil
	if created {
		quota, err = quotaRepo.CreateDefaultQuota(userID)
		if err != nil {
			return fmt.Errorf("failed to create quota for role sync: %w", err)
		}
	}
	if !created && !quota.IsDefaultForRole("user") {
		return nil
	}

	quota.ApplyResourceValues(models.DefaultQuotaForRole("admin"))
	quota.UpdatedAt = time.Now()
	if err := quotaRepo.Update(quota); err != nil {
		return fmt.Errorf("failed to apply administrator default quota: %w", err)
	}
	return nil
}

func isAdministratorRole(role string) bool {
	return strings.EqualFold(strings.TrimSpace(role), "admin")
}

// GetUserQuota gets quota for a user
func (s *quotaService) GetUserQuota(userID int) (*models.UserQuota, error) {
	quota, err := s.quotaRepo.GetByUserID(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get quota: %w", err)
	}

	if quota == nil {
		// Create default quota if not exists
		quota, err = s.quotaRepo.CreateDefaultQuota(userID)
		if err != nil {
			return nil, fmt.Errorf("failed to create default quota: %w", err)
		}
	}

	return quota, nil
}

// UpdateUserQuota updates quota for a user
func (s *quotaService) UpdateUserQuota(userID int, quota *models.UserQuota) error {
	existingQuota, err := s.quotaRepo.GetByUserID(userID)
	if err != nil {
		return fmt.Errorf("failed to get existing quota: %w", err)
	}

	if existingQuota == nil {
		// Create new quota
		quota.UserID = userID
		return s.quotaRepo.Create(quota)
	}

	// Update existing quota
	existingQuota.MaxInstances = quota.MaxInstances
	existingQuota.MaxCPUCores = quota.MaxCPUCores
	existingQuota.MaxMemoryGB = quota.MaxMemoryGB
	existingQuota.MaxStorageGB = quota.MaxStorageGB
	existingQuota.MaxGPUCount = quota.MaxGPUCount

	return s.quotaRepo.Update(existingQuota)
}

// CreateDefaultQuota creates default quota for a user
func (s *quotaService) CreateDefaultQuota(userID int) (*models.UserQuota, error) {
	return s.quotaRepo.CreateDefaultQuota(userID)
}

// CheckUserQuota checks if user has enough quota for new instance
func (s *quotaService) CheckUserQuota(userID int, requiredCPU float64, requiredMemory, requiredStorage int) error {
	quota, err := s.GetUserQuota(userID)
	if err != nil {
		return fmt.Errorf("failed to get user quota: %w", err)
	}

	// Check instance count (will be implemented when instance repo is ready)
	// For now, just check resource limits

	if requiredCPU > quota.MaxCPUCores {
		return errors.New("insufficient CPU quota")
	}

	if requiredMemory > quota.MaxMemoryGB {
		return errors.New("insufficient memory quota")
	}

	if requiredStorage > quota.MaxStorageGB {
		return errors.New("insufficient storage quota")
	}

	return nil
}
