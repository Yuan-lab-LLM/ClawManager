package handlers

import (
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

type tracePagingService interface {
	GetTracePage(string, string, int) (*services.AuditTraceDetail, error)
	GetTraceBody(string, string, int, int) (*repository.TraceBodyChunk, error)
}

func (h *AIObservabilityHandler) tracePagedRequest(c *gin.Context) bool {
	service, ok := h.service.(tracePagingService)
	if !ok {
		return false
	}
	if kind := c.Query("body_kind"); kind != "" {
		id, e := strconv.Atoi(c.Query("body_id"))
		if e != nil || id < 1 {
			utils.Error(c, http.StatusBadRequest, "Invalid body ID")
			return true
		}
		offset := 0
		if raw := c.Query("offset"); raw != "" {
			offset, e = strconv.Atoi(raw)
			if e != nil || offset < 0 {
				utils.Error(c, http.StatusBadRequest, "Invalid body offset")
				return true
			}
		}
		result, err := service.GetTraceBody(c.Param("traceId"), kind, id, offset)
		if err != nil {
			utils.HandleError(c, err)
			return true
		}
		utils.Success(c, http.StatusOK, "Trace body chunk", result)
		return true
	}
	limit := 200
	if raw := c.Query("limit"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n < 1 || n > 200 {
			utils.Error(c, http.StatusBadRequest, "Trace limit must be between 1 and 200")
			return true
		}
		limit = n
	}
	result, err := service.GetTracePage(c.Param("traceId"), c.Query("cursor"), limit)
	if err != nil {
		utils.HandleError(c, err)
		return true
	}
	utils.Success(c, http.StatusOK, "Trace page", result)
	return true
}
