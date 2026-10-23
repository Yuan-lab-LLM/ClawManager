package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/services"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
)

type GatewayKeyHandler struct{ service services.GatewayKeyService }

func NewGatewayKeyHandler(service services.GatewayKeyService) *GatewayKeyHandler {
	return &GatewayKeyHandler{service: service}
}

type createGatewayKeyRequest struct {
	Name               string   `json:"name" binding:"required"`
	Description        string   `json:"description"`
	OwnerUserID        int      `json:"owner_user_id" binding:"required"`
	AllowedModelGroups []string `json:"allowed_model_groups"`
	ExpiresAt          *string  `json:"expires_at"`
	KeyType            string   `json:"key_type"`
	RPMLimit           int      `json:"rpm_limit"`
	TPMLimit           int      `json:"tpm_limit"`
	ConcurrentLimit    int      `json:"concurrent_requests_limit"`
	BudgetLimit        float64  `json:"budget_limit"`
	BudgetPeriod       string   `json:"budget_period"`
}

func (h *GatewayKeyHandler) List(c *gin.Context) {
	items, err := h.service.List()
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Gateway keys retrieved successfully", items)
}

func (h *GatewayKeyHandler) Create(c *gin.Context) {
	var req createGatewayKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid gateway key request")
		return
	}
	var expiresAt *time.Time
	if raw := strings.TrimSpace(valueOrEmpty(req.ExpiresAt)); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			utils.Error(c, http.StatusBadRequest, "expires_at must be RFC3339")
			return
		}
		expiresAt = &parsed
	}
	actorID, _ := c.Get("userID")
	actor, _ := actorID.(int)
	key, raw, err := h.service.Create(services.CreateGatewayKeyRequest{Name: req.Name, Description: req.Description, OwnerUserID: req.OwnerUserID, CreatedByUserID: actor, AllowedModelGroups: req.AllowedModelGroups, ExpiresAt: expiresAt, KeyType: req.KeyType, RPMLimit: req.RPMLimit, TPMLimit: req.TPMLimit, ConcurrentLimit: req.ConcurrentLimit, BudgetLimit: req.BudgetLimit, BudgetPeriod: req.BudgetPeriod})
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "Gateway key created. Store the raw key now; it will not be shown again.", gin.H{"key": key, "token": raw})
}

func (h *GatewayKeyHandler) Revoke(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		utils.Error(c, http.StatusBadRequest, "Invalid gateway key ID")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)
	actorID, _ := c.Get("userID")
	actor, _ := actorID.(int)
	if err := h.service.Revoke(c.Request.Context(), id, actor, req.Reason); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Gateway key revoked", nil)
}

func (h *GatewayKeyHandler) Rotate(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		utils.Error(c, http.StatusBadRequest, "Invalid gateway key ID")
		return
	}
	actorID, _ := c.Get("userID")
	actor, _ := actorID.(int)
	key, raw, err := h.service.Rotate(c.Request.Context(), id, actor)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "Gateway key rotated. Store the replacement key now; it will not be shown again.", gin.H{"key": key, "token": raw})
}

func (h *GatewayKeyHandler) UpdateLimits(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		utils.Error(c, http.StatusBadRequest, "Invalid gateway key ID")
		return
	}
	var req struct {
		RPMLimit                int `json:"rpm_limit"`
		TPMLimit                int `json:"tpm_limit"`
		ConcurrentRequestsLimit int `json:"concurrent_requests_limit"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid gateway key limits")
		return
	}
	actorRaw, _ := c.Get("userID")
	actor, _ := actorRaw.(int)
	if err := h.service.UpdateLimits(c.Request.Context(), id, actor, req.RPMLimit, req.TPMLimit, req.ConcurrentRequestsLimit); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Gateway key limits updated", nil)
}

func (h *GatewayKeyHandler) UpdateBudget(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		utils.Error(c, http.StatusBadRequest, "Invalid gateway key ID")
		return
	}
	var req struct {
		Limit      float64 `json:"budget_limit"`
		Period     string  `json:"budget_period"`
		ResetUsage bool    `json:"reset_usage"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid gateway key budget")
		return
	}
	actorRaw, _ := c.Get("userID")
	actor, _ := actorRaw.(int)
	if err := h.service.UpdateBudget(c.Request.Context(), id, actor, req.Limit, req.Period, req.ResetUsage); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Gateway key budget updated", nil)
}

func (h *GatewayKeyHandler) BudgetUsage(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		utils.Error(c, http.StatusBadRequest, "Invalid gateway key ID")
		return
	}
	usage, err := h.service.BudgetUsage(c.Request.Context(), id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Gateway key budget usage retrieved", usage)
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
