package browserorigin

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gorilla/websocket"
)

type nativeContextKey struct{}

func WithNativeContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, nativeContextKey{}, true)
}
func IsNativeContext(ctx context.Context) bool {
	marked, _ := ctx.Value(nativeContextKey{}).(bool)
	return marked
}

// Browser Fetch Metadata is not an authentication mechanism. It prevents a
// different port/site from exercising credentials that browsers share by host.
// The existing signed capability is still authenticated by the native handler.
func AllowNativeRequest(r *http.Request, origin, manager string, bootstrap bool) bool {
	source := r.Header.Get("Origin")
	site := r.Header.Get("Sec-Fetch-Site")
	if source != "" && source != origin && !(bootstrap && source == manager) {
		return false
	}
	if site == "same-origin" {
		return source == "" || source == origin
	}
	// Chrome can omit Fetch Metadata on WebSocket handshakes. Its Origin still
	// identifies the exact native port; the instance capability is checked next.
	if site == "" && source == origin && r.Method == http.MethodGet && websocket.IsWebSocketUpgrade(r) {
		return true
	}
	if bootstrap && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		if source != "" && source != manager {
			return false
		}
		return site == "same-site" || site == "none" || site == ""
	}
	return site == "none" && source == "" && r.Header.Get("Sec-Fetch-Dest") == "document" && (r.Method == http.MethodGet || r.Method == http.MethodHead)
}

func AllowManagerRequest(r *http.Request, manager, hostname string, first, last int) bool {
	source := r.Header.Get("Origin")
	if source == manager {
		return true
	}
	if source != "" {
		u, err := url.Parse(source)
		if err == nil && strings.EqualFold(u.Hostname(), hostname) {
			port, _ := strconv.Atoi(u.Port())
			if port >= first && port <= last {
				return false
			}
		}
	}
	// Same-site includes every native/preview port; without Origin the manager
	// cannot tell which one initiated a cookie-bearing API request. Same-origin
	// documents and non-browser token clients keep their existing behavior.
	site := r.Header.Get("Sec-Fetch-Site")
	if source != "" && site != "" {
		return false
	}
	return site == "" || site == "none" || site == "same-origin"
}

func PlatformCookie(name string) bool {
	return strings.HasPrefix(name, "instance_access_") || strings.HasPrefix(name, "cm_preview_") || strings.HasPrefix(name, "clawmanager_") || strings.HasPrefix(name, "cm_hermes_desktop_") || strings.HasPrefix(name, "share_link_access") || strings.HasPrefix(name, "instance_external_")
}
func FilterPlatformCookies(header http.Header) {
	request := &http.Request{Header: header}
	cookies := request.Cookies()
	header.Del("Cookie")
	for _, cookie := range cookies {
		if !PlatformCookie(cookie.Name) {
			request.AddCookie(cookie)
		}
	}
}
