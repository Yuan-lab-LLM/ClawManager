package services

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"clawreef/internal/browserorigin"
	"clawreef/internal/models"
	"clawreef/internal/workbench"
)

type sharedPreviewResolver struct {
	mu     sync.RWMutex
	access *models.InstanceExternalAccess
	err    error
}

func (s *sharedPreviewResolver) ResolveShortLink(context.Context, string) (*models.InstanceExternalAccess, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.access == nil {
		return nil, s.err
	}
	v := *s.access
	return &v, s.err
}

func TestSharedWorkbenchPreviewRevokesAcrossBothOriginModes(t *testing.T) {
	for _, browserMode := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "single-host"}[browserMode], func(t *testing.T) {
			ctx := context.Background()
			endpoint := "https://runtime.test"
			instance := &models.Instance{ID: 42, UserID: 7, Status: "running", RuntimeGeneration: 3}
			binding := &models.InstanceRuntimeBinding{InstanceID: 42, RuntimePodID: 9, Generation: 3, GatewayID: "gw-42-3", GatewayPort: 3000}
			previews, _ := workbench.NewPreviews("https://p-{token}.preview.test", &previewTestStore{values: map[string]workbench.PreviewLease{}}, time.Hour)
			agent := &workbenchTestAgent{}
			service := NewInstanceWorkbenchService(workbenchTestInstances{instance: instance}, workbenchTestBindings{binding}, workbenchTestPods{&models.RuntimePod{ID: 9, AgentEndpoint: &endpoint}}, agent, previews)
			access := &models.InstanceExternalAccess{InstanceID: 42, Enabled: true, AuthMode: ExternalAccessModeShareLink, WorkspaceAccess: ExternalWorkspaceAccessWrite}
			resolver := &sharedPreviewResolver{access: access}
			service.SetSharedPreviewAccess(resolver)
			if browserMode {
				origins, _ := browserorigin.New("https://cm.test:30443", "31400-31401", &browserPreviewStore{})
				service.SetBrowserOrigins(origins)
			}
			shared := workbench.SharedPreviewAccess{Code: "sl_test", SessionBinding: ExternalAccessSessionBinding("sl_test", access), WorkspaceAccess: ExternalWorkspaceAccessWrite, ExpiresAt: time.Now().Add(5 * time.Minute)}
			if _, err := service.CreateSharedPreview(ctx, 8, 42, 3, "localhost:4000", shared); !errors.Is(err, ErrWorkbenchDenied) {
				t.Fatalf("shared preview substituted wrong owner: %v", err)
			}
			if _, err := service.CreateSharedPreview(ctx, 7, 99, 3, "localhost:4000", shared); !errors.Is(err, ErrWorkbenchDenied) {
				t.Fatalf("shared preview substituted wrong instance: %v", err)
			}
			result, err := service.CreateSharedPreview(ctx, 7, 42, 3, "localhost:4000", shared)
			if err != nil {
				t.Fatal(err)
			}
			address, _ := url.Parse(result["url"].(string))
			request := httptest.NewRequest("GET", address.String(), nil)
			lease, _, err := service.previewRequestTarget(ctx, request)
			if err != nil || lease.Shared == nil || lease.ExpiresAt.After(shared.ExpiresAt) {
				t.Fatalf("shared scope/expiry missing: %+v %v", lease, err)
			}
			for _, mutation := range []string{"disabled", "expired", "reset", "password", "downgraded", "none", "wrong instance", "resolver error"} {
				t.Run(mutation, func(t *testing.T) {
					changed := *access
					resolver.access = &changed
					resolver.err = nil
					switch mutation {
					case "disabled":
						changed.Enabled = false
					case "expired":
						past := time.Now().Add(-time.Minute)
						changed.ExpiresAt = &past
					case "reset":
						resolver.err = errors.New("reset link")
					case "password":
						changed.AuthMode = ExternalAccessModePassword
						hash := "new hash"
						changed.PasswordHash = &hash
					case "downgraded":
						changed.WorkspaceAccess = ExternalWorkspaceAccessRead
					case "none":
						changed.WorkspaceAccess = ExternalWorkspaceAccessNone
					case "wrong instance":
						changed.InstanceID = 99
					case "resolver error":
						resolver.err = errors.New("unavailable")
					}
					if _, _, e := service.previewRequestTarget(ctx, request); !errors.Is(e, workbench.ErrPreviewNotFound) {
						t.Fatalf("revoked preview admitted: %v", e)
					}
				})
			}
			resolver.access = access
			resolver.err = nil
			if _, _, e := service.previewRequestTarget(ctx, request); e != nil {
				t.Fatalf("valid share denied: %v", e)
			}
			// The runtime receives only owner/generation routing, never share authority.
			for _, req := range agent.requests {
				if req.PreviewScope.UserID != 7 || req.PreviewScope.InstanceID != 42 {
					t.Fatalf("wrong runtime scope: %+v", req)
				}
			}
		})
	}
}

func TestSharedWorkbenchScopeChangesWithWorkspacePermission(t *testing.T) {
	access := &models.InstanceExternalAccess{AuthMode: ExternalAccessModeShareLink, WorkspaceAccess: ExternalWorkspaceAccessRead}
	first := SharedWorkbenchScope("sl_test", access)
	access.WorkspaceAccess = ExternalWorkspaceAccessWrite
	if len(first) != 64 || first == SharedWorkbenchScope("sl_test", access) || first == SharedWorkbenchScope("other", access) {
		t.Fatal("workbench persistence scope is not isolated")
	}
}

func TestSharedWorkbenchPreviewClosesRevokedWebSocket(t *testing.T) {
	for _, browserMode := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "single-host"}[browserMode], func(t *testing.T) {
			closed := make(chan struct{})
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				encoded, e := base64.RawURLEncoding.DecodeString(r.Header.Get(workbenchPreviewHeader))
				if e != nil || strings.Contains(string(encoded), "sl_test") || strings.Contains(string(encoded), "session_binding") {
					t.Error("share authority leaked into runtime route")
				}
				conn, e := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
				if e != nil {
					return
				}
				defer conn.Close()
				_, _, _ = conn.ReadMessage()
				close(closed)
			}))
			defer runtime.Close()
			instance := &models.Instance{ID: 42, UserID: 7, Status: "running", RuntimeGeneration: 3}
			binding := &models.InstanceRuntimeBinding{InstanceID: 42, RuntimePodID: 9, Generation: 3, GatewayID: "gw-42-3", GatewayPort: 3000}
			previews, _ := workbench.NewPreviews("https://p-{token}.preview.test", &previewTestStore{values: map[string]workbench.PreviewLease{}}, time.Hour)
			service := NewInstanceWorkbenchService(workbenchTestInstances{instance: instance}, workbenchTestBindings{binding}, workbenchTestPods{&models.RuntimePod{ID: 9, AgentEndpoint: &runtime.URL}}, NewRuntimeAgentClientWithHTTPClient("test", runtime.Client()).(WorkbenchAgent), previews)
			service.previewRecheckInterval = 10 * time.Millisecond
			access := &models.InstanceExternalAccess{InstanceID: 42, Enabled: true, AuthMode: ExternalAccessModeShareLink, WorkspaceAccess: ExternalWorkspaceAccessRead}
			resolver := &sharedPreviewResolver{access: access}
			service.SetSharedPreviewAccess(resolver)
			shared := workbench.SharedPreviewAccess{Code: "sl_test", SessionBinding: ExternalAccessSessionBinding("sl_test", access), WorkspaceAccess: ExternalWorkspaceAccessRead, ExpiresAt: time.Now().Add(time.Minute)}
			lease, e := previews.CreateShared(context.Background(), workbench.PreviewScope{InstanceID: 42, UserID: 7, Generation: 3, GatewayID: "gw-42-3"}, "localhost:4000", shared)
			if e != nil {
				t.Fatal(e)
			}
			address, _ := url.Parse(previews.URL(lease))
			headers := http.Header{"Host": []string{address.Host}}
			if browserMode {
				origins, _ := browserorigin.New("https://cm.test:30443", "31400-31401", &browserPreviewStore{})
				service.SetBrowserOrigins(origins)
				route, e := origins.Bind(context.Background(), 42, browserorigin.Preview, browserPreviewTargetKey(lease.Target))
				if e != nil {
					t.Fatal(e)
				}
				origin := origins.URL(route)
				address, _ = url.Parse(origin)
				headers.Set("Host", address.Host)
				headers.Set("Origin", strings.TrimSuffix(origin, "/"))
				headers.Set("Sec-Fetch-Site", "same-origin")
				headers.Set("Cookie", browserPreviewCookie(route.Port)+"="+lease.ID)
			}
			proxy := httptest.NewServer(http.HandlerFunc(service.ServePreview))
			defer proxy.Close()
			conn, _, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/ws", headers)
			if e != nil {
				t.Fatal(e)
			}
			defer conn.Close()
			resolver.mu.Lock()
			changed := *access
			changed.Enabled = false
			resolver.access = &changed
			resolver.mu.Unlock()
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, _, e = conn.ReadMessage()
			requirePreviewWebSocketClosed(t, e)
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("revoked runtime stream stayed open")
			}
		})
	}
}
