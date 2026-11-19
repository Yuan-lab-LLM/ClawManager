package handlers

import (
	"clawreef/internal/repository"
	"clawreef/internal/utils"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
)

func handleObservationQueryError(c *gin.Context, err error) {
	if errors.Is(err, repository.ErrSessionCursorStale) {
		c.JSON(http.StatusConflict, gin.H{"success": false, "error": err.Error(), "data": gin.H{"state": "cursor_stale"}})
		return
	}
	var rebuilding *repository.SummaryRebuildingError
	if errors.As(err, &rebuilding) {
		c.Header("Retry-After", "2")
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": rebuilding.Error(), "data": gin.H{"state": "rebuilding", "processed": rebuilding.Processed, "target": rebuilding.Target, "pending_invocation_dependencies": rebuilding.PendingCosts}})
		return
	}
	utils.HandleError(c, err)
}
