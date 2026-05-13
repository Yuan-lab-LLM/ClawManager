package services

import (
	"fmt"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// InstanceUsageService defines application-level operations for instance
// resource usage monitoring.
type InstanceUsageService interface {
	// GetCurrentUsage returns the most recent usage snapshot for an instance.
	GetCurrentUsage(instanceID int) (*models.InstanceUsage, error)
	// GetHistory returns usage records for an instance within a time window.
	GetHistory(instanceID int, hours int) ([]models.InstanceUsage, error)
	// GetAllCurrentUsage returns the latest usage snapshot for every instance
	// that has at least one record (admin view).
	GetAllCurrentUsage() ([]models.InstanceUsage, error)
	// RecordUsage persists a single usage snapshot.
	RecordUsage(record *models.InstanceUsage) error
}

type instanceUsageService struct {
	repo repository.InstanceUsageRepository
}

// NewInstanceUsageService creates a new instance usage service.
func NewInstanceUsageService(repo repository.InstanceUsageRepository) InstanceUsageService {
	return &instanceUsageService{repo: repo}
}

func (s *instanceUsageService) GetCurrentUsage(instanceID int) (*models.InstanceUsage, error) {
	if instanceID <= 0 {
		return nil, fmt.Errorf("invalid instance ID")
	}
	return s.repo.GetLatestByInstanceID(instanceID)
}

func (s *instanceUsageService) GetHistory(instanceID int, hours int) ([]models.InstanceUsage, error) {
	if instanceID <= 0 {
		return nil, fmt.Errorf("invalid instance ID")
	}
	if hours <= 0 {
		hours = 24
	}
	if hours > 720 { // cap at 30 days
		hours = 720
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	return s.repo.ListByInstanceID(instanceID, since, 0)
}

func (s *instanceUsageService) GetAllCurrentUsage() ([]models.InstanceUsage, error) {
	return s.repo.ListLatestPerInstance()
}

func (s *instanceUsageService) RecordUsage(record *models.InstanceUsage) error {
	if record == nil {
		return fmt.Errorf("usage record is required")
	}
	if record.InstanceID <= 0 {
		return fmt.Errorf("instance ID is required")
	}
	return s.repo.Create(record)
}

