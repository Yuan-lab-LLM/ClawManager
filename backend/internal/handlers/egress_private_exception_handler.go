package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

// EgressPrivateExceptionHandler exposes admin CRUD for egress private exceptions.
type EgressPrivateExceptionHandler struct {
	service services.EgressPrivateExceptionService
}

type upsertEgressPrivateExceptionRequest struct {
	ScopeType   string  `json:"scope_type" binding:"required"`
	ScopeID     int     `json:"scope_id" binding:"required"`
	CIDR        string  `json:"cidr" binding:"required"`
	Port        int     `json:"port" binding:"required"`
	Enabled     *bool   `json:"enabled"`
	Description *string `json:"description,omitempty"`
}

// NewEgressPrivateExceptionHandler creates the admin handler.
func NewEgressPrivateExceptionHandler(service services.EgressPrivateExceptionService) *EgressPrivateExceptionHandler {
	return &EgressPrivateExceptionHandler{service: service}
}

// ListExceptions returns private exceptions, optionally filtered by scope.
func (h *EgressPrivateExceptionHandler) ListExceptions(c *gin.Context) {
	scopeType := strings.TrimSpace(c.Query("scope_type"))
	var scopeID *int
	if raw := strings.TrimSpace(c.Query("scope_id")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			utils.Error(c, http.StatusBadRequest, "invalid scope_id")
			return
		}
		scopeID = &parsed
	}
	items, err := h.service.List(scopeType, scopeID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Egress private exceptions retrieved successfully", gin.H{
		"items": items,
	})
}

// CreateException creates a private exception.
func (h *EgressPrivateExceptionHandler) CreateException(c *gin.Context) {
	var req upsertEgressPrivateExceptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	var createdBy *int
	if raw, ok := c.Get("userID"); ok {
		if userID, ok := raw.(int); ok && userID > 0 {
			createdBy = &userID
		}
	}
	item, err := h.service.Create(services.SaveEgressPrivateExceptionRequest{
		ScopeType:   req.ScopeType,
		ScopeID:     req.ScopeID,
		CIDR:        req.CIDR,
		Port:        req.Port,
		Enabled:     enabled,
		Description: req.Description,
		CreatedBy:   createdBy,
	})
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "Egress private exception created successfully", item)
}

// UpdateException updates a private exception.
func (h *EgressPrivateExceptionHandler) UpdateException(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		utils.Error(c, http.StatusBadRequest, "invalid exception id")
		return
	}
	var req upsertEgressPrivateExceptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	item, err := h.service.Update(id, services.SaveEgressPrivateExceptionRequest{
		ScopeType:   req.ScopeType,
		ScopeID:     req.ScopeID,
		CIDR:        req.CIDR,
		Port:        req.Port,
		Enabled:     enabled,
		Description: req.Description,
	})
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Egress private exception updated successfully", item)
}

// DeleteException deletes a private exception.
func (h *EgressPrivateExceptionHandler) DeleteException(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		utils.Error(c, http.StatusBadRequest, "invalid exception id")
		return
	}
	if err := h.service.Delete(id); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Egress private exception deleted successfully", nil)
}
