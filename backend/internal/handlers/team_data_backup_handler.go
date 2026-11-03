package handlers

import (
	"errors"
	"net/http"

	"clawreef/internal/systembackup"
	"github.com/gin-gonic/gin"
)

// Routes are registered on the application's existing authenticated admin
// group. Ordinary Team data backup does not impersonate formal acceptance.
type TeamDataBackupHandler struct{ service *systembackup.TeamDataService }

func NewTeamDataBackupHandler(s *systembackup.TeamDataService) *TeamDataBackupHandler {
	return &TeamDataBackupHandler{service: s}
}
func (h *TeamDataBackupHandler) Register(r *gin.RouterGroup) {
	r.GET("/team-data/overview", h.overview)
	r.GET("/team-data/jobs", h.list)
	r.POST("/team-data/jobs", h.create)
	r.GET("/team-data/jobs/:id", h.detail)
	r.POST("/team-data/jobs/:id/cancel", h.cancel)
	r.GET("/team-data/discover", h.discover)
	r.GET("/team-data/artifacts", h.artifacts)
	r.POST("/team-data/artifacts/:id/import", h.importArtifact)
	r.POST("/team-data/artifacts/:id/protection", h.protect)
	r.POST("/team-data/artifacts/:id/delete-preview", h.deletePreview)
	r.POST("/team-data/artifacts/:id/delete", h.deleteArtifact)
}
func teamDataHTTPError(c *gin.Context, e error) {
	switch e.Error() {
	case "backup_protected", "backup_in_use", "minimum_recovery_points", "backup_deleting", "catalog_changed", "state_conflict", "feature_disabled":
		c.JSON(409, gin.H{"success": false, "code": e.Error()})
	case "confirmation_invalid":
		c.JSON(400, gin.H{"success": false, "code": "confirmation_invalid"})
	default:
		backupError(c, e)
	}
}
func (h *TeamDataBackupHandler) protect(c *gin.Context) {
	if !h.available(c) {
		return
	}
	var body struct {
		Protected *bool `json:"protected"`
	}
	if !backupBody(c, &body) {
		return
	}
	if body.Protected == nil {
		teamDataHTTPError(c, errors.New("validation_failed"))
		return
	}
	if e := h.service.Protect(c.Request.Context(), c.Param("id"), c.GetInt("userID"), *body.Protected); e != nil {
		teamDataHTTPError(c, e)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": gin.H{"protected": *body.Protected}})
}
func (h *TeamDataBackupHandler) deletePreview(c *gin.Context) {
	if !h.available(c) {
		return
	}
	preview, e := h.service.DeletePreview(c.Request.Context(), c.Param("id"), c.GetInt("userID"))
	if e != nil {
		teamDataHTTPError(c, e)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": preview})
}
func (h *TeamDataBackupHandler) deleteArtifact(c *gin.Context) {
	if !h.available(c) {
		return
	}
	var body struct {
		Confirmation string `json:"confirmation"`
	}
	if !backupBody(c, &body) {
		return
	}
	j, e := h.service.DeleteConfirmed(c.Request.Context(), c.Param("id"), body.Confirmation, c.GetHeader("Idempotency-Key"), c.GetInt("userID"))
	if e != nil {
		teamDataHTTPError(c, e)
		return
	}
	c.JSON(202, gin.H{"success": true, "data": j})
}
func (h *TeamDataBackupHandler) discover(c *gin.Context) {
	if !h.available(c) {
		return
	}
	data, e := h.service.Discover(c.Request.Context(), c.Query("cursor"), 20)
	if e != nil {
		teamDataHTTPError(c, e)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": data})
}
func (h *TeamDataBackupHandler) artifacts(c *gin.Context) {
	if !h.available(c) {
		return
	}
	data, e := h.service.Catalog(c.Request.Context())
	if e != nil {
		teamDataHTTPError(c, e)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": gin.H{"items": data}})
}
func (h *TeamDataBackupHandler) importArtifact(c *gin.Context) {
	if !h.available(c) {
		return
	}
	if e := h.service.Import(c.Request.Context(), c.Param("id"), c.GetInt("userID")); e != nil {
		teamDataHTTPError(c, e)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": gin.H{"imported": true}})
}
func (h *TeamDataBackupHandler) available(c *gin.Context) bool {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	if c.GetInt("userID") < 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "code": "authentication_required"})
		return false
	}
	if h.service == nil {
		backupError(c, errors.New("not_ready"))
		return false
	}
	return true
}
func (h *TeamDataBackupHandler) overview(c *gin.Context) {
	if c.GetInt("userID") < 1 {
		c.JSON(401, gin.H{"success": false, "code": "authentication_required"})
		return
	}
	c.Header("Cache-Control", "no-store")
	if h.service == nil {
		c.JSON(200, gin.H{"success": true, "data": gin.H{"configured": false}})
		return
	}
	data, e := h.service.Overview(c.Request.Context())
	if e != nil {
		teamDataHTTPError(c, e)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": data})
}
func (h *TeamDataBackupHandler) list(c *gin.Context) {
	if !h.available(c) {
		return
	}
	items, e := h.service.List(c.Request.Context())
	if e != nil {
		teamDataHTTPError(c, e)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": gin.H{"items": items}})
}
func (h *TeamDataBackupHandler) detail(c *gin.Context) {
	if !h.available(c) {
		return
	}
	j, e := h.service.Get(c.Request.Context(), c.Param("id"))
	if e != nil {
		teamDataHTTPError(c, e)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": j})
}
func (h *TeamDataBackupHandler) create(c *gin.Context) {
	if !h.available(c) {
		return
	}
	var input struct {
		Operation string `json:"operation"`
		SourceID  string `json:"source_backup_id,omitempty"`
	}
	if !backupBody(c, &input) {
		return
	}
	j, e := h.service.Create(c.Request.Context(), c.GetInt("userID"), c.GetHeader("Idempotency-Key"), input.Operation, input.SourceID)
	if e != nil {
		teamDataHTTPError(c, e)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": j})
}
func (h *TeamDataBackupHandler) cancel(c *gin.Context) {
	if !h.available(c) {
		return
	}
	if e := h.service.Cancel(c.Request.Context(), c.Param("id")); e != nil {
		teamDataHTTPError(c, e)
		return
	}
	j, e := h.service.Get(c.Request.Context(), c.Param("id"))
	if e != nil {
		teamDataHTTPError(c, e)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": j})
}
