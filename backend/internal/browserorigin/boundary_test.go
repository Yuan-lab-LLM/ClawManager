package browserorigin

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserNativeBoundary(t *testing.T) {
	for _, tt := range []struct {
		name, site, origin, dest, method string
		bootstrap, want                  bool
	}{
		{"same origin", "same-origin", "https://cm.test:31400", "empty", "POST", false, true},
		{"root assets", "same-origin", "", "script", "GET", false, true},
		{"other port", "same-site", "https://cm.test:31401", "empty", "POST", false, false},
		{"missing origin image", "same-site", "", "image", "GET", false, false},
		{"forged origin", "same-origin", "https://evil.test", "empty", "POST", false, false},
		{"missing browser context", "", "", "", "GET", false, false},
		{"manager bootstrap", "same-site", "", "iframe", "GET", true, true},
		{"manager renewal", "same-site", "https://cm.test:30443", "empty", "GET", true, true},
		{"external bootstrap", "cross-site", "https://evil.test", "iframe", "GET", true, false},
		{"curl bearer bootstrap", "", "", "", "GET", true, true},
		{"cookie only bootstrap", "same-site", "", "iframe", "GET", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, "https://cm.test:31400/", nil)
			r.Header.Set("Sec-Fetch-Site", tt.site)
			r.Header.Set("Origin", tt.origin)
			r.Header.Set("Sec-Fetch-Dest", tt.dest)
			if got := AllowNativeRequest(r, "https://cm.test:31400", "https://cm.test:30443", tt.bootstrap); got != tt.want {
				t.Fatalf("allowed=%v want=%v", got, tt.want)
			}
		})
	}
}
func TestBrowserPlatformCookieFilter(t *testing.T) {
	h := httptest.NewRequest("GET", "https://cm.test:31400/", nil).Header
	h.Set("Cookie", "ordinary=ok; instance_access_118=private; instance_access_45=other; clawmanager_iei_session=manager; cm_preview_31400=cap; cm_hermes_desktop_25=desktop; share_link_access_code=shared; other=value")
	FilterPlatformCookies(h)
	if got := h.Get("Cookie"); got != "ordinary=ok; other=value" {
		t.Fatal(got)
	}
	h.Set("Cookie", "instance_access_118=private")
	FilterPlatformCookies(h)
	if h.Get("Cookie") != "" {
		t.Fatal("credential remains")
	}
}
func TestBrowserNativeContextCannotBeForgedByHeaders(t *testing.T) {
	r := httptest.NewRequest("GET", "https://cm.test/", nil)
	r.Header.Set("X-ClawManager-Browser-Origin", "native")
	if IsNativeContext(r.Context()) {
		t.Fatal("untrusted header marked native")
	}
	if !IsNativeContext(WithNativeContext(context.Background())) {
		t.Fatal("trusted context missing")
	}
}
func TestBrowserManagerCrossPortBoundary(t *testing.T) {
	for _, method := range []string{"GET", "POST", "OPTIONS"} {
		r := httptest.NewRequest(method, "https://cm.test:30443/api/v1/instances", strings.NewReader("{}"))
		r.Header.Set("Origin", "https://cm.test:31400")
		r.Header.Set("Sec-Fetch-Site", "same-site")
		if AllowManagerRequest(r, "https://cm.test:30443", "cm.test", 31400, 31527) {
			t.Fatalf("cross-port %s permitted", method)
		}
	}
	r := httptest.NewRequest("GET", "https://cm.test:30443/api/v1/me", nil)
	r.Header.Set("Sec-Fetch-Site", "same-site")
	if AllowManagerRequest(r, "https://cm.test:30443", "cm.test", 31400, 31527) {
		t.Fatal("missing-origin same-site cookie API permitted")
	}
	r.Header.Del("Sec-Fetch-Site")
	if !AllowManagerRequest(r, "https://cm.test:30443", "cm.test", 31400, 31527) {
		t.Fatal("non-browser token API blocked")
	}
}

func TestManagerDeniesCrossSiteBrowserOrigins(t *testing.T) {
	for _, site := range []string{"same-site", "cross-site", "same-origin"} {
		r := httptest.NewRequest("POST", "https://cm.test:30443/api/v1/action", nil)
		r.Header.Set("Origin", "https://evil.test")
		r.Header.Set("Sec-Fetch-Site", site)
		if AllowManagerRequest(r, "https://cm.test:30443", "cm.test", 31400, 31527) {
			t.Fatal("foreign browser origin accepted", site)
		}
	}
}
