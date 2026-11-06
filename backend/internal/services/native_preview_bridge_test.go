package services

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestNativePreviewBridgeOnlyAuthenticatedBrowserHTML(t *testing.T) {
	for _, native := range []string{RuntimeTypeOpenCode, RuntimeTypeDeepSeekHarness, RuntimeTypeOpenClaw} {
		for _, test := range []struct {
			name, contentType, body, dest string
			gzip                          bool
			status                        int
			bridge                        bool
		}{
			{"document", "text/html", "<html><head></head><body>Native UI</body></html>", "iframe", false, 200, true},
			{"gzip document", "text/html", "<html><head></head><body>Native UI</body></html>", "iframe", true, 200, true},
			{"asset", "application/javascript", "window.asset=true", "script", false, 200, false},
			{"HTML asset fallback", "text/html", "<html>Asset missing</html>", "script", false, 200, false},
			{"API", "application/json", `{"healthy":true}`, "empty", false, 200, false},
			{"error", "text/html", "<html>Forbidden</html>", "iframe", false, 403, false},
		} {
			t.Run(native+"/"+test.name, func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", test.contentType)
					w.Header().Set("Content-Security-Policy", "default-src 'self'")
					if test.gzip {
						w.Header().Set("Content-Encoding", "gzip")
					}
					w.WriteHeader(test.status)
					if test.gzip {
						z := gzip.NewWriter(w)
						_, _ = z.Write([]byte(test.body))
						_ = z.Close()
					} else {
						_, _ = w.Write([]byte(test.body))
					}
				}))
				defer upstream.Close()
				var service *InstanceProxyService
				var token *AccessToken
				if native == RuntimeTypeOpenCode {
					service, token = newOpenCodeV2ProxyTestService(t, upstream.URL, 170, "fixture-only")
				} else if native == RuntimeTypeDeepSeekHarness {
					service, token = newDeepSeekHarnessV2ProxyTestService(t, upstream.URL, 170, "fixture-only")
				} else {
					service, token = newOpenClawV2ProxyTestService(t, upstream.URL, 170)
				}
				service.httpClient = upstream.Client()
				target := "/"
				if test.dest != "iframe" {
					target = "/assets/test"
				}
				req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/170/proxy"+target, nil)
				req.Header.Set(DedicatedRuntimeOriginHeader, native)
				req.Header.Set("Sec-Fetch-Dest", test.dest)
				rec := httptest.NewRecorder()
				if err := service.ProxyRequest(req.Context(), 170, token.Token, rec, req); err != nil {
					t.Fatal(err)
				}
				got := rec.Body.String()
				if strings.Contains(got, `id="clawmanager-native-preview-bridge"`) != test.bridge {
					t.Fatalf("bridge presence=%v, want %v", strings.Contains(got, "clawmanager-native-preview-bridge"), test.bridge)
				}
				if test.bridge {
					if rec.Header().Get("Content-Encoding") != "" {
						t.Fatal("rewritten HTML retains stale encoding")
					}
					if rec.Header().Get("Content-Length") != strconv.Itoa(rec.Body.Len()) {
						t.Fatal("rewritten HTML length mismatch")
					}
					if rec.Header().Get("Content-Security-Policy") != "" {
						t.Fatal("existing native HTML CSP policy unexpectedly changed")
					}
				}
			})
		}
	}
}

func TestNativePreviewBridgeNegotiatesGzipAndPreservesUnsupportedEncoding(t *testing.T) {
	const opaque = "opaque compressed HTML bytes"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "gzip" {
			t.Error("browser br/zstd must not be forwarded for HTML rewriting")
		}
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Encoding", "br")
		_, _ = w.Write([]byte(opaque))
	}))
	defer upstream.Close()
	service, token := newOpenCodeV2ProxyTestService(t, upstream.URL, 170, "fixture-only")
	service.httpClient = upstream.Client()
	req := httptest.NewRequest("GET", "/api/v1/instances/170/proxy/", nil)
	req.Header.Set(DedicatedRuntimeOriginHeader, RuntimeTypeOpenCode)
	req.Header.Set("Sec-Fetch-Dest", "iframe")
	req.Header.Set("Accept-Encoding", "br, zstd, gzip")
	rec := httptest.NewRecorder()
	if err := service.ProxyRequest(req.Context(), 170, token.Token, rec, req); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || rec.Header().Get("Content-Encoding") != "br" || rec.Body.String() != opaque {
		t.Fatal("unsupported encoding must pass through without consumption or corruption")
	}
}

func TestNativePreviewHTMLBoundAndIdempotentInjection(t *testing.T) {
	response := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", nativePreviewHTMLLimit+1)))}
	if _, err := readNativeProxyHTML(response, true); err == nil {
		t.Fatal("oversized HTML must be bounded")
	}
	html := injectNativePreviewBridge("<html><head><script>app()</script></head></html>")
	if injectNativePreviewBridge(html) != html || strings.Index(html, "clawmanager-native-preview-bridge") > strings.Index(html, "app()") {
		t.Fatal("bridge must precede app scripts and be injected once")
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Sec-Fetch-Dest", "iframe")
	if nativePreviewBridgeDocument(req, "shell") {
		t.Fatal("arbitrary app/preview responses must never receive native bridge")
	}
}
