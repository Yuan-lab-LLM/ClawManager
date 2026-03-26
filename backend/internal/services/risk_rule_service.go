package services

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// RiskRuleService defines admin risk rule management operations.
type RiskRuleService interface {
	ListRules() ([]models.RiskRule, error)
	SaveRule(req SaveRiskRuleRequest) (*models.RiskRule, error)
	DeleteRule(ruleID string) error
	BulkSetEnabled(ruleIDs []string, enabled bool) error
	TestRules(req TestRiskRulesRequest) (RiskAnalysis, error)
}

// SaveRiskRuleRequest contains editable fields for risk rules.
type SaveRiskRuleRequest struct {
	RuleID      string
	DisplayName string
	Description *string
	Pattern     string
	Severity    string
	Action      string
	IsEnabled   bool
	SortOrder   int
}

// TestRiskRulesRequest contains data for rule preview and testing.
type TestRiskRulesRequest struct {
	Text string
	Rule *SaveRiskRuleRequest
}

type riskRuleService struct {
	repo repository.RiskRuleRepository
}

// NewRiskRuleService creates a new risk rule service.
func NewRiskRuleService(repo repository.RiskRuleRepository) RiskRuleService {
	return &riskRuleService{repo: repo}
}

func (s *riskRuleService) ListRules() ([]models.RiskRule, error) {
	items, err := s.repo.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list risk rules: %w", err)
	}
	return items, nil
}

func (s *riskRuleService) SaveRule(req SaveRiskRuleRequest) (*models.RiskRule, error) {
	rule, err := s.buildRuleModel(req)
	if err != nil {
		return nil, err
	}

	if err := s.repo.Upsert(rule); err != nil {
		return nil, fmt.Errorf("failed to save risk rule: %w", err)
	}

	return rule, nil
}

func (s *riskRuleService) DeleteRule(ruleID string) error {
	ruleID = strings.TrimSpace(ruleID)
	if ruleID == "" {
		return errors.New("rule id is required")
	}

	existing, err := s.repo.GetByRuleID(ruleID)
	if err != nil {
		return fmt.Errorf("failed to get risk rule: %w", err)
	}
	if existing == nil {
		return errors.New("risk rule not found")
	}

	existing.IsEnabled = false
	if err := s.repo.Upsert(existing); err != nil {
		return fmt.Errorf("failed to disable risk rule: %w", err)
	}
	return nil
}

func (s *riskRuleService) BulkSetEnabled(ruleIDs []string, enabled bool) error {
	if len(ruleIDs) == 0 {
		return errors.New("at least one rule id is required")
	}

	for _, ruleID := range ruleIDs {
		trimmed := strings.TrimSpace(ruleID)
		if trimmed == "" {
			return errors.New("rule id is required")
		}

		existing, err := s.repo.GetByRuleID(trimmed)
		if err != nil {
			return fmt.Errorf("failed to get risk rule: %w", err)
		}
		if existing == nil {
			return errors.New("risk rule not found")
		}

		existing.IsEnabled = enabled
		if err := s.repo.Upsert(existing); err != nil {
			return fmt.Errorf("failed to update risk rule status: %w", err)
		}
	}

	return nil
}

func (s *riskRuleService) TestRules(req TestRiskRulesRequest) (RiskAnalysis, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return RiskAnalysis{}, errors.New("sample text is required")
	}

	var rules []models.RiskRule
	if req.Rule != nil {
		rule, err := s.buildRuleModel(*req.Rule)
		if err != nil {
			return RiskAnalysis{}, err
		}
		rules = []models.RiskRule{*rule}
	} else {
		items, err := s.repo.ListEnabled()
		if err != nil {
			return RiskAnalysis{}, fmt.Errorf("failed to list enabled risk rules: %w", err)
		}
		rules = items
	}

	compiled := compileRiskRules(rules)
	return analyzeWithCompiledRules(text, compiled), nil
}

func (s *riskRuleService) buildRuleModel(req SaveRiskRuleRequest) (*models.RiskRule, error) {
	ruleID := strings.TrimSpace(req.RuleID)
	if ruleID == "" {
		return nil, errors.New("rule id is required")
	}

	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		return nil, errors.New("rule display name is required")
	}

	pattern := strings.TrimSpace(req.Pattern)
	if pattern == "" {
		return nil, errors.New("rule pattern is required")
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return nil, errors.New("rule pattern is invalid")
	}

	severity := strings.TrimSpace(req.Severity)
	if severity != models.RiskSeverityLow && severity != models.RiskSeverityMedium && severity != models.RiskSeverityHigh {
		return nil, errors.New("risk severity is invalid")
	}

	action := strings.TrimSpace(req.Action)
	if action == "require_approval" {
		action = models.RiskActionBlock
	}
	if action != models.RiskActionAllow && action != models.RiskActionRouteSecureModel && action != models.RiskActionBlock {
		return nil, errors.New("risk action is invalid")
	}

	var description *string
	if req.Description != nil {
		trimmed := strings.TrimSpace(*req.Description)
		if trimmed != "" {
			description = &trimmed
		}
	}

	return &models.RiskRule{
		RuleID:      ruleID,
		DisplayName: displayName,
		Description: description,
		Pattern:     pattern,
		Severity:    severity,
		Action:      action,
		IsEnabled:   req.IsEnabled,
		SortOrder:   req.SortOrder,
	}, nil
}
