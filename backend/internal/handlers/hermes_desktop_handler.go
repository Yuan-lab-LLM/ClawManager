package handlers

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

type HermesDesktopHandler struct {
	service *services.HermesDesktopService
}

func NewHermesDesktopHandler(service *services.HermesDesktopService) *HermesDesktopHandler {
	return &HermesDesktopHandler{service: service}
}

// HermesDesktopRedactTickets must run before gin.Logger. Keep the ticket only
// in request-local context; neither access logs nor downstream URLs see it.
func HermesDesktopRedactTickets() gin.HandlerFunc {
	return func(c *gin.Context) {
		redactInstanceProxyCredentials(c)
		if strings.Contains(c.Request.URL.Path, "/hermes-desktop/") {
			q := c.Request.URL.Query()
			if strings.HasSuffix(c.Request.URL.Path, "/ws") {
				c.Set("hermesDesktopTicket", q.Get("ticket"))
			}
			q.Del("ticket")
			q.Del("token")
			c.Request.URL.RawQuery = q.Encode()
			c.Request.RequestURI = c.Request.URL.RequestURI()
			// Recovery/request-dump loggers may include the raw Cookie header.
			// Only this BFF consumes its scoped cookie; keep it in Gin context
			// and remove browser cookies before downstream logging/recovery.
			for _, cookie := range c.Request.Cookies() {
				if strings.HasPrefix(cookie.Name, "cm_hermes_desktop_") {
					c.Set("hermesDesktopCookie:"+cookie.Name, cookie.Value)
				}
			}
			c.Request.Header.Del("Cookie")
			c.Request.Header.Del("Referer")
			c.Request.Header.Del("Sec-WebSocket-Protocol")
		}
		c.Next()
	}
}

func hermesDesktopOriginAllowed(r *http.Request, mutation bool) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return !mutation && r.Header.Get("Sec-Fetch-Site") == "same-origin"
	}
	u, err := url.Parse(origin)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || !strings.EqualFold(u.Host, r.Host) {
		return false
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return u.Scheme == scheme
}

func hermesDesktopHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
}

func hermesDesktopError(c *gin.Context, err error) {
	status := http.StatusBadGateway
	reason := "desktop_upstream_unavailable"
	switch {
	case errors.Is(err, services.ErrHermesDesktopUnauthorized):
		status = http.StatusUnauthorized
		reason = "desktop_session_required"
	case errors.Is(err, services.ErrHermesDesktopForbidden):
		status = http.StatusForbidden
		reason = "desktop_access_denied"
	case errors.Is(err, services.ErrHermesDesktopUnavailable):
		status = http.StatusServiceUnavailable
		reason = "desktop_unavailable"
	}
	c.AbortWithStatusJSON(status, gin.H{"success": false, "error": reason})
}

func hermesDesktopID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid_instance_id"})
		return 0, false
	}
	return id, true
}

// Bootstrap is exclusively registered behind normal user bearer middleware.
// GET is a read-only capability probe; POST activates the scoped cookie.
func (h *HermesDesktopHandler) activateDesktopSession(c *gin.Context, userID, instanceID int) (*services.HermesDesktopDescriptor, error) {
	d, token, err := h.service.Activate(c.Request.Context(), userID, instanceID)
	if err != nil {
		return nil, err
	}
	if d.Available {
		setHermesSessionCookie(c, services.HermesDesktopCookieName(instanceID), services.HermesDesktopBase(instanceID)+"/", token, *d.ExpiresAt)
	}
	return d, nil
}

func setHermesSessionCookie(c *gin.Context, name, path, value string, expires time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: name, Value: value, Path: path, Expires: expires,
		MaxAge: int(services.HermesDesktopSessionTTL.Seconds()), HttpOnly: true,
		Secure:   c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *HermesDesktopHandler) Bootstrap(c *gin.Context) {
	hermesDesktopHeaders(c)
	id, ok := hermesDesktopID(c)
	if !ok {
		return
	}
	if c.Request.Method == http.MethodGet {
		d, err := h.service.Describe(c.Request.Context(), c.GetInt("userID"), id)
		if err != nil {
			hermesDesktopError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "data": d})
		return
	}
	if !hermesDesktopOriginAllowed(c.Request, true) {
		hermesDesktopError(c, services.ErrHermesDesktopForbidden)
		return
	}
	d, err := h.activateDesktopSession(c, c.GetInt("userID"), id)
	if err != nil {
		hermesDesktopError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": d})
}

func (h *HermesDesktopHandler) authenticate(c *gin.Context) (*services.HermesDesktopClaims, bool) {
	hermesDesktopHeaders(c)
	id, ok := hermesDesktopID(c)
	if !ok {
		return nil, false
	}
	if !hermesDesktopOriginAllowed(c.Request, c.Request.Method != http.MethodGet || strings.HasSuffix(c.Request.URL.Path, "/ws")) {
		hermesDesktopError(c, services.ErrHermesDesktopForbidden)
		return nil, false
	}
	raw := c.GetString("hermesDesktopCookie:" + services.HermesDesktopCookieName(id))
	if raw == "" {
		hermesDesktopError(c, services.ErrHermesDesktopUnauthorized)
		return nil, false
	}
	claims, err := h.service.Authenticate(c.Request.Context(), raw, id)
	if err != nil {
		hermesDesktopError(c, err)
		return nil, false
	}
	return claims, true
}

func (h *HermesDesktopHandler) Session(c *gin.Context) {
	claims, ok := h.authenticate(c)
	if !ok {
		return
	}
	d, err := h.service.Describe(c.Request.Context(), claims.UserID, claims.InstanceID)
	if err != nil {
		hermesDesktopError(c, err)
		return
	}
	expiry := claims.ExpiresAt.Time
	d.ExpiresAt = &expiry
	c.JSON(http.StatusOK, gin.H{"success": true, "data": d})
}

func (h *HermesDesktopHandler) ClearSession(c *gin.Context) {
	claims, ok := h.authenticate(c)
	if !ok {
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: services.HermesDesktopCookieName(claims.InstanceID), Value: "", Path: services.HermesDesktopBase(claims.InstanceID) + "/", Expires: time.Unix(1, 0), MaxAge: -1, HttpOnly: true, Secure: c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https", SameSite: http.SameSiteStrictMode})
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *HermesDesktopHandler) API(c *gin.Context) {
	claims, ok := h.authenticate(c)
	if !ok {
		return
	}
	var requestBody []byte
	if c.Request.Method != http.MethodGet {
		var err error
		requestBody, err = io.ReadAll(io.LimitReader(c.Request.Body, (1<<20)+1))
		if err != nil || len(requestBody) > 1<<20 {
			hermesDesktopError(c, services.ErrHermesDesktopForbidden)
			return
		}
	}
	body, err := h.service.ProxyAPIRequest(c.Request.Context(), claims, c.Request.Method, c.Param("path"), c.Request.URL.Query(), requestBody)
	if err != nil {
		hermesDesktopError(c, err)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

func (h *HermesDesktopHandler) Ticket(c *gin.Context) {
	claims, ok := h.authenticate(c)
	if !ok {
		return
	}
	url, expiry, err := h.service.MintTicket(claims)
	if err != nil {
		hermesDesktopError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"url": url, "expires_at": expiry}})
}

func (h *HermesDesktopHandler) WebSocket(c *gin.Context) {
	claims, ok := h.authenticate(c)
	if !ok {
		return
	}
	err := h.service.ProxyWebSocket(c.Request.Context(), claims, c.GetString("hermesDesktopTicket"), c.Writer, c.Request)
	if err != nil && !c.Writer.Written() {
		hermesDesktopError(c, err)
	}
}
