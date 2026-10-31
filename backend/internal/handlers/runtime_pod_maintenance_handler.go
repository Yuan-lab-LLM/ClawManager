package handlers

import (
	"clawreef/internal/services"
	"clawreef/internal/services/k8s"
	"clawreef/internal/utils"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
)

type RuntimePodMaintenanceHandler struct {
	service *services.RuntimePodMaintenanceService
}

func NewRuntimePodMaintenanceHandler(service *services.RuntimePodMaintenanceService) *RuntimePodMaintenanceHandler {
	return &RuntimePodMaintenanceHandler{service: service}
}
func runtimeMaintenanceAdmin(c *gin.Context) bool {
	role, exists := c.Get("userRole")
	if !exists || role != "admin" {
		utils.Error(c, http.StatusForbidden, "Admin access required")
		return false
	}
	return true
}
func runtimeMaintenanceJSON(c *gin.Context, req any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(req); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("request must contain one JSON object")
	}
	return nil
}
func (h *RuntimePodMaintenanceHandler) ManagedPools(c *gin.Context) {
	if !runtimeMaintenanceAdmin(c) {
		return
	}
	result, err := h.service.ManagedPools(c.Request.Context())
	if err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Existing managed Runtime pools", result)
}
func (h *RuntimePodMaintenanceHandler) ScaleOptions(c *gin.Context) {
	if !runtimeMaintenanceAdmin(c) {
		return
	}
	result, err := h.service.ScaleOptions(c.Request.Context())
	if err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Runtime default expansion options", result)
}
func (h *RuntimePodMaintenanceHandler) Scale(c *gin.Context) {
	if !runtimeMaintenanceAdmin(c) {
		return
	}
	var req k8s.RuntimePodScaleRequest
	if err := runtimeMaintenanceJSON(c, &req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.service.Scale(c.Request.Context(), req)
	if err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusAccepted, "Runtime default expansion accepted", result)
}
func (h *RuntimePodMaintenanceHandler) Delete(c *gin.Context) {
	if !runtimeMaintenanceAdmin(c) {
		return
	}
	id, ok := parseRuntimePodID(c)
	if !ok {
		return
	}
	if id <= 0 {
		utils.Error(c, http.StatusBadRequest, "registered Runtime Pod id required; pending pools use the pool deletion endpoint")
		return
	}
	var req struct {
		PodUID string `json:"pod_uid"`
	}
	if err := runtimeMaintenanceJSON(c, &req); err != nil || req.PodUID == "" {
		utils.Error(c, http.StatusBadRequest, "pod_uid is required")
		return
	}
	if err := h.service.Delete(c.Request.Context(), id, req.PodUID); err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusAccepted, "Runtime Pod pool deletion accepted; persistent workspaces retained", gin.H{"id": id})
}
func (h *RuntimePodMaintenanceHandler) DeletePool(c *gin.Context) {
	if !runtimeMaintenanceAdmin(c) {
		return
	}
	var req struct {
		Namespace     string `json:"namespace"`
		DeploymentUID string `json:"deployment_uid"`
	}
	if err := runtimeMaintenanceJSON(c, &req); err != nil || req.Namespace == "" || req.DeploymentUID == "" {
		utils.Error(c, http.StatusBadRequest, "namespace and deployment_uid are required")
		return
	}
	if err := h.service.DeletePool(c.Request.Context(), req.Namespace, c.Param("name"), req.DeploymentUID); err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusAccepted, "Runtime Pod pool deletion accepted; persistent workspaces retained", gin.H{"namespace": req.Namespace, "deployment_name": c.Param("name")})
}
