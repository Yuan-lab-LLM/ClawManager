package services

import (
	"fmt"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type sessionModelTokenReporter interface {
	AggregateModelsByInstanceSession(int, string, repository.SessionUsageFilter) ([]models.ModelInvocation, error)
}
type sessionModelCostReporter interface {
	AggregateModelCostByInstanceSession(int, string, repository.SessionUsageFilter) ([]models.CostRecord, error)
}

func sessionInvocationModel(invocation models.ModelInvocation) string {
	if label := strings.TrimSpace(invocation.ActualProviderModel); label != "" {
		return label
	}
	return invocation.RequestedModel
}

func (s *aiObservabilityService) sessionModelBreakdown(instanceID int, sessionID string, filter repository.SessionUsageFilter, recent []models.ModelInvocation) (map[string]*CostBreakdownItem, error) {
	invocations := recent
	if reporter, ok := s.invocationRepo.(sessionModelTokenReporter); ok {
		var err error
		invocations, err = reporter.AggregateModelsByInstanceSession(instanceID, sessionID, filter)
		if err != nil {
			return nil, fmt.Errorf("failed to aggregate session model tokens: %w", err)
		}
	}
	totals := map[string]*CostBreakdownItem{}
	currency := ""
	rowFor := func(label string) *CostBreakdownItem {
		row := totals[label]
		if row == nil {
			row = &CostBreakdownItem{Label: label}
			totals[label] = row
		}
		return row
	}
	for _, invocation := range invocations {
		row := rowFor(sessionInvocationModel(invocation))
		row.PromptTokens += invocation.PromptTokens
		row.CompletionTokens += invocation.CompletionTokens
		row.TotalTokens += invocation.TotalTokens
	}
	if reporter, ok := s.costRepo.(sessionModelCostReporter); ok {
		costs, err := reporter.AggregateModelCostByInstanceSession(instanceID, sessionID, filter)
		if err != nil {
			return nil, fmt.Errorf("failed to aggregate session model costs: %w", err)
		}
		for _, cost := range costs {
			currency, err = repository.MergeCostCurrency(currency, cost.Currency)
			if err != nil {
				return nil, err
			}
			rowFor(cost.ModelName).EstimatedCost += cost.EstimatedCost
		}
		return totals, nil
	}
	if s.costRepo == nil {
		return totals, nil
	}
	// Compatibility for repository doubles: fetch each recent trace once and only
	// accept fees associated with an eligible attempt in the selected session.
	byTrace := map[string][]models.ModelInvocation{}
	for _, invocation := range recent {
		if invocation.Status == models.ModelInvocationStatusBlocked || (invocation.InstanceID != nil && *invocation.InstanceID != instanceID) || (invocation.SessionID != nil && *invocation.SessionID != sessionID) {
			continue
		}
		if filter.Since != nil && invocation.CreatedAt.Before(*filter.Since) {
			continue
		}
		if filter.Until != nil && !invocation.CreatedAt.Before(*filter.Until) {
			continue
		}
		byTrace[invocation.TraceID] = append(byTrace[invocation.TraceID], invocation)
	}
	seen := map[int]bool{}
	for trace, attempts := range byTrace {
		costs, err := s.costRepo.ListByTraceID(trace)
		if err != nil {
			return nil, fmt.Errorf("failed to list session trace costs: %w", err)
		}
		for _, cost := range costs {
			if (cost.ID != 0 && seen[cost.ID]) || (cost.InstanceID != nil && *cost.InstanceID != instanceID) || (cost.SessionID != nil && *cost.SessionID != sessionID) {
				continue
			}
			associated := attempts
			if cost.InvocationID != nil {
				associated = nil
				for _, attempt := range attempts {
					if attempt.ID == *cost.InvocationID {
						associated = append(associated, attempt)
						break
					}
				}
			}
			if len(associated) == 0 {
				continue
			}
			label := strings.TrimSpace(cost.ModelName)
			if label == "" {
				// Old doubles may omit the cost identity; only an unambiguous attempt model can fill it.
				label = sessionInvocationModel(associated[0])
				for _, attempt := range associated[1:] {
					if sessionInvocationModel(attempt) != label {
						label = "Unknown"
						break
					}
				}
			}
			currency, err = repository.MergeCostCurrency(currency, cost.Currency)
			if err != nil {
				return nil, err
			}
			rowFor(label).EstimatedCost += cost.EstimatedCost
			if cost.ID != 0 {
				seen[cost.ID] = true
			}
		}
	}
	return totals, nil
}
