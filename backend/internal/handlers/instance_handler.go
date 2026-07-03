package handlers

import (
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

// openclawMinArchiveBytes is the minimum acceptable size of an .openclaw
// export archive. A correctly-compressed tar.gz of a single empty file is
// already ~125 bytes, so anything smaller indicates a malformed or empty
// stream from the exec pipeline rather than a real workspace dump.
const openclawMinArchiveBytes = 100

const (
	defaultWorkspaceArchiveMaxMiB = int64(500)
	workspaceArchiveMaxMiBEnv     = "CLAWMANAGER_WORKSPACE_ARCHIVE_MAX_MIB"
	maxLiteBatchCreateCount       = 20
	maxLiteBatchDeleteCount       = 100
)

// desktopDirectProxyEnv toggles embedding the instance Service "host:port" into
// the access token so the edge gateway can proxy desktop traffic directly to the
// instance instead of relaying it through this control-plane process.
const desktopDirectProxyEnv = "CLAWMANAGER_DESKTOP_DIRECT_PROXY"

// desktopDirectProxyEnabled reports whether direct desktop proxying is enabled.
func desktopDirectProxyEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(desktopDirectProxyEnv))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func desktopProxyMode(directEnabled bool, upstream string) string {
	if !directEnabled {
		return "control-plane"
	}
	if strings.TrimSpace(upstream) == "" {
		return "fallback"
	}
	return "direct"
}

func (h *InstanceHandler) desktopAccessUpstream(c *gin.Context, instance *models.Instance, targetPort int32) (string, bool) {
	directProxyEnabled := desktopDirectProxyEnabled()
	if !directProxyEnabled {
		return "", false
	}
	if usesRuntimeGateway(instance) {
		return "", false
	}
	if !h.proxyService.IsWebtopInstanceType(instance.Type) {
		fmt.Printf("Desktop direct proxy fallback: unsupported desktop instance type instance=%d user=%d type=%s target_port=%d\n",
			instance.ID, instance.UserID, instance.Type, targetPort)
		return "", true
	}

	resolved, resolveErr := h.proxyService.ResolveUpstreamHostPort(
		c.Request.Context(),
		instance.UserID,
		instance.ID,
		targetPort,
	)
	if resolveErr != nil {
		fmt.Printf("Desktop direct proxy fallback: failed to resolve upstream instance=%d user=%d type=%s target_port=%d error=%v\n",
			instance.ID, instance.UserID, instance.Type, targetPort, resolveErr)
		return "", true
	}
	if strings.TrimSpace(resolved) == "" {
		fmt.Printf("Desktop direct proxy fallback: resolved empty upstream instance=%d user=%d type=%s target_port=%d\n",
			instance.ID, instance.UserID, instance.Type, targetPort)
		return "", true
	}

	fmt.Printf("Desktop direct proxy resolved: instance=%d user=%d type=%s target_port=%d upstream=%s\n",
		instance.ID, instance.UserID, instance.Type, targetPort, resolved)
	return resolved, true
}

func usesRuntimeGateway(instance *models.Instance) bool {
	if instance == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(instance.RuntimeType), services.RuntimeBackendGateway) {
		return true
	}
	if mode, ok := services.NormalizeInstanceMode(instance.InstanceMode); ok && mode == services.InstanceModeLite {
		return true
	}
	return false
}

func workspaceArchiveMaxMiB() int64 {
	value := strings.TrimSpace(os.Getenv(workspaceArchiveMaxMiBEnv))
	if value == "" {
		return defaultWorkspaceArchiveMaxMiB
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return defaultWorkspaceArchiveMaxMiB
	}
	return parsed
}

func workspaceArchiveMaxBytes() int64 {
	return workspaceArchiveMaxMiB() << 20
}

// InstanceHandler handles instance management requests
type InstanceHandler struct {
	instanceService               services.InstanceService
	instanceAgentService          services.InstanceAgentService
	runtimeStatusService          services.InstanceRuntimeStatusService
	instanceCommandService        services.InstanceCommandService
	instanceConfigRevisionService services.InstanceConfigRevisionService
	accessService                 *services.InstanceAccessService
	proxyService                  *services.InstanceProxyService
	shellService                  *services.InstanceShellService
	openClawTransferService       services.OpenClawTransferService
	openClawConfigService         services.OpenClawConfigService
	skillService                  services.SkillService
	externalAccessService         services.InstanceExternalAccessService
}

// NewInstanceHandler creates a new instance handler
func NewInstanceHandler(instanceService services.InstanceService, instanceAgentService services.InstanceAgentService, runtimeStatusService services.InstanceRuntimeStatusService, instanceCommandService services.InstanceCommandService, instanceConfigRevisionService services.InstanceConfigRevisionService, openClawConfigService services.OpenClawConfigService, skillService services.SkillService, externalAccessService services.InstanceExternalAccessService, proxyOptions ...services.InstanceProxyServiceOption) *InstanceHandler {
	accessService := services.NewInstanceAccessService()
	return &InstanceHandler{
		instanceService:               instanceService,
		instanceAgentService:          instanceAgentService,
		runtimeStatusService:          runtimeStatusService,
		instanceCommandService:        instanceCommandService,
		instanceConfigRevisionService: instanceConfigRevisionService,
		accessService:                 accessService,
		proxyService:                  services.NewInstanceProxyService(accessService, proxyOptions...),
		shellService:                  services.NewInstanceShellService(),
		openClawTransferService:       services.NewOpenClawTransferService(),
		openClawConfigService:         openClawConfigService,
		skillService:                  skillService,
		externalAccessService:         externalAccessService,
	}
}

// Shutdown releases resources held by the handler (e.g. background goroutines).
func (h *InstanceHandler) Shutdown() {
	if h.accessService != nil {
		h.accessService.Stop()
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

type ExternalAccessRequest struct {
	ExpiresMode   string     `json:"expires_mode,omitempty"`
	ExpiresPreset string     `json:"expires_preset,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}

// CreateInstanceRequest represents a create instance request
type CreateInstanceRequest struct {
	Name                 string                       `json:"name" binding:"required,min=3,max=50"`
	Description          *string                      `json:"description,omitempty"`
	Type                 string                       `json:"type" binding:"required,oneof=openclaw ubuntu debian centos custom webtop hermes"`
	Mode                 string                       `json:"mode" binding:"omitempty,oneof=lite pro"`
	InstanceMode         string                       `json:"instance_mode" binding:"omitempty,oneof=lite pro"`
	RuntimeType          string                       `json:"runtime_type" binding:"omitempty,oneof=gateway desktop shell"`
	DesktopStreamProfile string                       `json:"desktop_stream_profile,omitempty" binding:"omitempty,oneof=low standard high"`
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

type BatchCreateLiteInstanceTemplate struct {
	Type                 string                       `json:"type,omitempty"`
	CPUCores             float64                      `json:"cpu_cores,omitempty"`
	MemoryGB             int                          `json:"memory_gb,omitempty"`
	DiskGB               int                          `json:"disk_gb,omitempty"`
	OSType               string                       `json:"os_type,omitempty"`
	OSVersion            string                       `json:"os_version,omitempty"`
	ImageRegistry        *string                      `json:"image_registry,omitempty"`
	ImageTag             *string                      `json:"image_tag,omitempty"`
	EnvironmentOverrides map[string]string            `json:"environment_overrides,omitempty"`
	StorageClass         string                       `json:"storage_class,omitempty"`
	OpenClawConfigPlan   *services.OpenClawConfigPlan `json:"openclaw_config_plan,omitempty"`
	SkillIDs             []int                        `json:"skill_ids,omitempty"`
}

// BatchCreateLiteInstancesRequest represents a lite batch create request
type BatchCreateLiteInstancesRequest struct {
	NamePrefix string                           `json:"name_prefix" binding:"required,min=1,max=40"`
	Count      int                              `json:"count" binding:"required,min=1,max=20"`
	StartIndex int                              `json:"start_index" binding:"omitempty,min=0,max=9999"`
	Template   *BatchCreateLiteInstanceTemplate `json:"template,omitempty"`
}

type BatchCreateLiteInstanceResult struct {
	Name     string           `json:"name"`
	Status   string           `json:"status"`
	Instance *models.Instance `json:"instance,omitempty"`
	Error    string           `json:"error,omitempty"`
}

type BatchCreateLiteInstancesResponse struct {
	Requested int                             `json:"requested"`
	Created   int                             `json:"created"`
	Failed    int                             `json:"failed"`
	Results   []BatchCreateLiteInstanceResult `json:"results"`
}

type BatchDeleteLiteInstancesRequest struct {
	InstanceIDs []int `json:"instance_ids" binding:"required,min=1,max=100,dive,min=1"`
}

type BatchDeleteLiteInstanceResult struct {
	InstanceID int    `json:"instance_id"`
	Name       string `json:"name,omitempty"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

type BatchDeleteLiteInstancesResponse struct {
	Requested int                             `json:"requested"`
	Deleted   int                             `json:"deleted"`
	Failed    int                             `json:"failed"`
	Results   []BatchDeleteLiteInstanceResult `json:"results"`
}

// UpdateInstanceRequest represents an update instance request
type UpdateInstanceRequest struct {
	Name                 *string `json:"name,omitempty" binding:"omitempty,min=3,max=50"`
	Description          *string `json:"description,omitempty"`
	DesktopStreamProfile *string `json:"desktop_stream_profile,omitempty" binding:"omitempty,oneof=low standard high"`
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

	createReq := instanceCreateRequestToService(req)

	instance, err := h.instanceService.Create(userID.(int), createReq)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	skillIDs, err := h.resolveCreateInstanceSkillIDs(userID.(int), req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	for _, skillID := range skillIDs {
		if _, err := h.skillService.AttachSkillToInstance(instance.ID, skillID); err != nil {
			utils.HandleError(c, err)
			return
		}
	}

	utils.Success(c, http.StatusCreated, "Instance created successfully", instance)
}

func instanceCreateRequestToService(req CreateInstanceRequest) services.CreateInstanceRequest {
	return services.CreateInstanceRequest{
		Name:                 req.Name,
		Description:          req.Description,
		Type:                 req.Type,
		Mode:                 req.Mode,
		InstanceMode:         req.InstanceMode,
		RuntimeType:          req.RuntimeType,
		DesktopStreamProfile: req.DesktopStreamProfile,
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
}

func (h *InstanceHandler) BatchCreateLiteInstances(c *gin.Context) {
	userID, _ := c.Get("userID")

	var req BatchCreateLiteInstancesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	createRequests, handlerRequests, err := buildLiteBatchCreateRequests(req)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.instanceService.ValidateCreateRequests(userID.(int), createRequests); err != nil {
		utils.HandleError(c, err)
		return
	}

	response := BatchCreateLiteInstancesResponse{
		Requested: len(createRequests),
		Results:   make([]BatchCreateLiteInstanceResult, 0, len(createRequests)),
	}

	for idx, createReq := range createRequests {
		handlerReq := handlerRequests[idx]
		result := BatchCreateLiteInstanceResult{
			Name:   createReq.Name,
			Status: "created",
		}
		instance, err := h.instanceService.Create(userID.(int), createReq)
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			response.Failed++
			response.Results = append(response.Results, result)
			continue
		}
		result.Instance = instance

		skillIDs, err := h.resolveCreateInstanceSkillIDs(userID.(int), handlerReq)
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			response.Failed++
			response.Results = append(response.Results, result)
			continue
		}
		attachFailed := false
		for _, skillID := range skillIDs {
			if _, err := h.skillService.AttachSkillToInstance(instance.ID, skillID); err != nil {
				result.Status = "failed"
				result.Error = err.Error()
				response.Failed++
				response.Results = append(response.Results, result)
				attachFailed = true
				break
			}
		}
		if attachFailed {
			continue
		}

		response.Created++
		response.Results = append(response.Results, result)
	}

	status := http.StatusCreated
	if response.Failed > 0 {
		status = http.StatusMultiStatus
	}
	utils.Success(c, status, "Lite instances batch create completed", response)
}

func buildLiteBatchCreateRequests(req BatchCreateLiteInstancesRequest) ([]services.CreateInstanceRequest, []CreateInstanceRequest, error) {
	count := req.Count
	if count <= 0 {
		return nil, nil, fmt.Errorf("count is required")
	}
	if count > maxLiteBatchCreateCount {
		return nil, nil, fmt.Errorf("count must be at most %d", maxLiteBatchCreateCount)
	}

	prefix := strings.TrimSpace(req.NamePrefix)
	if prefix == "" {
		return nil, nil, fmt.Errorf("name prefix is required")
	}

	startIndex := req.StartIndex
	if startIndex <= 0 {
		startIndex = 1
	}

	template := CreateInstanceRequest{}
	if req.Template != nil {
		template.Type = req.Template.Type
		template.CPUCores = req.Template.CPUCores
		template.MemoryGB = req.Template.MemoryGB
		template.DiskGB = req.Template.DiskGB
		template.OSType = req.Template.OSType
		template.OSVersion = req.Template.OSVersion
		template.ImageRegistry = req.Template.ImageRegistry
		template.ImageTag = req.Template.ImageTag
		template.EnvironmentOverrides = req.Template.EnvironmentOverrides
		template.StorageClass = req.Template.StorageClass
		template.OpenClawConfigPlan = req.Template.OpenClawConfigPlan
		template.SkillIDs = req.Template.SkillIDs
	}
	template.Type = strings.ToLower(strings.TrimSpace(template.Type))
	if template.Type == "" {
		template.Type = "openclaw"
	}
	if template.Type != "openclaw" && template.Type != "hermes" {
		return nil, nil, fmt.Errorf("lite batch create supports openclaw or hermes instances")
	}
	if template.CPUCores <= 0 {
		template.CPUCores = 2
	}
	if template.MemoryGB <= 0 {
		template.MemoryGB = 4
	}
	if template.DiskGB <= 0 {
		template.DiskGB = 20
	}
	if strings.TrimSpace(template.OSType) == "" {
		template.OSType = template.Type
	}
	if strings.TrimSpace(template.OSVersion) == "" {
		template.OSVersion = "latest"
	}
	template.Mode = services.InstanceModeLite
	template.InstanceMode = services.InstanceModeLite
	template.RuntimeType = services.RuntimeBackendGateway
	template.GPUEnabled = false
	template.GPUCount = 0
	template.DesktopStreamProfile = ""

	createRequests := make([]services.CreateInstanceRequest, 0, count)
	handlerRequests := make([]CreateInstanceRequest, 0, count)
	for offset := 0; offset < count; offset++ {
		index := startIndex + offset
		item := template
		item.Name = fmt.Sprintf("%s-%03d", prefix, index)
		if len(item.Name) > 50 {
			return nil, nil, fmt.Errorf("generated instance name %q is too long", item.Name)
		}
		handlerRequests = append(handlerRequests, item)
		createRequests = append(createRequests, instanceCreateRequestToService(item))
	}

	return createRequests, handlerRequests, nil
}
func (h *InstanceHandler) resolveCreateInstanceSkillIDs(userID int, req CreateInstanceRequest) ([]int, error) {
	seen := map[int]struct{}{}
	result := make([]int, 0, len(req.SkillIDs))
	for _, skillID := range req.SkillIDs {
		if skillID <= 0 {
			continue
		}
		if _, exists := seen[skillID]; exists {
			continue
		}
		seen[skillID] = struct{}{}
		result = append(result, skillID)
	}

	if h.openClawConfigService != nil {
		bundleSkillIDs, err := h.openClawConfigService.ResolveBundleSkillIDs(userID, req.OpenClawConfigPlan)
		if err != nil {
			return nil, err
		}
		for _, skillID := range bundleSkillIDs {
			if skillID <= 0 {
				continue
			}
			if _, exists := seen[skillID]; exists {
				continue
			}
			seen[skillID] = struct{}{}
			result = append(result, skillID)
		}
	}

	return result, nil
}

func (h *InstanceHandler) BatchDeleteLiteInstances(c *gin.Context) {
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")

	var req BatchDeleteLiteInstancesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	if len(req.InstanceIDs) > maxLiteBatchDeleteCount {
		utils.Error(c, http.StatusBadRequest, fmt.Sprintf("instance_ids must include at most %d items", maxLiteBatchDeleteCount))
		return
	}

	seen := map[int]struct{}{}
	instances := make([]*models.Instance, 0, len(req.InstanceIDs))
	for _, id := range req.InstanceIDs {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}

		instance, err := h.instanceService.GetByID(id)
		if err != nil {
			utils.HandleError(c, err)
			return
		}
		if instance == nil {
			utils.Error(c, http.StatusNotFound, fmt.Sprintf("instance %d not found", id))
			return
		}
		if userRole != "admin" && instance.UserID != userID.(int) {
			utils.Error(c, http.StatusForbidden, "Access denied")
			return
		}
		if !isLiteInstance(instance) {
			utils.Error(c, http.StatusBadRequest, fmt.Sprintf("instance %d is not a lite instance", id))
			return
		}
		instances = append(instances, instance)
	}

	response := BatchDeleteLiteInstancesResponse{
		Requested: len(instances),
		Results:   make([]BatchDeleteLiteInstanceResult, 0, len(instances)),
	}
	for _, instance := range instances {
		result := BatchDeleteLiteInstanceResult{
			InstanceID: instance.ID,
			Name:       instance.Name,
			Status:     "deleting",
		}
		if err := h.instanceService.Delete(instance.ID); err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			response.Failed++
		} else {
			response.Deleted++
		}
		response.Results = append(response.Results, result)
	}

	status := http.StatusAccepted
	if response.Failed > 0 {
		status = http.StatusMultiStatus
	}
	utils.Success(c, status, "Lite instances batch delete started", response)
}

func isLiteInstance(instance *models.Instance) bool {
	if instance == nil {
		return false
	}
	if mode, ok := services.NormalizeInstanceMode(instance.InstanceMode); ok && mode == services.InstanceModeLite {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(instance.RuntimeType), services.RuntimeBackendGateway)
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
		Name:                 req.Name,
		Description:          req.Description,
		DesktopStreamProfile: req.DesktopStreamProfile,
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

	utils.Success(c, http.StatusAccepted, "Instance deletion started", nil)
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

	if userRole != "admin" {
		utils.Success(c, http.StatusOK, "Instance status retrieved successfully", gin.H{
			"instance_status": buildUserSafeInstanceStatus(instance, status),
		})
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

func buildUserSafeInstanceStatus(instance *models.Instance, status *services.InstanceStatus) map[string]interface{} {
	if status == nil {
		return map[string]interface{}{}
	}
	payload := map[string]interface{}{
		"instance_id":  status.InstanceID,
		"status":       status.Status,
		"availability": status.Availability,
		"created_at":   status.CreatedAt,
	}
	if payload["availability"] == "" {
		payload["availability"] = availabilityForInstanceStatus(status.Status)
	}
	if status.StartedAt != nil {
		payload["started_at"] = status.StartedAt
	}
	if instance == nil {
		return payload
	}
	if agentType, ok := services.NormalizeV2RuntimeType(instance.Type); ok && (strings.EqualFold(strings.TrimSpace(instance.RuntimeType), "gateway") || strings.EqualFold(strings.TrimSpace(instance.InstanceMode), "lite")) {
		payload["agent_type"] = agentType
		payload["workspace_usage_bytes"] = instance.WorkspaceUsageBytes
	}
	return payload
}

func availabilityForInstanceStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running":
		return "available"
	case "creating":
		return "starting"
	default:
		return "unavailable"
	}
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
	if !strings.EqualFold(instance.Type, "openclaw") && !strings.EqualFold(instance.Type, "hermes") {
		utils.Error(c, http.StatusBadRequest, "Only managed runtime instances support config revisions")
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

	if strings.EqualFold(strings.TrimSpace(instance.RuntimeType), "shell") {
		utils.Error(c, http.StatusBadRequest, "Desktop access is not available for shell runtime instances")
		return
	}

	// Generate proxy entry URL. The actual Service remains internal-only.
	accessURL := h.proxyService.GetProxyURLForInstance(instance, "")

	if accessURL == "" {
		utils.Error(c, http.StatusServiceUnavailable, "Unable to generate access URL")
		return
	}

	targetPort := h.proxyService.GetTargetPortForInstance(instance)

	// When direct desktop proxying is enabled, embed the instance Service
	// "host:port" into the token so the edge gateway can dial the instance
	// directly. On any failure we fall back to an empty upstream, which keeps
	// the request flowing through the in-process control-plane proxy.
	upstream, directProxyEnabled := h.desktopAccessUpstream(c, instance, targetPort)

	// Generate access token (valid for 1 hour)
	maxAgeSeconds := int(time.Hour.Seconds())
	token, err := h.accessService.GenerateToken(
		userID.(int),
		instance.ID,
		instance.Type,
		accessURL,
		upstream,
		targetPort,
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
		"token":                    token.Token,
		"access_url":               accessURL,
		"proxy_url":                h.proxyService.GetProxyURLForInstance(instance, token.Token),
		"expires_at":               token.ExpiresAt,
		"desktop_proxy_mode":       desktopProxyMode(directProxyEnabled, upstream),
		"desktop_upstream_present": upstream != "",
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

func (h *InstanceHandler) StreamShell(c *gin.Context) {
	instance, ok := h.requireOwnedInstance(c)
	if !ok {
		return
	}

	if !strings.EqualFold(strings.TrimSpace(instance.RuntimeType), "shell") {
		utils.Error(c, http.StatusBadRequest, "Shell access is only available for shell runtime instances")
		return
	}

	if instance.Status != "running" {
		utils.Error(c, http.StatusBadRequest, "Instance is not running")
		return
	}

	if err := h.shellService.Stream(c.Request.Context(), instance.UserID, instance.ID, c.Writer, c.Request); err != nil {
		if !c.Writer.Written() {
			utils.HandleError(c, err)
			return
		}
		fmt.Printf("Shell stream for instance %d closed with error: %v\n", instance.ID, err)
	}
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

	token, ok := h.proxyAccessToken(c, id)
	if !ok {
		return
	}

	h.proxyInstanceWithToken(c, id, token)
}

func (h *InstanceHandler) proxyAccessToken(c *gin.Context, id int) (string, bool) {
	cookieName := fmt.Sprintf("instance_access_%d", id)
	if cookieToken, err := c.Cookie(cookieName); err == nil && strings.TrimSpace(cookieToken) != "" {
		if accessToken, validateErr := h.accessService.ValidateToken(cookieToken); validateErr == nil && accessToken.InstanceID == id {
			return cookieToken, true
		}
	}

	queryToken := strings.TrimSpace(c.Query("token"))
	if queryToken == "" {
		utils.Error(c, http.StatusBadRequest, "Access token required")
		return "", false
	}
	accessToken, err := h.accessService.ValidateToken(queryToken)
	if err != nil || accessToken.InstanceID != id {
		utils.Error(c, http.StatusUnauthorized, "Access token expired or invalid")
		return "", false
	}

	// Promote only a validated ClawManager access token. Runtime applications may
	// also use a token query parameter for their own websocket/session protocol.
	c.SetCookie(
		cookieName,
		queryToken,
		int(time.Hour.Seconds()),
		fmt.Sprintf("/api/v1/instances/%d/proxy", id),
		"",
		false,
		true,
	)
	return queryToken, true
}

func (h *InstanceHandler) proxyInstanceWithToken(c *gin.Context, id int, token string) {
	// Check if it's a WebSocket upgrade request
	if strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
		if err := h.proxyService.ProxyWebSocket(c.Request.Context(), id, token, c.Writer, c.Request); err != nil {
			if errors.Is(err, services.ErrInstanceGatewayUnavailable) {
				http.Error(c.Writer, "Instance gateway is not available", http.StatusServiceUnavailable)
			} else {
				http.Error(c.Writer, err.Error(), http.StatusBadGateway)
			}
		}
		return
	}

	// Proxy regular HTTP request
	if err := h.proxyService.ProxyRequest(c.Request.Context(), id, token, c.Writer, c.Request); err != nil {
		// Log the error
		fmt.Printf("Proxy error for instance %d: %v\n", id, err)

		// Return appropriate error response
		if err.Error() == "invalid token: token expired" ||
			err.Error() == "invalid token: invalid token" {
			http.Error(c.Writer, "Access token expired or invalid", http.StatusUnauthorized)
		} else if err.Error() == "token does not match instance" {
			http.Error(c.Writer, "Token does not match instance", http.StatusForbidden)
		} else if errors.Is(err, services.ErrInstanceGatewayUnavailable) {
			http.Error(c.Writer, "Instance gateway is not available", http.StatusServiceUnavailable)
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
		if errors.Is(err, services.ErrOpenClawWorkspaceMissing) {
			utils.Error(c, http.StatusNotFound, "openclaw workspace is empty or missing")
			return
		}
		utils.HandleError(c, err)
		return
	}

	if len(archive) < openclawMinArchiveBytes {
		utils.Error(c, http.StatusInternalServerError, "export produced an empty archive")
		return
	}
	maxArchiveBytes := workspaceArchiveMaxBytes()
	if int64(len(archive)) > maxArchiveBytes {
		utils.Error(c, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("archive too large; maximum archive size is %d MiB", maxArchiveBytes>>20))
		return
	}

	filename := fmt.Sprintf("%s.openclaw.tar.gz", sanitizeDownloadName(instance.Name))
	c.Header("Content-Type", "application/gzip")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	c.Header("Content-Length", strconv.Itoa(len(archive)))
	c.Data(http.StatusOK, "application/gzip", archive)
}

func (h *InstanceHandler) ExportHermes(c *gin.Context) {
	instance, ok := h.requireOwnedInstance(c)
	if !ok {
		return
	}

	if instance.Type != "hermes" {
		utils.Error(c, http.StatusBadRequest, "hermes import/export is only available for hermes instances")
		return
	}

	if instance.Status != "running" {
		utils.Error(c, http.StatusBadRequest, "instance must be running to export .hermes")
		return
	}

	archive, err := h.openClawTransferService.ExportHermes(c.Request.Context(), instance.UserID, instance.ID)
	if err != nil {
		if errors.Is(err, services.ErrHermesWorkspaceMissing) {
			utils.Error(c, http.StatusNotFound, "hermes workspace is empty or missing")
			return
		}
		utils.HandleError(c, err)
		return
	}

	if len(archive) < openclawMinArchiveBytes {
		utils.Error(c, http.StatusInternalServerError, "export produced an empty archive")
		return
	}
	maxArchiveBytes := workspaceArchiveMaxBytes()
	if int64(len(archive)) > maxArchiveBytes {
		utils.Error(c, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("archive too large; maximum archive size is %d MiB", maxArchiveBytes>>20))
		return
	}

	filename := fmt.Sprintf("%s.hermes.tar.gz", sanitizeDownloadName(instance.Name, "hermes-workspace"))
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

	// Cap the request body at the same deployment limit used by nginx. When
	// the request reaches the backend, MaxBytesReader trips ParseMultipartForm
	// (invoked by c.FormFile) with a typed *http.MaxBytesError.
	maxArchiveBytes := workspaceArchiveMaxBytes()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxArchiveBytes)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			utils.Error(c, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("archive too large; maximum upload size is %d MiB", maxArchiveBytes>>20))
			return
		}
		utils.Error(c, http.StatusBadRequest, "file is required")
		return
	}

	if fileHeader.Size > maxArchiveBytes {
		utils.Error(c, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("archive too large; maximum upload size is %d MiB", maxArchiveBytes>>20))
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	defer file.Close()

	if err := h.openClawTransferService.Import(c.Request.Context(), instance.UserID, instance.ID, io.LimitReader(file, maxArchiveBytes)); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "OpenClaw workspace imported successfully", nil)
}

func (h *InstanceHandler) ImportHermes(c *gin.Context) {
	instance, ok := h.requireOwnedInstance(c)
	if !ok {
		return
	}

	if instance.Type != "hermes" {
		utils.Error(c, http.StatusBadRequest, "hermes import/export is only available for hermes instances")
		return
	}

	if instance.Status != "running" {
		utils.Error(c, http.StatusBadRequest, "instance must be running to import .hermes")
		return
	}

	maxArchiveBytes := workspaceArchiveMaxBytes()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxArchiveBytes)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			utils.Error(c, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("archive too large; maximum upload size is %d MiB", maxArchiveBytes>>20))
			return
		}
		utils.Error(c, http.StatusBadRequest, "file is required")
		return
	}

	if fileHeader.Size > maxArchiveBytes {
		utils.Error(c, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("archive too large; maximum upload size is %d MiB", maxArchiveBytes>>20))
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	defer file.Close()

	if err := h.openClawTransferService.ImportHermes(c.Request.Context(), instance.UserID, instance.ID, io.LimitReader(file, maxArchiveBytes)); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Hermes workspace imported successfully", nil)
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

func (h *InstanceHandler) GetExternalAccess(c *gin.Context) {
	instance, ok := h.requireOwnedInstance(c)
	if !ok {
		return
	}
	if h.externalAccessService == nil {
		utils.Error(c, http.StatusServiceUnavailable, "External access is not configured")
		return
	}
	access, err := h.externalAccessService.Get(c.Request.Context(), instance.ID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	payload := gin.H{"external_access": access}
	if shareURL := services.ExternalAccessShareURL(access); shareURL != "" {
		payload["share_url"] = shareURL
	}
	if password := services.ExternalAccessPassword(access); password != "" {
		payload["password"] = password
	}
	utils.Success(c, http.StatusOK, "External access retrieved successfully", payload)
}

func (h *InstanceHandler) EnableShareLink(c *gin.Context) {
	instance, ok := h.requireOwnedInstance(c)
	if !ok {
		return
	}
	if h.externalAccessService == nil {
		utils.Error(c, http.StatusServiceUnavailable, "External access is not configured")
		return
	}
	var req ExternalAccessRequest
	if err := c.ShouldBindJSON(&req); err != nil && err != io.EOF {
		utils.ValidationError(c, err)
		return
	}
	userID, _ := c.Get("userID")
	result, err := h.externalAccessService.EnableShareLink(c.Request.Context(), instance.ID, userID.(int), externalAccessExpirationRequest(req))
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Share link enabled successfully", result)
}

func (h *InstanceHandler) CreateExternalAccessPassword(c *gin.Context) {
	instance, ok := h.requireOwnedInstance(c)
	if !ok {
		return
	}
	if h.externalAccessService == nil {
		utils.Error(c, http.StatusServiceUnavailable, "External access is not configured")
		return
	}
	var req ExternalAccessRequest
	if err := c.ShouldBindJSON(&req); err != nil && err != io.EOF {
		utils.ValidationError(c, err)
		return
	}
	userID, _ := c.Get("userID")
	result, err := h.externalAccessService.CreatePassword(c.Request.Context(), instance.ID, userID.(int), externalAccessExpirationRequest(req))
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Share link password created successfully", result)
}

func (h *InstanceHandler) DisableExternalAccess(c *gin.Context) {
	instance, ok := h.requireOwnedInstance(c)
	if !ok {
		return
	}
	if h.externalAccessService == nil {
		utils.Error(c, http.StatusServiceUnavailable, "External access is not configured")
		return
	}
	if err := h.externalAccessService.Disable(c.Request.Context(), instance.ID); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "External access disabled successfully", nil)
}

func (h *InstanceHandler) OpenShortExternalAccess(c *gin.Context) {
	if h.externalAccessService == nil {
		utils.Error(c, http.StatusServiceUnavailable, "External access is not configured")
		return
	}
	code := strings.TrimSpace(c.Param("code"))
	access, err := h.externalAccessService.ResolveShortLink(c.Request.Context(), code)
	if err != nil {
		utils.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	var instance *models.Instance
	var token string
	canonicalAccessURL := ""
	switch access.AuthMode {
	case services.ExternalAccessModeShareLink:
		if _, err := h.externalAccessService.ValidateShortLink(c.Request.Context(), code, ""); err != nil {
			utils.Error(c, http.StatusUnauthorized, err.Error())
			return
		}
	case services.ExternalAccessModePassword:
		token = h.validShortLinkAccessToken(c, code, access.InstanceID)
		if token == "" {
			password := externalPassword(c)
			isPasswordFormPost := c.Request.Method == http.MethodPost && password == ""
			if isPasswordFormPost {
				password = c.PostForm("password")
			}
			if strings.TrimSpace(password) == "" {
				if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
					renderShortLinkPasswordForm(c, code, "")
					return
				}
				utils.Error(c, http.StatusUnauthorized, "share link password is required")
				return
			}
			if _, err := h.externalAccessService.ValidateShortLink(c.Request.Context(), code, password); err != nil {
				if c.Request.Method == http.MethodPost || shortExternalAccessWantsHTML(c) {
					renderShortLinkPasswordForm(c, code, "Invalid password")
					return
				}
				utils.Error(c, http.StatusUnauthorized, err.Error())
				return
			}
			var ok bool
			instance, ok = h.requireExternalAccessInstance(c, access)
			if !ok {
				return
			}
			instanceToken, ok := h.issueShortExternalAccessToken(c, instance, code)
			if !ok {
				return
			}
			token = instanceToken.Token
			canonicalAccessURL = instanceToken.AccessURL
			if c.Request.Method == http.MethodPost {
				c.Redirect(http.StatusSeeOther, shortExternalAccessEntryPath(code))
				return
			}
		}
	default:
		utils.Error(c, http.StatusBadRequest, "Unsupported share link mode")
		return
	}

	if instance == nil {
		var ok bool
		instance, ok = h.requireExternalAccessInstance(c, access)
		if !ok {
			return
		}
	}
	if token == "" {
		instanceToken, ok := h.issueShortExternalAccessToken(c, instance, code)
		if !ok {
			return
		}
		token = instanceToken.Token
		canonicalAccessURL = instanceToken.AccessURL
	} else {
		setShortExternalAccessCookies(c, instance.ID, code, token, int(time.Hour.Seconds()))
		canonicalAccessURL = h.proxyService.GetProxyURLForInstance(instance, "")
	}

	originalPath := c.Request.URL.Path
	if redirectTarget := shortExternalAccessEntryRedirectTarget(c.Request.Method, originalPath, code, canonicalAccessURL); redirectTarget != "" {
		c.Redirect(http.StatusSeeOther, redirectTarget)
		return
	}

	originalRawPath := c.Request.URL.RawPath
	originalAuthorization := c.Request.Header.Get("Authorization")
	originalPassword := c.Request.Header.Get("X-Password")
	c.Request.URL.Path = shortExternalAccessProxyPath(originalPath, code, instance.ID)
	c.Request.URL.RawPath = ""
	c.Request.Header.Del("Authorization")
	c.Request.Header.Del("X-Password")
	defer func() {
		c.Request.URL.Path = originalPath
		c.Request.URL.RawPath = originalRawPath
		if originalAuthorization != "" {
			c.Request.Header.Set("Authorization", originalAuthorization)
		}
		if originalPassword != "" {
			c.Request.Header.Set("X-Password", originalPassword)
		}
	}()

	h.proxyInstanceWithToken(c, instance.ID, token)
}

func bearerToken(header string) string {
	value := strings.TrimSpace(header)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(value), "bearer ") {
		return strings.TrimSpace(value[len("bearer "):])
	}
	return value
}

func externalPassword(c *gin.Context) string {
	if password := strings.TrimSpace(c.GetHeader("X-Password")); password != "" {
		return password
	}
	return bearerToken(c.GetHeader("Authorization"))
}

func externalAccessExpirationRequest(req ExternalAccessRequest) services.ExternalAccessExpirationRequest {
	return services.ExternalAccessExpirationRequest{
		Mode:      strings.TrimSpace(req.ExpiresMode),
		Preset:    strings.TrimSpace(req.ExpiresPreset),
		ExpiresAt: req.ExpiresAt,
	}
}

func (h *InstanceHandler) requireExternalAccessInstance(c *gin.Context, access *models.InstanceExternalAccess) (*models.Instance, bool) {
	if access == nil {
		utils.Error(c, http.StatusUnauthorized, "External access is not enabled")
		return nil, false
	}
	instance, err := h.instanceService.GetByID(access.InstanceID)
	if err != nil {
		utils.HandleError(c, err)
		return nil, false
	}
	if instance == nil {
		utils.Error(c, http.StatusNotFound, "Instance not found")
		return nil, false
	}
	if instance.Status != "running" {
		utils.Error(c, http.StatusServiceUnavailable, "Instance is not running")
		return nil, false
	}
	if strings.EqualFold(strings.TrimSpace(instance.RuntimeType), "shell") {
		utils.Error(c, http.StatusBadRequest, "External desktop access is not available for shell runtime instances")
		return nil, false
	}
	return instance, true
}

func (h *InstanceHandler) issueShortExternalAccessToken(c *gin.Context, instance *models.Instance, code string) (*services.AccessToken, bool) {
	accessURL := h.proxyService.GetProxyURLForInstance(instance, "")
	if accessURL == "" {
		utils.Error(c, http.StatusServiceUnavailable, "Unable to generate access URL")
		return nil, false
	}
	targetPort := h.proxyService.GetTargetPortForInstance(instance)
	upstream, _ := h.desktopAccessUpstream(c, instance, targetPort)
	instanceToken, err := h.accessService.GenerateToken(
		instance.UserID,
		instance.ID,
		instance.Type,
		accessURL,
		upstream,
		targetPort,
		1*time.Hour,
	)
	if err != nil {
		utils.HandleError(c, err)
		return nil, false
	}
	setShortExternalAccessCookies(c, instance.ID, code, instanceToken.Token, int(time.Hour.Seconds()))
	return instanceToken, true
}

func (h *InstanceHandler) validShortLinkAccessToken(c *gin.Context, code string, instanceID int) string {
	if h == nil || h.accessService == nil {
		return ""
	}
	token, err := c.Cookie(shortExternalAccessCookieName(code))
	if err != nil || strings.TrimSpace(token) == "" {
		return ""
	}
	accessToken, err := h.accessService.ValidateToken(token)
	if err != nil || accessToken.InstanceID != instanceID {
		return ""
	}
	return token
}

func setShortExternalAccessCookies(c *gin.Context, instanceID int, code, token string, maxAge int) {
	c.SetCookie(
		fmt.Sprintf("instance_access_%d", instanceID),
		token,
		maxAge,
		fmt.Sprintf("/api/v1/instances/%d/proxy", instanceID),
		"",
		false,
		true,
	)
	c.SetCookie(
		shortExternalAccessCookieName(code),
		token,
		maxAge,
		shortExternalAccessCookiePath(code),
		"",
		false,
		true,
	)
}

func shortExternalAccessCookieName(code string) string {
	code = strings.Trim(strings.TrimSpace(code), "/")
	var builder strings.Builder
	for _, r := range code {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '_' || r == '-':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	if builder.Len() == 0 {
		return "share_link_access"
	}
	return "share_link_access_" + builder.String()
}

func shortExternalAccessCookiePath(code string) string {
	code = strings.Trim(strings.TrimSpace(code), "/")
	if code == "" {
		return "/s"
	}
	return "/s/" + code
}

func shortExternalAccessEntryPath(code string) string {
	return shortExternalAccessCookiePath(code) + "/"
}

func shortExternalAccessEntryRedirectTarget(method, requestPath, code, canonicalPath string) string {
	if method != http.MethodGet && method != http.MethodHead {
		return ""
	}
	path := strings.TrimSpace(requestPath)
	entryPath := shortExternalAccessEntryPath(code)
	if path != entryPath && path != strings.TrimSuffix(entryPath, "/") {
		return ""
	}
	target := strings.TrimSpace(canonicalPath)
	if target == "" {
		return ""
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Path == "" {
		return ""
	}
	if !strings.HasPrefix(parsed.Path, "/api/v1/instances/") {
		return ""
	}
	return parsed.Path
}

func shortExternalAccessWantsHTML(c *gin.Context) bool {
	accept := strings.ToLower(c.GetHeader("Accept"))
	return strings.Contains(accept, "text/html")
}

func renderShortLinkPasswordForm(c *gin.Context, code, errorMessage string) {
	action := html.EscapeString(shortExternalAccessEntryPath(code))
	errorHTML := ""
	if strings.TrimSpace(errorMessage) != "" {
		errorHTML = fmt.Sprintf(`<div class="error">%s</div>`, html.EscapeString(errorMessage))
	}
	body := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Share link password</title>
  <style>
    :root { color-scheme: light; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #f6f8fb; color: #0f172a; }
    main { width: min(92vw, 380px); border: 1px solid #e2e8f0; border-radius: 8px; background: #fff; box-shadow: 0 18px 45px rgba(15, 23, 42, .08); padding: 28px; }
    h1 { margin: 0 0 18px; font-size: 20px; line-height: 1.25; }
    label { display: block; margin-bottom: 8px; font-size: 13px; font-weight: 600; color: #334155; }
    input { box-sizing: border-box; width: 100%%; height: 42px; border: 1px solid #cbd5e1; border-radius: 6px; padding: 0 12px; font-size: 14px; outline: none; }
    input:focus { border-color: #4f46e5; box-shadow: 0 0 0 3px rgba(79, 70, 229, .14); }
    button { width: 100%%; height: 42px; margin-top: 14px; border: 0; border-radius: 6px; background: #4f46e5; color: white; font-size: 14px; font-weight: 700; cursor: pointer; }
    .error { margin-bottom: 12px; border: 1px solid #fecaca; border-radius: 6px; background: #fef2f2; color: #b91c1c; padding: 10px 12px; font-size: 13px; }
  </style>
</head>
<body>
  <main>
    <h1>Share link password</h1>
    %s
    <form method="post" action="%s" autocomplete="off">
      <label for="share-link-password">Password</label>
      <input id="share-link-password" name="password" type="password" autofocus required>
      <button type="submit">Open</button>
    </form>
  </main>
</body>
</html>`, errorHTML, action)
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(body))
}

func shortExternalAccessProxyPath(requestPath, code string, instanceID int) string {
	internalPrefix := fmt.Sprintf("/api/v1/instances/%d/proxy", instanceID)
	shortPrefix := fmt.Sprintf("/s/%s", strings.Trim(strings.TrimSpace(code), "/"))
	path := strings.TrimSpace(requestPath)
	if path == "" || path == shortPrefix || path == shortPrefix+"/" {
		return internalPrefix + "/"
	}
	if strings.HasPrefix(path, shortPrefix+"/") {
		return internalPrefix + strings.TrimPrefix(path, shortPrefix)
	}
	return internalPrefix + "/"
}

func sanitizeDownloadName(name string, fallback ...string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		if len(fallback) > 0 && strings.TrimSpace(fallback[0]) != "" {
			return strings.TrimSpace(fallback[0])
		}
		return "openclaw-workspace"
	}

	replacer := strings.NewReplacer("\\", "-", "/", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-")
	name = replacer.Replace(name)
	name = strings.ReplaceAll(name, " ", "-")
	return name
}
