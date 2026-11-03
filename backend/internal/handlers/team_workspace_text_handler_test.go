package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type handlerTeamTextRepo struct{ repository.TeamRepository }

func (*handlerTeamTextRepo) GetTeamByID(id int) (*models.Team, error) {
	if id != 17 {
		return nil, nil
	}
	return &models.Team{ID: 17, UserID: 9}, nil
}

type handlerTeamAudit struct {
	entries []*models.TeamWorkspaceFileAudit
	fail    bool
}

func (r *handlerTeamAudit) Create(_ context.Context, a *models.TeamWorkspaceFileAudit) error {
	r.entries = append(r.entries, a)
	return nil
}
func TestTeamWorkspaceTextHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := t.TempDir()
	root := k8s.TeamSharedWorkspacePath(base, 9, 17)
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("original"), 0640); err != nil {
		t.Fatal(err)
	}
	audit := &handlerTeamAudit{}
	handler := NewTeamHandler(services.NewTeamService(&handlerTeamTextRepo{}, nil, services.WithTeamRuntimeWorkspaceRoot(base), services.WithTeamWorkspaceAuditRepository(audit)))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if c.GetHeader("Test-Owner") == "9" {
			c.Set("userID", 9)
		} else if c.GetHeader("Test-Owner") == "8" {
			c.Set("userID", 8)
		}
	})
	router.GET("/teams/:id/text", handler.WorkspaceText)
	router.PUT("/teams/:id/text", handler.WorkspaceText)
	req := func(method, url, body, owner string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Test-Owner", owner)
		router.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		url, owner string
		code       int
	}{{"/teams/17/text?path=a.md", "", 401}, {"/teams/17/text?path=a.md", "8", 403}, {"/teams/18/text?path=a.md", "9", 404}, {"/teams/17/text?path=../a.md", "9", 403}, {"/teams/17/text?path=missing.md", "9", 404}, {"/teams/17/text?path=a.png", "9", 422}} {
		w := req("GET", tc.url, "", tc.owner)
		if w.Code != tc.code {
			t.Fatalf("%s owner%s: %d %s", tc.url, tc.owner, w.Code, w.Body.String())
		}
	}
	w := req("GET", "/teams/17/text?path=a.md", "", "9")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Data services.WorkspaceText `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	save := func(text, version string) *httptest.ResponseRecorder {
		data, _ := json.Marshal(map[string]string{"path": "a.md", "text": text, "version": version})
		return req("PUT", "/teams/17/text", string(data), "9")
	}
	if w = save("", response.Data.Version); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(audit.entries) != 1 || audit.entries[0].TeamID != 17 || audit.entries[0].UserID != 9 || audit.entries[0].Action != "edit_attempt" {
		t.Fatal(audit.entries)
	}
	if w = save("stale", response.Data.Version); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = save(strings.Repeat("a", 1<<20+1), response.Data.Version); w.Code != 413 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = save("\x00", response.Data.Version); w.Code != 422 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = req("PUT", "/teams/17/text", `{"path":"a.md"}`, "9"); w.Code != 400 {
		t.Fatal(w.Code)
	}
}
