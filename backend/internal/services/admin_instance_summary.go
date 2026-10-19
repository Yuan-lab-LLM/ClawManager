package services

import (
	"clawreef/internal/models"
	"fmt"
)

func (s *instanceService) GetAdminSummary() (*models.InstanceSummary, error) {
	repo, ok := s.instanceRepo.(interface {
		SummarizeAll() (*models.InstanceSummary, error)
	})
	if !ok {
		return nil, fmt.Errorf("admin instance aggregation is unavailable")
	}
	return repo.SummarizeAll()
}
