package northbound

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"clawreef/internal/ownerrecords"
	"github.com/gin-gonic/gin"
)

type ownerUsageReader interface {
	Dashboard(context.Context, string, *uint64, string, time.Time) (*ownerrecords.Dashboard, error)
}

func (s *CoreService) SetOwnerUsage(store ownerUsageReader) { s.ownerUsage = store }

func (h *CoreHandler) GetOwnerUsage(c *gin.Context) {
	principal := currentPrincipal(c)
	if principal == nil || principal.UserID <= 0 || !principal.HasScope(ScopeOwnerUsageRead) {
		writeError(c, apiError(403, "SCOPE_DENIED", "Insufficient permission", nil))
		return
	}
	owner := strings.TrimSpace(c.Query("owner"))
	period := c.DefaultQuery("range", "7d")
	if owner == "" || utf8.RuneCountInString(owner) > 128 || (period != "7d" && period != "30d" && period != "month") {
		writeError(c, apiError(400, "INVALID_REQUEST", "owner and a supported range are required", nil))
		return
	}
	if h.service.ownerUsage == nil {
		writeError(c, apiError(503, "USAGE_UNAVAILABLE", "Usage statistics unavailable", nil))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	scope := uint64(principal.UserID)
	data, err := h.service.ownerUsage.Dashboard(ctx, owner, &scope, period, time.Now())
	if err != nil {
		writeError(c, apiError(503, "USAGE_UNAVAILABLE", "Usage statistics temporarily unavailable", err))
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, data)
}
