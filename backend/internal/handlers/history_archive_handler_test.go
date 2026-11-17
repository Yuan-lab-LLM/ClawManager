package handlers

import (
	"clawreef/internal/historymaintenance"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeHistoryReader struct {
	owner int64
	admin bool
	query historymaintenance.Query
}

func (f *fakeHistoryReader) Get(_ context.Context, kind string, id, owner int64, admin bool) (historymaintenance.Record, error) {
	f.owner = owner
	f.admin = admin
	return historymaintenance.Record{Kind: kind, ID: id, OwnerID: owner, Payload: json.RawMessage(`{"id":7,"user_id":12,"request_payload":"abcdefgh","created_at":"2026-01-01T00:00:00Z"}`)}, nil
}
func (f *fakeHistoryReader) ListPage(_ context.Context, q historymaintenance.Query) (historymaintenance.Page, error) {
	f.query = q
	return historymaintenance.Page{Records: []historymaintenance.Record{}, NextID: 9, HasMore: true}, nil
}
func historyRouter(fake *fakeHistoryReader, admin bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &HistoryArchiveHandler{reader: fake, admin: admin}
	r.Use(func(c *gin.Context) { c.Set("userID", 12); c.Next() })
	r.GET("/history/:kind", h.List)
	r.GET("/history/:kind/export", h.Export)
	r.GET("/history/:kind/:id", h.Get)
	r.GET("/history/:kind/:id/body", h.Body)
	return r
}
func TestHistoryHandlerOwnerCannotBeOverridden(t *testing.T) {
	fake := &fakeHistoryReader{}
	r := historyRouter(fake, false)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/history/model_invocations/7?user_id=99&admin=true", nil))
	if w.Code != 200 || fake.owner != 12 || fake.admin {
		t.Fatalf("authorization scope %d %+v", w.Code, fake)
	}
}
func TestHistoryHandlerBoundsAndExportCursor(t *testing.T) {
	fake := &fakeHistoryReader{}
	r := historyRouter(fake, false)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/history/model_invocations?limit=201", nil))
	if w.Code != 400 {
		t.Fatalf("unboundedlimit%d", w.Code)
	}
	w = httptest.NewRecorder()
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	url := "/history/model_invocations/export?since=" + since.Format(time.RFC3339) + "&until=" + since.Add(24*time.Hour).Format(time.RFC3339)
	r.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
	if w.Code != 200 || w.Header().Get("X-History-Next-Cursor") != "9" || w.Header().Get("X-History-Has-More") != "true" {
		t.Fatalf("export cursor%d %v", w.Code, w.Header())
	}
}
func TestHistoryBodyUsesExplicitByteCursor(t *testing.T) {
	fake := &fakeHistoryReader{}
	r := historyRouter(fake, false)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/history/model_invocations/7/body?field=request_payload&offset=2&limit=3", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), base64.StdEncoding.EncodeToString([]byte("cde"))) {
		t.Fatal(w.Body.String())
	}
}
