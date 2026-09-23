package handlers

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

const (
	browserWorkerNamespaceEnv = "CLAWMANAGER_BROWSER_WORKER_NAMESPACE"
	browserWorkerTokenType    = "openclaw-browser"
	browserWorkerPort         = 5800
	browserWorkerSessionTTL   = time.Hour
)

type browserWorkerDescriptor struct {
	Enabled       bool    `json:"enabled"`
	Available     bool    `json:"available"`
	InstanceID    int     `json:"instance_id"`
	AccessURL     string  `json:"access_url,omitempty"`
	Reason        string  `json:"reason,omitempty"`
	ExpiresAt     string  `json:"expires_at,omitempty"`
	Status        string  `json:"status,omitempty"`
	LastError     *string `json:"last_error,omitempty"`
	DisplayWidth  int     `json:"display_width,omitempty"`
	DisplayHeight int     `json:"display_height,omitempty"`
}

func browserWorkerNamespace() string {
	value := strings.TrimSpace(os.Getenv(browserWorkerNamespaceEnv))
	if value == "" {
		return "clawmanager-system"
	}
	return value
}

func browserWorkerBase(instanceID int) string {
	return fmt.Sprintf("/api/v1/instances/%d/browser-proxy", instanceID)
}

func browserWorkerCookieName(instanceID int) string {
	return fmt.Sprintf("cm_browser_worker_%d", instanceID)
}

func browserWorkerTarget(instanceID int) *url.URL {
	return &url.URL{
		Scheme: "http",
		Host: fmt.Sprintf(
			"clawbrowser-%d.%s.svc.cluster.local:%d",
			instanceID,
			browserWorkerNamespace(),
			browserWorkerPort,
		),
	}
}

func browserWorkerSupported(instance *models.Instance) bool {
	return instance != nil &&
		strings.EqualFold(strings.TrimSpace(instance.Type), services.RuntimeTypeOpenClaw) &&
		strings.EqualFold(strings.TrimSpace(instance.InstanceMode), services.InstanceModeLite)
}

func browserWorkerHealthy(instanceID int) bool {
	target := browserWorkerTarget(instanceID)
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			Proxy:       nil,
			DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Get(target.String() + "/")
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode >= 200 && response.StatusCode < 400
}

func (h *InstanceHandler) describeBrowserWorker(instance *models.Instance) browserWorkerDescriptor {
	descriptor := browserWorkerDescriptor{InstanceID: instance.ID}
	if h.browserWorkerService == nil {
		descriptor.Reason = "feature_disabled"
		return descriptor
	}
	config, err := h.browserWorkerService.Get(instance.ID)
	if err != nil {
		descriptor.Reason = "browser_worker_config_error"
		return descriptor
	}
	descriptor.Status, descriptor.LastError = config.Status, config.LastError
	descriptor.DisplayWidth, descriptor.DisplayHeight = config.DisplayWidth, config.DisplayHeight
	if !config.Enabled {
		descriptor.Reason = "feature_disabled"
		return descriptor
	}
	descriptor.Enabled = true
	if !browserWorkerSupported(instance) {
		descriptor.Reason = "unsupported_instance"
		return descriptor
	}
	if !strings.EqualFold(strings.TrimSpace(instance.Status), "running") {
		descriptor.Reason = "instance_not_running"
		return descriptor
	}
	if !browserWorkerHealthy(instance.ID) {
		descriptor.Reason = "browser_worker_unavailable"
		_ = h.browserWorkerService.SetObserved(instance.ID, models.BrowserWorkerStatusDegraded, nil)
		return descriptor
	}
	descriptor.Available = true
	descriptor.Status = models.BrowserWorkerStatusReady
	_ = h.browserWorkerService.SetObserved(instance.ID, models.BrowserWorkerStatusReady, nil)
	descriptor.AccessURL = browserWorkerBase(instance.ID) + "/"
	return descriptor
}

// GetBrowserWorker reports whether this OpenClaw Lite instance has a visible,
// persistent Browser Worker attached. This endpoint uses normal bearer auth.
func (h *InstanceHandler) GetBrowserWorker(c *gin.Context) {
	instance, ok := h.requireOwnedInstance(c)
	if !ok {
		return
	}
	utils.Success(c, http.StatusOK, "Browser Worker status retrieved", h.describeBrowserWorker(instance))
}

// GenerateBrowserWorkerAccess mints a short-lived, HttpOnly, instance-scoped
// cookie. Browser assets and WebSocket upgrades never receive a token in URLs.
func (h *InstanceHandler) GenerateBrowserWorkerAccess(c *gin.Context) {
	instance, ok := h.requireOwnedInstance(c)
	if !ok {
		return
	}
	descriptor := h.describeBrowserWorker(instance)
	if !descriptor.Enabled {
		utils.Error(c, http.StatusNotFound, "Browser Worker is not enabled for this instance")
		return
	}
	if !descriptor.Available {
		utils.Error(c, http.StatusServiceUnavailable, descriptor.Reason)
		return
	}

	accessURL := browserWorkerBase(instance.ID) + "/"
	token, err := h.accessService.GenerateToken(
		c.GetInt("userID"),
		instance.ID,
		browserWorkerTokenType,
		accessURL,
		"",
		browserWorkerPort,
		browserWorkerSessionTTL,
	)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     browserWorkerCookieName(instance.ID),
		Value:    token.Token,
		Path:     browserWorkerBase(instance.ID),
		Expires:  token.ExpiresAt,
		MaxAge:   int(browserWorkerSessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteStrictMode,
	})
	descriptor.ExpiresAt = token.ExpiresAt.UTC().Format(time.RFC3339)
	utils.Success(c, http.StatusOK, "Browser Worker access generated", descriptor)
}

func browserWorkerOriginAllowed(request *http.Request) bool {
	if site := request.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		return false
	}
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return false
	}
	scheme := "http"
	forwardedProto := strings.TrimSpace(strings.Split(request.Header.Get("X-Forwarded-Proto"), ",")[0])
	if request.TLS != nil || strings.EqualFold(forwardedProto, "https") {
		scheme = "https"
	}
	if !strings.EqualFold(parsed.Scheme, scheme) {
		return false
	}
	if strings.EqualFold(parsed.Host, request.Host) {
		return true
	}
	// The bundled Nginx terminates TLS and forwards to the Go server using its
	// internal Service Host. Module-script requests carry Origin, so compare it
	// with the trusted reverse-proxy Host as well as request.Host.
	forwardedHost := strings.TrimSpace(strings.Split(request.Header.Get("X-Forwarded-Host"), ",")[0])
	return forwardedHost != "" && strings.EqualFold(parsed.Host, forwardedHost)
}

// ProxyBrowserWorker exposes only the Browser Worker GUI port through a
// same-origin, cookie-authenticated reverse proxy. The CDP port stays private.
func (h *InstanceHandler) ProxyBrowserWorker(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 || h.browserWorkerService == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	config, configErr := h.browserWorkerService.Get(id)
	if configErr != nil || !config.Enabled {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if !browserWorkerOriginAllowed(c.Request) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	rawToken, err := c.Cookie(browserWorkerCookieName(id))
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	access, err := h.accessService.ValidateToken(rawToken)
	expectedAccessURL := browserWorkerBase(id) + "/"
	if err != nil || access.InstanceID != id || access.InstanceType != browserWorkerTokenType ||
		access.TargetPort != browserWorkerPort || access.AccessURL != expectedAccessURL {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	instance, err := h.instanceService.GetByID(id)
	if err != nil || !browserWorkerSupported(instance) || !strings.EqualFold(strings.TrimSpace(instance.Status), "running") {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}

	base := browserWorkerBase(id)
	if c.Request.URL.Path == base {
		c.Redirect(http.StatusTemporaryRedirect, base+"/")
		return
	}
	path := c.Param("path")
	if path == "" {
		path = "/"
	}
	c.Request.URL.Path = path
	c.Request.URL.RawPath = ""
	c.Request.Header.Del("Cookie")
	c.Request.Header.Del("Authorization")
	c.Request.Header.Set("X-Forwarded-Prefix", base)

	target := browserWorkerTarget(id)
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(request *http.Request) {
		originalDirector(request)
		request.Host = target.Host
	}
	proxy.ErrorHandler = func(writer http.ResponseWriter, _ *http.Request, _ error) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte(`{"success":false,"error":"browser_worker_unavailable"}`))
	}
	proxy.FlushInterval = -1
	proxy.ServeHTTP(c.Writer, c.Request)
}
