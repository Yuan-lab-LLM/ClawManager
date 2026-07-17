package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

type AgentHandler struct {
	agentService          services.InstanceAgentService
	commandService        services.InstanceCommandService
	runtimeStatusService  services.InstanceRuntimeStatusService
	configRevisionService services.InstanceConfigRevisionService
	skillService          services.SkillService
}

func NewAgentHandler(agentService services.InstanceAgentService, commandService services.InstanceCommandService, runtimeStatusService services.InstanceRuntimeStatusService, configRevisionService services.InstanceConfigRevisionService, skillService services.SkillService) *AgentHandler {
	return &AgentHandler{
		agentService:          agentService,
		commandService:        commandService,
		runtimeStatusService:  runtimeStatusService,
		configRevisionService: configRevisionService,
		skillService:          skillService,
	}
}

func (h *AgentHandler) Register(c *gin.Context) {
	bootstrapToken := extractBearerToken(c.GetHeader("Authorization"))
	if bootstrapToken == "" {
		utils.Error(c, http.StatusUnauthorized, "Agent bootstrap token is required")
		return
	}

	var req services.AgentRegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	resp, err := h.agentService.Register(bootstrapToken, req, c.ClientIP())
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Agent registered successfully", resp)
}

func (h *AgentHandler) Heartbeat(c *gin.Context) {
	session, ok := h.authenticateAgentSession(c)
	if !ok {
		return
	}

	var req services.AgentHeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	if req.Timestamp.IsZero() {
		req.Timestamp = time.Now().UTC()
	}

	resp, err := h.agentService.Heartbeat(session, req, c.ClientIP())
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Agent heartbeat accepted", resp)
}

func (h *AgentHandler) NextCommand(c *gin.Context) {
	session, ok := h.authenticateAgentSession(c)
	if !ok {
		return
	}

	command, err := h.commandService.GetNextForAgent(session)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Agent next command retrieved successfully", gin.H{"command": command})
}

func (h *AgentHandler) StartCommand(c *gin.Context) {
	session, ok := h.authenticateAgentSession(c)
	if !ok {
		return
	}
	commandID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid command ID")
		return
	}
	var req struct {
		AgentID   string     `json:"agent_id" binding:"required"`
		StartedAt *time.Time `json:"started_at,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	if req.AgentID != session.Agent.AgentID {
		utils.Error(c, http.StatusForbidden, "Agent ID does not match session")
		return
	}
	if err := h.commandService.MarkStarted(session, commandID, req.StartedAt); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Agent command marked as started", nil)
}

func (h *AgentHandler) FinishCommand(c *gin.Context) {
	session, ok := h.authenticateAgentSession(c)
	if !ok {
		return
	}
	commandID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid command ID")
		return
	}

	var req services.AgentCommandFinishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	if err := h.commandService.MarkFinished(session, commandID, req); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Agent command result accepted", nil)
}

func (h *AgentHandler) ReportState(c *gin.Context) {
	session, ok := h.authenticateAgentSession(c)
	if !ok {
		return
	}

	var req services.AgentStateReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	if err := h.runtimeStatusService.Report(session, req, c.ClientIP()); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Agent state reported successfully", nil)
}

func (h *AgentHandler) GetConfigRevision(c *gin.Context) {
	session, ok := h.authenticateAgentSession(c)
	if !ok {
		return
	}
	revisionID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid config revision ID")
		return
	}
	revision, err := h.configRevisionService.GetByID(revisionID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	if revision.InstanceID != session.Instance.ID {
		utils.Error(c, http.StatusForbidden, "Access denied")
		return
	}
	utils.Success(c, http.StatusOK, "Config revision retrieved successfully", gin.H{"revision": revision})
}

func (h *AgentHandler) ReportSkillInventory(c *gin.Context) {
	session, ok := h.authenticateAgentSession(c)
	if !ok {
		return
	}
	var req services.AgentSkillInventoryReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	if strings.TrimSpace(req.AgentID) != "" && strings.TrimSpace(req.AgentID) != session.Agent.AgentID {
		utils.Error(c, http.StatusForbidden, "Agent ID does not match session")
		return
	}
	if err := h.skillService.SyncAgentSkills(session.Instance.ID, req); err != nil {
		utils.HandleError(c, err)
		return
	}
	h.skillService.CompletePendingSkillInventorySync(session.Instance.ID)
	utils.Success(c, http.StatusOK, "Agent skill inventory reported successfully", nil)
}

func (h *AgentHandler) UploadSkillPackage(c *gin.Context) {
	session, ok := h.authenticateAgentSession(c)
	if !ok {
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "file is required")
		return
	}
	req := services.AgentSkillPackageUploadRequest{
		AgentID:      strings.TrimSpace(c.PostForm("agent_id")),
		SkillID:      strings.TrimSpace(c.PostForm("skill_id")),
		SkillVersion: strings.TrimSpace(c.PostForm("skill_version")),
		Identifier:   strings.TrimSpace(c.PostForm("identifier")),
		ContentMD5:   strings.TrimSpace(c.PostForm("content_md5")),
		Source:       strings.TrimSpace(c.PostForm("source")),
	}
	if req.AgentID != "" && req.AgentID != session.Agent.AgentID {
		utils.Error(c, http.StatusForbidden, "Agent ID does not match session")
		return
	}
	item, err := h.skillService.UploadAgentSkillPackage(c.Request.Context(), session.Instance.ID, req, fileHeader)
	if err != nil {
		utils.HandleHubError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "Agent skill package uploaded successfully", item)
}

func (h *AgentHandler) DownloadSkillVersion(c *gin.Context) {
	if _, ok := h.authenticateAgentSession(c); !ok {
		return
	}
	content, fileName, err := h.skillService.DownloadSkillVersionByExternalID(c.Param("skillVersion"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileName))
	c.Data(http.StatusOK, "application/octet-stream", content)
}

func (h *AgentHandler) authenticateAgentSession(c *gin.Context) (*services.AgentSession, bool) {
	sessionToken := extractBearerToken(c.GetHeader("Authorization"))
	if sessionToken == "" {
		utils.Error(c, http.StatusUnauthorized, "Agent session token is required")
		return nil, false
	}
	session, err := h.agentService.AuthenticateSession(sessionToken)
	if err != nil {
		utils.HandleError(c, err)
		return nil, false
	}
	return session, true
}

func extractBearerToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parts := strings.SplitN(raw, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
