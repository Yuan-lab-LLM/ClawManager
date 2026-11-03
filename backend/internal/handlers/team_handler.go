package handlers

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

type TeamHandler struct {
	teamService services.TeamService
}

func NewTeamHandler(teamService services.TeamService) *TeamHandler {
	return &TeamHandler{teamService: teamService}
}

// ResourceScopeGuard is applied to every legacy CM Team route, including files
// and history. Owner Teams use their separate authenticated adapter.
func (h *TeamHandler) ResourceScopeGuard(c *gin.Context) {
	if c.Param("id") == "" {
		c.Next()
		return
	}
	id, valid := parseTeamID(c)
	if !valid {
		c.Abort()
		return
	}
	userID, ok := c.Get("userID")
	checker, capable := h.teamService.(interface{ CheckCMTeam(int, int) error })
	if !ok || !capable {
		utils.Error(c, http.StatusNotFound, "Team not found")
		c.Abort()
		return
	}
	if err := checker.CheckCMTeam(userID.(int), id); err != nil {
		utils.HandleError(c, err)
		c.Abort()
		return
	}
	c.Next()
}

func (h *TeamHandler) CreateTeam(c *gin.Context) {
	userID, _ := c.Get("userID")
	var req services.CreateTeamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	team, err := h.teamService.CreateTeam(userID.(int), req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "Team created successfully", team)
}

func (h *TeamHandler) ListTeams(c *gin.Context) {
	userID, _ := c.Get("userID")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	teams, err := h.teamService.ListTeams(userID.(int), (page-1)*limit, limit)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Teams retrieved successfully", gin.H{
		"teams": teams.Teams,
		"total": teams.Total,
		"page":  page,
		"limit": limit,
	})
}

func (h *TeamHandler) GetTeam(c *gin.Context) {
	userID, _ := c.Get("userID")
	teamID, ok := parseTeamID(c)
	if !ok {
		return
	}
	team, err := h.teamService.GetTeam(userID.(int), teamID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Team retrieved successfully", team)
}

func (h *TeamHandler) ListTasks(c *gin.Context) {
	userID, _ := c.Get("userID")
	teamID, ok := parseTeamID(c)
	if !ok {
		return
	}
	tasks, err := h.teamService.ListTeamTasks(
		userID.(int),
		teamID,
		parseOptionalIntQuery(c, "before_id"),
		parseOptionalIntQuery(c, "limit"),
	)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Team tasks retrieved successfully", tasks)
}

func (h *TeamHandler) ListEvents(c *gin.Context) {
	userID, _ := c.Get("userID")
	teamID, ok := parseTeamID(c)
	if !ok {
		return
	}
	events, err := h.teamService.ListTeamEvents(
		userID.(int),
		teamID,
		parseOptionalIntQuery(c, "before_id"),
		parseOptionalIntQuery(c, "limit"),
	)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Team events retrieved successfully", events)
}

func (h *TeamHandler) ListWorkspaceFiles(c *gin.Context) {
	userID, _ := c.Get("userID")
	teamID, ok := parseTeamID(c)
	if !ok {
		return
	}
	payload, err := h.teamService.ListWorkspaceFiles(c.Request.Context(), userID.(int), teamID, c.Query("path"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Team workspace files retrieved successfully", payload)
}

func (h *TeamHandler) PreviewWorkspaceFile(c *gin.Context) {
	userID, _ := c.Get("userID")
	teamID, ok := parseTeamID(c)
	if !ok {
		return
	}
	payload, err := h.teamService.PreviewWorkspaceFile(c.Request.Context(), userID.(int), teamID, c.Query("path"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Team workspace file preview retrieved successfully", payload)
}

func (h *TeamHandler) DownloadWorkspaceFile(c *gin.Context) {
	userID, _ := c.Get("userID")
	teamID, ok := parseTeamID(c)
	if !ok {
		return
	}
	payload, err := h.teamService.DownloadWorkspaceFile(c.Request.Context(), userID.(int), teamID, c.Query("path"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(payload.Name, `"`, "")+`"`)
	c.Data(http.StatusOK, payload.ContentType, payload.Data)
}

func (h *TeamHandler) CreateWorkspaceFolder(c *gin.Context) {
	userID, _ := c.Get("userID")
	teamID, ok := parseTeamID(c)
	if !ok {
		return
	}
	var req services.TeamWorkspaceFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	if err := h.teamService.CreateWorkspaceFolder(c.Request.Context(), userID.(int), teamID, req); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "Team workspace folder created successfully", nil)
}

func (h *TeamHandler) RenameWorkspaceEntry(c *gin.Context) {
	userID, _ := c.Get("userID")
	teamID, ok := parseTeamID(c)
	if !ok {
		return
	}
	var req services.TeamWorkspaceRenameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	if err := h.teamService.RenameWorkspaceEntry(c.Request.Context(), userID.(int), teamID, req); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Team workspace entry renamed successfully", nil)
}

func (h *TeamHandler) DeleteWorkspaceEntry(c *gin.Context) {
	userID, _ := c.Get("userID")
	teamID, ok := parseTeamID(c)
	if !ok {
		return
	}
	if err := h.teamService.DeleteWorkspaceEntry(c.Request.Context(), userID.(int), teamID, c.Query("path")); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Team workspace entry deleted successfully", nil)
}

func (h *TeamHandler) UploadWorkspaceFiles(c *gin.Context) {
	userID, _ := c.Get("userID")
	teamID, ok := parseTeamID(c)
	if !ok {
		return
	}
	form, err := c.MultipartForm()
	if err != nil {
		utils.ValidationError(c, err)
		return
	}
	files := form.File["files"]
	relativePaths := form.Value["relative_paths"]
	if err := h.teamService.UploadWorkspaceFiles(c.Request.Context(), userID.(int), teamID, c.PostForm("path"), files, relativePaths); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "Team workspace files uploaded successfully", nil)
}

func (h *TeamHandler) DispatchTask(c *gin.Context) {
	userID, _ := c.Get("userID")
	teamID, ok := parseTeamID(c)
	if !ok {
		return
	}

	var req services.DispatchTeamTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil && err != io.EOF {
		utils.ValidationError(c, err)
		return
	}
	task, err := h.teamService.DispatchTask(userID.(int), teamID, req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "Team task dispatched successfully", task)
}

func (h *TeamHandler) DeleteTeam(c *gin.Context) {
	userID, _ := c.Get("userID")
	teamID, ok := parseTeamID(c)
	if !ok {
		return
	}
	if err := h.teamService.DeleteTeam(userID.(int), teamID); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Team deleted successfully", gin.H{"id": teamID})
}

func (h *TeamHandler) DeleteMember(c *gin.Context) {
	userID, _ := c.Get("userID")
	teamID, ok := parseTeamID(c)
	if !ok {
		return
	}
	memberID := c.Param("memberID")
	if err := h.teamService.DeleteMember(userID.(int), teamID, memberID); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Team member deleted successfully", gin.H{"member_id": memberID})
}

func parseTeamID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid Team ID")
		return 0, false
	}
	return id, true
}

func parseOptionalIntQuery(c *gin.Context, key string) int {
	raw := c.Query(key)
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0
	}
	return value
}
