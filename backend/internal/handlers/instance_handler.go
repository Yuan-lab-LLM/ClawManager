package handlers

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

// InstanceHandler handles instance management requests
type InstanceHandler struct {
	instanceService               services.InstanceService
	instanceAgentService          services.InstanceAgentService
	runtimeStatusService          services.InstanceRuntimeStatusService
	instanceCommandService        services.InstanceCommandService
	instanceConfigRevisionService services.InstanceConfigRevisionService
	accessService                 *services.InstanceAccessService
	proxyService                  *services.InstanceProxyService
	openClawTransferService       services.OpenClawTransferService
	openClawConfigService         services.OpenClawConfigService
	skillService                  services.SkillService
}

// NewInstanceHandler creates a new instance handler
func NewInstanceHandler(instanceService services.InstanceService, instanceAgentService services.InstanceAgentService, runtimeStatusService services.InstanceRuntimeStatusService, instanceCommandService services.InstanceCommandService, instanceConfigRevisionService services.InstanceConfigRevisionService, openClawConfigService services.OpenClawConfigService, skillService services.SkillService) *InstanceHandler {
	accessService := services.NewInstanceAccessService()
	return &InstanceHandler{
		instanceService:               instanceService,
		instanceAgentService:          instanceAgentService,
		runtimeStatusService:          runtimeStatusService,
		instanceCommandService:        instanceCommandService,
		instanceConfigRevisionService: instanceConfigRevisionService,
		accessService:                 accessService,
		proxyService:                  services.NewInstanceProxyService(accessService),
		openClawTransferService:       services.NewOpenClawTransferService(),
		openClawConfigService:         openClawConfigService,
		skillService:                  skillService,
	}
}

type InstanceRuntimeDetailsResponse struct {
	Runtime  *services.InstanceRuntimeStatusPayload `json:"runtime,omitempty"`
	Agent    *services.InstanceAgentPayload         `json:"agent,omitempty"`
	Commands []services.InstanceCommandPayload      `json:"commands,omitempty"`
}

type CreateRuntimeCommandRequest struct {
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

type PublishConfigRevisionRequest struct {
	SnapshotID int `json:"snapshot_id" binding:"required,min=1"`
}

// CreateInstanceRequest represents a create instance request
type CreateInstanceRequest struct {
	Name                 string                       `json:"name" binding:"required,min=3,max=50"`
	Description          *string                      `json:"description,omitempty"`
	Type                 string                       `json:"type" binding:"required,oneof=openclaw ubuntu debian centos custom webtop"`
	CPUCores             float64                      `json:"cpu_cores" binding:"required,min=0.1,max=32"`
	MemoryGB             int                          `json:"memory_gb" binding:"required,min=1,max=128"`
	DiskGB               int                          `json:"disk_gb" binding:"required,min=10,max=1000"`
	GPUEnabled           bool                         `json:"gpu_enabled"`
	GPUCount             int                          `json:"gpu_count" binding:"min=0,max=4"`
	OSType               string                       `json:"os_type" binding:"required"`
	OSVersion            string                       `json:"os_version" binding:"required"`
	ImageRegistry        *string                      `json:"image_registry,omitempty"`
	ImageTag             *string                      `json:"image_tag,omitempty"`
	EnvironmentOverrides map[string]string            `json:"environment_overrides,omitempty"`
	StorageClass         string                       `json:"storage_class"`
	OpenClawConfigPlan   *services.OpenClawConfigPlan `json:"openclaw_config_plan,omitempty"`
	SkillIDs             []int                        `json:"skill_ids,omitempty"`
}

// UpdateInstanceRequest represents an update instance request
type UpdateInstanceRequest struct {
	Name        *string `json:"name,omitempty" binding:"omitempty,min=3,max=50"`
	Description *string `json:"description,omitempty"`
}

// ListInstancesRequest represents a list instances request
type ListInstancesRequest struct {
	Page   int    `form:"page,default=1"`
	Limit  int    `form:"limit,default=20"`
	Status string `form:"status,omitempty"`
}

// ListInstances lists instances owned by the current user (workspace view).
//
// This endpoint is always caller-scoped — the caller's role is intentionally
// not consulted. An admin using /instances sees only instances they personally
// own. Admin-scoped cross-user listing lives on /admin/instances and is gated
// by the admin middleware; see ListAllInstances below.
func (h *InstanceHandler) ListInstances(c *gin.Context) {
	userID, _ := c.Get("userID")

	var req ListInstancesRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	// Calculate offset
	offset := (req.Page - 1) * req.Limit

	instances, total, err := h.instanceService.GetByUserID(userID.(int), offset, req.Limit)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"instances": instances,
		"total":     total,
		"page":      req.Page,
		"limit":     req.Limit,
	}

	utils.Success(c, http.StatusOK, "Instances retrieved successfully", response)
}

// ListAllInstances lists every instance across all users (admin console view).
//
// Gated by the admin middleware on the /admin/instances route group. The
// admin role badge only controls which API surface is reachable — it does
// not widen the caller-scoped /instances endpoint.
func (h *InstanceHandler) ListAllInstances(c *gin.Context) {
	var req ListInstancesRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	offset := (req.Page - 1) * req.Limit

	instances, total, err := h.instanceService.GetAllInstances(offset, req.Limit)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"instances": instances,
		"total":     total,
		"page":      req.Page,
		"limit":     req.Limit,
	}

	utils.Success(c, http.StatusOK, "Instances retrieved successfully", response)
}

// CreateInstance creates a new instance
func (h *InstanceHandler) CreateInstance(c *gin.Context) {
	userID, _ := c.Get("userID")

	var req CreateInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	createReq := services.CreateInstanceRequest{
		Name:                 req.Name,
		Description:          req.Description,
		Type:                 req.Type,
		CPUCores:             req.CPUCores,
		MemoryGB:             req.MemoryGB,
		DiskGB:               req.DiskGB,
		GPUEnabled:           req.GPUEnabled,
		GPUCount:             req.GPUCount,
		OSType:               req.OSType,
		OSVersion:            req.OSVersion,
		ImageRegistry:        req.ImageRegistry,
		ImageTag:             req.ImageTag,
		EnvironmentOverrides: req.EnvironmentOverrides,
		StorageClass:         req.StorageClass,
		OpenClawConfigPlan:   req.OpenClawConfigPlan,
	}

	instance, err := h.instanceService.Create(userID.(int), createReq)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	for _, skillID := range req.SkillIDs {
		if _, err := h.skillService.AttachSkillToInstance(instance.ID, skillID); err != nil {
			utils.HandleError(c, err)
			return
		}
	}

	utils.Success(c, http.StatusCreated, "Instance created successfully", instance)
}

// GetInstance gets an instance by ID
func (h *InstanceHandler) GetInstance(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	instance, err := h.instanceService.GetByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	if instance == nil {
		utils.Error(c, http.StatusNotFound, "Instance not found")
		return
	}

	// Check ownership (only admin or owner can view)
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	if userRole != "admin" && instance.UserID != userID.(int) {
		utils.Error(c, http.StatusForbidden, "Access denied")
		return
	}

	runtime, _ := h.runtimeStatusService.GetByInstanceID(instance.ID)
	agent, _ := h.instanceAgentService.GetPayloadByInstanceID(instance.ID)

	utils.Success(c, http.StatusOK, "Instance retrieved successfully", gin.H{
		"instance": instance,
		"runtime":  runtime,
		"agent":    agent,
	})
}

// UpdateInstance updates an instance
func (h *InstanceHandler) UpdateInstance(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get instance first to check ownership
	instance, err := h.instanceService.GetByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	if instance == nil {
		utils.Error(c, http.StatusNotFound, "Instance not found")
		return
	}

	// Check ownership (only admin or owner can update)
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	if userRole != "admin" && instance.UserID != userID.(int) {
		utils.Error(c, http.StatusForbidden, "Access denied")
		return
	}

	var req UpdateInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	updateReq := services.UpdateInstanceRequest{
		Name:        req.Name,
		Description: req.Description,
	}

	if err := h.instanceService.Update(id, updateReq); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Instance updated successfully", nil)
}

// DeleteInstance deletes an instance
func (h *InstanceHandler) DeleteInstance(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get instance first to check ownership
	instance, err := h.instanceService.GetByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	if instance == nil {
		utils.Error(c, http.StatusNotFound, "Instance not found")
		return
	}

	// Check ownership (only admin or owner can delete)
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	if userRole != "admin" && instance.UserID != userID.(int) {
		utils.Error(c, http.StatusForbidden, "Access denied")
		return
	}

	if err := h.instanceService.Delete(id); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Instance deleted successfully", nil)
}

// StartInstance starts an instance
func (h *InstanceHandler) StartInstance(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get instance first to check ownership
	instance, err := h.instanceService.GetByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	if instance == nil {
		utils.Error(c, http.StatusNotFound, "Instance not found")
		return
	}

	// Check ownership (only admin or owner can start)
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	if userRole != "admin" && instance.UserID != userID.(int) {
		utils.Error(c, http.StatusForbidden, "Access denied")
		return
	}

	if err := h.instanceService.Start(id); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Instance started successfully", nil)
}

// StopInstance stops an instance
func (h *InstanceHandler) StopInstance(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get instance first to check ownership
	instance, err := h.instanceService.GetByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	if instance == nil {
		utils.Error(c, http.StatusNotFound, "Instance not found")
		return
	}

	// Check ownership (only admin or owner can stop)
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	if userRole != "admin" && instance.UserID != userID.(int) {
		utils.Error(c, http.StatusForbidden, "Access denied")
		return
	}

	if err := h.instanceService.Stop(id); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Instance stopped successfully", nil)
}

// RestartInstance restarts an instance
func (h *InstanceHandler) RestartInstance(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get instance first to check ownership
	instance, err := h.instanceService.GetByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	if instance == nil {
		utils.Error(c, http.StatusNotFound, "Instance not found")
		return
	}

	// Check ownership (only admin or owner can restart)
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	if userRole != "admin" && instance.UserID != userID.(int) {
		utils.Error(c, http.StatusForbidden, "Access denied")
		return
	}

	if err := h.instanceService.Restart(id); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Instance restarted successfully", nil)
}

// GetInstanceStatus gets the detailed status of an instance
func (h *InstanceHandler) GetInstanceStatus(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get instance first to check ownership
	instance, err := h.instanceService.GetByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	if instance == nil {
		utils.Error(c, http.StatusNotFound, "Instance not found")
		return
	}

	// Check ownership (only admin or owner can view status)
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	if userRole != "admin" && instance.UserID != userID.(int) {
		utils.Error(c, http.StatusForbidden, "Access denied")
		return
	}

	status, err := h.instanceService.GetInstanceStatus(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	runtime, _ := h.runtimeStatusService.GetByInstanceID(id)
	agent, _ := h.instanceAgentService.GetPayloadByInstanceID(id)

	utils.Success(c, http.StatusOK, "Instance status retrieved successfully", gin.H{
		"instance_status": status,
		"runtime":         runtime,
		"agent":           agent,
	})
}

func (h *InstanceHandler) GetRuntimeDetails(c *gin.Context) {
	id, _, ok := h.resolveOwnedInstance(c)
	if !ok {
		return
	}

	runtime, err := h.runtimeStatusService.GetByInstanceID(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	agent, err := h.instanceAgentService.GetPayloadByInstanceID(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	commands, err := h.instanceCommandService.ListByInstanceID(id, 20)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Instance runtime details retrieved successfully", InstanceRuntimeDetailsResponse{
		Runtime:  runtime,
		Agent:    agent,
		Commands: commands,
	})
}

func (h *InstanceHandler) CreateRuntimeCommand(c *gin.Context) {
	id, _, ok := h.resolveOwnedInstance(c)
	if !ok {
		return
	}

	commandKey := strings.TrimSpace(c.Param("command"))
	commandType := ""
	switch commandKey {
	case "start":
		commandType = services.InstanceCommandTypeStartOpenClaw
	case "stop":
		commandType = services.InstanceCommandTypeStopOpenClaw
	case "restart":
		commandType = services.InstanceCommandTypeRestartOpenClaw
	case "collect-system-info":
		commandType = services.InstanceCommandTypeCollectSystemInfo
	case "health-check":
		commandType = services.InstanceCommandTypeHealthCheck
	default:
		utils.Error(c, http.StatusBadRequest, "Invalid runtime command")
		return
	}

	var req CreateRuntimeCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil && err != io.EOF {
		utils.ValidationError(c, err)
		return
	}

	userID, _ := c.Get("userID")
	issuedBy := userID.(int)
	command, err := h.instanceCommandService.Create(id, &issuedBy, services.CreateInstanceCommandRequest{
		CommandType:    commandType,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusCreated, "Instance runtime command created successfully", command)
}

func (h *InstanceHandler) ListConfigRevisions(c *gin.Context) {
	id, _, ok := h.resolveOwnedInstance(c)
	if !ok {
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	items, err := h.instanceConfigRevisionService.ListByInstanceID(id, limit)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Instance config revisions retrieved successfully", items)
}

func (h *InstanceHandler) PublishConfigRevision(c *gin.Context) {
	id, instance, ok := h.resolveOwnedInstance(c)
	if !ok {
		return
	}
	if !strings.EqualFold(instance.Type, "openclaw") {
		utils.Error(c, http.StatusBadRequest, "Only openclaw instances support config revisions")
		return
	}

	var req PublishConfigRevisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	userID, _ := c.Get("userID")
	snapshot, err := h.openClawConfigService.GetSnapshot(userID.(int), req.SnapshotID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	if snapshot.InstanceID != nil && *snapshot.InstanceID != id {
		utils.Error(c, http.StatusBadRequest, "Snapshot does not belong to this instance")
		return
	}

	modelSnapshot := &models.OpenClawInjectionSnapshot{
		ID:                   snapshot.ID,
		InstanceID:           snapshot.InstanceID,
		UserID:               snapshot.UserID,
		BundleID:             snapshot.BundleID,
		Mode:                 snapshot.Mode,
		RenderedManifestJSON: string(snapshot.Manifest),
	}

	issuedBy := userID.(int)
	revision, err := h.instanceConfigRevisionService.CreateFromSnapshot(id, modelSnapshot, &issuedBy)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	command, err := h.instanceCommandService.Create(id, &issuedBy, services.CreateInstanceCommandRequest{
		CommandType:    services.InstanceCommandTypeApplyConfigRevision,
		IdempotencyKey: fmt.Sprintf("apply-config-revision-%d", revision.ID),
		Payload: map[string]interface{}{
			"revision_id": revision.ID,
			"snapshot_id": snapshot.ID,
		},
		TimeoutSeconds: 300,
	})
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusCreated, "Instance config revision published successfully", gin.H{
		"revision": revision,
		"command":  command,
	})
}

func (h *InstanceHandler) resolveOwnedInstance(c *gin.Context) (int, *models.Instance, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return 0, nil, false
	}

	instance, err := h.instanceService.GetByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return 0, nil, false
	}
	if instance == nil {
		utils.Error(c, http.StatusNotFound, "Instance not found")
		return 0, nil, false
	}

	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	if userRole != "admin" && instance.UserID != userID.(int) {
		utils.Error(c, http.StatusForbidden, "Access denied")
		return 0, nil, false
	}

	return id, instance, true
}

// GenerateAccessToken generates an access token for an instance
func (h *InstanceHandler) GenerateAccessToken(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get instance first to check ownership
	instance, err := h.instanceService.GetByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	if instance == nil {
		utils.Error(c, http.StatusNotFound, "Instance not found")
		return
	}

	// Check ownership (only admin or owner can generate access token)
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	if userRole != "admin" && instance.UserID != userID.(int) {
		utils.Error(c, http.StatusForbidden, "Access denied")
		return
	}

	// Check if instance is running
	if instance.Status != "running" {
		utils.Error(c, http.StatusBadRequest, "Instance is not running")
		return
	}

	// Generate proxy entry URL. The actual Service remains internal-only.
	accessURL := h.proxyService.GetProxyURL(instance.ID, "")

	if accessURL == "" {
		utils.Error(c, http.StatusServiceUnavailable, "Unable to generate access URL")
		return
	}

	// Generate access token (valid for 1 hour)
	maxAgeSeconds := int(time.Hour.Seconds())
	token, err := h.accessService.GenerateToken(
		userID.(int),
		instance.ID,
		instance.Type,
		accessURL,
		h.proxyService.GetTargetPortForInstance(instance),
		1*time.Hour,
	)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Store the short-lived access token in an HttpOnly cookie so iframe subresources
	// and websocket requests can reuse it without leaking the token in URLs.
	c.SetCookie(
		fmt.Sprintf("instance_access_%d", instance.ID),
		token.Token,
		maxAgeSeconds,
		fmt.Sprintf("/api/v1/instances/%d/proxy", instance.ID),
		"",
		false,
		true,
	)

	// Return token and URLs
	response := map[string]interface{}{
		"token":      token.Token,
		"access_url": accessURL,
		"proxy_url":  h.proxyService.GetProxyURL(instance.ID, token.Token),
		"expires_at": token.ExpiresAt,
	}

	utils.Success(c, http.StatusOK, "Access token generated successfully", response)
}

// AccessInstance handles instance access via token
func (h *InstanceHandler) AccessInstance(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Validate access token
	token := c.Query("token")
	if token == "" {
		utils.Error(c, http.StatusBadRequest, "Access token required")
		return
	}

	accessToken, err := h.accessService.ValidateToken(token)
	if err != nil {
		utils.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	// Verify instance ID matches
	if accessToken.InstanceID != id {
		utils.Error(c, http.StatusForbidden, "Invalid access token for this instance")
		return
	}

	// Redirect to actual access URL
	c.Redirect(http.StatusTemporaryRedirect, accessToken.AccessURL)
}

// ForceSync manually triggers a status sync
func (h *InstanceHandler) ForceSync(c *gin.Context) {
	// Get instance ID from URL
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get instance first to check ownership
	instance, err := h.instanceService.GetByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	if instance == nil {
		utils.Error(c, http.StatusNotFound, "Instance not found")
		return
	}

	// Check ownership
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	if userRole != "admin" && instance.UserID != userID.(int) {
		utils.Error(c, http.StatusForbidden, "Access denied")
		return
	}

	// Force sync
	if err := h.instanceService.ForceSyncInstance(id); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Instance status synced", nil)
}

// ProxyInstance proxies requests to an instance
func (h *InstanceHandler) ProxyInstance(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get token from query parameter
	token := c.Query("token")
	if token == "" {
		cookieToken, err := c.Cookie(fmt.Sprintf("instance_access_%d", id))
		if err != nil || cookieToken == "" {
			utils.Error(c, http.StatusBadRequest, "Access token required")
			return
		}
		token = cookieToken
	} else {
		// Promote the one-time query token into a cookie so iframe subresources and
		// websocket requests can reuse it without appending the token everywhere.
		c.SetCookie(
			fmt.Sprintf("instance_access_%d", id),
			token,
			int(time.Hour.Seconds()),
			fmt.Sprintf("/api/v1/instances/%d/proxy", id),
			"",
			false,
			true,
		)
	}

	// Check if it's a WebSocket upgrade request
	if strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
		err = h.proxyService.ProxyWebSocket(c.Request.Context(), id, token, c.Writer, c.Request)
		if err != nil {
			http.Error(c.Writer, err.Error(), http.StatusBadGateway)
		}
		return
	}

	// Proxy regular HTTP request
	err = h.proxyService.ProxyRequest(c.Request.Context(), id, token, c.Writer, c.Request)
	if err != nil {
		// Log the error
		fmt.Printf("Proxy error for instance %d: %v\n", id, err)

		// Return appropriate error response
		if err.Error() == "invalid token: token expired" ||
			err.Error() == "invalid token: invalid token" {
			http.Error(c.Writer, "Access token expired or invalid", http.StatusUnauthorized)
		} else if err.Error() == "token does not match instance" {
			http.Error(c.Writer, "Token does not match instance", http.StatusForbidden)
		} else {
			http.Error(c.Writer, fmt.Sprintf("Failed to proxy request: %v", err), http.StatusBadGateway)
		}
	}
}

func (h *InstanceHandler) ExportOpenClaw(c *gin.Context) {
	instance, ok := h.requireOwnedInstance(c)
	if !ok {
		return
	}

	if instance.Type != "openclaw" {
		utils.Error(c, http.StatusBadRequest, "openclaw import/export is only available for openclaw instances")
		return
	}

	if instance.Status != "running" {
		utils.Error(c, http.StatusBadRequest, "instance must be running to export .openclaw")
		return
	}

	archive, err := h.openClawTransferService.Export(c.Request.Context(), instance.UserID, instance.ID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	filename := fmt.Sprintf("%s.openclaw.tar.gz", sanitizeDownloadName(instance.Name))
	c.Header("Content-Type", "application/gzip")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	c.Header("Content-Length", strconv.Itoa(len(archive)))
	c.Data(http.StatusOK, "application/gzip", archive)
}

func (h *InstanceHandler) ImportOpenClaw(c *gin.Context) {
	instance, ok := h.requireOwnedInstance(c)
	if !ok {
		return
	}

	if instance.Type != "openclaw" {
		utils.Error(c, http.StatusBadRequest, "openclaw import/export is only available for openclaw instances")
		return
	}

	if instance.Status != "running" {
		utils.Error(c, http.StatusBadRequest, "instance must be running to import .openclaw")
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "file is required")
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	defer file.Close()

	if err := h.openClawTransferService.Import(c.Request.Context(), instance.UserID, instance.ID, io.LimitReader(file, 512<<20)); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "OpenClaw workspace imported successfully", nil)
}

func (h *InstanceHandler) requireOwnedInstance(c *gin.Context) (*models.Instance, bool) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return nil, false
	}

	instance, err := h.instanceService.GetByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return nil, false
	}

	if instance == nil {
		utils.Error(c, http.StatusNotFound, "Instance not found")
		return nil, false
	}

	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	if userRole != "admin" && instance.UserID != userID.(int) {
		utils.Error(c, http.StatusForbidden, "Access denied")
		return nil, false
	}

	return instance, true
}

func sanitizeDownloadName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "openclaw-workspace"
	}

	replacer := strings.NewReplacer("\\", "-", "/", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-")
	name = replacer.Replace(name)
	name = strings.ReplaceAll(name, " ", "-")
	return name
}
