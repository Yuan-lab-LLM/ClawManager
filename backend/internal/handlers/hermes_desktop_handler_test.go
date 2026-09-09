package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHermesDesktopOriginGuard(t *testing.T) {
	for _, tc := range []struct {
		name, origin, site string
		mutation, want     bool
	}{
		{"same origin", "https://manager.example", "same-origin", true, true},
		{"foreign host", "https://evil.example", "cross-site", true, false},
		{"sibling subdomain", "https://other.manager.example", "same-site", true, false},
		{"null", "null", "", true, false},
		{"no Origin mutation", "", "same-origin", true, false},
		{"same origin read", "", "same-origin", false, true},
		{"unmarked read", "", "", false, false},
		{"downgrade", "http://manager.example", "same-origin", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "https://manager.example/api", nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			if got := hermesDesktopOriginAllowed(r, tc.mutation); got != tc.want {
				t.Fatalf("allowed=%v", got)
			}
		})
	}
}

func TestHermesDesktopLogoutHook(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, fail := range []bool{false, true} {
		called := 0
		handler := NewAuthHandler(nil)
		handler.SetDesktopLogoutHook(func(ctx context.Context, userID int) error {
			called++
			if userID != 45 {
				t.Error("logout user missing")
			}
			if fail {
				return errors.New("offline")
			}
			return nil
		})
		router := gin.New()
		router.POST("/logout", func(c *gin.Context) { c.Set("userID", 45); handler.Logout(c) })
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("POST", "/logout", nil))
		want := http.StatusOK
		if fail {
			want = http.StatusServiceUnavailable
		}
		if recorder.Code != want || called != 1 {
			t.Fatalf("status=%d called=%d", recorder.Code, called)
		}
	}
}

func TestHermesDesktopTicketRedactedBeforeLogging(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var logged string
	r.Use(HermesDesktopRedactTickets(), func(c *gin.Context) { logged = c.Request.URL.String(); c.Next() })
	r.GET("/api/v1/instances/:id/hermes-desktop/ws", func(c *gin.Context) {
		if c.GetString("hermesDesktopTicket") != "test-secret-ticket" {
			t.Error("missing request-local ticket")
		}
		c.Status(http.StatusNoContent)
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/instances/123/hermes-desktop/ws?ticket=test-secret-ticket&token=another-secret", nil))
	if rec.Code != 204 || strings.Contains(logged, "secret") {
		t.Fatalf("status=%d log=%s", rec.Code, logged)
	}
}

func TestHermesDesktopRecoveryDoesNotDumpCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logs bytes.Buffer
	router := gin.New()
	router.Use(HermesDesktopRedactTickets(), gin.RecoveryWithWriter(&logs))
	router.GET("/api/v1/instances/:id/hermes-desktop/ws", func(c *gin.Context) {
		if strings.Contains(c.Request.RequestURI, "private-ws-ticket") || c.GetHeader("Cookie") != "" {
			t.Error("raw request still contains credentials")
		}
		if c.GetString("hermesDesktopCookie:cm_hermes_desktop_123") != "private-cm-cookie" {
			t.Error("BFF scoped cookie was not preserved in request-local context")
		}
		panic("test recovery")
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/api/v1/instances/123/hermes-desktop/ws?ticket=private-ws-ticket", nil)
	request.AddCookie(&http.Cookie{Name: "cm_hermes_desktop_123", Value: "private-cm-cookie"})
	router.ServeHTTP(recorder, request)
	if recorder.Code != 500 || strings.Contains(logs.String(), "private-ws-ticket") || strings.Contains(logs.String(), "private-cm-cookie") {
		t.Fatalf("unsafe recovery log/status: status=%d log=%s", recorder.Code, logs.String())
	}
}
