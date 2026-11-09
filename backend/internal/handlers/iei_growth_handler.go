package handlers

import (
	"clawreef/internal/northbound"
	"clawreef/internal/ownerrecords"
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

var ownerActivityLimiter = northbound.NewFixedWindowLimiter(120, time.Minute)
var ownerGrowthLimiter = northbound.NewFixedWindowLimiter(20, time.Minute)
var ownerReportLimiter = northbound.NewFixedWindowLimiter(20, time.Minute)

// Authentication, not Dashboard mounting, establishes a daily portal visit.
// This endpoint performs no ranking, history or growth snapshot read.
func (h *IEISystemHandler) RecordOwnerVisit(c *gin.Context) {
	h.noStore(c)
	session, ok := h.requireSession(c)
	if !ok || !ownerWriteRequest(c) {
		return
	}
	if !ownerGrowthLimiter.Allow(session.Email) {
		c.JSON(429, gin.H{"error": "Please retry later"})
		return
	}
	s, ok := h.ownerStore(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := s.VisitToday(ctx, session.Email, time.Now()); err != nil {
		c.JSON(503, gin.H{"error": "Visit temporarily unavailable"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *IEISystemHandler) ownerStore(c *gin.Context) (*ownerrecords.Store, bool) {
	s, ok := h.dashboard.(*ownerrecords.Store)
	if !ok || !s.Available() {
		c.JSON(503, gin.H{"error": "Owner analytics unavailable"})
		return nil, false
	}
	return s, ok
}
func ownerWriteRequest(c *gin.Context) bool {
	if c.GetHeader("X-Owner-Action") != "1" {
		c.JSON(403, gin.H{"error": "Invalid portal request"})
		return false
	}
	if origin := c.GetHeader("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != c.Request.Host || (u.Scheme != "https" && u.Scheme != "http") {
			c.JSON(403, gin.H{"error": "Invalid portal origin"})
			return false
		}
	}
	return true
}
func (h *IEISystemHandler) GetGrowth(c *gin.Context) {
	h.noStore(c)
	session, ok := h.requireSession(c)
	if !ok {
		return
	}
	if !ownerGrowthLimiter.Allow(session.Email) {
		c.JSON(429, gin.H{"error": "Please retry later"})
		return
	}
	s, ok := h.ownerStore(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if c.Request.Method == http.MethodPost {
		if !ownerWriteRequest(c) {
			return
		}
		var err error
		if c.Query("visit") == "true" {
			err = s.VisitToday(ctx, session.Email, time.Now())
		} else {
			// A refresh cannot force foreground reconciliation.
		}
		if err != nil {
			c.JSON(503, gin.H{"error": "Growth update unavailable"})
			return
		}
	}
	data, err := s.CachedGrowth(ctx, session.Email, time.Now())
	if err != nil {
		c.JSON(503, gin.H{"error": "Growth statistics unavailable"})
		return
	}
	c.JSON(200, gin.H{"data": data})
}
func (h *IEISystemHandler) RecordOwnerActivity(c *gin.Context) {
	h.noStore(c)
	session, ok := h.requireSession(c)
	if !ok {
		return
	}
	if !ownerActivityLimiter.Allow(session.Email) {
		c.JSON(429, gin.H{"error": "Please retry later"})
		return
	}
	if !ownerWriteRequest(c) {
		return
	}
	instance, ok := h.requireOwnedSupportedInstance(c, session.Email)
	if !ok {
		return
	}
	if instance.Status != "running" {
		c.JSON(409, gin.H{"error": "Instance is not running"})
		return
	}
	s, ok := h.ownerStore(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2048)
	var pulse ownerrecords.ActivityPulse
	if c.ShouldBindJSON(&pulse) != nil {
		c.JSON(400, gin.H{"error": "Invalid activity pulse"})
		return
	}
	if err := s.BufferActivity(session.Email, uint64(instance.ID), pulse, time.Now()); err != nil {
		c.JSON(400, gin.H{"error": "Activity pulse could not be recorded"})
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"accepted": true}})
}
func (h *IEISystemHandler) GetOwnerReport(c *gin.Context) {
	h.noStore(c)
	session, ok := h.requireSession(c)
	if !ok {
		return
	}
	if !ownerReportLimiter.Allow(session.Email) {
		c.JSON(429, gin.H{"error": "Please retry later"})
		return
	}
	if c.Request.Method == http.MethodPost && !ownerWriteRequest(c) {
		return
	}
	s, ok := h.ownerStore(c)
	if !ok {
		return
	}
	kind := c.DefaultQuery("kind", "week")
	date := c.Query("date")
	loc, _ := time.LoadLocation("Asia/Shanghai")
	at, err := time.ParseInLocation("2006-01-02", date, loc)
	now := time.Now()
	if err != nil || (kind != "week" && kind != "month") || at.After(now) || at.Before(now.AddDate(-2, 0, 0)) {
		c.JSON(400, gin.H{"error": "Invalid report period"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	var data *ownerrecords.Report
	if revision := c.Query("revision"); revision != "" && c.Request.Method == http.MethodGet {
		n, e := strconv.ParseUint(revision, 10, 64)
		if e != nil || n == 0 {
			c.JSON(400, gin.H{"error": "Invalid revision"})
			return
		}
		data, err = s.SavedReport(ctx, session.Email, kind, date, n, now)
	} else if c.Request.Method == http.MethodGet {
		data, err = s.CachedReport(ctx, session.Email, kind, date, now)
	} else {
		data, err = s.Report(ctx, session.Email, kind, date, now, c.Request.Method == http.MethodPost)
	}
	if err != nil {
		c.JSON(503, gin.H{"error": "Report temporarily unavailable"})
		return
	}
	c.JSON(200, gin.H{"data": data})
}
