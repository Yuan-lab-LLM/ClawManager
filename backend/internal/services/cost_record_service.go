package services

import (
	"fmt"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// CostRecordService defines application-level operations for token and money accounting.
type CostRecordService interface {
	RecordCost(record *models.CostRecord) error
	ListCostsByTraceID(traceID string) ([]models.CostRecord, error)
	ListCostsByUserID(userID, limit int) ([]models.CostRecord, error)
}

type costRecordService struct {
	repo repository.CostRecordRepository
}

// NewCostRecordService creates a new cost record service.
func NewCostRecordService(repo repository.CostRecordRepository) CostRecordService {
	return &costRecordService{repo: repo}
}

func (s *costRecordService) RecordCost(record *models.CostRecord) error {
	if record == nil {
		return fmt.Errorf("cost record is required")
	}
	if strings.TrimSpace(record.TraceID) == "" {
		return fmt.Errorf("trace id is required")
	}
	if strings.TrimSpace(record.ProviderType) == "" {
		return fmt.Errorf("provider type is required")
	}
	if strings.TrimSpace(record.ModelName) == "" {
		return fmt.Errorf("model name is required")
	}
	if strings.TrimSpace(record.Currency) == "" {
		record.Currency = "USD"
	}

	return s.repo.Create(record)
}

func (s *costRecordService) ListCostsByTraceID(traceID string) ([]models.CostRecord, error) {
	items, err := s.repo.ListByTraceID(strings.TrimSpace(traceID))
	if err != nil {
		return nil, fmt.Errorf("failed to list cost records by trace: %w", err)
	}
	return items, nil
}

func (s *costRecordService) ListCostsByUserID(userID, limit int) ([]models.CostRecord, error) {
	items, err := s.repo.ListByUserID(userID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list cost records by user: %w", err)
	}
	return items, nil
}
