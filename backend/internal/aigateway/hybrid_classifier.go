package aigateway

import (
	"regexp"
	"strings"
)

// RuleAssessment describes coverage, NOT difficulty or statistical confidence.
// Only full, deliberately narrow task patterns may bypass the external model.
// A nonzero heuristic score alone never counts as a decisive match.
type RuleAssessment struct {
	Status  string   `json:"status"` // matched, ambiguous, unmatched
	Reason  string   `json:"reason"`
	RuleIDs []string `json:"rule_ids"`
}

var hybridTaskPatterns = []struct {
	id      string
	tier    int
	pattern *regexp.Regexp
}{
	{"greeting_v1", 1, regexp.MustCompile(`(?i)^(你好|您好|嗨|哈喽|早上好|晚上好|谢谢|多谢|再见|hello|hi|hey|thanks|thank you|good morning|good evening)$`)},
	{"arithmetic_v1", 1, regexp.MustCompile(`(?i)^(请计算\s*|计算\s*|what is\s+)?[0-9]{1,8}\s*[+*/×÷-]\s*[0-9]{1,8}(\s*(等于几|等于多少|是多少|=\s*\?))?$`)},
	{"small_function_v1", 2, regexp.MustCompile(`(?i)^(请|请你|帮我|请帮我)?\s*(用\s*)?(写|编写|实现)\s*(一个\s*)?(python|javascript|typescript|go|golang|java|c\+\+)\s*(的\s*)?(排序|去重|求和|判断闰年|闰年判断|反转字符串|字符串反转|二分查找)\s*函数$`)},
	{"small_function_en_v1", 2, regexp.MustCompile(`(?i)^(please\s+)?(write|implement)\s+(a\s+)?(python|javascript|typescript|go|java)\s+function\s+to\s+(sort (a list|an array)|remove duplicates|reverse a string|check (for )?leap years)$`)},
}

// Assess the original turn, not just a stripped instruction: otherwise quotes,
// extra requirements or code omitted by the heuristic could become false hits.
// This conservative first version abstains on contextual/compound tasks. Add
// coverage only with positive AND near-miss regression cases.
func assessRoutingRules(input RoutingClassificationInput, d routingDecision) *RuleAssessment {
	assessment := &RuleAssessment{Status: "ambiguous", Reason: "weak_rule_match", RuleIDs: []string{}}
	if len(input.Turns) == 0 {
		assessment.Status, assessment.Reason = "unmatched", "no_user_task"
		return assessment
	}
	for _, turn := range input.Turns {
		if turn.Truncated || turn.HasNonText {
			assessment.Reason = "unsupported_input"
			return assessment
		}
	}
	text := strings.TrimSpace(input.Turns[len(input.Turns)-1].Text)
	if text == "" {
		assessment.Status, assessment.Reason = "unmatched", "no_user_task"
		return assessment
	}
	if routingContinuation.MatchString(text) || routingReference.MatchString(text) {
		assessment.Reason = "context_required"
		return assessment
	}
	if routingCompoundAction.MatchString(text) {
		assessment.Reason = "compound_task"
		return assessment
	}
	text = strings.TrimRight(text, " \t\r\n。.!！?？")
	for _, rule := range hybridTaskPatterns {
		if rule.pattern.MatchString(text) && rule.tier == d.RequiredTier {
			assessment.Status, assessment.Reason = "matched", "explicit_task_pattern"
			assessment.RuleIDs = []string{rule.id}
			return assessment
		}
	}
	if len(d.Signals) == 0 {
		assessment.Status, assessment.Reason = "unmatched", "no_rule_match"
	}
	return assessment
}

type HybridClassificationPreview struct {
	RuleAssessment *RuleAssessment `json:"rule_assessment"`
	WouldCallLaya  bool            `json:"would_call_laya"`
	Source         string          `json:"source"` // rules, laya, rules_fallback
	RequiredTier   int             `json:"required_tier"`
	FallbackReason string          `json:"fallback_reason,omitempty"`
	DurationMS     float64         `json:"duration_ms"`
}

// A manual comparison intentionally calls both classifiers. This preview reuses
// its results; it is not a second live request and never changes the live mode.
func hybridComparisonPreview(input RoutingClassificationInput, result ClassifierComparison) *HybridClassificationPreview {
	rule := result.Rule.Classification
	p := &HybridClassificationPreview{RuleAssessment: rule.RuleAssessment, Source: "rules", RequiredTier: rule.RequiredTier, DurationMS: result.Rule.DurationMS}
	if p.RuleAssessment.Status == "matched" {
		return p
	}
	// Apply the same preflight as the live adapter, even when comparison failed
	// earlier (for example because of missing credentials).
	if _, _, err := layaPayload(input); err != nil {
		p.Source, p.FallbackReason = "rules_fallback", layaFallbackReason(err)
		return p
	}
	p.WouldCallLaya = true
	p.DurationMS += result.Laya.DurationMS
	if result.Laya.Status == "success" && result.Laya.Classification != nil {
		p.Source, p.RequiredTier = "laya", result.Laya.Classification.RequiredTier
	} else {
		p.Source, p.FallbackReason = "rules_fallback", result.Laya.ErrorCode
	}
	return p
}
