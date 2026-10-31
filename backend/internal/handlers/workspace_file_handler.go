package handlers

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

type WorkspaceFileHandler struct {
	instanceService             services.InstanceService
	fileService                 services.WorkspaceFileService
	runtimeWorkspaceFileService services.WorkspaceFileService
	externalAccessService       services.InstanceExternalAccessService
	instanceAccessService       *services.InstanceAccessService
	skillRepo                   repository.SkillRepository
}

const (
	sharedWorkspaceContextKey = "shared-instance-workspace"
	sharedWorkspaceCSRFHeader = "X-ClawManager-Share-CSRF"
	ieiWorkspaceContextKey    = "iei-instance-workspace"
)

type createWorkspaceFolderRequest struct {
	Path string `json:"path" binding:"required"`
}

type renameWorkspaceEntryRequest struct {
	OldPath string `json:"old_path" binding:"required"`
	NewPath string `json:"new_path" binding:"required"`
}

func NewWorkspaceFileHandler(instanceService services.InstanceService, fileService services.WorkspaceFileService, runtimeWorkspaceFileService ...services.WorkspaceFileService) *WorkspaceFileHandler {
	runtimeFileService := fileService
	if len(runtimeWorkspaceFileService) > 0 && runtimeWorkspaceFileService[0] != nil {
		runtimeFileService = runtimeWorkspaceFileService[0]
	}
	return &WorkspaceFileHandler{
		instanceService:             instanceService,
		fileService:                 fileService,
		runtimeWorkspaceFileService: runtimeFileService,
	}
}

func (h *WorkspaceFileHandler) SetSkillRepository(skillRepo repository.SkillRepository) {
	h.skillRepo = skillRepo
}

func (h *WorkspaceFileHandler) SetExternalAccessServices(externalAccessService services.InstanceExternalAccessService, instanceAccessService *services.InstanceAccessService) {
	h.externalAccessService = externalAccessService
	h.instanceAccessService = instanceAccessService
}

func (h *WorkspaceFileHandler) SharedList(c *gin.Context) {
	h.markSharedRequest(c)
	h.List(c)
}

func (h *WorkspaceFileHandler) SharedPreview(c *gin.Context) {
	h.markSharedRequest(c)
	h.Preview(c)
}

func (h *WorkspaceFileHandler) SharedDownload(c *gin.Context) {
	h.markSharedRequest(c)
	h.Download(c)
}

func (h *WorkspaceFileHandler) SharedUpload(c *gin.Context) {
	h.markSharedRequest(c)
	h.Upload(c)
}

func (h *WorkspaceFileHandler) SharedMkdir(c *gin.Context) {
	h.markSharedRequest(c)
	h.Mkdir(c)
}

func (h *WorkspaceFileHandler) SharedRename(c *gin.Context) {
	h.markSharedRequest(c)
	h.Rename(c)
}

func (h *WorkspaceFileHandler) SharedDelete(c *gin.Context) {
	h.markSharedRequest(c)
	h.Delete(c)
}

func (h *WorkspaceFileHandler) markSharedRequest(c *gin.Context) {
	c.Set(sharedWorkspaceContextKey, true)
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
}

func (h *WorkspaceFileHandler) List(c *gin.Context) {
	_, service, scope, ok := h.workspaceScope(c)
	if !ok {
		return
	}
	entries, err := service.List(c.Request.Context(), scope, c.Query("path"))
	if err != nil {
		handleWorkspaceFileError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Workspace files retrieved successfully", gin.H{"entries": entries})
}

func (h *WorkspaceFileHandler) SharedSearch(c *gin.Context) {
	h.markSharedRequest(c)
	h.Search(c)
}

func (h *WorkspaceFileHandler) Search(c *gin.Context) {
	_, service, scope, ok := h.workspaceScope(c)
	if !ok {
		return
	}
	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil {
		limit = 0
	}
	limit = services.NormalizeWorkspaceSearchLimit(limit)
	entries, err := service.Search(c.Request.Context(), scope, c.Query("q"), limit)
	if err != nil {
		handleWorkspaceFileError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Workspace search completed", gin.H{"entries": entries, "limit": limit})
}

func (h *WorkspaceFileHandler) Preview(c *gin.Context) {
	_, service, scope, ok := h.workspaceScope(c)
	if !ok {
		return
	}

	if strings.EqualFold(strings.TrimSpace(c.Query("raw")), "1") {
		file, contentType, size, err := service.OpenPreview(c.Request.Context(), scope, c.Query("path"))
		if err != nil {
			handleWorkspaceFileError(c, err)
			return
		}
		defer file.Close()
		filename := safeWorkspaceDownloadName(filepath.Base(c.Query("path")))
		streamWorkspaceFile(c, file, filename, contentType, "inline", size)
		return
	}

	preview, err := service.Preview(c.Request.Context(), scope, c.Query("path"))
	if err != nil {
		handleWorkspaceFileError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Workspace preview retrieved successfully", gin.H{"preview": preview})
}

func (h *WorkspaceFileHandler) Download(c *gin.Context) {
	_, service, scope, ok := h.workspaceScope(c)
	if !ok {
		return
	}
	file, filename, size, err := service.OpenDownload(c.Request.Context(), scope, c.Query("path"))
	if err != nil {
		handleWorkspaceFileError(c, err)
		return
	}
	defer file.Close()

	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	streamWorkspaceFile(c, file, filename, contentType, "attachment", size)
}

func (h *WorkspaceFileHandler) Upload(c *gin.Context) {
	_, service, scope, ok := h.workspaceScope(c)
	if !ok {
		return
	}

	maxBytes := services.WorkspaceUploadMaxBytes()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes+(1<<20))
	fileHeader, err := c.FormFile("file")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			utils.Error(c, http.StatusRequestEntityTooLarge, "workspace upload exceeds maximum size")
			return
		}
		utils.Error(c, http.StatusBadRequest, "file is required")
		return
	}
	if fileHeader.Size > maxBytes {
		utils.Error(c, http.StatusRequestEntityTooLarge, "workspace upload exceeds maximum size")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		handleWorkspaceFileError(c, err)
		return
	}
	defer file.Close()

	entry, err := service.Upload(c.Request.Context(), scope, c.Query("path"), fileHeader.Filename, file, fileHeader.Size)
	if err != nil {
		handleWorkspaceFileError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "Workspace file uploaded successfully", entry)
}

func (h *WorkspaceFileHandler) Mkdir(c *gin.Context) {
	_, service, scope, ok := h.workspaceScope(c)
	if !ok {
		return
	}
	var req createWorkspaceFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	entry, err := service.Mkdir(c.Request.Context(), scope, req.Path)
	if err != nil {
		handleWorkspaceFileError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "Workspace folder created successfully", entry)
}

func (h *WorkspaceFileHandler) Rename(c *gin.Context) {
	_, service, scope, ok := h.workspaceScope(c)
	if !ok {
		return
	}
	var req renameWorkspaceEntryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	entry, err := service.Rename(c.Request.Context(), scope, req.OldPath, req.NewPath)
	if err != nil {
		handleWorkspaceFileError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Workspace entry renamed successfully", entry)
}

func (h *WorkspaceFileHandler) Delete(c *gin.Context) {
	instance, service, scope, ok := h.workspaceScope(c)
	if !ok {
		return
	}
	path := c.Query("path")
	if err := service.Delete(c.Request.Context(), scope, path); err != nil {
		handleWorkspaceFileError(c, err)
		return
	}
	if err := h.syncSkillDeletionFromWorkspacePath(instance.ID, path); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Workspace entry deleted successfully", nil)
}

func (h *WorkspaceFileHandler) syncSkillDeletionFromWorkspacePath(instanceID int, path string) error {
	if h.skillRepo == nil {
		return nil
	}
	return h.skillRepo.MarkInstanceSkillsRemovedByWorkspacePath(instanceID, path, time.Now().UTC())
}

func (h *WorkspaceFileHandler) workspaceScope(c *gin.Context) (*models.Instance, services.WorkspaceFileService, services.WorkspaceFileScope, bool) {
	if shared, _ := c.Get(sharedWorkspaceContextKey); shared == true {
		return h.sharedWorkspaceScope(c)
	}
	if rawInstance, exists := c.Get(ieiWorkspaceContextKey); exists {
		instance, ok := rawInstance.(*models.Instance)
		if !ok || instance == nil {
			utils.Error(c, http.StatusUnauthorized, "IEI workspace session is invalid")
			return nil, nil, services.WorkspaceFileScope{}, false
		}
		return h.instanceWorkspaceScope(c, instance, "iei_")
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return nil, nil, services.WorkspaceFileScope{}, false
	}

	instance, err := h.instanceService.GetByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return nil, nil, services.WorkspaceFileScope{}, false
	}
	if instance == nil {
		utils.Error(c, http.StatusNotFound, "Instance not found")
		return nil, nil, services.WorkspaceFileScope{}, false
	}

	userIDRaw, ok := c.Get("userID")
	if !ok {
		utils.Error(c, http.StatusUnauthorized, "Unauthorized")
		return nil, nil, services.WorkspaceFileScope{}, false
	}
	userID, ok := userIDRaw.(int)
	if !ok {
		utils.Error(c, http.StatusUnauthorized, "Unauthorized")
		return nil, nil, services.WorkspaceFileScope{}, false
	}
	roleRaw, _ := c.Get("userRole")
	userRole, _ := roleRaw.(string)
	if userRole != "admin" && instance.UserID != userID {
		utils.Error(c, http.StatusForbidden, "Access denied")
		return nil, nil, services.WorkspaceFileScope{}, false
	}
	return h.instanceWorkspaceScope(c, instance, "")
}

func (h *WorkspaceFileHandler) sharedWorkspaceScope(c *gin.Context) (*models.Instance, services.WorkspaceFileService, services.WorkspaceFileScope, bool) {
	if h.externalAccessService == nil || h.instanceAccessService == nil {
		utils.Error(c, http.StatusServiceUnavailable, "Shared workspace access is not configured")
		return nil, nil, services.WorkspaceFileScope{}, false
	}
	code := strings.TrimSpace(c.Param("code"))
	access, err := h.externalAccessService.ResolveShortLink(c.Request.Context(), code)
	if err != nil {
		utils.Error(c, http.StatusUnauthorized, err.Error())
		return nil, nil, services.WorkspaceFileScope{}, false
	}
	if access.AuthMode != services.ExternalAccessModeShareLink &&
		access.AuthMode != services.ExternalAccessModePassword {
		utils.Error(c, http.StatusBadRequest, "Unsupported share link mode")
		return nil, nil, services.WorkspaceFileScope{}, false
	}
	workspaceAccess, err := services.NormalizeExternalWorkspaceAccess(access.WorkspaceAccess)
	if err != nil || workspaceAccess == services.ExternalWorkspaceAccessNone {
		utils.Error(c, http.StatusForbidden, "Workspace access is not enabled for this share link")
		return nil, nil, services.WorkspaceFileScope{}, false
	}
	isWrite := c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead
	if isWrite && workspaceAccess != services.ExternalWorkspaceAccessWrite {
		utils.Error(c, http.StatusForbidden, "This share link has read-only workspace access")
		return nil, nil, services.WorkspaceFileScope{}, false
	}

	token, cookieErr := c.Cookie(shortExternalAccessCookieName(code))
	if cookieErr != nil || strings.TrimSpace(token) == "" {
		utils.Error(c, http.StatusUnauthorized, "Open the share link before accessing workspace files")
		return nil, nil, services.WorkspaceFileScope{}, false
	}
	accessToken, tokenErr := h.instanceAccessService.ValidateToken(token)
	if tokenErr != nil ||
		accessToken.InstanceID != access.InstanceID ||
		accessToken.SessionBinding != sharedExternalAccessSessionBinding(code, access) {
		utils.Error(c, http.StatusUnauthorized, "Share session expired or invalid")
		return nil, nil, services.WorkspaceFileScope{}, false
	}
	if isWrite {
		expected := sharedExternalAccessCSRFToken(code, token)
		actual := strings.TrimSpace(c.GetHeader(sharedWorkspaceCSRFHeader))
		if expected == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) != 1 {
			utils.Error(c, http.StatusForbidden, "Invalid share request")
			return nil, nil, services.WorkspaceFileScope{}, false
		}
	}

	instance, err := h.instanceService.GetByID(access.InstanceID)
	if err != nil {
		utils.HandleError(c, err)
		return nil, nil, services.WorkspaceFileScope{}, false
	}
	if instance == nil {
		utils.Error(c, http.StatusNotFound, "Instance not found")
		return nil, nil, services.WorkspaceFileScope{}, false
	}
	if accessToken.UserID != instance.UserID {
		utils.Error(c, http.StatusUnauthorized, "Share session does not match instance owner")
		return nil, nil, services.WorkspaceFileScope{}, false
	}
	return h.instanceWorkspaceScope(c, instance, "share_")
}

func (h *WorkspaceFileHandler) instanceWorkspaceScope(c *gin.Context, instance *models.Instance, auditActionPrefix string) (*models.Instance, services.WorkspaceFileService, services.WorkspaceFileScope, bool) {
	if isDesktopWorkspaceInstance(instance) {
		return instance, h.runtimeWorkspaceFileService, services.WorkspaceFileScope{
			InstanceID:        instance.ID,
			UserID:            instance.UserID,
			RuntimeGeneration: instance.RuntimeGeneration,
			WorkspacePath:     "/config",
			AuditActionPrefix: auditActionPrefix,
		}, true
	}
	if instance.WorkspacePath == nil || strings.TrimSpace(*instance.WorkspacePath) == "" {
		utils.Error(c, http.StatusNotFound, "Workspace not found")
		return nil, nil, services.WorkspaceFileScope{}, false
	}

	return instance, h.fileService, services.WorkspaceFileScope{
		InstanceID:        instance.ID,
		UserID:            instance.UserID,
		RuntimeGeneration: instance.RuntimeGeneration,
		WorkspacePath:     *instance.WorkspacePath,
		AuditActionPrefix: auditActionPrefix,
	}, true
}

func isDesktopWorkspaceInstance(instance *models.Instance) bool {
	if instance == nil {
		return false
	}
	if services.IsWindowsVMRuntimeInstance(instance) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(instance.InstanceMode), services.InstanceModePro) ||
		strings.EqualFold(strings.TrimSpace(instance.RuntimeType), services.RuntimeBackendDesktop)
}

func streamWorkspaceFile(c *gin.Context, file io.Reader, filename, contentType, disposition string, size int64) {
	safeName := safeWorkspaceDownloadName(filename)
	c.Header("Content-Type", contentType)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", workspaceContentDisposition(disposition, safeName))
	if size >= 0 {
		c.Header("Content-Length", strconv.FormatInt(size, 10))
	}
	if _, err := io.Copy(c.Writer, file); err != nil && !c.Writer.Written() {
		utils.HandleError(c, err)
	}
}

func workspaceContentDisposition(disposition, filename string) string {
	disposition = strings.TrimSpace(strings.ToLower(disposition))
	if disposition != "inline" {
		disposition = "attachment"
	}
	escaped := strings.ReplaceAll(filename, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return fmt.Sprintf("%s; filename=\"%s\"; filename*=UTF-8''%s", disposition, escaped, url.PathEscape(filename))
}

func safeWorkspaceDownloadName(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	name = filepath.Base(name)
	if name == "." || name == "/" || name == "" {
		name = "download"
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		default:
			return r
		}
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "download"
	}
	return name
}

func handleWorkspaceFileError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrWorkspaceUpgradeBusy):
		utils.Error(c, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrWorkspacePathNotFound):
		utils.Error(c, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrWorkspacePreviewTooLarge), errors.Is(err, services.ErrWorkspaceUploadTooLarge):
		utils.Error(c, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, services.ErrWorkspacePathEscape):
		utils.Error(c, http.StatusForbidden, "Access denied")
	case errors.Is(err, services.ErrWorkspacePathInvalid),
		errors.Is(err, services.ErrWorkspaceDirectoryExpected),
		errors.Is(err, services.ErrWorkspaceFileExpected),
		errors.Is(err, services.ErrWorkspaceRootOperation),
		errors.Is(err, services.ErrWorkspaceFileNameInvalid),
		errors.Is(err, services.ErrWorkspaceEntryExists):
		utils.Error(c, http.StatusBadRequest, err.Error())
	default:
		utils.HandleError(c, err)
	}
}
