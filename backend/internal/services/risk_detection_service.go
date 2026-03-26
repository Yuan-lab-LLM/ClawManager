package services

import (
	"fmt"
	"regexp"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// RiskMatch is a normalized detector match.
type RiskMatch struct {
	RuleID       string `json:"rule_id"`
	RuleName     string `json:"rule_name"`
	Severity     string `json:"severity"`
	Action       string `json:"action"`
	MatchSummary string `json:"match_summary"`
}

// RiskAnalysis is the aggregate result of scanning request content.
type RiskAnalysis struct {
	IsSensitive     bool        `json:"is_sensitive"`
	HighestSeverity string      `json:"highest_severity"`
	HighestAction   string      `json:"highest_action"`
	Hits            []RiskMatch `json:"hits"`
}

// RiskDetectionService defines message scanning operations.
type RiskDetectionService interface {
	AnalyzeText(text string) RiskAnalysis
}

type compiledRiskRule struct {
	rule    models.RiskRule
	pattern *regexp.Regexp
}

type riskDetectionService struct {
	repo repository.RiskRuleRepository
}

// NewRiskDetectionService creates a risk detector backed by configurable rules.
func NewRiskDetectionService(repo repository.RiskRuleRepository) RiskDetectionService {
	return &riskDetectionService{repo: repo}
}

func (s *riskDetectionService) AnalyzeText(text string) RiskAnalysis {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return RiskAnalysis{
			HighestSeverity: models.RiskSeverityLow,
			HighestAction:   models.RiskActionAllow,
			Hits:            []RiskMatch{},
		}
	}

	rules, err := s.repo.ListEnabled()
	if err != nil || len(rules) == 0 {
		return RiskAnalysis{
			HighestSeverity: models.RiskSeverityLow,
			HighestAction:   models.RiskActionAllow,
			Hits:            []RiskMatch{},
		}
	}

	compiled := compileRiskRules(rules)
	return analyzeWithCompiledRules(normalized, compiled)
}

func analyzeWithCompiledRules(normalized string, compiled []compiledRiskRule) RiskAnalysis {
	hits := []RiskMatch{}
	for _, item := range compiled {
		if item.pattern == nil || !item.pattern.MatchString(normalized) {
			continue
		}
		summary := strings.TrimSpace(derefString(item.rule.Description))
		if summary == "" {
			summary = fmt.Sprintf("Matched configured rule %s.", item.rule.DisplayName)
		}
		hits = append(hits, RiskMatch{
			RuleID:       item.rule.RuleID,
			RuleName:     item.rule.DisplayName,
			Severity:     item.rule.Severity,
			Action:       item.rule.Action,
			MatchSummary: summary,
		})
	}

	highestSeverity := models.RiskSeverityLow
	highestAction := models.RiskActionAllow
	for _, hit := range hits {
		if compareRiskSeverity(hit.Severity, highestSeverity) > 0 {
			highestSeverity = hit.Severity
		}
		if compareRiskAction(hit.Action, highestAction) > 0 {
			highestAction = hit.Action
		}
	}

	return RiskAnalysis{
		IsSensitive:     len(hits) > 0,
		HighestSeverity: highestSeverity,
		HighestAction:   highestAction,
		Hits:            hits,
	}
}

func compileRiskRules(rules []models.RiskRule) []compiledRiskRule {
	compiled := make([]compiledRiskRule, 0, len(rules))
	for _, rule := range rules {
		pattern := strings.TrimSpace(rule.Pattern)
		if pattern == "" {
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		compiled = append(compiled, compiledRiskRule{
			rule:    rule,
			pattern: re,
		})
	}
	return compiled
}

func compareRiskSeverity(left, right string) int {
	rank := map[string]int{
		models.RiskSeverityLow:    1,
		models.RiskSeverityMedium: 2,
		models.RiskSeverityHigh:   3,
	}
	return rank[left] - rank[right]
}

func compareRiskAction(left, right string) int {
	if left == "require_approval" {
		left = models.RiskActionBlock
	}
	if right == "require_approval" {
		right = models.RiskActionBlock
	}
	rank := map[string]int{
		models.RiskActionAllow:            1,
		models.RiskActionRouteSecureModel: 2,
		models.RiskActionBlock:            3,
	}
	return rank[left] - rank[right]
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
