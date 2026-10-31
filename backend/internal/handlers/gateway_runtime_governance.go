package handlers

import (
	"clawreef/internal/aigateway"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/utils"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"strings"
)

// attachRuntimeTask accepts exact runtime turn identity only. Neither the
// accounting fallback "main" nor a caller-supplied user/instance is sufficient.
func attachRuntimeTask(c *gin.Context, req *aigateway.ChatCompletionRequest) bool {
	taskID := c.GetHeader("X-ClawManager-Task-ID")
	if taskID == "" {
		if c.GetInt("runtimeGovernanceProtocol") > 0 {
			utils.Error(c, http.StatusConflict, "Runtime task identity required; this instance cannot fall back to legacy mode")
			return false
		}
		if c.GetHeader("X-ClawManager-Session-ID") != "" || c.GetHeader("X-ClawManager-Runtime-Generation") != "" {
			utils.Error(c, http.StatusBadRequest, "Incomplete runtime task identity")
			return false
		}
		return true
	}
	generation, err := strconv.Atoi(c.GetHeader("X-ClawManager-Runtime-Generation"))
	if err != nil {
		utils.Error(c, 400, "Invalid runtime generation")
		return false
	}
	t := models.GatewayTaskIdentity{SessionID: c.GetHeader("X-ClawManager-Session-ID"), TaskID: taskID, Generation: generation}
	if !authenticateRuntimeTask(c, &t) {
		return false
	}
	req.RuntimeTask = &t
	req.OpenClawSessionKey = &t.SessionID
	req.SessionID = &t.SessionID
	return true
}

func authenticateRuntimeTask(c *gin.Context, t *models.GatewayTaskIdentity) bool {
	t.UserID, _ = gatewayUserID(c)
	t.InstanceID = c.GetInt("instanceID")
	actual, exists := c.Get("runtimeGeneration")
	generation, ok := actual.(int)
	if !exists || !ok || t.InstanceID <= 0 || t.Generation != generation {
		utils.HandleError(c, models.ErrModelUnavailable)
		return false
	}
	if _, err := repository.GatewayTaskKey(*t); err != nil {
		utils.Error(c, 400, "Invalid runtime task identity")
		return false
	}
	return true
}

type GatewayRuntimeHandler struct {
	tasks   repository.GatewayTaskRepository
	gateway aigateway.Service
	audit   services.AuditEventService
}

func NewGatewayRuntimeHandler(tasks repository.GatewayTaskRepository, gateway aigateway.Service, audit services.AuditEventService) *GatewayRuntimeHandler {
	return &GatewayRuntimeHandler{tasks: tasks, gateway: gateway, audit: audit}
}
func (h *GatewayRuntimeHandler) CheckTask(c *gin.Context) {
	var task models.GatewayTaskIdentity
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if err := c.ShouldBindJSON(&task); err != nil {
		utils.Error(c, 400, "Invalid runtime task")
		return
	}
	if !authenticateRuntimeTask(c, &task) {
		return
	}
	state, err := h.tasks.Check(c.Request.Context(), task)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	if state.Revoked || state.Finished {
		utils.HandleError(c, models.ErrModelUnavailable)
		return
	}
	c.JSON(200, state)
}
func (h *GatewayRuntimeHandler) FinishTask(c *gin.Context) {
	var body struct {
		models.GatewayTaskIdentity
		Cancelled bool `json:"cancelled"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if err := c.ShouldBindJSON(&body); err != nil {
		utils.Error(c, 400, "Invalid runtime task receipt")
		return
	}
	if !authenticateRuntimeTask(c, &body.GatewayTaskIdentity) {
		return
	}
	state, err := h.tasks.Check(c.Request.Context(), body.GatewayTaskIdentity)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	if err := h.tasks.Finish(c.Request.Context(), body.GatewayTaskIdentity, body.Cancelled); err != nil {
		utils.HandleError(c, err)
		return
	}
	if body.Cancelled && state.Revoked && !state.RuntimeAcknowledged && h.audit != nil {
		taskKey, _ := repository.GatewayTaskKey(body.GatewayTaskIdentity)
		detailsJSON, _ := json.Marshal(gin.H{"task_id": body.TaskID, "session_id": body.SessionID, "generation": body.Generation, "runtime_acknowledged": true})
		details := string(detailsJSON)
		sessionID := utils.NormalizeOpenClawSessionID(body.SessionID, c.GetString("instanceType"))
		var auditSession *string
		if len(sessionID) <= 100 {
			auditSession = &sessionID
		}
		_ = h.audit.RecordEvent(&models.AuditEvent{TraceID: "cm-task-" + taskKey, InstanceID: &body.InstanceID, UserID: &body.UserID, SessionID: auditSession,
			Details:   &details,
			EventType: "gateway.model_access.runtime_acknowledged", TrafficClass: models.TrafficClassLLM, Severity: models.AuditSeverityWarn,
			Message: "Runtime acknowledged that the affected turn stopped; completed tool side effects were not rolled back."})
	}
	c.JSON(200, gin.H{"accepted": true})
}
func (h *GatewayRuntimeHandler) Catalog(c *gin.Context) {
	userID, ok := gatewayUserID(c)
	if !ok || c.GetInt("instanceID") <= 0 {
		utils.Error(c, 403, "Instance credential required")
		return
	}
	lister, ok := h.gateway.(interface {
		RuntimeCatalog(int, []string) ([]models.RuntimeCatalogModel, error)
	})
	if !ok {
		utils.Error(c, 503, "Runtime catalog unavailable")
		return
	}
	var groups []string
	if value, exists := c.Get("gatewayAllowedModelGroups"); exists {
		groups, _ = value.([]string)
	}
	items, err := lister.RuntimeCatalog(userID, groups)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	raw, _ := json.Marshal(items)
	hash := sha256.Sum256(raw)
	revision := hex.EncodeToString(hash[:])
	c.Header("Cache-Control", "no-store")
	c.Header("ETag", `"`+revision+`"`)
	if strings.Trim(c.GetHeader("If-None-Match"), `"`) == revision {
		c.Status(304)
		return
	}
	c.JSON(200, gin.H{"revision": revision, "models": items, "generation": c.GetInt("runtimeGeneration")})
}
