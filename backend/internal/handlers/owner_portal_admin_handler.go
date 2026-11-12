package handlers

import (
	"clawreef/internal/ownerrecords"
	"clawreef/internal/utils"
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
	"net/http"
	"strconv"
	"time"
)

type OwnerPortalAdminHandler struct {
	store        *ownerrecords.Store
	retryLimiter *rate.Limiter
}

func NewOwnerPortalAdminHandler(s *ownerrecords.Store) *OwnerPortalAdminHandler {
	return &OwnerPortalAdminHandler{store: s, retryLimiter: rate.NewLimiter(rate.Every(time.Second), 3)}
}
func (h *OwnerPortalAdminHandler) SyncRecords(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	limit, e1 := strconv.Atoi(c.DefaultQuery("limit", "20"))
	days, e2 := strconv.Atoi(c.DefaultQuery("days", "1"))
	before, e3 := strconv.ParseUint(c.DefaultQuery("before", "0"), 10, 64)
	instance, e4 := strconv.ParseUint(c.DefaultQuery("instance", "0"), 10, 64)
	id, e5 := strconv.ParseUint(c.DefaultQuery("id", "0"), 10, 64)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil {
		utils.Error(c, 400, "无效查询参数")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	v, e := h.store.SyncRecords(ctx, ownerrecords.SyncQuery{Kind: c.DefaultQuery("kind", "sync_batch"), Status: c.Query("status"), Owner: c.Query("owner"), Instance: instance, Before: before, Limit: limit, Days: days}, id)
	if e != nil {
		utils.Error(c, 503, "同步记录查询未完成，请缩小范围后重试")
		return
	}
	utils.Success(c, 200, "Sync records", v)
}
func (h *OwnerPortalAdminHandler) SyncOverview(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
	defer cancel()
	v, e := h.store.SyncOverview(ctx)
	if e != nil {
		utils.Error(c, 503, "批次状态暂不可读")
		return
	}
	utils.Success(c, 200, "Sync overview", v)
}
func (h *OwnerPortalAdminHandler) RetrySync(c *gin.Context) {
	if !h.retryLimiter.Allow() {
		utils.Error(c, 429, "操作过于频繁，请稍后重试")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	var req struct {
		ID        uint64 `json:"id"`
		Updated   string `json:"updated"`
		Confirmed bool   `json:"confirmed"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || req.ID == 0 || len(req.Updated) > 40 || !req.Confirmed {
		utils.Error(c, 400, "请确认单条重试请求")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
	defer cancel()
	e := h.store.RetrySync(ctx, req.ID, req.Updated, adminActorID(c), time.Now())
	if e != nil {
		if errors.Is(e, ownerrecords.ErrSyncConflict) {
			utils.Error(c, 409, e.Error())
		} else {
			utils.Error(c, 503, "重试入队未完成，请刷新记录后检查")
		}
		return
	}
	utils.Success(c, 200, "Queued for next batch", gin.H{"queued": true})
}
func (h *OwnerPortalAdminHandler) Users(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 || page > 100000 || len(c.Query("search")) > 256 {
		utils.Error(c, 400, "无效的分页或搜索条件")
		return
	}
	v, e := h.store.PortalUsers(ctx, c.Query("search"), c.Query("sort"), page, time.Now())
	if e != nil {
		utils.Error(c, 503, "门户统计暂不可用，请稍后重试")
		return
	}
	utils.Success(c, 200, "Portal statistics", v)
}
func (h *OwnerPortalAdminHandler) Calendar(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	year, e := strconv.Atoi(c.Query("year"))
	if e != nil || year < 2000 || year > 2100 {
		utils.Error(c, 400, "年份不正确")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	head, e := h.store.CalendarHead(ctx, year)
	if e != nil {
		utils.Error(c, 503, "日历读取失败")
		return
	}
	history, e := h.store.CalendarVersions(ctx, year)
	if e != nil {
		utils.Error(c, 503, "历史读取失败")
		return
	}
	utils.Success(c, 200, "Calendar", gin.H{"head": head, "history": history})
}
func (h *OwnerPortalAdminHandler) MutateCalendar(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 270<<10)
	var req struct {
		Document        json.RawMessage `json:"document"`
		DocumentJSON    string          `json:"document_json"`
		Action          string          `json:"action"`
		ExpectedVersion uint64          `json:"expected_version"`
		Year            int             `json:"year"`
		Revision        uint64          `json:"revision"`
		Confirmed       bool            `json:"confirmed"`
	}
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if e := dec.Decode(&req); e != nil {
		utils.Error(c, 400, "无效的日历请求")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	var d ownerrecords.CalendarDocument
	var e error
	if req.Action == "rollback" {
		if req.Year < 2000 || req.Year > 2100 || req.Revision == 0 {
			utils.Error(c, 400, "无效的恢复版本")
			return
		}
		var v ownerrecords.CalendarVersion
		v, e = h.store.CalendarRevision(ctx, req.Year, req.Revision)
		d = v.Document
	} else {
		if req.DocumentJSON != "" {
			req.Document = json.RawMessage(req.DocumentJSON)
		}
		d, e = ownerrecords.ParseCalendar(req.Document)
	}
	if e != nil {
		utils.Error(c, 400, "日历校验失败: "+e.Error())
		return
	}
	if req.Action == "preview" {
		v, e := h.store.PreviewCalendar(ctx, d, time.Now())
		if e != nil {
			utils.Error(c, 400, "预览失败，请重试: "+e.Error())
			return
		}
		utils.Success(c, 200, "Calendar preview", v)
		return
	}
	if (req.Action != "publish" && req.Action != "rollback") || !req.Confirmed {
		utils.Error(c, 400, "请先预览并确认发布或恢复")
		return
	}
	// A rollback replaces the complete selected revision, including partial coverage.
	v, e := h.store.PublishCalendar(ctx, d, req.ExpectedVersion, adminActorID(c), time.Now(), req.Action == "rollback")
	if e != nil {
		status := 503
		if errors.Is(e, ownerrecords.ErrCalendarConflict) {
			status = 409
		}
		utils.Error(c, status, "日历发布失败: "+e.Error())
		return
	}
	utils.Success(c, 200, "Calendar published", v)
}
