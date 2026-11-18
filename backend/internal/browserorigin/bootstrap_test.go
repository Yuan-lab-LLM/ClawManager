package browserorigin

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestBootstrapDocument(t *testing.T) {
	for _, location := range []string{"/app?q=1", "/a%2Fb?q=1#part", "/?q=</script><script>alert(1)</script>"} {
		r := httptest.NewRequest("GET", "https://cm.test:31400/?token=cap", nil)
		r.Header.Set("Sec-Fetch-Dest", "iframe")
		w := httptest.NewRecorder()
		if !ServeBootstrapDocument(w, r, location) || w.Code != 200 || w.Header().Get("Location") != "" {
			t.Fatalf("bootstrap response: %d %v", w.Code, w.Header())
		}
		body := w.Body.String()
		start, end := strings.Index(body, "<script>"), strings.Index(body, "</script>")
		if start < 0 || end < start || strings.Count(body, "<script>") != 1 || strings.Count(body, "</script>") != 1 {
			t.Fatalf("unsafe script: %s", body)
		}
		script := body[start+len("<script>") : end]
		hash := sha256.Sum256([]byte(script))
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'sha256-"+base64.StdEncoding.EncodeToString(hash[:])+"'") || strings.Contains(w.Header().Get("Content-Security-Policy"), "unsafe-inline") {
			t.Fatal("bootstrap script not protected by CSP hash")
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatal(w.Header())
		}
		if output := os.Getenv("CM_BROWSER_BOOTSTRAP_OUTPUT"); output != "" && (location == "/a%2Fb?q=1#part" || location == "/app?q=1") {
			if location == "/app?q=1" {
				output += ".inherited"
			}
			data, _ := json.Marshal(map[string]any{"headers": w.Header(), "body": body})
			if err := os.WriteFile(output, data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestBootstrapRejectsForeignLocations(t *testing.T) {
	for _, location := range []string{"", "relative", "//evil.test/", "https://evil.test/", "/\\evil.test/", "/%5cevil.test/", "/%2f%2fevil.test/", "/\n/evil.test/"} {
		r := httptest.NewRequest("GET", "https://cm.test:31400/", nil)
		r.Header.Set("Sec-Fetch-Dest", "document")
		w := httptest.NewRecorder()
		if !ServeBootstrapDocument(w, r, location) || w.Code != 400 || strings.Contains(w.Body.String(), "<script>") {
			t.Fatalf("unsafe location %q: %d %s", location, w.Code, w.Body.String())
		}
	}
}

func TestBootstrapLeavesNonDocumentRequestsAlone(t *testing.T) {
	for _, tc := range []struct{ method, dest string }{{"HEAD", "iframe"}, {"POST", "document"}, {"GET", ""}, {"GET", "empty"}, {"GET", "image"}} {
		r := httptest.NewRequest(tc.method, "https://cm.test:31400/", nil)
		r.Header.Set("Sec-Fetch-Dest", tc.dest)
		w := httptest.NewRecorder()
		if ServeBootstrapDocument(w, r, "/") || w.Body.Len() != 0 || len(w.Header()) != 0 {
			t.Fatalf("changed non-document %+v", tc)
		}
	}
}
