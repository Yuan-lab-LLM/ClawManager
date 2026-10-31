package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/config"
	"clawreef/internal/models"
	"clawreef/internal/northbound"
	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

const (
	ieiSystemSessionCookie        = "clawmanager_iei_session"
	ieiSystemSessionBindingPrefix = "iei:"
)

type IEISystemHandler struct {
	ownerTeams      *northbound.OwnerTeamAPI
	dashboard       ownerDashboardReader
	cfg             config.IEISystemConfig
	sso             *services.IEISSOService
	instances       services.InstanceService
	instanceHandler *InstanceHandler
	workspace       *WorkspaceFileHandler
	lifecycle       ieiLifecycleService
	hermesDesktop   ieiHermesDesktopActivator
}

type ieiHermesDesktopActivator interface {
	Activate(context.Context, int, int) (*services.HermesDesktopDescriptor, string, error)
}

type ieiLifecycleService interface {
	SubmitLifecycle(principal northbound.Principal, idempotencyKey string, instanceID int, mode, action string) (*models.NorthboundOperation, bool, error)
	GetOperation(userID int, operationID string) (*models.NorthboundOperation, error)
	GetLatestLifecycleOperation(userID, instanceID int) (*models.NorthboundOperation, error)
}

type ieiSessionLifecycleService interface {
	GetOperationForSession(operationID, sessionID string) (*models.NorthboundOperation, error)
}

type ieiSessionExchangeRequest struct {
	Token string `json:"token" binding:"required,max=4096"`
}

type ieiInstanceView struct {
	WorkspaceAvailable bool       `json:"workspace_available"`
	WorkspaceRoot      string     `json:"workspace_root"`
	ID                 int        `json:"id"`
	Owner              string     `json:"owner"`
	Name               string     `json:"name"`
	Alias              *string    `json:"alias,omitempty"`
	Description        *string    `json:"description,omitempty"`
	Type               string     `json:"type"`
	RuntimeType        string     `json:"runtime_type"`
	RuntimeVariant     string     `json:"runtime_variant,omitempty"`
	InstanceMode       string     `json:"instance_mode"`
	Status             string     `json:"status"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
}

type ieiLifecycleOperationView struct {
	OperationID  string     `json:"operation_id"`
	Action       string     `json:"action"`
	Status       string     `json:"status"`
	InstanceID   *int       `json:"instance_id,omitempty"`
	ErrorCode    *string    `json:"error_code,omitempty"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func NewIEISystemHandler(
	cfg config.IEISystemConfig,
	sso *services.IEISSOService,
	instances services.InstanceService,
	instanceHandler *InstanceHandler,
	lifecycle ...ieiLifecycleService,
) *IEISystemHandler {
	handler := &IEISystemHandler{cfg: cfg, sso: sso, instances: instances, instanceHandler: instanceHandler}
	if len(lifecycle) > 0 {
		handler.lifecycle = lifecycle[0]
	}
	return handler
}

func (h *IEISystemHandler) SetWorkspaceFileHandler(workspace *WorkspaceFileHandler) {
	h.workspace = workspace
}

func (h *IEISystemHandler) SetOwnerTeamAPI(api *northbound.OwnerTeamAPI) { h.ownerTeams = api }

func (h *IEISystemHandler) RegisterOwnerTeamRoutes(group *gin.RouterGroup) {
	northbound.RegisterOwnerTeamRoutes(group, func() *northbound.OwnerTeamAPI { return h.ownerTeams }, func(c *gin.Context) (*northbound.OwnerTeamIdentity, bool) {
		session, ok := h.requireSession(c)
		if !ok {
			return nil, false
		}
		if c.Request.Method != http.MethodGet {
			if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
				utils.Error(c, 403, "Cross-site request denied")
				return nil, false
			}
			if origin := strings.TrimRight(c.GetHeader("Origin"), "/"); origin != "" {
				parsed, err := url.Parse(origin)
				if err != nil || parsed.Host != c.Request.Host || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
					utils.Error(c, 403, "Cross-site request denied")
					return nil, false
				}
			}
		}
		return &northbound.OwnerTeamIdentity{Owner: session.Email, SessionID: "iei-team:" + session.SessionID, Source: "iei"}, true
	})
}

func (h *IEISystemHandler) SetLifecycleService(lifecycle ieiLifecycleService) {
	h.lifecycle = lifecycle
}

func (h *IEISystemHandler) SetHermesDesktopActivator(activator ieiHermesDesktopActivator) {
	h.hermesDesktop = activator
}

func (h *IEISystemHandler) SetHermesDesktopService(service *services.HermesDesktopService) {
	h.hermesDesktop = service
}

func (h *IEISystemHandler) ExchangeSession(c *gin.Context) {
	h.noStore(c)
	if h.sso == nil || !h.sso.Enabled() {
		utils.Error(c, http.StatusServiceUnavailable, "IEI system SSO is not configured")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	var request ieiSessionExchangeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		utils.Error(c, http.StatusBadRequest, "token is required")
		return
	}
	session, err := h.sso.ExchangeExternalToken(request.Token)
	if err != nil {
		h.clearSessionCookie(c)
		utils.Error(c, http.StatusUnauthorized, "IEI system token is invalid or expired")
		return
	}
	h.setSessionCookie(c, session.Token, session.ExpiresAt)
	utils.Success(c, http.StatusOK, "IEI system session established", gin.H{
		"owner":      session.Email,
		"expires_at": session.ExpiresAt,
	})
}

func (h *IEISystemHandler) GetSession(c *gin.Context) {
	h.noStore(c)
	session, ok := h.requireSession(c)
	if !ok {
		return
	}
	utils.Success(c, http.StatusOK, "IEI system session is valid", gin.H{
		"owner":      session.Email,
		"expires_at": session.ExpiresAt,
	})
}

// RefreshSession renews a still-valid ClawManager portal session. IEI is only
// involved in the initial external-token exchange; active browser sessions are
// maintained locally after that trust boundary has been crossed.
func (h *IEISystemHandler) RefreshSession(c *gin.Context) {
	h.noStore(c)
	session, ok := h.requireSession(c)
	if !ok {
		return
	}
	renewed, err := h.sso.RenewSession(session.Token)
	if err != nil {
		h.clearSessionCookie(c)
		utils.Error(c, http.StatusUnauthorized, "IEI system session is invalid or expired")
		return
	}
	h.setSessionCookie(c, renewed.Token, renewed.ExpiresAt)
	utils.Success(c, http.StatusOK, "IEI system session renewed", gin.H{
		"owner":      renewed.Email,
		"expires_at": renewed.ExpiresAt,
	})
}

func (h *IEISystemHandler) DeleteSession(c *gin.Context) {
	h.noStore(c)
	if token, err := c.Cookie(ieiSystemSessionCookie); err == nil && token != "" {
		if err := h.sso.RevokeSession(token); err != nil && !errors.Is(err, services.ErrInvalidIEISession) {
			utils.Error(c, 503, "Unable to revoke session; please retry")
			return
		}
	}
	h.clearSessionCookie(c)
	utils.Success(c, http.StatusOK, "IEI system session cleared", nil)
}

func (h *IEISystemHandler) ListInstances(c *gin.Context) {
	h.noStore(c)
	session, ok := h.requireSession(c)
	if !ok {
		return
	}
	page := positiveQueryInt(c, "page", 1)
	limit := positiveQueryInt(c, "limit", 100)
	if limit > 100 {
		limit = 100
	}
	ownerService, ok := h.instances.(services.IEISystemInstanceService)
	if !ok {
		utils.Error(c, http.StatusServiceUnavailable, "IEI system instance lookup is unavailable")
		return
	}
	instances, total, err := ownerService.GetSupportedByOwnerEmail(session.Email, (page-1)*limit, limit)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	views := make([]ieiInstanceView, 0, len(instances))
	for idx := range instances {
		if h.ownedSupportedInstance(&instances[idx], session.Email) {
			views = append(views, newIEIInstanceView(&instances[idx]))
		}
	}
	utils.Success(c, http.StatusOK, "IEI instances retrieved", gin.H{
		"instances": views,
		"total":     total,
		"page":      page,
		"limit":     limit,
	})
}

func (h *IEISystemHandler) GetInstance(c *gin.Context) {
	h.noStore(c)
	session, ok := h.requireSession(c)
	if !ok {
		return
	}
	instance, ok := h.requireOwnedSupportedInstance(c, session.Email)
	if !ok {
		return
	}
	utils.Success(c, http.StatusOK, "IEI instance retrieved", gin.H{
		"instance": newIEIInstanceView(instance),
	})
}

// RestartInstance restarts an IEI-owned instance through the existing runtime
// lifecycle service. The dedicated IEI session and exact owner match are
// required; the normal user JWT handler must not be reused for this surface.
func (h *IEISystemHandler) RestartInstance(c *gin.Context) {
	h.noStore(c)
	session, ok := h.requireSession(c)
	if !ok {
		return
	}
	instance, ok := h.requireOwnedSupportedInstance(c, session.Email)
	if !ok {
		return
	}

	status := strings.ToLower(strings.TrimSpace(instance.Status))
	if status != "running" && status != "error" {
		utils.Error(c, http.StatusConflict, "Instance is not running")
		return
	}
	h.submitLifecycle(c, session, instance, "restart")
}

// ResetInstance replaces an IEI-owned runtime with a clean instance. The old
// identity and data are removed only after the replacement is healthy.
func (h *IEISystemHandler) ResetInstance(c *gin.Context) {
	h.noStore(c)
	session, ok := h.requireSession(c)
	if !ok {
		return
	}
	instance, ok := h.requireOwnedSupportedInstance(c, session.Email)
	if !ok {
		return
	}

	status := strings.ToLower(strings.TrimSpace(instance.Status))
	if status != "running" && status != "stopped" && status != "error" {
		utils.Error(c, http.StatusConflict, "Instance cannot be reset while a lifecycle operation is in progress")
		return
	}
	var request northbound.ConfirmInstanceResetRequest
	if err := c.ShouldBindJSON(&request); err != nil || !request.ConfirmDataLoss {
		utils.Error(c, http.StatusBadRequest, "Reset permanently deletes all instance data; set confirm_data_loss to true")
		return
	}
	h.submitLifecycle(c, session, instance, "reset")
}

func (h *IEISystemHandler) GetLatestLifecycleOperation(c *gin.Context) {
	h.noStore(c)
	session, ok := h.requireSession(c)
	if !ok {
		return
	}
	instance, ok := h.requireOwnedSupportedInstance(c, session.Email)
	if !ok {
		return
	}
	if h.lifecycle == nil {
		utils.Error(c, http.StatusServiceUnavailable, "Instance lifecycle service is unavailable")
		return
	}
	item, err := h.lifecycle.GetLatestLifecycleOperation(instance.UserID, instance.ID)
	if err != nil {
		h.lifecycleError(c, err, "Unable to load instance lifecycle status")
		return
	}
	if item == nil {
		utils.Success(c, http.StatusOK, "No instance lifecycle operation", gin.H{"operation": nil})
		return
	}
	utils.Success(c, http.StatusOK, "Instance lifecycle operation retrieved", gin.H{"operation": newIEILifecycleOperationView(item)})
}

func (h *IEISystemHandler) GetLifecycleOperation(c *gin.Context) {
	h.noStore(c)
	session, ok := h.requireSession(c)
	if !ok {
		return
	}
	instance, ok := h.requireOwnedSupportedInstance(c, session.Email)
	if !ok {
		return
	}
	if h.lifecycle == nil {
		utils.Error(c, http.StatusServiceUnavailable, "Instance lifecycle service is unavailable")
		return
	}
	item, err := h.lifecycle.GetOperation(instance.UserID, strings.TrimSpace(c.Param("operationID")))
	if err != nil || item == nil || item.InstanceID == nil || *item.InstanceID != instance.ID {
		if err != nil {
			var apiErr *northbound.APIError
			if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
				h.lifecycleError(c, err, "Unable to load instance lifecycle status")
				return
			}
		}
		utils.Error(c, http.StatusNotFound, "Instance lifecycle operation not found")
		return
	}
	utils.Success(c, http.StatusOK, "Instance lifecycle operation retrieved", gin.H{"operation": newIEILifecycleOperationView(item)})
}

// GetSessionLifecycleOperation keeps a replacement reset observable after the
// source instance has been hidden or deleted and the operation points at the
// newly created instance ID.
func (h *IEISystemHandler) GetSessionLifecycleOperation(c *gin.Context) {
	h.noStore(c)
	session, ok := h.requireSession(c)
	if !ok {
		return
	}
	if h.lifecycle == nil {
		utils.Error(c, http.StatusServiceUnavailable, "Instance lifecycle service is unavailable")
		return
	}
	sessionLifecycle, supported := h.lifecycle.(ieiSessionLifecycleService)
	if !supported {
		utils.Error(c, http.StatusServiceUnavailable, "Session lifecycle lookup is unavailable")
		return
	}
	item, err := sessionLifecycle.GetOperationForSession(
		strings.TrimSpace(c.Param("operationID")),
		ieiOperationSessionID(session.SessionID),
	)
	if err != nil || item == nil {
		if err != nil {
			var apiErr *northbound.APIError
			if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
				h.lifecycleError(c, err, "Unable to load instance lifecycle status")
				return
			}
		}
		utils.Error(c, http.StatusNotFound, "Instance lifecycle operation not found")
		return
	}
	utils.Success(c, http.StatusOK, "Instance lifecycle operation retrieved", gin.H{"operation": newIEILifecycleOperationView(item)})
}

func (h *IEISystemHandler) submitLifecycle(c *gin.Context, session *services.IEISession, instance *models.Instance, action string) {
	if h.lifecycle == nil {
		utils.Error(c, http.StatusServiceUnavailable, "Instance lifecycle service is unavailable")
		return
	}
	mode, ok := services.NormalizeInstanceMode(instance.InstanceMode)
	if !ok {
		utils.Error(c, http.StatusConflict, "Instance lifecycle mode is invalid")
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" {
		idempotencyKey = ieiLifecycleIdempotencyKey(session.SessionID, instance.ID, action)
	}
	item, replayed, err := h.lifecycle.SubmitLifecycle(
		northbound.Principal{UserID: instance.UserID, SessionID: ieiOperationSessionID(session.SessionID)},
		idempotencyKey,
		instance.ID,
		mode,
		action,
	)
	if err != nil {
		h.lifecycleError(c, err, "Unable to submit instance lifecycle operation")
		return
	}
	if replayed {
		c.Header("Idempotent-Replayed", "true")
	}
	utils.Success(c, http.StatusAccepted, "Instance lifecycle operation submitted", gin.H{
		"operation": newIEILifecycleOperationView(item),
	})
}

func (h *IEISystemHandler) lifecycleError(c *gin.Context, err error, fallback string) {
	var apiErr *northbound.APIError
	if errors.As(err, &apiErr) {
		utils.Error(c, apiErr.Status, apiErr.Message)
		return
	}
	utils.Error(c, http.StatusServiceUnavailable, fallback)
}

func (h *IEISystemHandler) GenerateInstanceAccess(c *gin.Context) {
	h.noStore(c)
	session, ok := h.requireSession(c)
	if !ok {
		return
	}
	instance, ok := h.requireOwnedSupportedInstance(c, session.Email)
	if !ok {
		return
	}
	if instance.Status != "running" {
		utils.Error(c, http.StatusConflict, "Instance is not running")
		return
	}
	if h.lifecycle != nil {
		operation, err := h.lifecycle.GetLatestLifecycleOperation(instance.UserID, instance.ID)
		if err != nil {
			h.lifecycleError(c, err, "Unable to verify instance lifecycle status")
			return
		}
		if operation != nil && (operation.Status == "queued" || operation.Status == "processing" || operation.Status == "batch_pending") {
			utils.Error(c, http.StatusConflict, "Instance lifecycle operation is in progress")
			return
		}
	}
	if strings.EqualFold(strings.TrimSpace(instance.RuntimeType), "shell") {
		utils.Error(c, http.StatusBadRequest, "Desktop access is not available for shell runtime instances")
		return
	}
	if strings.EqualFold(strings.TrimSpace(instance.Type), services.RuntimeTypeHermes) &&
		strings.EqualFold(strings.TrimSpace(instance.RuntimeType), services.RuntimeBackendGateway) &&
		strings.EqualFold(strings.TrimSpace(instance.InstanceMode), services.InstanceModeLite) {
		if h.hermesDesktop == nil {
			utils.Error(c, http.StatusServiceUnavailable, "Hermes Desktop Web access is unavailable")
			return
		}
		descriptor, token, err := h.hermesDesktop.Activate(c.Request.Context(), instance.UserID, instance.ID)
		if err != nil {
			utils.Error(c, http.StatusServiceUnavailable, "Unable to activate Hermes Desktop Web")
			return
		}
		if descriptor == nil || !descriptor.Available || strings.TrimSpace(descriptor.RendererURL) == "" || strings.TrimSpace(token) == "" || descriptor.ExpiresAt == nil {
			utils.Error(c, http.StatusServiceUnavailable, "Unable to activate Hermes Desktop Web")
			return
		}
		setHermesSessionCookie(c, services.HermesDesktopCookieName(instance.ID), services.HermesDesktopBase(instance.ID)+"/", token, *descriptor.ExpiresAt)
		workspaceAvailable := instance.WorkspacePath != nil && strings.TrimSpace(*instance.WorkspacePath) != ""
		utils.Success(c, http.StatusOK, "IEI instance access granted", gin.H{
			"access_url":               descriptor.RendererURL,
			"expires_at":               descriptor.ExpiresAt,
			"desktop_proxy_mode":       "hermes_desktop_web",
			"desktop_upstream_present": false,
			"workspace_available":      workspaceAvailable,
			"workspace_root":           "Workspace",
		})
		return
	}
	if h.instanceHandler == nil || h.instanceHandler.accessService == nil || h.instanceHandler.proxyService == nil {
		utils.Error(c, http.StatusServiceUnavailable, "Instance access is unavailable")
		return
	}

	accessURL := h.instanceHandler.proxyService.GetProxyURLForInstance(instance, "")
	if strings.TrimSpace(accessURL) == "" {
		utils.Error(c, http.StatusServiceUnavailable, "Unable to generate access URL")
		return
	}
	targetPort := h.instanceHandler.proxyService.GetTargetPortForInstance(instance)
	// IEI-bound tokens always use the control-plane proxy. Same-origin requests
	// revalidate the HttpOnly IEI session; dedicated OpenCode/DSH origins use the
	// signed instance-scoped handoff capability because browsers cannot send the
	// management-origin IEI cookie there. The handoff cannot outlive the IEI
	// session expiry used below.
	upstream := ""
	directProxyEnabled := false
	duration := time.Until(session.ExpiresAt)
	if duration > time.Hour {
		duration = time.Hour
	}
	if duration <= time.Second {
		h.clearSessionCookie(c)
		utils.Error(c, http.StatusUnauthorized, "IEI system session is invalid or expired")
		return
	}
	accessToken, err := h.instanceHandler.accessService.GenerateBoundToken(
		instance.UserID,
		instance.ID,
		instance.Type,
		accessURL,
		upstream,
		targetPort,
		duration,
		ieiSystemSessionBinding(session.SessionID),
	)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	proxyURL := h.instanceHandler.proxyService.GetProxyURLForInstance(instance, accessToken.Token)
	browserURL := browserAccessEntryURL(accessURL, proxyURL)
	// OpenClaw Lite bootstraps its control UI through the shared proxy and can
	// issue early navigation/subresource requests before the path-scoped
	// instance cookie is available in every browser/proxy combination. Carry
	// the short-lived ClawManager capability on the first URL for this runtime
	// only. The proxy strips it before forwarding to OpenClaw and promotes it
	// to the existing HttpOnly cookie, while all other instance types retain
	// their established token-free same-origin entry behavior.
	if isOpenClawLiteInstance(instance) {
		browserURL = proxyURL
	}
	workspaceAvailable := isDesktopWorkspaceInstance(instance) ||
		(instance.WorkspacePath != nil && strings.TrimSpace(*instance.WorkspacePath) != "")
	workspaceRoot := "Workspace"
	if isDesktopWorkspaceInstance(instance) {
		workspaceRoot = "/config"
	}
	h.setInstanceAccessCookie(c, instance.ID, accessToken.Token, accessToken.ExpiresAt)
	utils.Success(c, http.StatusOK, "IEI instance access granted", gin.H{
		"access_url":               browserURL,
		"expires_at":               accessToken.ExpiresAt,
		"desktop_proxy_mode":       desktopProxyMode(directProxyEnabled, upstream),
		"desktop_upstream_present": upstream != "",
		"workspace_available":      workspaceAvailable,
		"workspace_root":           workspaceRoot,
	})
}

func (h *IEISystemHandler) ListWorkspace(c *gin.Context) {
	h.handleWorkspace(c, (*WorkspaceFileHandler).List)
}

func (h *IEISystemHandler) SearchWorkspace(c *gin.Context) {
	h.handleWorkspace(c, (*WorkspaceFileHandler).Search)
}

func (h *IEISystemHandler) PreviewWorkspace(c *gin.Context) {
	h.handleWorkspace(c, (*WorkspaceFileHandler).Preview)
}

func (h *IEISystemHandler) DownloadWorkspace(c *gin.Context) {
	h.handleWorkspace(c, (*WorkspaceFileHandler).Download)
}

func (h *IEISystemHandler) UploadWorkspace(c *gin.Context) {
	h.handleWorkspace(c, (*WorkspaceFileHandler).Upload)
}

func (h *IEISystemHandler) CreateWorkspaceFolder(c *gin.Context) {
	h.handleWorkspace(c, (*WorkspaceFileHandler).Mkdir)
}

func (h *IEISystemHandler) RenameWorkspaceEntry(c *gin.Context) {
	h.handleWorkspace(c, (*WorkspaceFileHandler).Rename)
}

func (h *IEISystemHandler) DeleteWorkspaceEntry(c *gin.Context) {
	h.handleWorkspace(c, (*WorkspaceFileHandler).Delete)
}

// handleWorkspace authenticates the dedicated IEI session and checks the
// instance owner before delegating to the existing workspace implementation.
// It deliberately does not create or validate a ShareLink session.
func (h *IEISystemHandler) handleWorkspace(c *gin.Context, next func(*WorkspaceFileHandler, *gin.Context)) {
	h.noStore(c)
	session, ok := h.requireSession(c)
	if !ok {
		return
	}
	instance, ok := h.requireOwnedSupportedInstance(c, session.Email)
	if !ok {
		return
	}
	if h.workspace == nil {
		utils.Error(c, http.StatusServiceUnavailable, "IEI workspace access is unavailable")
		return
	}
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		if instance.Status == "creating" || instance.Status == "deleting" {
			utils.Error(c, http.StatusConflict, "实例正在创建或删除，暂不可修改工作区。")
			return
		}
		if h.lifecycle != nil {
			op, err := h.lifecycle.GetLatestLifecycleOperation(instance.UserID, instance.ID)
			if err != nil {
				utils.HandleError(c, err)
				return
			}
			if op != nil && (op.Status == "queued" || op.Status == "processing" || op.Status == "batch_pending") {
				utils.Error(c, http.StatusConflict, "实例正在执行维护操作，完成后才能修改工作区。")
				return
			}
		}
	}

	// Pass only the instance that was authorized by the dedicated IEI owner
	// check. WorkspaceFileHandler uses this server-side context instead of the
	// normal JWT or ShareLink authentication paths.
	c.Set(ieiWorkspaceContextKey, instance)
	next(h.workspace, c)
}

func (h *IEISystemHandler) requireSession(c *gin.Context) (*services.IEISession, bool) {
	if h.sso == nil || !h.sso.Enabled() {
		utils.Error(c, http.StatusServiceUnavailable, "IEI system SSO is not configured")
		return nil, false
	}
	rawToken, err := c.Cookie(ieiSystemSessionCookie)
	if err != nil {
		utils.Error(c, http.StatusUnauthorized, "IEI system session is required")
		return nil, false
	}
	session, err := h.sso.ValidateSession(rawToken)
	if err != nil {
		h.clearSessionCookie(c)
		utils.Error(c, http.StatusUnauthorized, "IEI system session is invalid or expired")
		return nil, false
	}
	return session, true
}

func (h *IEISystemHandler) requireOwnedSupportedInstance(c *gin.Context, owner string) (*models.Instance, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return nil, false
	}
	instance, err := h.instances.GetByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return nil, false
	}
	// Deliberately collapse missing, unsupported and wrong-owner cases so this
	// public endpoint cannot be used to enumerate other owners' instances.
	if !h.ownedSupportedInstance(instance, owner) {
		utils.Error(c, http.StatusNotFound, "Instance not found")
		return nil, false
	}
	return instance, true
}

func (h *IEISystemHandler) ownedSupportedInstance(instance *models.Instance, owner string) bool {
	if instance == nil || instance.Owner == nil || !strings.EqualFold(strings.TrimSpace(*instance.Owner), strings.TrimSpace(owner)) {
		return false
	}
	mode, ok := services.NormalizeInstanceMode(instance.InstanceMode)
	if !ok {
		return false
	}
	typeName := strings.ToLower(strings.TrimSpace(instance.Type))
	if mode == services.InstanceModeLite {
		switch typeName {
		case services.RuntimeTypeOpenClaw, services.RuntimeTypeHermes, "opencode", "deepseek-harness":
			return true
		default:
			return false
		}
	}
	if mode != services.InstanceModePro {
		return false
	}
	switch typeName {
	case services.RuntimeTypeOpenClaw, services.RuntimeTypeHermes, services.RuntimeTypeOpenCode, services.RuntimeTypeDeepSeekHarness:
		return true
	case "workbuddy":
		return strings.EqualFold(strings.TrimSpace(instance.RuntimeVariant), services.WorkbuddyRuntimeLinux)
	default:
		return false
	}
}

func newIEIInstanceView(instance *models.Instance) ieiInstanceView {
	owner := ""
	if instance.Owner != nil {
		owner = *instance.Owner
	}
	return ieiInstanceView{
		WorkspaceAvailable: isDesktopWorkspaceInstance(instance) || (instance.WorkspacePath != nil && strings.TrimSpace(*instance.WorkspacePath) != ""),
		WorkspaceRoot: func() string {
			if isDesktopWorkspaceInstance(instance) {
				return "/config"
			}
			return "Workspace"
		}(),
		ID:             instance.ID,
		Owner:          owner,
		Name:           instance.Name,
		Alias:          instance.Alias,
		Description:    instance.Description,
		Type:           instance.Type,
		RuntimeType:    instance.RuntimeType,
		RuntimeVariant: instance.RuntimeVariant,
		InstanceMode:   instance.InstanceMode,
		Status:         instance.Status,
		CreatedAt:      instance.CreatedAt,
		UpdatedAt:      instance.UpdatedAt,
		StartedAt:      instance.StartedAt,
	}
}

func newIEILifecycleOperationView(item *models.NorthboundOperation) ieiLifecycleOperationView {
	action := "restart"
	if strings.HasSuffix(item.OperationType, "_reset") {
		action = "reset"
	}
	return ieiLifecycleOperationView{
		OperationID:  item.OperationID,
		Action:       action,
		Status:       item.Status,
		InstanceID:   item.InstanceID,
		ErrorCode:    item.ErrorCode,
		ErrorMessage: item.ErrorMessage,
		CreatedAt:    item.CreatedAt,
		StartedAt:    item.StartedAt,
		FinishedAt:   item.FinishedAt,
		UpdatedAt:    item.UpdatedAt,
	}
}

func ieiOperationSessionID(sessionID string) string {
	digest := sha256.Sum256([]byte("clawmanager-iei-operation:" + strings.TrimSpace(sessionID)))
	// northbound_operations.session_id is VARCHAR(64). Keep an explicit IEI
	// namespace while staying within that deployed schema.
	return "iei_" + hex.EncodeToString(digest[:])[:60]
}

func ieiLifecycleIdempotencyKey(sessionID string, instanceID int, action string) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s:%d", sessionID, instanceID, action, time.Now().UnixNano())))
	return "iei_" + hex.EncodeToString(digest[:])[:32]
}

func positiveQueryInt(c *gin.Context, name string, fallback int) int {
	value, err := strconv.Atoi(c.DefaultQuery(name, strconv.Itoa(fallback)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func (h *IEISystemHandler) setSessionCookie(c *gin.Context, token string, expiresAt time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     ieiSystemSessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   max(1, int(time.Until(expiresAt).Seconds())),
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *IEISystemHandler) clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     ieiSystemSessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *IEISystemHandler) setInstanceAccessCookie(c *gin.Context, instanceID int, token string, expiresAt time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     fmt.Sprintf("instance_access_%d", instanceID),
		Value:    token,
		Path:     fmt.Sprintf("/api/v1/instances/%d/proxy", instanceID),
		MaxAge:   max(1, int(time.Until(expiresAt).Seconds())),
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func isOpenClawLiteInstance(instance *models.Instance) bool {
	if instance == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(instance.Type), services.RuntimeTypeOpenClaw) &&
		strings.EqualFold(strings.TrimSpace(instance.RuntimeType), services.RuntimeBackendGateway) &&
		strings.EqualFold(strings.TrimSpace(instance.InstanceMode), services.InstanceModeLite)
}

func (h *IEISystemHandler) noStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Referrer-Policy", "no-referrer")
}

func ieiSystemSessionBinding(sessionID string) string {
	digest := sha256.Sum256([]byte("clawmanager-ieisystem:" + strings.TrimSpace(sessionID)))
	return ieiSystemSessionBindingPrefix + hex.EncodeToString(digest[:])
}
