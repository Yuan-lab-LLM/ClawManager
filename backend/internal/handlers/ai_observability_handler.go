package handlers

import (
	"errors"
	"net/http"

	"clawreef/internal/repository"
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
	Cursor string `form:"cursor"`
	Since  string `form:"since"`
	Until  string `form:"until"`
	Page   int    `form:"page,default=1"`
	Limit  int    `form:"limit,default=100"`
	Search string `form:"search"`
	Status string `form:"status"`
	Model  string `form:"model"`
}

// CostQueryRequest binds supported cost query filters.
type CostQueryRequest struct {
	Cursor string `form:"cursor"`
	Since  string `form:"since"`
	Until  string `form:"until"`
	Page   int    `form:"page,default=1"`
	Limit  int    `form:"limit,default=100"`
	Search string `form:"search"`
}

// SessionUsageOverviewQueryRequest binds admin session usage overview filters.
type SessionUsageOverviewQueryRequest struct {
	Cursor string `form:"cursor"`
	Page   int    `form:"page,default=1"`
	Limit  int    `form:"limit,default=20"`
	Search string `form:"search"`
	Since  string `form:"since"`
	Until  string `form:"until"`
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

	since, err := parseOptionalRFC3339(req.Since)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid since timestamp")
		return
	}
	until, err := parseOptionalRFC3339(req.Until)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid until timestamp")
		return
	}
	// Cursor requests retain the original frozen window; do not default to now again.
	if req.Cursor == "" {
		since, until, err = normalizeSessionUsageTimeRange(since, until)
		if err != nil {
			utils.Error(c, http.StatusBadRequest, err.Error())
			return
		}
	}
	items, err := h.service.ListAuditItems(services.AuditQuery{
		Cursor: req.Cursor, Since: since, Until: until,
		Page:   req.Page,
		Limit:  req.Limit,
		Search: req.Search,
		Status: req.Status,
		Model:  req.Model,
	})
	if err != nil {
		if errors.Is(err, repository.ErrInvalidAuditQuery) {
			utils.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		handleObservationQueryError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "AI audit items retrieved successfully", items)
}

// GetTraceDetail returns full trace detail for a model invocation trace.
func (h *AIObservabilityHandler) GetTraceDetail(c *gin.Context) {
	if h.tracePagedRequest(c) {
		return
	}
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

	since, err := parseOptionalRFC3339(req.Since)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid since timestamp")
		return
	}
	until, err := parseOptionalRFC3339(req.Until)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid until timestamp")
		return
	}
	since, until, err = normalizeSessionUsageTimeRange(since, until)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	overview, err := h.service.GetCostOverview(services.CostQuery{
		Cursor: req.Cursor, Since: since, Until: until,
		Page:   req.Page,
		Limit:  req.Limit,
		Search: req.Search,
	})
	if err != nil {
		handleObservationQueryError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "AI cost overview retrieved successfully", overview)
}

// GetLLMGovernanceOverview returns managed-runtime LLM governance summary for admins.
func (h *AIObservabilityHandler) GetLLMGovernanceOverview(c *gin.Context) {
	overview, err := h.service.GetLLMGovernanceOverview()
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "LLM governance overview retrieved successfully", overview)
}

// GetSessionUsageOverview returns cross-instance session token usage for admins.
func (h *AIObservabilityHandler) GetSessionUsageOverview(c *gin.Context) {
	var req SessionUsageOverviewQueryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	since, err := parseOptionalRFC3339(req.Since)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid since timestamp")
		return
	}
	until, parseUntilErr := parseOptionalRFC3339(req.Until)
	if parseUntilErr != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid until timestamp")
		return
	}
	since, until, err = normalizeSessionUsageTimeRange(since, until)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	overview, err := h.service.GetAdminSessionUsageOverview(services.InstanceSessionUsageOverviewQuery{
		Cursor: req.Cursor,
		Page:   req.Page,
		Limit:  req.Limit,
		Search: req.Search,
		Since:  since,
		Until:  until,
	})
	if err != nil {
		handleObservationQueryError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Session usage overview retrieved successfully", overview)
}
