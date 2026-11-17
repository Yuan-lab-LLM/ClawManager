package handlers

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/historymaintenance"
	"clawreef/internal/repository"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
)

type historyArchiveReader interface {
	Get(context.Context, string, int64, int64, bool) (historymaintenance.Record, error)
	ListPage(context.Context, historymaintenance.Query) (historymaintenance.Page, error)
}
type HistoryArchiveHandler struct {
	reader historyArchiveReader
	admin  bool
}

// admin is fixed by the protected route group, never by a request parameter.
func NewHistoryArchiveHandler(reader *historymaintenance.Service, admin bool) *HistoryArchiveHandler {
	return &HistoryArchiveHandler{reader: reader, admin: admin}
}
func (h *HistoryArchiveHandler) scope(c *gin.Context) (int64, bool) {
	if h.reader == nil {
		utils.Error(c, 503, "History archive unavailable")
		return 0, false
	}
	user, exists := c.Get("userID")
	id, ok := user.(int)
	if !exists || !ok || id <= 0 {
		utils.Error(c, 401, "Unauthorized")
		return 0, false
	}
	if !h.admin {
		return int64(id), true
	}
	if raw := c.Query("user_id"); raw != "" {
		owner, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || owner <= 0 {
			utils.Error(c, 400, "Invalid owner")
			return 0, false
		}
		return owner, true
	}
	return 0, true
}
func (h *HistoryArchiveHandler) query(c *gin.Context) (historymaintenance.Query, bool) {
	owner, ok := h.scope(c)
	if !ok {
		return historymaintenance.Query{}, false
	}
	q := historymaintenance.Query{Kind: c.Param("kind"), OwnerID: owner, Admin: h.admin, Limit: 100, Until: time.Now().UTC()}
	q.Since = q.Until.AddDate(0, 0, -30)
	for _, field := range []struct {
		name  string
		value *time.Time
	}{{"since", &q.Since}, {"until", &q.Until}} {
		if raw := c.Query(field.name); raw != "" {
			at, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				utils.Error(c, 400, "Invalid history window")
				return q, false
			}
			*field.value = at.UTC()
		}
	}
	if !q.Until.After(q.Since) || q.Until.Sub(q.Since) > 90*24*time.Hour {
		utils.Error(c, 400, "History windows must be at most90days; continue exports with explicit windows")
		return q, false
	}
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			utils.Error(c, 400, "History pages require1..200rows")
			return q, false
		}
		q.Limit = n
	}
	for _, field := range []struct {
		name  string
		value *int64
	}{{"after_id", &q.AfterID}, {"instance_id", &q.InstanceID}, {"team_id", &q.TeamID}} {
		if raw := c.Query(field.name); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || n < 0 {
				utils.Error(c, 400, "Invalid history cursor or filter")
				return q, false
			}
			*field.value = n
		}
	}
	if _, err := historymaintenance.MonthlyTable(q.Kind, q.Until); err != nil {
		utils.Error(c, 400, "Unsupported history kind")
		return q, false
	}
	q.TraceID = strings.TrimSpace(c.Query("trace_id"))
	q.SessionID = strings.TrimSpace(c.Query("session_id"))
	return q, true
}
func historyReadError(c *gin.Context, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		utils.Error(c, 404, "History record not found")
	} else {
		utils.Error(c, 503, "History archive verification unavailable; original content was not substituted")
	}
}
func (h *HistoryArchiveHandler) List(c *gin.Context) {
	q, ok := h.query(c)
	if !ok {
		return
	}
	page, err := h.reader.ListPage(c.Request.Context(), q)
	if err != nil {
		historyReadError(c, err)
		return
	}
	for i := range page.Records {
		page.Records[i].Payload = nil
	}
	utils.Success(c, 200, "History archive page", page)
}
func (h *HistoryArchiveHandler) record(c *gin.Context) (historymaintenance.Record, bool) {
	owner, ok := h.scope(c)
	if !ok {
		return historymaintenance.Record{}, false
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		utils.Error(c, 400, "Invalid history identity")
		return historymaintenance.Record{}, false
	}
	if _, err = historymaintenance.MonthlyTable(c.Param("kind"), time.Now()); err != nil {
		utils.Error(c, 400, "Unsupported history kind")
		return historymaintenance.Record{}, false
	}
	r, err := h.reader.Get(c.Request.Context(), c.Param("kind"), id, owner, h.admin)
	if err != nil {
		historyReadError(c, err)
		return r, false
	}
	return r, true
}
func (h *HistoryArchiveHandler) Get(c *gin.Context) {
	r, ok := h.record(c)
	if !ok {
		return
	}
	if c.Query("include_body") == "true" {
		raw, err := repository.HydrateArchivedPayload(c.Request.Context(), r.Kind, r.Payload)
		if err != nil {
			historyReadError(c, err)
			return
		}
		if len(raw) > historymaintenance.MaxPageBytes {
			utils.Error(c, 413, "Detail exceeds8MiB; fetch bodies with the byte cursor endpoint")
			return
		}
		r.Payload = raw
	}
	utils.Success(c, 200, "Verified history record", r)
}
func (h *HistoryArchiveHandler) Export(c *gin.Context) {
	q, ok := h.query(c)
	if !ok {
		return
	}
	page, err := h.reader.ListPage(c.Request.Context(), q)
	if err != nil {
		historyReadError(c, err)
		return
	}
	var bytes int
	for i := range page.Records {
		if c.Query("include_body") == "false" {
			page.Records[i].Payload = nil
			continue
		}
		raw, e := repository.HydrateArchivedPayload(c.Request.Context(), page.Records[i].Kind, page.Records[i].Payload)
		if e != nil {
			historyReadError(c, e)
			return
		}
		if bytes+len(raw) > historymaintenance.MaxPageBytes {
			if i == 0 {
				utils.Error(c, 413, "Body exceeds8MiB; use explicit byte cursor or metadata export")
				return
			}
			page.Records = page.Records[:i]
			page.HasMore = true
			page.NextID = page.Records[i-1].ID
			break
		}
		page.Records[i].Payload = raw
		bytes += len(raw)
	}
	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Content-Disposition", `attachment; filename="history.ndjson"`)
	c.Header("X-History-Next-Cursor", strconv.FormatInt(page.NextID, 10))
	c.Header("X-History-Has-More", strconv.FormatBool(page.HasMore))
	c.Status(http.StatusOK)
	encoder := json.NewEncoder(c.Writer)
	for _, record := range page.Records {
		if err = encoder.Encode(record); err != nil {
			return
		}
	}
}
func (h *HistoryArchiveHandler) Body(c *gin.Context) {
	r, ok := h.record(c)
	if !ok {
		return
	}
	allowed := map[string][]string{"model_invocations": {"request_payload", "response_payload"}, "audit_events": {"details"}, "chat_messages": {"content"}, "risk_hits": {"match_summary"}, "team_events": {"payload_json"}, "team_tasks": {"payload_json", "result_json"}}
	field := c.Query("field")
	found := false
	for _, value := range allowed[r.Kind] {
		if field == value {
			found = true
		}
	}
	if !found {
		utils.Error(c, 400, "Unsupported history body field")
		return
	}
	offset := int64(0)
	limit := int64(1 << 20)
	for _, param := range []struct {
		name  string
		value *int64
	}{{"offset", &offset}, {"limit", &limit}} {
		if raw := c.Query(param.name); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || n < 0 {
				utils.Error(c, 400, "Invalid body byte cursor")
				return
			}
			*param.value = n
		}
	}
	if limit < 1 || limit > 1<<20 {
		utils.Error(c, 400, "Body pages require1..1048576bytes")
		return
	}
	raw, err := repository.HydrateArchivedPayload(c.Request.Context(), r.Kind, r.Payload)
	if err != nil {
		historyReadError(c, err)
		return
	}
	var payload map[string]json.RawMessage
	if err = json.Unmarshal(raw, &payload); err != nil {
		historyReadError(c, err)
		return
	}
	var body string
	if value := payload[field]; len(value) == 0 || string(value) == "null" {
		utils.Error(c, 404, "Body is absent; inspect record body_status for retention state")
		return
	} else if err = json.Unmarshal(value, &body); err != nil {
		historyReadError(c, err)
		return
	}
	total := int64(len(body))
	if offset > total {
		utils.Error(c, 416, "Body byte cursor exceeds content")
		return
	}
	end := offset + limit
	if end > total {
		end = total
	}
	utils.Success(c, 200, "Verified history body page", gin.H{"field": field, "offset": offset, "next_offset": end, "total_bytes": total, "has_more": end < total, "encoding": "base64", "data_base64": base64.StdEncoding.EncodeToString([]byte(body[offset:end]))})
}
