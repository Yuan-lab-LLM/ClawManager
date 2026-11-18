package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"clawreef/internal/services"
	"clawreef/internal/utils"
	"clawreef/internal/workbench"
	"github.com/gin-gonic/gin"
)

type instanceWorkbench interface {
	Capabilities(context.Context, int, bool, int) (map[string]any, error)
	Execute(context.Context, int, bool, int, int, string, json.RawMessage) (json.RawMessage, error)
	CreatePreview(context.Context, int, bool, int, int, string) (map[string]any, error)
}
type InstanceWorkbenchHandler struct {
	service         instanceWorkbench
	sharedInstances sharedWorkspaceInstances
	sharedExternal  services.InstanceExternalAccessService
	sharedTokens    *services.InstanceAccessService
}

const sharedWorkbenchSessionKey = "validated-shared-workbench-session"

func NewSharedInstanceWorkbenchHandler(service instanceWorkbench, instances sharedWorkspaceInstances, external services.InstanceExternalAccessService, tokens *services.InstanceAccessService) *InstanceWorkbenchHandler {
	return &InstanceWorkbenchHandler{service: service, sharedInstances: instances, sharedExternal: external, sharedTokens: tokens}
}
func (h *InstanceWorkbenchHandler) identity(c *gin.Context, requireWrite bool) (userID, instanceID int, admin, ok bool) {
	if h.sharedInstances == nil {
		return workbenchIdentity(c)
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	session, valid := resolveSharedWorkspaceSession(c, h.sharedInstances, h.sharedExternal, h.sharedTokens, requireWrite)
	if !valid {
		return
	}
	c.Set(sharedWorkbenchSessionKey, session)
	return session.instance.UserID, session.instance.ID, false, true
}
func sharedWorkbenchSession(c *gin.Context) *sharedWorkspaceSession {
	value, _ := c.Get(sharedWorkbenchSessionKey)
	session, _ := value.(*sharedWorkspaceSession)
	return session
}
func sharedReadGit(data map[string]json.RawMessage) bool {
	var operation string
	if json.Unmarshal(data["operation"], &operation) != nil {
		return false
	}
	switch operation {
	case "projects", "status", "diff", "branches", "history", "identity":
		return true
	}
	return false
}

func NewInstanceWorkbenchHandler(service instanceWorkbench) *InstanceWorkbenchHandler {
	return &InstanceWorkbenchHandler{service: service}
}
func (h *InstanceWorkbenchHandler) Register(group *gin.RouterGroup) {
	group.GET("/capabilities", h.capabilities)
	group.POST("/git", h.operation("git"))
	group.GET("/terminals", h.operation("terminal.list"))
	group.POST("/terminals", h.operation("terminal.create"))
	group.GET("/terminals/:session", h.operation("terminal.get"))
	group.DELETE("/terminals/:session", h.operation("terminal.close"))
	group.GET("/terminals/:session/output", h.operation("terminal.read"))
	group.POST("/terminals/:session/input", h.operation("terminal.write"))
	group.POST("/terminals/:session/resize", h.operation("terminal.resize"))
	group.POST("/previews", h.preview)
	group.GET("/processes", h.operation("process.list"))
	group.POST("/processes/:process/stop", h.operation("process.stop"))
	group.POST("/processes/:process/restart", h.operation("process.restart"))
}
func workbenchIdentity(c *gin.Context) (userID, instanceID int, admin, ok bool) {
	value, exists := c.Get("userID")
	userID, _ = value.(int)
	if !exists || userID <= 0 {
		utils.Error(c, http.StatusUnauthorized, "authentication required")
		return
	}
	instanceID, err := strconv.Atoi(c.Param("id"))
	if err != nil || instanceID <= 0 {
		utils.Error(c, http.StatusBadRequest, "invalid instance")
		return
	}
	role, _ := c.Get("userRole")
	admin = role == "admin"
	ok = true
	return
}
func workbenchFailure(c *gin.Context, err error) {
	if browserOriginCapacityFailure(c, err) {
		return
	}
	status := http.StatusServiceUnavailable
	code := "workbench_unavailable"
	message := "实例环境暂不可用，请刷新后重试"
	switch {
	case errors.Is(err, services.ErrWorkbenchDenied):
		status = 403
		code = "access_denied"
		message = "没有访问此实例的权限"
	case errors.Is(err, services.ErrWorkbenchStale):
		status = 409
		code = "stale_generation"
		message = "实例运行环境已变更，请刷新工作台"
	case errors.Is(err, services.ErrWorkbenchUnavailable):
		message = "请先启动实例，再使用工作台"
	case errors.Is(err, services.ErrRuntimeAgentUnsupported):
		status = 501
		code = "upgrade_required"
		message = "当前运行镜像不支持工作台，请升级运行镜像"
	case errors.Is(err, workbench.ErrInvalidPreviewURL):
		status = 400
		code = "invalid_url"
		message = "请输入实例内 localhost 的 HTTP 或 HTTPS 地址"
	case errors.Is(err, workbench.ErrPreviewLimit):
		status = 429
		code = "preview_limit"
		message = "预览数量已达上限，请稍后再试"
	default:
		var operation *services.WorkbenchOperationError
		if errors.As(err, &operation) {
			status = 400
			code = operation.Code
			message = operation.Message
			switch operation.Code {
			case "instance_busy", "stop_unconfirmed":
				status = http.StatusLocked
			case "cancelled":
				status = http.StatusConflict
			case "timeout":
				status = http.StatusGatewayTimeout
			}
		}
	}
	c.JSON(status, gin.H{"success": false, "error": message, "error_code": code})
}
func workbenchGeneration(c *gin.Context) (int, bool) {
	n, err := strconv.Atoi(c.Query("generation"))
	if err != nil || n <= 0 {
		workbenchFailure(c, services.ErrWorkbenchStale)
		return 0, false
	}
	return n, true
}
func workbenchBody(c *gin.Context) (map[string]json.RawMessage, bool) {
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodDelete {
		return map[string]json.RawMessage{}, true
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	data := map[string]json.RawMessage{}
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(&data); err != nil || data == nil {
		utils.Error(c, 400, "invalid JSON request")
		return nil, false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		utils.Error(c, 400, "invalid JSON request")
		return nil, false
	}
	return data, true
}
func (h *InstanceWorkbenchHandler) capabilities(c *gin.Context) {
	user, id, admin, ok := h.identity(c, false)
	if !ok {
		return
	}
	result, err := h.service.Capabilities(c.Request.Context(), user, admin, id)
	if err != nil {
		workbenchFailure(c, err)
		return
	}
	if session := sharedWorkbenchSession(c); session != nil && session.preview.WorkspaceAccess != services.ExternalWorkspaceAccessWrite {
		result["terminal"] = false
		result["processes"] = false
	}
	utils.Success(c, 200, "", result)
}
func (h *InstanceWorkbenchHandler) operation(operation string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, id, admin, ok := h.identity(c, operation != "git")
		if !ok {
			return
		}
		generation, ok := workbenchGeneration(c)
		if !ok {
			return
		}
		data, ok := workbenchBody(c)
		if !ok {
			return
		}
		if session := sharedWorkbenchSession(c); session != nil && session.preview.WorkspaceAccess != services.ExternalWorkspaceAccessWrite && !sharedReadGit(data) {
			utils.Error(c, http.StatusForbidden, "This share link has read-only workspace access")
			return
		}
		for _, key := range []string{"session", "process"} {
			if value := c.Param(key); value != "" {
				if len(value) > 128 || strings.ContainsAny(value, "/\\\x00") {
					utils.Error(c, 400, "invalid session")
					return
				}
				data[key], _ = json.Marshal(value)
			}
		}
		if operation == "terminal.read" {
			cursor, err := strconv.ParseUint(c.DefaultQuery("cursor", "0"), 10, 64)
			if err != nil {
				utils.Error(c, 400, "invalid output cursor")
				return
			}
			data["cursor"], _ = json.Marshal(cursor)
		}
		payload, _ := json.Marshal(data)
		result, err := h.service.Execute(c.Request.Context(), user, admin, id, generation, operation, payload)
		if err != nil {
			workbenchFailure(c, err)
			return
		}
		utils.Success(c, 200, "", result)
	}
}
func (h *InstanceWorkbenchHandler) preview(c *gin.Context) {
	user, id, admin, ok := h.identity(c, false)
	if !ok {
		return
	}
	generation, ok := workbenchGeneration(c)
	if !ok {
		return
	}
	body, ok := workbenchBody(c)
	if !ok {
		return
	}
	var address string
	if json.Unmarshal(body["url"], &address) != nil || len(address) > 8192 {
		workbenchFailure(c, workbench.ErrInvalidPreviewURL)
		return
	}
	var result map[string]any
	var err error
	if session := sharedWorkbenchSession(c); session != nil {
		creator, supported := h.service.(interface {
			CreateSharedPreview(context.Context, int, int, int, string, workbench.SharedPreviewAccess) (map[string]any, error)
		})
		if !supported {
			workbenchFailure(c, services.ErrWorkbenchDenied)
			return
		}
		result, err = creator.CreateSharedPreview(c.Request.Context(), user, id, generation, address, session.preview)
	} else {
		result, err = h.service.CreatePreview(c.Request.Context(), user, admin, id, generation, address)
	}
	if err != nil {
		workbenchFailure(c, err)
		return
	}
	utils.Success(c, 200, "", result)
}
