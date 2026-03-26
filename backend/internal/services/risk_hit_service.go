package services

import (
	"fmt"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// RiskHitService defines operations for persisted risk hits.
type RiskHitService interface {
	RecordHits(traceID string, sessionID, requestID *string, userID, instanceID, invocationID *int, action string, hits []RiskMatch) error
	ListHitsByTraceID(traceID string) ([]models.RiskHit, error)
}

type riskHitService struct {
	repo repository.RiskHitRepository
}

// NewRiskHitService creates a new risk hit service.
func NewRiskHitService(repo repository.RiskHitRepository) RiskHitService {
	return &riskHitService{repo: repo}
}

func (s *riskHitService) RecordHits(traceID string, sessionID, requestID *string, userID, instanceID, invocationID *int, action string, hits []RiskMatch) error {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" || len(hits) == 0 {
		return nil
	}
	if strings.TrimSpace(action) == "" {
		action = models.RiskActionAllow
	}

	for _, hit := range hits {
		record := &models.RiskHit{
			TraceID:      traceID,
			SessionID:    sessionID,
			RequestID:    requestID,
			UserID:       userID,
			InstanceID:   instanceID,
			InvocationID: invocationID,
			RuleID:       strings.TrimSpace(hit.RuleID),
			RuleName:     strings.TrimSpace(hit.RuleName),
			Severity:     strings.TrimSpace(hit.Severity),
			Action:       action,
			MatchSummary: strings.TrimSpace(hit.MatchSummary),
		}
		if record.RuleID == "" || record.RuleName == "" || record.Severity == "" || record.MatchSummary == "" {
			return fmt.Errorf("risk hit record is incomplete")
		}
		if err := s.repo.Create(record); err != nil {
			return fmt.Errorf("failed to record risk hit: %w", err)
		}
	}

	return nil
}

func (s *riskHitService) ListHitsByTraceID(traceID string) ([]models.RiskHit, error) {
	items, err := s.repo.ListByTraceID(strings.TrimSpace(traceID))
	if err != nil {
		return nil, fmt.Errorf("failed to list risk hits by trace: %w", err)
	}
	return items, nil
}
