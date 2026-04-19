package services

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"

	"github.com/gorilla/websocket"
)

// Sentinel errors for proxy namespace routing.
var (
	ErrInstanceNotFound = errors.New("instance not found")
	ErrOwnerMismatch    = errors.New("access token owner does not match current instance owner")
)

// instanceOwnerLookup is the minimal interface the proxy needs to verify
// instance ownership. Defined at the consumer (proxy) per Go idiom.
type instanceOwnerLookup interface {
	GetByID(id int) (*models.Instance, error)
}

// ownerCacheEntry stores a cached owner lookup result.
type ownerCacheEntry struct {
	ownerID   int
	expiresAt time.Time
}

// ownerCache is a small TTL cache for instance→ownerID lookups.
type ownerCache struct {
	entries sync.Map
	ttl     time.Duration
	now     func() time.Time // injectable clock for testing
	lookup  instanceOwnerLookup
}

func newOwnerCache(lookup instanceOwnerLookup, ttl time.Duration, clock func() time.Time) *ownerCache {
	return &ownerCache{
		ttl:    ttl,
		now:    clock,
		lookup: lookup,
	}
}

func (c *ownerCache) Get(ctx context.Context, instanceID int) (int, error) {
	if entry, ok := c.entries.Load(instanceID); ok {
		e := entry.(ownerCacheEntry)
		if c.now().Before(e.expiresAt) {
			return e.ownerID, nil
		}
	}

	instance, err := c.lookup.GetByID(instanceID)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInstanceNotFound, err)
	}
	if instance == nil {
		return 0, ErrInstanceNotFound
	}

	c.entries.Store(instanceID, ownerCacheEntry{
		ownerID:   instance.UserID,
		expiresAt: c.now().Add(c.ttl),
	})
	return instance.UserID, nil
}

// InstanceProxyService handles proxying requests to instance pods
type InstanceProxyService struct {
	serviceService *k8s.ServiceService
	accessService  *InstanceAccessService
	httpClient     *http.Client
	cache          *ownerCache
}

// NewInstanceProxyService creates a new instance proxy service
func NewInstanceProxyService(accessService *InstanceAccessService, instanceLookup instanceOwnerLookup) *InstanceProxyService {
	return &InstanceProxyService{
		serviceService: k8s.NewServiceService(),
		accessService:  accessService,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Don't follow redirects automatically, let the client handle them
				return http.ErrUseLastResponse
			},
		},
		cache: newOwnerCache(instanceLookup, 30*time.Second, time.Now),
	}
}

// resolveOwnerID is the sole path from access token to k8s namespace.
func (s *InstanceProxyService) resolveOwnerID(ctx context.Context, t *AccessToken) (int, error) {
	dbOwner, err := s.cache.Get(ctx, t.InstanceID)
	if err != nil {
		return 0, err
	}
	// Legacy tokens (pre-fix) have OwnerID == 0 — trust DB, no mismatch check.
	if t.OwnerID != 0 && t.OwnerID != dbOwner {
		return 0, ErrOwnerMismatch
	}
	return dbOwner, nil
}

// ProxyRequest proxies a request to an instance
func (s *InstanceProxyService) ProxyRequest(ctx context.Context, instanceID int, token string, w http.ResponseWriter, r *http.Request) error {
	// Handle CORS preflight request
	if r.Method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, HEAD, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.WriteHeader(http.StatusNoContent)
		return nil
	}

	// Validate access token
	accessToken, err := s.accessService.ValidateToken(token)
	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}

	// Verify instance ID matches
	if accessToken.InstanceID != instanceID {
		return fmt.Errorf("token does not match instance")
	}

	// Extract the actual path from the request (remove the proxy prefix)
	targetPath := s.extractTargetPath(r.URL.Path, instanceID, accessToken.InstanceType)
	targetPort := s.resolveTargetPort(accessToken.InstanceType, accessToken.TargetPort, targetPath)

	// Resolve the instance owner for namespace routing.
	ownerID, err := s.resolveOwnerID(ctx, accessToken)
	if err != nil {
		return fmt.Errorf("failed to resolve instance owner: %w", err)
	}

	// Get service info for the instance (create if not exists)
	serviceInfo, err := s.getOrCreateService(ctx, ownerID, instanceID, targetPort)
	if err != nil {
		return fmt.Errorf("failed to get or create service: %w", err)
	}

	// Build target URL
	targetURL := &url.URL{
		Scheme: s.resolveTargetScheme(accessToken.InstanceType, false),
		Host:   s.resolveProxyHost(ctx, ownerID, instanceID, serviceInfo),
		Path:   targetPath,
	}

	// Copy query parameters (excluding token)
	queryParams := r.URL.Query()
	queryParams.Del("token")
	if len(queryParams) > 0 {
		targetURL.RawQuery = queryParams.Encode()
	}

	// Create new request with longer timeout for streaming
	proxyCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	proxyReq, err := http.NewRequestWithContext(proxyCtx, r.Method, targetURL.String(), r.Body)
	if err != nil {
		return fmt.Errorf("failed to create proxy request: %w", err)
	}

	// Copy headers
	for key, values := range r.Header {
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// Set X-Forwarded headers
	proxyReq.Header.Set("X-Forwarded-For", r.RemoteAddr)
	proxyReq.Header.Set("X-Forwarded-Host", r.Host)
	proxyReq.Header.Set("X-Forwarded-Proto", requestScheme(r))
	proxyReq.Header.Set("X-Forwarded-Prefix", fmt.Sprintf("/api/v1/instances/%d/proxy", instanceID))
	proxyReq.Header.Del("Accept-Encoding")

	// Remove hop-by-hop headers
	s.removeHopByHopHeaders(proxyReq.Header)

	// Execute request
	resp, err := s.httpClient.Do(proxyReq)
	if err != nil {
		return fmt.Errorf("failed to execute proxy request: %w", err)
	}
	defer resp.Body.Close()

	// Add CORS headers to response
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Credentials", "true")

	if location := resp.Header.Get("Location"); location != "" {
		resp.Header.Set("Location", s.rewriteRedirectLocation(instanceID, location))
	}

	if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("failed to read upstream html: %w", readErr)
		}
		if closeErr := resp.Body.Close(); closeErr != nil {
			return fmt.Errorf("failed to close upstream html body: %w", closeErr)
		}

		modifiedBody := injectProxyBase(string(body), fmt.Sprintf("/api/v1/instances/%d/proxy/", instanceID))
		resp.Body = io.NopCloser(bytes.NewReader([]byte(modifiedBody)))
		resp.ContentLength = int64(len(modifiedBody))
		resp.Header.Set("Content-Length", strconv.Itoa(len(modifiedBody)))
		resp.Header.Del("ETag")
		resp.Header.Del("Last-Modified")
	}

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Del("X-Frame-Options")
	w.Header().Del("Content-Security-Policy")

	// Remove hop-by-hop headers from response
	s.removeHopByHopHeaders(w.Header())

	// Write status code
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to copy response body: %w", err)
	}

	return nil
}

// ProxyWebSocket handles WebSocket upgrade requests
func (s *InstanceProxyService) ProxyWebSocket(ctx context.Context, instanceID int, token string, w http.ResponseWriter, r *http.Request) error {
	// Handle CORS preflight request
	if r.Method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, HEAD, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.WriteHeader(http.StatusNoContent)
		return nil
	}

	// Validate access token
	accessToken, err := s.accessService.ValidateToken(token)
	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}

	// Verify instance ID matches
	if accessToken.InstanceID != instanceID {
		return fmt.Errorf("token does not match instance")
	}

	// Extract the actual path from the request
	targetPath := s.extractTargetPath(r.URL.Path, instanceID, accessToken.InstanceType)
	targetPort := s.resolveTargetPort(accessToken.InstanceType, accessToken.TargetPort, targetPath)

	// Resolve the instance owner for namespace routing.
	ownerID, err := s.resolveOwnerID(ctx, accessToken)
	if err != nil {
		return fmt.Errorf("failed to resolve instance owner: %w", err)
	}

	// Get service info for the instance
	serviceInfo, err := s.getOrCreateService(ctx, ownerID, instanceID, targetPort)
	if err != nil {
		return fmt.Errorf("failed to get or create service: %w", err)
	}

	// WebSocket upstream uses ws/wss explicitly.
	targetURL := &url.URL{
		Scheme: s.resolveTargetScheme(accessToken.InstanceType, true),
		Host:   s.resolveProxyHost(ctx, ownerID, instanceID, serviceInfo),
		Path:   targetPath,
	}

	// Copy query parameters (excluding token)
	queryParams := r.URL.Query()
	queryParams.Del("token")
	if len(queryParams) > 0 {
		targetURL.RawQuery = queryParams.Encode()
	}

	upstreamHeader := http.Header{}
	for key, values := range r.Header {
		for _, value := range values {
			upstreamHeader.Add(key, value)
		}
	}
	upstreamHeader.Del("Host")
	upstreamHeader.Del("Connection")
	upstreamHeader.Del("Upgrade")
	upstreamHeader.Del("Sec-Websocket-Key")
	upstreamHeader.Del("Sec-Websocket-Version")
	upstreamHeader.Del("Sec-Websocket-Extensions")
	upstreamHeader.Set("X-Forwarded-Proto", requestScheme(r))
	upstreamHeader.Set("X-Forwarded-Prefix", fmt.Sprintf("/api/v1/instances/%d/proxy", instanceID))

	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 30 * time.Second,
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
	}

	upstreamConn, resp, err := dialer.DialContext(ctx, targetURL.String(), upstreamHeader)
	if err != nil {
		if resp != nil {
			defer resp.Body.Close()
		}
		return fmt.Errorf("failed to connect upstream websocket: %w", err)
	}
	defer upstreamConn.Close()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	responseHeader := http.Header{}
	if protocol := upstreamConn.Subprotocol(); protocol != "" {
		responseHeader.Set("Sec-WebSocket-Protocol", protocol)
	}

	clientConn, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return fmt.Errorf("failed to upgrade client websocket: %w", err)
	}
	defer clientConn.Close()

	errCh := make(chan error, 2)
	pipe := func(dst, src *websocket.Conn) {
		for {
			messageType, payload, readErr := src.ReadMessage()
			if readErr != nil {
				errCh <- readErr
				return
			}
			if writeErr := dst.WriteMessage(messageType, payload); writeErr != nil {
				errCh <- writeErr
				return
			}
		}
	}

	go pipe(upstreamConn, clientConn)
	go pipe(clientConn, upstreamConn)

	select {
	case <-ctx.Done():
		return nil
	case <-errCh:
		return nil
	}
}

// removeHopByHopHeaders removes hop-by-hop headers
func (s *InstanceProxyService) removeHopByHopHeaders(header http.Header) {
	hopByHopHeaders := []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailers",
		"Transfer-Encoding",
		"Upgrade",
	}

	for _, h := range hopByHopHeaders {
		header.Del(h)
	}

	// Remove headers listed in Connection header
	if connections := header.Get("Connection"); connections != "" {
		for _, h := range strings.Split(connections, ",") {
			header.Del(strings.TrimSpace(h))
		}
	}
}

// getOrCreateService gets service info or creates the service if it doesn't exist
func (s *InstanceProxyService) getOrCreateService(ctx context.Context, userID, instanceID int, targetPort int32) (*k8s.ServiceInfo, error) {
	// Try to get existing service
	serviceInfo, err := s.serviceService.GetServiceInfo(ctx, userID, instanceID, targetPort)
	if err == nil {
		return serviceInfo, nil
	}

	// Service doesn't exist, need to create it
	serviceConfig := k8s.ServiceConfig{
		InstanceID:      instanceID,
		InstanceName:    fmt.Sprintf("instance-%d", instanceID),
		UserID:          userID,
		ContainerPort:   targetPort,
		AdditionalPorts: s.getAdditionalPorts(targetPort),
	}

	fmt.Printf("Service not found for instance %d, creating new service...\n", instanceID)
	serviceInfo, err = s.serviceService.CreateService(ctx, serviceConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create service: %w", err)
	}

	fmt.Printf("Service created successfully for instance %d (ClusterIP: %s)\n", instanceID, serviceInfo.ClusterIP)
	return serviceInfo, nil
}

// extractTargetPath extracts the target path from the proxy URL
// Input: /api/v1/instances/24/proxy/vnc.html
// Output: /vnc.html
func (s *InstanceProxyService) extractTargetPath(requestPath string, instanceID int, instanceType string) string {
	prefix := fmt.Sprintf("/api/v1/instances/%d/proxy", instanceID)
	if usesWebtopImage(instanceType) {
		if strings.HasPrefix(requestPath, prefix) {
			path := requestPath
			if path == "" {
				return prefix + "/"
			}
			return path
		}
		return prefix + "/"
	}

	if strings.HasPrefix(requestPath, prefix) {
		path := strings.TrimPrefix(requestPath, prefix)
		if path == "" {
			return "/"
		}
		return path
	}
	return requestPath
}

// GetProxyURL generates a proxy URL for frontend
func (s *InstanceProxyService) GetProxyURL(instanceID int, token string) string {
	if token == "" {
		return fmt.Sprintf("/api/v1/instances/%d/proxy/", instanceID)
	}

	return fmt.Sprintf("/api/v1/instances/%d/proxy/?token=%s", instanceID, token)
}

// GetTargetPortForInstance returns the service target port used by the instance type.
func (s *InstanceProxyService) GetTargetPortForInstance(instance *models.Instance) int32 {
	if instance == nil {
		return 3001
	}

	return buildRuntimeConfig(instance.Type, instance.OSType, instance.OSVersion, instance.ImageRegistry, instance.ImageTag).Port
}

func (s *InstanceProxyService) resolveTargetPort(instanceType string, defaultPort int32, targetPath string) int32 {
	if usesWebtopImage(instanceType) {
		if defaultPort == 0 {
			return 3001
		}
		return defaultPort
	}

	if defaultPort == 0 {
		defaultPort = 3000
	}

	switch {
	case strings.HasPrefix(targetPath, "/websocket"),
		strings.HasPrefix(targetPath, "/websockets"),
		strings.HasPrefix(targetPath, "/signaling"),
		strings.HasPrefix(targetPath, "/turn"):
		return 8082
	default:
		return defaultPort
	}
}

func (s *InstanceProxyService) getAdditionalPorts(targetPort int32) []int32 {
	if targetPort == 3000 || targetPort == 8082 {
		return []int32{3000, 8082}
	}

	return nil
}

func (s *InstanceProxyService) resolveTargetScheme(instanceType string, websocket bool) string {
	if usesHTTPSUpstream(instanceType) {
		if websocket {
			return "wss"
		}
		return "https"
	}

	if websocket {
		return "ws"
	}

	return "http"
}

func usesHTTPSUpstream(instanceType string) bool {
	switch instanceType {
	case "ubuntu", "webtop", "openclaw":
		return true
	default:
		return false
	}
}

func (s *InstanceProxyService) resolveProxyHost(ctx context.Context, ownerID, instanceID int, serviceInfo *k8s.ServiceInfo) string {
	return fmt.Sprintf("%s:%d", serviceInfo.ClusterIP, serviceInfo.TargetPort)
}

func injectProxyBase(html, proxyBase string) string {
	baseTag := fmt.Sprintf(`<base href="%s">`, proxyBase)
	for _, tag := range []string{"<head>", "<Head>", "<HEAD>"} {
		if idx := strings.Index(html, tag); idx != -1 {
			return html[:idx+len(tag)] + baseTag + html[idx+len(tag):]
		}
	}

	return baseTag + html
}

func (s *InstanceProxyService) rewriteRedirectLocation(instanceID int, location string) string {
	if strings.HasPrefix(location, "/") && !strings.HasPrefix(location, "/api/v1/instances/") {
		return fmt.Sprintf("/api/v1/instances/%d/proxy%s", instanceID, location)
	}

	return location
}

func requestScheme(r *http.Request) string {
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
