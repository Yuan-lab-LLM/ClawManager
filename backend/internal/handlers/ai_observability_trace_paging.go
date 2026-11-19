package handlers

import (
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/utils"
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"strings"
)

type tracePagingService interface {
	GetTracePage(string, string, int) (*services.AuditTraceDetail, error)
	GetTraceBody(string, string, int, int) (*repository.TraceBodyChunk, error)
}

func (h *AIObservabilityHandler) tracePagedRequest(c *gin.Context) bool {
	if raw, present := c.GetQueryArray("flow_ids"); present {
		h.traceFlowRequest(c, raw)
		return true
	}
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

func (h *AIObservabilityHandler) traceFlowRequest(c *gin.Context, raw []string) {
	if len(raw) != 1 || len(raw[0]) > services.TraceFlowBatchLimit*20 {
		utils.Error(c, http.StatusBadRequest, "Invalid trace flow IDs")
		return
	}
	parts := strings.Split(raw[0], ",")
	if len(parts) > services.TraceFlowBatchLimit {
		utils.Error(c, http.StatusBadRequest, "Too many trace flow IDs")
		return
	}
	ids := make([]int, 0, len(parts))
	seen := make(map[int]bool, len(parts))
	for _, part := range parts {
		id, err := strconv.Atoi(part)
		if err != nil || id < 1 || seen[id] {
			utils.Error(c, http.StatusBadRequest, "Invalid trace flow IDs")
			return
		}
		ids = append(ids, id)
		seen[id] = true
	}
	previous := 0
	if values, present := c.GetQueryArray("flow_previous_id"); present {
		var err error
		previous, err = strconv.Atoi(values[0])
		if len(values) != 1 || err != nil || previous < 1 || seen[previous] {
			utils.Error(c, http.StatusBadRequest, "Invalid previous trace flow ID")
			return
		}
	}
	service, ok := h.service.(interface {
		GetTraceFlow(context.Context, string, []int, int) (*services.AuditTraceFlowPreview, error)
	})
	if !ok {
		utils.Error(c, http.StatusServiceUnavailable, "Trace flow preview unavailable")
		return
	}
	result, err := service.GetTraceFlow(c.Request.Context(), c.Param("traceId"), ids, previous)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Trace execution flow", result)
}
