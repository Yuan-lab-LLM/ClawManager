package handlers

import (
	"net/http"

	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

// RiskRuleHandler handles admin risk rule requests.
type RiskRuleHandler struct {
	service services.RiskRuleService
}

// UpsertRiskRuleRequest defines editable fields for risk rules.
type UpsertRiskRuleRequest struct {
	RuleID      string  `json:"rule_id" binding:"required"`
	DisplayName string  `json:"display_name" binding:"required"`
	Description *string `json:"description,omitempty"`
	Pattern     string  `json:"pattern" binding:"required"`
	Severity    string  `json:"severity" binding:"required"`
	Action      string  `json:"action" binding:"required"`
	IsEnabled   bool    `json:"is_enabled"`
	SortOrder   int     `json:"sort_order"`
}

type TestRiskRuleRequest struct {
	Text string                 `json:"text" binding:"required"`
	Rule *UpsertRiskRuleRequest `json:"rule,omitempty"`
}

type BulkRiskRuleStatusRequest struct {
	RuleIDs   []string `json:"rule_ids" binding:"required"`
	IsEnabled bool     `json:"is_enabled"`
}

// NewRiskRuleHandler creates a new risk rule handler.
func NewRiskRuleHandler(service services.RiskRuleService) *RiskRuleHandler {
	return &RiskRuleHandler{service: service}
}

// ListRules returns all risk rules for admin management.
func (h *RiskRuleHandler) ListRules(c *gin.Context) {
	items, err := h.service.ListRules()
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Risk rules retrieved successfully", gin.H{
		"items": items,
	})
}

// UpsertRule creates or updates a risk rule.
func (h *RiskRuleHandler) UpsertRule(c *gin.Context) {
	var req UpsertRiskRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	item, err := h.service.SaveRule(services.SaveRiskRuleRequest{
		RuleID:      req.RuleID,
		DisplayName: req.DisplayName,
		Description: req.Description,
		Pattern:     req.Pattern,
		Severity:    req.Severity,
		Action:      req.Action,
		IsEnabled:   req.IsEnabled,
		SortOrder:   req.SortOrder,
	})
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Risk rule saved successfully", item)
}

// TestRules evaluates enabled rules or a draft rule against sample text.
func (h *RiskRuleHandler) TestRules(c *gin.Context) {
	var req TestRiskRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	var draft *services.SaveRiskRuleRequest
	if req.Rule != nil {
		draft = &services.SaveRiskRuleRequest{
			RuleID:      req.Rule.RuleID,
			DisplayName: req.Rule.DisplayName,
			Description: req.Rule.Description,
			Pattern:     req.Rule.Pattern,
			Severity:    req.Rule.Severity,
			Action:      req.Rule.Action,
			IsEnabled:   req.Rule.IsEnabled,
			SortOrder:   req.Rule.SortOrder,
		}
	}

	result, err := h.service.TestRules(services.TestRiskRulesRequest{
		Text: req.Text,
		Rule: draft,
	})
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Risk rule test completed successfully", result)
}

// DeleteRule disables a risk rule.
func (h *RiskRuleHandler) DeleteRule(c *gin.Context) {
	ruleID := c.Param("ruleId")
	if err := h.service.DeleteRule(ruleID); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Risk rule disabled successfully", nil)
}

// BulkUpdateStatus enables or disables multiple risk rules.
func (h *RiskRuleHandler) BulkUpdateStatus(c *gin.Context) {
	var req BulkRiskRuleStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	if err := h.service.BulkSetEnabled(req.RuleIDs, req.IsEnabled); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Risk rule status updated successfully", nil)
}
