package northbound

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawreef/internal/config"
	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
)

type CoreClient struct {
	baseURL         *url.URL
	client          *http.Client
	internalSecret  string
	runtimeSettings RuntimeSettingsProvider
}

func (c *CoreClient) UseRuntimeSettings(provider RuntimeSettingsProvider) {
	if c != nil {
		c.runtimeSettings = provider
	}
}

func NewCoreClient(cfg config.NorthboundConfig) (*CoreClient, error) {
	baseURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(cfg.CoreBaseURL), "/"))
	if err != nil || baseURL.Scheme != "https" || baseURL.Host == "" {
		return nil, apiError(500, "CONFIG_ERROR", "Invalid northbound Core URL", err)
	}
	tlsConfig, err := GatewayTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	if err := requireStrongSecret("northbound internal JWT secret", cfg.InternalJWTSecret); err != nil {
		return nil, apiError(500, "CONFIG_ERROR", err.Error(), err)
	}
	return &CoreClient{
		baseURL:        baseURL,
		internalSecret: cfg.InternalJWTSecret,
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: &http.Transport{TLSClientConfig: tlsConfig},
		},
	}, nil
}

func (c *CoreClient) SetTimeout(timeout time.Duration) {
	if c != nil && c.client != nil && timeout > 0 {
		c.client.Timeout = timeout
	}
}

func (c *CoreClient) Forward(ginContext *gin.Context, principal Principal) error {
	client := *c.client
	if c.runtimeSettings != nil {
		timeout := time.Duration(c.runtimeSettings.Current().CoreRequestTimeoutSeconds) * time.Second
		if timeout > 0 {
			client.Timeout = timeout
		}
	}
	path := ginContext.Request.URL.Path
	fileTransfer := ownerTeamFileTransfer(ginContext)
	const publicPrefix = "/api/northbound/v1"
	if !strings.HasPrefix(path, publicPrefix) {
		return apiError(404, "NOT_FOUND", "Not found", nil)
	}
	internalPath := "/internal/northbound/v1" + strings.TrimPrefix(path, publicPrefix)
	target := *c.baseURL
	target.Path = strings.TrimRight(c.baseURL.Path, "/") + internalPath
	target.RawQuery = ginContext.Request.URL.RawQuery

	var requestBody io.Reader = ginContext.Request.Body
	if !fileTransfer {
		body, err := io.ReadAll(ginContext.Request.Body)
		if err != nil {
			return apiError(400, "INVALID_REQUEST", "Unable to read request", err)
		}
		requestBody = bytes.NewReader(body)
	}
	requestID := requestIDFromContext(ginContext)
	internalToken, err := issueInternalToken(c.internalSecret, principal, requestID)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ginContext.Request.Context(), ginContext.Request.Method, target.String(), requestBody)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+internalToken)
	request.Header.Set("Content-Type", "application/json")
	if fileTransfer {
		request.Header.Set("Content-Type", ginContext.GetHeader("Content-Type"))
	}
	request.Header.Set("X-Request-ID", requestID)
	if key := strings.TrimSpace(ginContext.GetHeader("Idempotency-Key")); key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	response, err := client.Do(request)
	if err != nil {
		return apiError(503, "DEPENDENCY_UNAVAILABLE", "Northbound Core is unavailable", err)
	}
	defer response.Body.Close()
	if fileTransfer && ginContext.Request.Method == http.MethodGet && response.StatusCode == http.StatusOK {
		headers := map[string]string{"Cache-Control": "no-store", "X-Content-Type-Options": "nosniff"}
		if value := response.Header.Get("Content-Disposition"); value != "" {
			headers["Content-Disposition"] = value
		}
		ginContext.DataFromReader(response.StatusCode, response.ContentLength, response.Header.Get("Content-Type"), response.Body, headers)
		return nil
	}
	responseLimit := int64(2 << 20)
	if ownerTeamTextSave(ginContext) || strings.HasSuffix(ginContext.FullPath(), "/teams/:id/workspace/text") || strings.HasSuffix(ginContext.FullPath(), "/teams/:id/workspace/preview") {
		responseLimit = 8 << 20
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil {
		return apiError(503, "DEPENDENCY_UNAVAILABLE", "Invalid response from Northbound Core", err)
	}
	if int64(len(responseBody)) > responseLimit {
		return apiError(502, "RESPONSE_TOO_LARGE", "Response exceeds the bounded JSON transport limit", nil)
	}
	for _, header := range []string{"Content-Type", "Location", "Idempotent-Replayed", "Retry-After", "Cache-Control"} {
		if value := response.Header.Get(header); value != "" {
			ginContext.Header(header, value)
		}
	}
	if principal.AuthType == "api_key" && response.StatusCode == http.StatusAccepted {
		var op struct {
			ID string `json:"operation_id"`
		}
		if json.Unmarshal(responseBody, &op) == nil {
			ginContext.Set("northboundOperationID", op.ID)
		}
	}
	ginContext.Data(response.StatusCode, response.Header.Get("Content-Type"), responseBody)
	return nil
}

type fixedWindowEntry struct {
	window time.Time
	count  int
}

type FixedWindowLimiter struct {
	mu      sync.Mutex
	entries map[string]fixedWindowEntry
	limit   int
	window  time.Duration
}

func NewFixedWindowLimiter(limit int, window time.Duration) *FixedWindowLimiter {
	return &FixedWindowLimiter{entries: map[string]fixedWindowEntry{}, limit: limit, window: window}
}

func (l *FixedWindowLimiter) Allow(key string) bool {
	return l.AllowLimit(key, l.limit)
}

func (l *FixedWindowLimiter) AllowLimit(key string, limit int) bool {
	if l == nil || limit <= 0 || l.window <= 0 {
		return true
	}
	now := time.Now().UTC()
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.entries[key]
	if entry.window.IsZero() || now.Sub(entry.window) >= l.window {
		entry = fixedWindowEntry{window: now, count: 0}
	}
	if entry.count >= limit {
		l.entries[key] = entry
		return false
	}
	entry.count++
	l.entries[key] = entry
	if len(l.entries) > 10000 {
		for candidate, value := range l.entries {
			if now.Sub(value.window) >= l.window*2 {
				delete(l.entries, candidate)
			}
		}
	}
	return true
}

func DynamicRateLimit(limiter *FixedWindowLimiter, provider RuntimeSettingsProvider, limit func(*models.NorthboundAdminSettings) int, key func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		settings := provider.Current()
		allowed := limiter.AllowLimit(key(c), limit(settings))
		if p := currentPrincipal(c); p != nil && p.AuthType == "api_key" && allowed {
			allowed = limiter.AllowLimit("api-key:"+strconv.Itoa(p.UserID)+":"+p.KeyID, limit(settings))
		}
		if !allowed {
			c.Header("Retry-After", "60")
			writeError(c, apiError(429, "RATE_LIMITED", "Too many requests", nil))
			c.Abort()
			return
		}
		c.Next()
	}
}

func NorthboundEnabled(provider RuntimeSettingsProvider) gin.HandlerFunc {
	return func(c *gin.Context) {
		if provider != nil && !provider.Current().APIEnabled {
			writeError(c, apiError(http.StatusServiceUnavailable, "NORTHBOUND_DISABLED", "Northbound API is disabled by an administrator", nil))
			c.Abort()
			return
		}
		c.Next()
	}
}

func RateLimit(limiter *FixedWindowLimiter, key func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !limiter.Allow(key(c)) {
			c.Header("Retry-After", "60")
			writeError(c, apiError(429, "RATE_LIMITED", "Too many requests", nil))
			c.Abort()
			return
		}
		c.Next()
	}
}

func RequestContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if !validRequestID(requestID) {
			requestID, _ = randomToken("req_", 16)
		}
		c.Set("requestID", requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

func validRequestID(value string) bool {
	if len(value) < 8 || len(value) > 96 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' || character == ':' {
			continue
		}
		return false
	}
	return true
}

func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := maxBytes
		if ownerTeamFileTransfer(c) && c.Request.Method == http.MethodPost {
			limit = 64 << 20
		}
		if ownerTeamTextSave(c) {
			limit = 8 << 20
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}

func ownerTeamFileTransfer(c *gin.Context) bool {
	path := c.FullPath()
	allowed := strings.HasPrefix(path, "/api/northbound/v1/teams/:id/workspace/") || strings.HasPrefix(path, "/internal/northbound/v1/teams/:id/workspace/")
	return allowed && (c.Request.Method == http.MethodPost && strings.HasSuffix(path, "/upload") || c.Request.Method == http.MethodGet && strings.HasSuffix(path, "/download"))
}
func ownerTeamTextSave(c *gin.Context) bool {
	path := c.FullPath()
	return c.Request.Method == http.MethodPut && (path == "/api/northbound/v1/teams/:id/workspace/text" || path == "/internal/northbound/v1/teams/:id/workspace/text")
}

// RejectSuspiciousRequest closes common path/header normalization gaps before
// Gin route matching. Unknown or ambiguous paths deliberately look like 404s.
func RejectSuspiciousRequest() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestPath := c.Request.URL.Path
		escapedPath := strings.ToLower(c.Request.URL.EscapedPath())
		if c.Request.Host == "" || strings.Contains(requestPath, "\\") || strings.Contains(requestPath, "//") ||
			path.Clean(requestPath) != requestPath || strings.Contains(escapedPath, "%2f") ||
			strings.Contains(escapedPath, "%5c") || strings.Contains(escapedPath, "%25") {
			writeError(c, apiError(404, "NOT_FOUND", "Not found", nil))
			c.Abort()
			return
		}
		for _, header := range []string{"Authorization", "X-API-Key", "Content-Type", "Idempotency-Key"} {
			if len(c.Request.Header.Values(header)) > 1 {
				writeError(c, apiError(400, "INVALID_REQUEST", "Duplicate security-sensitive header", nil))
				c.Abort()
				return
			}
		}
		if _, present := c.Request.Header["X-Api-Key"]; present {
			if len(c.Request.Header.Values("Authorization")) > 0 {
				writeError(c, apiError(400, "INVALID_REQUEST", "Use exactly one authentication credential", nil))
				c.Abort()
				return
			}
			if requestPath == "/api/northbound/v1/auth/challenge" || requestPath == "/api/northbound/v1/auth/login" || requestPath == "/api/northbound/v1/auth/refresh" {
				writeError(c, apiError(400, "AUTH_METHOD_UNSUPPORTED", "API keys call business endpoints directly", nil))
				c.Abort()
				return
			}
		}
		bodylessPost := requestPath == "/api/northbound/v1/auth/challenge" ||
			requestPath == "/api/northbound/v1/auth/logout"
		if c.Request.Method == http.MethodPost && !bodylessPost {
			contentType := strings.ToLower(strings.TrimSpace(strings.SplitN(c.GetHeader("Content-Type"), ";", 2)[0]))
			if contentType != "application/json" {
				writeError(c, apiError(http.StatusUnsupportedMediaType, "INVALID_REQUEST", "Content-Type must be application/json", nil))
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

func requestIDFromContext(c *gin.Context) string {
	value, _ := c.Get("requestID")
	requestID, _ := value.(string)
	return requestID
}

func NotFound(c *gin.Context) {
	writeError(c, apiError(http.StatusNotFound, "NOT_FOUND", "Not found", nil))
}

func RegisterGatewayRoutes(router *gin.Engine, auth *AuthHandler, core *CoreClient, providers ...RuntimeSettingsProvider) {
	var provider RuntimeSettingsProvider
	if len(providers) > 0 {
		provider = providers[0]
	}
	challengeLimiter := NewFixedWindowLimiter(10, time.Minute)
	loginLimiter := NewFixedWindowLimiter(5, time.Minute)
	createLimiter := NewFixedWindowLimiter(10, time.Minute)
	shareMutationLimiter := NewFixedWindowLimiter(10, time.Minute)
	queryLimiter := NewFixedWindowLimiter(120, time.Minute)

	api := router.Group("/api/northbound/v1")
	if provider != nil {
		api.Use(NorthboundEnabled(provider))
	}
	authGroup := api.Group("/auth")
	challengeRate := RateLimit(challengeLimiter, func(c *gin.Context) string { return c.ClientIP() })
	loginRate := RateLimit(loginLimiter, func(c *gin.Context) string { return c.ClientIP() })
	if provider != nil {
		challengeRate = DynamicRateLimit(challengeLimiter, provider, func(s *models.NorthboundAdminSettings) int { return s.ChallengeRatePerMinute }, func(c *gin.Context) string { return c.ClientIP() })
		loginRate = DynamicRateLimit(loginLimiter, provider, func(s *models.NorthboundAdminSettings) int { return s.LoginRatePerMinute }, func(c *gin.Context) string { return c.ClientIP() })
	}
	authGroup.POST("/challenge", challengeRate, auth.Challenge)
	authGroup.POST("/login", BodyLimit(32<<10), loginRate, auth.Login)
	authGroup.POST("/refresh", auth.Refresh)
	authenticatedAuth := authGroup.Group("")
	authenticatedAuth.Use(auth.Middleware())
	authenticatedAuth.POST("/logout", auth.Logout)
	authenticatedAuth.GET("/me", auth.Me)

	resources := api.Group("")
	resources.Use(auth.Middleware())
	for _, route := range OwnerTeamRoutes {
		route := route
		guard := RequireScope(route.Scope)
		if route.Scope == "" {
			guard = RequireAnyScope(ScopeTeamsCreate, ScopeTeamsRead, ScopeTeamsManage, ScopeTeamsDelete, ScopeTeamTemplatesGenerate, ScopeTeamTemplatesWrite)
		}
		resources.Handle(route.Method, route.Path, guard, RateLimit(queryLimiter, func(c *gin.Context) string { return strconv.Itoa(currentPrincipal(c).UserID) }), func(c *gin.Context) {
			if err := core.Forward(c, *currentPrincipal(c)); err != nil {
				writeError(c, err)
			}
		})
	}
	createRate := RateLimit(createLimiter, func(c *gin.Context) string {
		principal := currentPrincipal(c)
		if principal == nil {
			return c.ClientIP()
		}
		return strconv.Itoa(principal.UserID)
	})
	if provider != nil {
		createRate = DynamicRateLimit(createLimiter, provider, func(s *models.NorthboundAdminSettings) int { return s.CreateRatePerMinute }, func(c *gin.Context) string {
			principal := currentPrincipal(c)
			if principal == nil {
				return c.ClientIP()
			}
			return strconv.Itoa(principal.UserID)
		})
	}
	resources.POST("/lite-instances", RequireScope(ScopeLiteCreate), createRate, func(c *gin.Context) {
		if err := core.Forward(c, *currentPrincipal(c)); err != nil {
			writeError(c, err)
		}
	})
	resources.POST("/pro-instances", RequireScope(ScopeProCreate), createRate, func(c *gin.Context) {
		if err := core.Forward(c, *currentPrincipal(c)); err != nil {
			writeError(c, err)
		}
	})
	for _, route := range []struct {
		path  string
		scope string
	}{
		{"/lite-instances/:id/restart", ScopeLiteRestart},
		{"/lite-instances/:id/reset", ScopeLiteReset},
		{"/lite-instances/:id/delete", ScopeLiteDelete},
		{"/pro-instances/:id/restart", ScopeProRestart},
		{"/pro-instances/:id/reset", ScopeProReset},
		{"/pro-instances/:id/delete", ScopeProDelete},
	} {
		route := route
		resources.POST(route.path, RequireScope(route.scope), createRate, func(c *gin.Context) {
			if err := core.Forward(c, *currentPrincipal(c)); err != nil {
				writeError(c, err)
			}
		})
	}
	queryRateLimit := RateLimit(queryLimiter, func(c *gin.Context) string {
		principal := currentPrincipal(c)
		if principal == nil {
			return c.ClientIP()
		}
		return strconv.Itoa(principal.UserID)
	})
	if provider != nil {
		queryRateLimit = DynamicRateLimit(queryLimiter, provider, func(s *models.NorthboundAdminSettings) int { return s.QueryRatePerMinute }, func(c *gin.Context) string {
			principal := currentPrincipal(c)
			if principal == nil {
				return c.ClientIP()
			}
			return strconv.Itoa(principal.UserID)
		})
	}
	resources.GET("/lite-instances", RequireScope(ScopeLiteRead), queryRateLimit, func(c *gin.Context) {
		if err := core.Forward(c, *currentPrincipal(c)); err != nil {
			writeError(c, err)
		}
	})
	resources.GET("/lite-instances/:id", RequireScope(ScopeLiteRead), queryRateLimit, func(c *gin.Context) {
		if err := core.Forward(c, *currentPrincipal(c)); err != nil {
			writeError(c, err)
		}
	})
	resources.GET("/owners/usage", RequireScope(ScopeOwnerUsageRead), queryRateLimit, func(c *gin.Context) {
		if err := core.Forward(c, *currentPrincipal(c)); err != nil {
			writeError(c, err)
		}
	})
	resources.GET("/pro-instances", RequireScope(ScopeProRead), queryRateLimit, func(c *gin.Context) {
		if err := core.Forward(c, *currentPrincipal(c)); err != nil {
			writeError(c, err)
		}
	})
	resources.GET("/pro-instances/:id", RequireScope(ScopeProRead), queryRateLimit, func(c *gin.Context) {
		if err := core.Forward(c, *currentPrincipal(c)); err != nil {
			writeError(c, err)
		}
	})
	shareMutationRateLimit := RateLimit(shareMutationLimiter, func(c *gin.Context) string {
		principal := currentPrincipal(c)
		if principal == nil {
			return c.ClientIP()
		}
		return strconv.Itoa(principal.UserID)
	})
	if provider != nil {
		shareMutationRateLimit = DynamicRateLimit(shareMutationLimiter, provider, func(s *models.NorthboundAdminSettings) int { return s.ShareRatePerMinute }, func(c *gin.Context) string {
			principal := currentPrincipal(c)
			if principal == nil {
				return c.ClientIP()
			}
			return strconv.Itoa(principal.UserID)
		})
	}
	resources.POST("/lite-instances/:id/external-access/password", RequireScope(ScopeShareLinkManage), shareMutationRateLimit, func(c *gin.Context) {
		if err := core.Forward(c, *currentPrincipal(c)); err != nil {
			writeError(c, err)
		}
	})
	resources.POST("/lite-instances/:id/external-access/share-link/reset", RequireScope(ScopeShareLinkReset), shareMutationRateLimit, func(c *gin.Context) {
		if err := core.Forward(c, *currentPrincipal(c)); err != nil {
			writeError(c, err)
		}
	})
	resources.POST("/lite-instances/:id/external-access/password/reset", RequireScope(ScopeShareLinkReset), shareMutationRateLimit, func(c *gin.Context) {
		if err := core.Forward(c, *currentPrincipal(c)); err != nil {
			writeError(c, err)
		}
	})
	resources.GET("/operations/:id", RequireAnyScope(ScopeLiteCreate, ScopeLiteRead, ScopeProCreate, ScopeProRead, ScopeLiteRestart, ScopeLiteReset, ScopeLiteDelete, ScopeProRestart, ScopeProReset, ScopeProDelete, ScopeTeamsCreate, ScopeTeamsDelete, ScopeTeamsManage, ScopeTeamTemplatesGenerate), queryRateLimit, func(c *gin.Context) {
		if err := core.Forward(c, *currentPrincipal(c)); err != nil {
			writeError(c, err)
		}
	})
}

func ShutdownServer(ctx context.Context, server *http.Server) error {
	if server == nil {
		return nil
	}
	return server.Shutdown(ctx)
}
