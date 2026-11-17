package handlers

import (
	"clawreef/internal/middleware"
	"clawreef/internal/services"
	"clawreef/internal/services/k8s"
	"clawreef/internal/utils"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntimePublicArtifactDownloadUsesTeamOwnerAuthorizationAndFullBytes(t *testing.T) {
	filename := os.Getenv("RUNTIME_PUBLIC_FIXTURE")
	if filename == "" {
		t.Skip("requires actual Runtime publication fixture")
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Event     map[string]interface{} `json:"event"`
		Body      string                 `json:"body"`
		SharedDir string                 `json:"sharedDir"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	reference, ok := fixture.Event["resultBodyRef"].(string)
	if !ok || !strings.HasPrefix(reference, "/team/results/") {
		t.Fatal("not a Team workspace reference")
	}
	relative := strings.TrimPrefix(reference, "/team/")
	actual, err := os.ReadFile(filepath.Join(fixture.SharedDir, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != fixture.Body {
		t.Fatal("Runtime artifact lost full bytes")
	}
	digest := sha256.Sum256(actual)
	if hex.EncodeToString(digest[:]) != fixture.Event["resultBodySHA256"] {
		t.Fatal("Runtime artifact digest mismatch")
	}
	base := t.TempDir()
	root := k8s.TeamSharedWorkspacePath(base, 9, 17)
	target := filepath.Join(root, filepath.FromSlash(relative))
	if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, actual, 0640); err != nil {
		t.Fatal(err)
	}
	handler := NewTeamHandler(services.NewTeamService(&handlerTeamTextRepo{}, nil, services.WithTeamRuntimeWorkspaceRoot(base)))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	t.Setenv("JWT_SECRET", "runtime-public-artifact-test-secret")
	router.Use(middleware.Auth())
	router.GET("/api/v1/teams/:id/workspace/download", handler.DownloadWorkspaceFile)
	for _, tc := range []struct {
		owner string
		code  int
	}{{"", 401}, {"8", 403}, {"9", 200}} {
		req := httptest.NewRequest("GET", "/api/v1/teams/17/workspace/download?path="+url.QueryEscape(relative), nil)
		if tc.owner != "" {
			owner := 8
			if tc.owner == "9" {
				owner = 9
			}
			token, err := utils.GenerateToken(utils.TokenClaims{UserID: owner, TokenType: "access"}, "runtime-public-artifact-test-secret", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Fatalf("owner=%s status=%d body=%s", tc.owner, w.Code, w.Body.String())
		}
		if tc.code == 200 && w.Body.String() != fixture.Body {
			t.Fatal("authenticated download did not return the exact complete Runtime body")
		}
	}
}
