package middleware

import (
	"context"
	"net/http/httptest"
	"testing"

	"clawreef/internal/backupgate"
	"github.com/gin-gonic/gin"
)

func TestBackupBoundaryBlocksReadsWithSideEffectsAndPreservesControl(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(backupRequestBoundary("installation", func(context.Context) (func(), error) { return nil, backupgate.ErrMaintenance }))
	r.GET("/api/v1/version", func(c *gin.Context) { c.Status(204) })
	r.GET("/api/v1/admin/system-backup/overview", func(c *gin.Context) { c.Status(204) })
	r.GET("/api/v1/teams/:id/files", func(c *gin.Context) { t.Error("business handler entered maintenance"); c.Status(204) })
	for _, tc := range []struct {
		path   string
		status int
	}{{"/api/v1/version", 204}, {"/api/v1/admin/system-backup/overview", 204}, {"/api/v1/teams/1/files", 503}, {"/api/v1/admin/system-backup/unknown", 503}} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.status {
			t.Fatal(tc, w.Code)
		}
	}
}
func TestBackupBoundaryReleasesOnlyAfterHandlerReturnsOrPanics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, panics := range []bool{false, true} {
		released := false
		r := gin.New()
		r.Use(gin.Recovery(), backupRequestBoundary("installation", func(context.Context) (func(), error) { return func() { released = true }, nil }))
		r.GET("/write", func(c *gin.Context) {
			if released {
				t.Error("released before handler finished")
			}
			if panics {
				panic("fixture")
			}
			c.Status(204)
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/write", nil))
		if !released {
			t.Fatal("leaked handler fence")
		}
	}
}
