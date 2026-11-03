package handlers

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"clawreef/internal/systembackup"
	"github.com/gin-gonic/gin"
)

type SystemBackupHandler struct {
	store      systembackup.Store
	bindings   systembackup.RoleBindings
	configured bool
	strongAuth *systembackup.StrongAuthVerifier
}

func (h *SystemBackupHandler) SetStrongAuthVerifier(v *systembackup.StrongAuthVerifier) {
	h.strongAuth = v
}

func NewSystemBackupHandler(s systembackup.Store, b systembackup.RoleBindings) *SystemBackupHandler {
	return &SystemBackupHandler{store: s, bindings: b, configured: s.DB != nil && s.InstallationID != ""}
}
func (h *SystemBackupHandler) Register(r *gin.RouterGroup) {
	r.GET("/overview", h.overview)
	r.GET("/config", h.config)
	r.PUT("/config", h.saveConfig)
	r.POST("/config/authorization", h.configAuthorization)
	r.GET("/tasks", h.list)
	r.POST("/tasks", h.create)
	r.GET("/tasks/:id", h.detail)
	r.GET("/tasks/:id/events", h.events)
	r.GET("/tasks/:id/verification-report", h.verificationReport)
	r.POST("/tasks/:id/cancel", h.cancel)
}

func (h *SystemBackupHandler) verificationReport(c *gin.Context) {
	if !h.allow(c, systembackup.Read) {
		return
	}
	v, e := h.store.VerificationReport(c.Request.Context(), c.Param("id"))
	if e != nil {
		backupError(c, e)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": v})
}
func backupError(c *gin.Context, err error) {
	code := systembackup.PublicError(err)
	status := 500
	switch code {
	case "validation_failed":
		status = 400
	case "permission_denied":
		status = 403
	case "not_found":
		status = 404
	case "not_ready", "artifact_unavailable":
		status = 503
	case "version_conflict", "idempotency_conflict", "state_conflict", "commit_started", "conflict_active_job":
		status = 409
	case "unsupported_runner_p1", "unsupported_profile_p1", "capacity_exceeded":
		status = 422
	case "strong_auth_required", "strong_auth_expired", "strong_auth_invalid_issuer", "strong_auth_invalid", "strong_auth_replayed":
		status = 401
	case "rate_limited":
		status = 429
	}
	if status == 503 || status == 429 {
		c.Header("Retry-After", "15")
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"success": false, "code": code, "error": code})
}
func (h *SystemBackupHandler) allow(c *gin.Context, p systembackup.Permission) bool {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	id := c.GetInt("userID")
	if id < 1 {
		c.JSON(401, gin.H{"success": false, "code": "authentication_required"})
		return false
	}
	if !h.configured || len(h.bindings) == 0 {
		backupError(c, errors.New("not_ready"))
		return false
	}
	if !h.bindings.Allows(id, p) {
		backupError(c, errors.New("permission_denied"))
		return false
	}
	return true
}
func backupBody(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10)
	raw, e := io.ReadAll(c.Request.Body)
	if e == nil {
		e = systembackup.DecodeStrictJSON(raw, v)
		if e == nil {
			return true
		}
	}
	var size *http.MaxBytesError
	if errors.As(e, &size) {
		c.JSON(413, gin.H{"success": false, "code": "payload_too_large"})
		return false
	}
	backupError(c, errors.New("validation_failed"))
	return false
}
func (h *SystemBackupHandler) overview(c *gin.Context) {
	if !h.allow(c, systembackup.Read) {
		return
	}
	v, e := h.store.Readiness(c.Request.Context())
	if e != nil {
		backupError(c, e)
		return
	}
	permissions := []systembackup.Permission{}
	for _, p := range []systembackup.Permission{systembackup.Read, systembackup.Operate, systembackup.Configure, systembackup.SecurityConfigure, systembackup.Destructive} {
		if h.bindings.Allows(c.GetInt("userID"), p) {
			permissions = append(permissions, p)
		}
	}
	c.JSON(200, gin.H{"success": true, "data": gin.H{"readiness": v, "permissions": permissions}})
}
func (h *SystemBackupHandler) config(c *gin.Context) {
	if !h.allow(c, systembackup.Read) {
		return
	}
	v, e := h.store.Config(c.Request.Context())
	if e != nil {
		backupError(c, e)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": v})
}
func (h *SystemBackupHandler) saveConfig(c *gin.Context) {
	if !h.allow(c, systembackup.Configure) {
		return
	}
	var input systembackup.ConfigChange
	if !backupBody(c, &input) {
		return
	}
	v, replay, e := h.store.SaveConfigAuthorized(c.Request.Context(), c.GetInt("userID"), c.GetHeader("Idempotency-Key"), input, systembackup.SessionBinding(c.GetHeader("Authorization")), c.GetHeader("X-System-Backup-Proof"), h.bindings, h.strongAuth)
	if e != nil {
		backupError(c, e)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": v, "idempotent_replay": replay})
}

// This prepares a binding for a trusted MFA issuer, not an MFA proof. It never
// consumes a nonce or gives the application the issuer's signing capability.
func (h *SystemBackupHandler) configAuthorization(c *gin.Context) {
	if !h.allow(c, systembackup.Configure) {
		return
	}
	var input systembackup.ConfigChange
	if !backupBody(c, &input) {
		return
	}
	if h.strongAuth == nil || h.strongAuth.Validate() != nil || h.strongAuth.Audience != h.store.InstallationID {
		backupError(c, errors.New("not_ready"))
		return
	}
	key := c.GetHeader("Idempotency-Key")
	if len(key) < 8 || len(key) > 128 {
		backupError(c, errors.New("validation_failed"))
		return
	}
	hash, e := systembackup.RequestHash(input)
	if e != nil {
		backupError(c, e)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": gin.H{"aud": h.store.InstallationID, "sub": strconv.Itoa(c.GetInt("userID")), "session": systembackup.SessionBinding(c.GetHeader("Authorization")), "endpoint": "PUT /config", "request_hash": hash, "idempotency_key": key}})
}
func (h *SystemBackupHandler) list(c *gin.Context) {
	if !h.allow(c, systembackup.Read) {
		return
	}
	v, e := h.store.List(c.Request.Context(), 100)
	if e != nil {
		backupError(c, e)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": gin.H{"items": v}})
}
func (h *SystemBackupHandler) detail(c *gin.Context) {
	if !h.allow(c, systembackup.Read) {
		return
	}
	v, e := h.store.Get(c.Request.Context(), c.Param("id"))
	if e != nil {
		backupError(c, e)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": v})
}
func (h *SystemBackupHandler) events(c *gin.Context) {
	if !h.allow(c, systembackup.Read) {
		return
	}
	after := uint64(0)
	if value := c.Query("after"); value != "" {
		n, e := strconv.ParseUint(value, 10, 64)
		if e != nil {
			backupError(c, errors.New("validation_failed"))
			return
		}
		after = n
	}
	v, e := h.store.Events(c.Request.Context(), c.Param("id"), after)
	if e != nil {
		backupError(c, e)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": gin.H{"items": v}})
}
func (h *SystemBackupHandler) create(c *gin.Context) {
	if !h.allow(c, systembackup.Operate) {
		return
	}
	var input systembackup.CreateTaskRequest
	if !backupBody(c, &input) {
		return
	}
	v, replay, e := h.store.Create(c.Request.Context(), c.GetInt("userID"), c.GetHeader("Idempotency-Key"), input, systembackup.AdmitReady)
	if e != nil {
		backupError(c, e)
		return
	}
	c.Header("Location", "/api/v1/admin/system-backup/tasks/"+v.ID)
	c.JSON(202, gin.H{"success": true, "data": v, "idempotent_replay": replay})
}
func (h *SystemBackupHandler) cancel(c *gin.Context) {
	if !h.allow(c, systembackup.Operate) {
		return
	}
	var input struct {
		Version uint64 `json:"expected_version"`
	}
	if !backupBody(c, &input) {
		return
	}
	v, replay, e := h.store.CancelIdempotent(c.Request.Context(), c.Param("id"), input.Version, c.GetInt("userID"), c.GetHeader("Idempotency-Key"))
	if e != nil {
		backupError(c, e)
		return
	}
	c.Header("Location", "/api/v1/admin/system-backup/tasks/"+v.ID)
	c.JSON(202, gin.H{"success": true, "data": v, "idempotent_replay": replay})
}
