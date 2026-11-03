package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/services"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type previewSignerFunc func(context.Context, runtimeauth.Identity, string, string) (services.ManagedTeamPreviewGrant, error)

func (f previewSignerFunc) Sign(c context.Context, id runtimeauth.Identity, p, m string) (services.ManagedTeamPreviewGrant, error) {
	return f(c, id, p, m)
}
func TestManagedPreviewFixedOperation(t *testing.T) {
	for _, body := range []string{`{"path":"site/index.html","mode":"interactive"}`, `{"path":"site/index.html","mode":"interactive","team":99}`, `{"path":"a","mode":"static"} {}`, strings.Repeat("x", 4097)} {
		calls := 0
		signer := previewSignerFunc(func(ctx context.Context, id runtimeauth.Identity, path, mode string) (services.ManagedTeamPreviewGrant, error) {
			calls++
			if id != egressCapabilityIdentity || path != "site/index.html" || mode != "interactive" {
				t.Fatal("caller identity/path changed")
			}
			return services.ManagedTeamPreviewGrant{URL: "http://clawmanager-egress-proxy.ns.svc:3128/v3/test", Mode: mode, ExpiresAt: time.Now().Add(time.Minute)}, nil
		})
		r := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/services/team/preview-sign", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		op, err := NewRuntimeTeamPreviewOperation(signer, egressCapabilityIdentity, r)
		rec := httptest.NewRecorder()
		if err == nil {
			op.Serve(rec, r)
		}
		if strings.Contains(body, `"team"`) || strings.Contains(body, "} {") || len(body) > 4096 {
			if calls != 0 {
				t.Fatal("invalid request reached signer")
			}
			continue
		}
		if err != nil || rec.Code != 200 || calls != 1 {
			t.Fatalf("valid operation failed: %v %d %d", err, rec.Code, calls)
		}
		var value services.ManagedTeamPreviewGrant
		if json.Unmarshal(rec.Body.Bytes(), &value) != nil || value.Mode != "interactive" {
			t.Fatal("wrong grant response")
		}
	}
}
func TestManagedPreviewLegacyStickyMarkerDeniesHistoricalLinks(t *testing.T) {
	for _, marker := range []string{models.TeamPreviewSecurityProfile, "unknown-future"} {
		h, target, repo := protectedPreviewFixture(t, nil)
		repo.team.PreviewSecurityProfile = marker
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", target, nil)
		rec := httptest.NewRecorder()
		c, _ = gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("GET", target, nil)
		h.Handle(c)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("sticky marker %q allowed historical v1: %d", marker, rec.Code)
		}
	}
}

func TestManagedPreviewOperationClosesIncompleteBody(t *testing.T) {
	calls := atomic.Int32{}
	signer := previewSignerFunc(func(context.Context, runtimeauth.Identity, string, string) (services.ManagedTeamPreviewGrant, error) {
		calls.Add(1)
		return services.ManagedTeamPreviewGrant{}, nil
	})
	closed := make(chan struct{})
	var once sync.Once
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		op, err := NewRuntimeTeamPreviewOperation(signer, egressCapabilityIdentity, r)
		if err != nil {
			t.Error(err)
			return
		}
		op.Serve(w, r)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			once.Do(func() { close(closed) })
		}
	}
	server.Start()
	defer server.Close()
	client, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err = fmt.Fprint(client, "POST /api/v1/runtime/services/team/preview-sign HTTP/1.1\r\nHost: test\r\nContent-Type: application/json\r\nContent-Length: 1024\r\n\r\n{"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-time.After(4 * time.Second):
		t.Fatal("incomplete signing body retained HTTP connection after read deadline")
	}
	if calls.Load() != 0 {
		t.Fatal("incomplete body reached signer")
	}
}
