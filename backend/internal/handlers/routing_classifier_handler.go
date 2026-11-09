package handlers

import (
	"clawreef/internal/aigateway"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
	"net/http"
)

type RoutingClassifierHandler struct {
	service *aigateway.ClassifierSettingsService
}

func NewRoutingClassifierHandler(service *aigateway.ClassifierSettingsService) *RoutingClassifierHandler {
	return &RoutingClassifierHandler{service: service}
}
func (h *RoutingClassifierHandler) Get(c *gin.Context) {
	view, err := h.service.Get(c.Request.Context())
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, aigateway.ClassifierErrorCode(err))
		return
	}
	utils.Success(c, http.StatusOK, "Classifier settings retrieved", view)
}
func (h *RoutingClassifierHandler) Save(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	var draft aigateway.ClassifierSettingsDraft
	if c.ShouldBindJSON(&draft) != nil {
		utils.Error(c, http.StatusBadRequest, "invalid_request")
		return
	}
	view, err := h.service.Save(c.Request.Context(), draft)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, aigateway.ClassifierErrorCode(err))
		return
	}
	utils.Success(c, http.StatusOK, "Classifier settings saved; production routing unchanged", view)
}
func (h *RoutingClassifierHandler) Compare(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 65536)
	var req aigateway.ClassifierCompareRequest
	if c.ShouldBindJSON(&req) != nil {
		utils.Error(c, http.StatusBadRequest, "invalid_request")
		return
	}
	result, err := h.service.Compare(c.Request.Context(), req)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, aigateway.ClassifierErrorCode(err))
		return
	}
	utils.Success(c, http.StatusOK, "Manual classifier comparison complete", result)
}

func (h *RoutingClassifierHandler) SetMode(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2048)
	var draft aigateway.ClassifierModeDraft
	if c.ShouldBindJSON(&draft) != nil {
		utils.Error(c, http.StatusBadRequest, "invalid_request")
		return
	}
	view, err := h.service.SaveMode(c.Request.Context(), draft)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, aigateway.ClassifierErrorCode(err))
		return
	}
	utils.Success(c, http.StatusOK, "Routing mode saved for subsequent Auto requests", view)
}
