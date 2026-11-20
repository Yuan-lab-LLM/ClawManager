package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
)

// Only the cookie-authenticated portal registers these routes. Public API keys,
// CM JWTs and file share links do not grant an owner terminal session.
func (h *IEISystemHandler) RegisterWorkbenchRoutes(group *gin.RouterGroup, service instanceWorkbench) {
	handler := &InstanceWorkbenchHandler{service: service, resolveIdentity: h.workbenchIdentity}
	handler.Register(group.Group("/instances/:id/workbench"))
}

func (h *IEISystemHandler) workbenchIdentity(c *gin.Context, _ bool) (int, int, bool, bool) {
	h.noStore(c)
	session, ok := h.requireSession(c)
	if !ok {
		return 0, 0, false, false
	}
	instance, ok := h.requireOwnedSupportedInstance(c, session.Email)
	if !ok {
		return 0, 0, false, false
	}
	// Check authoritative membership too: an owner string on a member cannot
	// grant access to a different owner's Team or an ambiguous binding.
	if _, ok := h.instanceTeamID(c, instance); !ok {
		return 0, 0, false, false
	}
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		if c.GetHeader("X-Owner-Action") != "1" || c.GetHeader("Sec-Fetch-Site") == "cross-site" {
			utils.Error(c, http.StatusForbidden, "Explicit owner action required")
			return 0, 0, false, false
		}
		if origin := strings.TrimRight(c.GetHeader("Origin"), "/"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || parsed.Host != c.Request.Host || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
				utils.Error(c, http.StatusForbidden, "Cross-site request denied")
				return 0, 0, false, false
			}
		}
		if h.lifecycle != nil {
			op, err := h.lifecycle.GetLatestLifecycleOperation(instance.UserID, instance.ID)
			if err != nil {
				utils.HandleError(c, err)
				return 0, 0, false, false
			}
			if op != nil && (op.Status == "queued" || op.Status == "processing" || op.Status == "batch_pending") {
				utils.Error(c, http.StatusConflict, "实例正在执行维护操作，完成后才能使用工作台操作。")
				return 0, 0, false, false
			}
		}
	}
	return instance.UserID, instance.ID, false, true
}

func (h *IEISystemHandler) instanceTeamID(c *gin.Context, instance *models.Instance) (int, bool) {
	if h.ownerTeams == nil {
		return 0, true
	}
	id, err := h.ownerTeams.InstanceTeamForOwner(instance.ID, *instance.Owner)
	if err != nil {
		utils.Error(c, http.StatusServiceUnavailable, "无法确认实例团队归属，请稍后重试。")
		return 0, false
	}
	return id, true
}
