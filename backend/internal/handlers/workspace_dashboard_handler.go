package handlers

import (
	"clawreef/internal/workspacedashboard"
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

type workspaceDashboardReader interface {
	Dashboard(context.Context, int, string, time.Time) (*workspacedashboard.Dashboard, error)
	Usage(context.Context, int, string, time.Time) (*workspacedashboard.UsageDashboard, error)
}

func (h *WorkspaceDashboardHandler) GetUsage(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	value, ok := c.Get("userID")
	id, valid := value.(int)
	if !ok || !valid || id <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}
	period := c.DefaultQuery("range", "7d")
	if period != "today" && period != "7d" && period != "30d" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported range"})
		return
	}
	if h.reader == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Workspace temporarily unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	data, err := h.reader.Usage(ctx, id, period, time.Now())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Workspace temporarily unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

type WorkspaceDashboardHandler struct{ reader workspaceDashboardReader }

func NewWorkspaceDashboardHandler(reader workspaceDashboardReader) *WorkspaceDashboardHandler {
	return &WorkspaceDashboardHandler{reader: reader}
}
func (h *WorkspaceDashboardHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	value, ok := c.Get("userID")
	id, valid := value.(int)
	if !ok || !valid || id <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}
	period := c.DefaultQuery("range", "7d")
	if period != "today" && period != "7d" && period != "30d" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported range"})
		return
	}
	if h.reader == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Workspace temporarily unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	data, err := h.reader.Dashboard(ctx, id, period, time.Now())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Workspace temporarily unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}
