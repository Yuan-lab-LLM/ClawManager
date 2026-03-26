package services

import (
	"fmt"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// ModelInvocationService defines application-level operations for governed model calls.
type ModelInvocationService interface {
	RecordInvocation(invocation *models.ModelInvocation) error
	GetInvocationByID(id int) (*models.ModelInvocation, error)
	ListInvocationsByTraceID(traceID string) ([]models.ModelInvocation, error)
	ListInvocationsBySessionID(sessionID string, limit int) ([]models.ModelInvocation, error)
	ListInvocationsByUserID(userID, limit int) ([]models.ModelInvocation, error)
}

type modelInvocationService struct {
	repo repository.ModelInvocationRepository
}

// NewModelInvocationService creates a new model invocation service.
func NewModelInvocationService(repo repository.ModelInvocationRepository) ModelInvocationService {
	return &modelInvocationService{repo: repo}
}

func (s *modelInvocationService) RecordInvocation(invocation *models.ModelInvocation) error {
	if invocation == nil {
		return fmt.Errorf("model invocation is required")
	}
	if strings.TrimSpace(invocation.TraceID) == "" {
		return fmt.Errorf("trace id is required")
	}
	if strings.TrimSpace(invocation.RequestID) == "" {
		return fmt.Errorf("request id is required")
	}
	if strings.TrimSpace(invocation.ProviderType) == "" {
		return fmt.Errorf("provider type is required")
	}
	if strings.TrimSpace(invocation.RequestedModel) == "" {
		return fmt.Errorf("requested model is required")
	}
	if strings.TrimSpace(invocation.ActualProviderModel) == "" {
		return fmt.Errorf("actual provider model is required")
	}
	if strings.TrimSpace(invocation.TrafficClass) == "" {
		invocation.TrafficClass = models.TrafficClassLLM
	}
	if strings.TrimSpace(invocation.Status) == "" {
		invocation.Status = models.ModelInvocationStatusPending
	}

	return s.repo.Create(invocation)
}

func (s *modelInvocationService) GetInvocationByID(id int) (*models.ModelInvocation, error) {
	item, err := s.repo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get model invocation: %w", err)
	}
	return item, nil
}

func (s *modelInvocationService) ListInvocationsByTraceID(traceID string) ([]models.ModelInvocation, error) {
	items, err := s.repo.ListByTraceID(strings.TrimSpace(traceID))
	if err != nil {
		return nil, fmt.Errorf("failed to list model invocations by trace: %w", err)
	}
	return items, nil
}

func (s *modelInvocationService) ListInvocationsBySessionID(sessionID string, limit int) ([]models.ModelInvocation, error) {
	items, err := s.repo.ListBySessionID(strings.TrimSpace(sessionID), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list model invocations by session: %w", err)
	}
	return items, nil
}

func (s *modelInvocationService) ListInvocationsByUserID(userID, limit int) ([]models.ModelInvocation, error) {
	items, err := s.repo.ListByUserID(userID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list model invocations by user: %w", err)
	}
	return items, nil
}
