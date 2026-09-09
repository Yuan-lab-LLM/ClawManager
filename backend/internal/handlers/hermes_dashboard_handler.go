package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

func (h *InstanceHandler) SetHermesDesktopService(s *services.HermesDesktopService) {
	h.hermesDesktop = NewHermesDesktopHandler(s)
}

func (h *HermesDesktopHandler) activateDashboardSession(c *gin.Context, userID, instanceID int) (*services.HermesDesktopDescriptor, error) {
	d, raw, err := h.service.ActivateDashboard(c.Request.Context(), userID, instanceID)
	if err != nil {
		return nil, err
	}
	setHermesSessionCookie(c, services.HermesDashboardCookieName(instanceID), services.HermesDashboardBase(instanceID)+"/", raw, *d.ExpiresAt)
	return d, nil
}

// Called only after the ordinary ClawManager bearer/ownership checks in
// GenerateAccessToken. Neither an old instance_access cookie nor a Hermes
// cookie can bootstrap this server-side authorization lease.
func (h *HermesDesktopHandler) DashboardBootstrap(c *gin.Context, id int) {
	hermesDesktopHeaders(c)
	if !hermesDesktopOriginAllowed(c.Request, true) {
		hermesDesktopError(c, services.ErrHermesDesktopForbidden)
		return
	}
	d, err := h.activateDashboardSession(c, c.GetInt("userID"), id)
	if err != nil {
		hermesDesktopError(c, err)
		return
	}
	// Keep the legacy access response shape, but only a clean URL and CM lease
	// expiry are public; no bearer, upstream address or Hermes credential.
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"access_url": d.RendererURL, "proxy_url": d.RendererURL, "expires_at": d.ExpiresAt, "desktop_proxy_mode": "control-plane", "desktop_upstream_present": false}})
}

func (h *HermesDesktopHandler) Dashboard(c *gin.Context) {
	hermesDesktopHeaders(c)
	c.Header("Content-Security-Policy", "frame-ancestors 'self'")
	id, ok := hermesDesktopID(c)
	if !ok {
		return
	}
	isWS := strings.EqualFold(c.GetHeader("Upgrade"), "websocket")
	if !hermesDesktopOriginAllowed(c.Request, isWS || (c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead)) {
		hermesDesktopError(c, services.ErrHermesDesktopForbidden)
		return
	}
	r := originalInstanceProxyRequest(c)
	raw, err := r.Cookie(services.HermesDashboardCookieName(id))
	if err != nil {
		hermesDesktopError(c, services.ErrHermesDesktopUnauthorized)
		return
	}
	claims, err := h.service.AuthenticateDashboard(c.Request.Context(), raw.Value, id)
	if err != nil {
		hermesDesktopError(c, err)
		return
	}
	path := strings.TrimPrefix(c.Request.URL.Path, services.HermesDashboardBase(id))
	if path == "" {
		path = "/"
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		hermesDesktopError(c, services.ErrHermesDesktopForbidden)
		return
	}
	if isWS {
		if len(query["ticket"]) != 1 || query.Get("ticket") == "" || r.Header.Get("Sec-WebSocket-Protocol") != "" {
			hermesDesktopError(c, services.ErrHermesDesktopForbidden)
			return
		}
		// Browser ticket is CM-owned. Never inherit arbitrary browser protocols
		// or cookie headers into the separately authenticated upstream socket.
		ticket := query.Get("ticket")
		query.Del("ticket")
		err = h.service.ProxyDashboardWebSocket(c.Request.Context(), claims, ticket, path, query, c.Writer, c.Request)
	} else {
		err = h.service.ProxyDashboardHTTP(c.Request.Context(), claims, c.Request.Method, path, query, c.Writer)
	}
	if err != nil && !c.Writer.Written() {
		hermesDesktopError(c, err)
	}
}

// Preserve compatibility for non-Hermes proxy consumers in a private clone;
// c.Request stays credential-free even when Gin Recovery dumps the request.
func originalInstanceProxyRequest(c *gin.Context) *http.Request {
	r := c.Request.Clone(c.Request.Context())
	if headers, exists := c.Get("instanceProxyOriginalHeaders"); exists {
		r.Header = headers.(http.Header).Clone()
	}
	if query, exists := c.Get("instanceProxyOriginalQuery"); exists {
		r.URL.RawQuery = query.(string)
	}
	return r
}

func redactInstanceProxyCredentials(c *gin.Context) {
	if !strings.HasPrefix(c.Request.URL.Path, "/api/v1/instances/") {
		return
	}
	if strings.HasSuffix(c.Request.URL.Path, "/access") {
		// Activation accepts the platform Authorization header, never query
		// credentials or browser cookies. Strip irrelevant material before Gin
		// captures the request for access/recovery logs; keep bearer auth intact.
		c.Request.URL.RawQuery = ""
		c.Request.RequestURI = c.Request.URL.RequestURI()
		c.Request.Header.Del("Cookie")
		c.Request.Header.Del("Referer")
		c.Request.Header.Del("Sec-WebSocket-Protocol")
		return
	}
	if !strings.Contains(c.Request.URL.Path, "/proxy") {
		return
	}
	c.Set("instanceProxyOriginalHeaders", c.Request.Header.Clone())
	c.Set("instanceProxyOriginalQuery", c.Request.URL.RawQuery)
	query := c.Request.URL.Query()
	for key := range query {
		lower := strings.ToLower(strings.ReplaceAll(key, "_", ""))
		if strings.Contains(lower, "token") || strings.Contains(lower, "ticket") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "credential") || strings.Contains(lower, "apikey") {
			query.Del(key)
		}
	}
	c.Request.URL.RawQuery = query.Encode()
	c.Request.RequestURI = c.Request.URL.RequestURI()
	for key := range c.Request.Header {
		lower := strings.ToLower(key)
		if lower == "cookie" || lower == "referer" || lower == "sec-websocket-protocol" || strings.Contains(lower, "authorization") || strings.Contains(lower, "token") || strings.Contains(lower, "api-key") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") {
			c.Request.Header.Del(key)
		}
	}
}
