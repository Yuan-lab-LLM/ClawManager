package handlers

import (
	"context"
	"net/http"
	"time"

	"clawreef/internal/ownerrecords"
	"github.com/gin-gonic/gin"
)

type ownerDashboardReader interface {
	Dashboard(context.Context, string, *uint64, string, time.Time) (*ownerrecords.Dashboard, error)
}

func (h *IEISystemHandler) SetOwnerDashboard(reader ownerDashboardReader) { h.dashboard = reader }

func (h *IEISystemHandler) GetDashboard(c *gin.Context) {
	h.noStore(c)
	session, ok := h.requireSession(c)
	if !ok {
		return
	}
	if h.dashboard == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Owner dashboard is not enabled"})
		return
	}
	period := c.DefaultQuery("range", "7d")
	if period != "7d" && period != "30d" && period != "month" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported range"})
		return
	}
	// No owner supplied by the browser is ever used for authorization.
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	read := h.dashboard.Dashboard
	if store, ok := h.dashboard.(*ownerrecords.Store); ok {
		if !store.Available() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Owner statistics temporarily unavailable"})
			return
		}
		read = store.CachedDashboard
	}
	data, err := read(ctx, session.Email, nil, period, time.Now())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Owner statistics temporarily unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}
