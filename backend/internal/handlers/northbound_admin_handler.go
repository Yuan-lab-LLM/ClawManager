package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
)

type NorthboundAdminHandler struct {
	service *services.NorthboundAdminService
}

func (h *NorthboundAdminHandler) APIKeys(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := strconv.Atoi(c.Query("user_id"))
	if err != nil || id <= 0 {
		utils.Error(c, 400, "Invalid user_id")
		return
	}
	result, err := h.service.ListAPIKeys(id)
	if err != nil {
		utils.Error(c, 400, err.Error())
		return
	}
	utils.Success(c, 200, "API keys retrieved", result)
}

func (h *NorthboundAdminHandler) MutateAPIKey(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	var req services.APIKeyMutation
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Error(c, 400, "Invalid API key request")
		return
	}
	result, err := h.service.MutateAPIKey(c.Request.Context(), adminActorID(c), req)
	if err != nil {
		status := 400
		if errors.Is(err, services.ErrNorthboundSettingsConflict) {
			status = 409
		}
		utils.Error(c, status, err.Error())
		return
	}
	utils.Success(c, 200, "API key saved", result)
}

func NewNorthboundAdminHandler(service *services.NorthboundAdminService) *NorthboundAdminHandler {
	return &NorthboundAdminHandler{service: service}
}

func adminActorID(c *gin.Context) int { value, _ := c.Get("userID"); id, _ := value.(int); return id }

func (h *NorthboundAdminHandler) Overview(c *gin.Context) {
	value, err := h.service.Overview(c.Request.Context())
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Northbound settings retrieved", value)
}

func (h *NorthboundAdminHandler) SaveSettings(c *gin.Context) {
	var req models.NorthboundAdminSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	value, err := h.service.SaveSettings(c.Request.Context(), adminActorID(c), &req)
	if err != nil {
		if errors.Is(err, services.ErrNorthboundSettingsConflict) {
			utils.Error(c, http.StatusConflict, err.Error())
			return
		}
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Northbound settings saved", value)
}

func (h *NorthboundAdminHandler) SaveSettingsSection(c *gin.Context) {
	var req struct {
		Version int64                      `json:"version"`
		Fields  map[string]json.RawMessage `json:"fields"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	result, err := h.service.SaveSettingsSection(c.Request.Context(), adminActorID(c), c.Param("section"), req.Version, req.Fields)
	if err != nil {
		status := 400
		if errors.Is(err, services.ErrNorthboundSettingsConflict) {
			status = 409
		}
		utils.Error(c, status, err.Error())
		return
	}
	utils.Success(c, 200, "Settings section saved", result)
}

func (h *NorthboundAdminHandler) SaveCaller(c *gin.Context) {
	var req models.NorthboundCallerPolicy
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	value, err := h.service.SaveCallerPolicy(c.Request.Context(), adminActorID(c), &req)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Northbound caller policy saved", value)
}

func (h *NorthboundAdminHandler) SetExternalNodePort(c *gin.Context) {
	var req struct {
		NodePort int `json:"node_port" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	value, err := h.service.SetExternalNodePort(c.Request.Context(), adminActorID(c), req.NodePort)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Northbound external NodePort updated", value)
}

func (h *NorthboundAdminHandler) DownloadCA(c *gin.Context) {
	value, err := h.service.PublicCA(c.Request.Context())
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	c.Header("Content-Disposition", "attachment; filename=clawmanager-northbound-ca.crt")
	c.Data(http.StatusOK, "application/x-x509-ca-cert", value)
}

func (h *NorthboundAdminHandler) DownloadPreparedCA(c *gin.Context) {
	value, err := h.service.PreparedCA(c.Request.Context())
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	c.Header("Content-Disposition", "attachment; filename=clawmanager-northbound-managed-ca.crt")
	c.Data(http.StatusOK, "application/x-x509-ca-cert", value)
}
func (h *NorthboundAdminHandler) PrepareCertificate(c *gin.Context) {
	var req services.CertificatePrepareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	value, err := h.service.PrepareManagedCertificate(c.Request.Context(), adminActorID(c), req)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Managed northbound certificate prepared", value)
}
func (h *NorthboundAdminHandler) ActivateCertificate(c *gin.Context) {
	var req struct {
		Confirmation string `json:"confirmation"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	if req.Confirmation != "ACTIVATE" {
		utils.Error(c, http.StatusBadRequest, "confirmation must be ACTIVATE")
		return
	}
	value, err := h.service.ActivateManagedCertificate(c.Request.Context(), adminActorID(c))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Managed northbound certificate activated", value)
}
