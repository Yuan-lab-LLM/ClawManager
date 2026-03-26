package handlers

import (
	"net/http"

	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

// AIObservabilityHandler exposes admin reporting endpoints for audit and cost data.
type AIObservabilityHandler struct {
	service services.AIObservabilityService
}

// AuditQueryRequest binds supported audit list filters.
type AuditQueryRequest struct {
	Page   int    `form:"page,default=1"`
	Limit  int    `form:"limit,default=100"`
	Search string `form:"search"`
	Status string `form:"status"`
	Model  string `form:"model"`
}

// CostQueryRequest binds supported cost query filters.
type CostQueryRequest struct {
	Page   int    `form:"page,default=1"`
	Limit  int    `form:"limit,default=100"`
	Search string `form:"search"`
}

// NewAIObservabilityHandler creates a new observability handler.
func NewAIObservabilityHandler(service services.AIObservabilityService) *AIObservabilityHandler {
	return &AIObservabilityHandler{service: service}
}

// ListAuditItems returns recent audit entries for AI model invocations.
func (h *AIObservabilityHandler) ListAuditItems(c *gin.Context) {
	var req AuditQueryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	items, err := h.service.ListAuditItems(services.AuditQuery{
		Page:   req.Page,
		Limit:  req.Limit,
		Search: req.Search,
		Status: req.Status,
		Model:  req.Model,
	})
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "AI audit items retrieved successfully", items)
}

// GetTraceDetail returns full trace detail for a model invocation trace.
func (h *AIObservabilityHandler) GetTraceDetail(c *gin.Context) {
	traceID := c.Param("traceId")
	detail, err := h.service.GetTraceDetail(traceID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "AI trace detail retrieved successfully", detail)
}

// GetCostOverview returns token and money overview data for admin reporting.
func (h *AIObservabilityHandler) GetCostOverview(c *gin.Context) {
	var req CostQueryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	overview, err := h.service.GetCostOverview(services.CostQuery{
		Page:   req.Page,
		Limit:  req.Limit,
		Search: req.Search,
	})
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "AI cost overview retrieved successfully", overview)
}
