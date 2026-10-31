package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestOpenCodeBrowserStorageScopeFollowsInstanceCreation(t *testing.T) {
	workspace := "/workspaces/opencode/user-1/instance-2"
	instance := models.Instance{ID: 2, UserID: 1, CreatedAt: time.Date(2026, 10, 13, 1, 55, 45, 0, time.UTC), WorkspacePath: &workspace}
	scope := openCodeBrowserStorageScope(&instance)
	instance.Name = "renamed"
	instance.RuntimeGeneration = 7
	instance.UpdatedAt = instance.CreatedAt.Add(time.Hour)
	if got := openCodeBrowserStorageScope(&instance); got != scope {
		t.Fatal("rename or restart must not discard this instance's browser state")
	}
	for _, change := range []struct {
		name  string
		apply func(*models.Instance)
	}{
		{"new instance", func(i *models.Instance) { i.ID++ }},
		{"different owner", func(i *models.Instance) { i.UserID++ }},
		{"recreated ID", func(i *models.Instance) { i.CreatedAt = i.CreatedAt.Add(time.Second) }},
		{"different workspace", func(i *models.Instance) { w := workspace + "-new"; i.WorkspacePath = &w }},
	} {
		t.Run(change.name, func(t *testing.T) {
			copy := instance
			change.apply(&copy)
			if openCodeBrowserStorageScope(&copy) == scope {
				t.Fatal("unrelated instance shares browser state")
			}
		})
	}
}

func TestOpenCodeBrowserStorageBootstrapRunsBeforeApplication(t *testing.T) {
	html := `<!doctype html><html><HEAD class="main"><script>window.app=true</script></HEAD></html>`
	result := injectOpenCodeBrowserStorageScope(html, "test-scope")
	if strings.Index(result, "clawmanager-opencode-storage-scope") > strings.Index(result, "window.app=true") {
		t.Fatal("storage must be isolated before upstream application reads it")
	}
	if again := injectOpenCodeBrowserStorageScope(result, "test-scope"); again != result {
		t.Fatal("bootstrap must not be injected twice")
	}
}

func TestOpenCodeBrowserStorageProxyIsolatesDedicatedHTMLOnly(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"upstream"`)
		if r.URL.Path == "/" {
			if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
				t.Error("conditional HTML request can bypass browser storage bootstrap")
			}
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><script src="/_assets/app.js"></script></head></html>`))
			return
		}
		if r.Header.Get("If-None-Match") != `"upstream"` {
			t.Error("asset caching unexpectedly disabled")
		}
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(`window.asset=true;`))
	}))
	defer upstream.Close()
	service, token := newOpenCodeV2ProxyTestService(t, upstream.URL, 170, "igt_test_only")
	service.httpClient = upstream.Client()
	for _, path := range []string{"/", "/_assets/app.js"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/170/proxy"+path, nil)
		req.Header.Set(DedicatedRuntimeOriginHeader, RuntimeTypeOpenCode)
		req.Header.Set("If-None-Match", `"upstream"`)
		req.Header.Set("If-Modified-Since", "Tue, 13 Oct 2026 00:00:00 GMT")
		rec := httptest.NewRecorder()
		if err := service.ProxyRequest(req.Context(), 170, token.Token, rec, req); err != nil {
			t.Fatal(err)
		}
		if path == "/" {
			if !strings.Contains(rec.Body.String(), "clawmanager-opencode-storage-scope") {
				t.Fatal("dedicated origin missing storage isolation")
			}
			if strings.Contains(rec.Body.String(), "igt_test_only") {
				t.Fatal("credential leaked into browser HTML")
			}
			if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("ETag") != "" {
				t.Fatal("instance HTML must not reuse upstream validators")
			}
			if !strings.Contains(rec.Body.String(), `src="/_assets/app.js"`) {
				t.Fatal("dedicated-origin asset path was rewritten")
			}
		} else if rec.Body.String() != `window.asset=true;` || rec.Header().Get("ETag") != `"upstream"` {
			t.Fatal("asset response should retain normal caching and contents")
		}
	}
}
