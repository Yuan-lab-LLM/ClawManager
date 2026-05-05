package services

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"

	"github.com/gorilla/websocket"
)

// InstanceProxyService handles proxying requests to instance pods
type InstanceProxyService struct {
	serviceService  *k8s.ServiceService
	accessService   *InstanceAccessService
	httpClient      *http.Client
	serviceResolver func(ctx context.Context, userID, instanceID int, targetPort int32) (*k8s.ServiceInfo, error)
}

type InstanceProxyUpstreamAuth struct {
	OpenClawGatewayToken string
}

var errControlUIWebSocketConnectFailed = errors.New("control-ui websocket connect failed")

const controlUIWebSocketFirstFrameTimeout = 10 * time.Second

func ControlUIUpstreamAuthForInstance(instance *models.Instance, scope InstanceAccessScope) (InstanceProxyUpstreamAuth, error) {
	if scope.AccessMode != AccessModeControlUI {
		return InstanceProxyUpstreamAuth{}, nil
	}
	if instance == nil || instance.AccessToken == nil || strings.TrimSpace(*instance.AccessToken) == "" {
		return InstanceProxyUpstreamAuth{}, fmt.Errorf("control-ui upstream token is not configured")
	}
	return InstanceProxyUpstreamAuth{OpenClawGatewayToken: strings.TrimSpace(*instance.AccessToken)}, nil
}

// NewInstanceProxyService creates a new instance proxy service
func NewInstanceProxyService(accessService *InstanceAccessService) *InstanceProxyService {
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
	}
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

	scope := InstanceAccessScope{
		InstanceID:  instanceID,
		AccessMode:  AccessModeDesktop,
		TargetPort:  accessToken.TargetPort,
		RoutePrefix: fmt.Sprintf("/api/v1/instances/%d/proxy", instanceID),
	}
	if scope.TargetPort == 0 {
		scope.TargetPort = DefaultDesktopTargetPort
	}
	if _, err := validateAccessTokenScope(accessToken, scope); err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}

	return s.proxyRequest(ctx, scope, accessToken, InstanceProxyUpstreamAuth{}, w, r)
}

// ProxyRequestWithScope proxies a request to an instance using the active route scope.
func (s *InstanceProxyService) ProxyRequestWithScope(ctx context.Context, scope InstanceAccessScope, token string, w http.ResponseWriter, r *http.Request) error {
	return s.ProxyRequestWithScopeAndUpstreamAuth(ctx, scope, token, InstanceProxyUpstreamAuth{}, w, r)
}

// ProxyRequestWithScopeAndUpstreamAuth proxies a request using route auth plus optional server-side upstream auth.
func (s *InstanceProxyService) ProxyRequestWithScopeAndUpstreamAuth(ctx context.Context, scope InstanceAccessScope, token string, upstreamAuth InstanceProxyUpstreamAuth, w http.ResponseWriter, r *http.Request) error {
	// Handle CORS preflight request
	if r.Method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, HEAD, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.WriteHeader(http.StatusNoContent)
		return nil
	}

	accessToken, err := s.accessService.ValidateTokenForScope(token, scope)
	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}
	if accessToken.InstanceID != scope.InstanceID {
		return fmt.Errorf("token does not match instance")
	}

	return s.proxyRequest(ctx, scope, accessToken, upstreamAuth, w, r)
}

func (s *InstanceProxyService) proxyRequest(ctx context.Context, scope InstanceAccessScope, accessToken *AccessToken, upstreamAuth InstanceProxyUpstreamAuth, w http.ResponseWriter, r *http.Request) error {
	// Extract the actual path from the request (remove the proxy prefix)
	targetPath := s.extractTargetPathForScope(r.URL.Path, scope, accessToken.InstanceType)
	targetPort := scope.TargetPort
	if scope.AccessMode == AccessModeDesktop {
		targetPort = s.resolveTargetPort(accessToken.InstanceType, scope.TargetPort, targetPath)
	}

	// Get service info for the instance (create if not exists)
	serviceInfo, err := s.getOrCreateService(ctx, accessToken.UserID, scope.InstanceID, targetPort)
	if err != nil {
		return fmt.Errorf("failed to get or create service: %w", err)
	}

	// Build target URL
	targetURL := &url.URL{
		Scheme: s.resolveTargetSchemeForScope(accessToken.InstanceType, scope, false),
		Host:   s.resolveProxyHost(ctx, accessToken.UserID, scope.InstanceID, serviceInfo),
		Path:   targetPath,
	}

	targetURL.RawQuery = upstreamRawQueryForScope(scope, r.URL.Query())

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
	proxyReq.Header.Set("X-Forwarded-Prefix", scope.RoutePrefix)
	proxyReq.Header.Del("Accept-Encoding")

	// Remove hop-by-hop headers
	s.removeHopByHopHeaders(proxyReq.Header)
	if err := s.applyControlUIUpstreamAuth(proxyReq.Header, scope, upstreamAuth); err != nil {
		return err
	}

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
		resp.Header.Set("Location", s.rewriteRedirectLocation(scope, location))
	}

	if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("failed to read upstream html: %w", readErr)
		}
		if closeErr := resp.Body.Close(); closeErr != nil {
			return fmt.Errorf("failed to close upstream html body: %w", closeErr)
		}

		proxyBase := strings.TrimRight(scope.RoutePrefix, "/") + "/"
		modifiedBody := injectProxyBase(string(body), proxyBase)
		if scope.AccessMode == AccessModeControlUI {
			modifiedBody = rewriteControlUIIconHrefs(modifiedBody, proxyBase)
		}
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

	scope := InstanceAccessScope{
		InstanceID:  instanceID,
		AccessMode:  AccessModeDesktop,
		TargetPort:  accessToken.TargetPort,
		RoutePrefix: fmt.Sprintf("/api/v1/instances/%d/proxy", instanceID),
	}
	if scope.TargetPort == 0 {
		scope.TargetPort = DefaultDesktopTargetPort
	}
	if _, err := validateAccessTokenScope(accessToken, scope); err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}

	return s.proxyWebSocket(ctx, scope, accessToken, InstanceProxyUpstreamAuth{}, w, r)
}

// ProxyWebSocketWithScope handles WebSocket upgrade requests using the active route scope.
func (s *InstanceProxyService) ProxyWebSocketWithScope(ctx context.Context, scope InstanceAccessScope, token string, w http.ResponseWriter, r *http.Request) error {
	return s.ProxyWebSocketWithScopeAndUpstreamAuth(ctx, scope, token, InstanceProxyUpstreamAuth{}, w, r)
}

// ProxyWebSocketWithScopeAndUpstreamAuth handles WebSocket upgrade requests with optional server-side upstream auth.
func (s *InstanceProxyService) ProxyWebSocketWithScopeAndUpstreamAuth(ctx context.Context, scope InstanceAccessScope, token string, upstreamAuth InstanceProxyUpstreamAuth, w http.ResponseWriter, r *http.Request) error {
	// Handle CORS preflight request
	if r.Method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, HEAD, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.WriteHeader(http.StatusNoContent)
		return nil
	}

	accessToken, err := s.accessService.ValidateTokenForScope(token, scope)
	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}
	if accessToken.InstanceID != scope.InstanceID {
		return fmt.Errorf("token does not match instance")
	}

	return s.proxyWebSocket(ctx, scope, accessToken, upstreamAuth, w, r)
}

func (s *InstanceProxyService) proxyWebSocket(ctx context.Context, scope InstanceAccessScope, accessToken *AccessToken, upstreamAuth InstanceProxyUpstreamAuth, w http.ResponseWriter, r *http.Request) error {
	// Extract the actual path from the request
	targetPath := s.extractTargetPathForScope(r.URL.Path, scope, accessToken.InstanceType)
	targetPort := scope.TargetPort
	if scope.AccessMode == AccessModeDesktop {
		targetPort = s.resolveTargetPort(accessToken.InstanceType, scope.TargetPort, targetPath)
	}

	// Get service info for the instance
	serviceInfo, err := s.getOrCreateService(ctx, accessToken.UserID, scope.InstanceID, targetPort)
	if err != nil {
		return fmt.Errorf("failed to get or create service: %w", err)
	}

	// WebSocket upstream uses ws/wss explicitly.
	targetURL := &url.URL{
		Scheme: s.resolveTargetSchemeForScope(accessToken.InstanceType, scope, true),
		Host:   s.resolveProxyHost(ctx, accessToken.UserID, scope.InstanceID, serviceInfo),
		Path:   targetPath,
	}

	targetURL.RawQuery = upstreamRawQueryForScope(scope, r.URL.Query())

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
	upstreamHeader.Set("X-Forwarded-Prefix", scope.RoutePrefix)
	if err := s.applyControlUIUpstreamAuth(upstreamHeader, scope, upstreamAuth); err != nil {
		return err
	}

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
		return fmt.Errorf("failed to connect upstream websocket")
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

	if scope.AccessMode == AccessModeControlUI {
		if err := s.bridgeControlUIFirstConnect(clientConn, upstreamConn, upstreamAuth); err != nil {
			closeControlUIWebSocketConnectFailure(clientConn, upstreamConn)
			return nil
		}
	}

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

func (s *InstanceProxyService) bridgeControlUIFirstConnect(clientConn, upstreamConn *websocket.Conn, upstreamAuth InstanceProxyUpstreamAuth) error {
	token := strings.TrimSpace(upstreamAuth.OpenClawGatewayToken)
	if token == "" {
		return errControlUIWebSocketConnectFailed
	}

	if err := clientConn.SetReadDeadline(time.Now().Add(controlUIWebSocketFirstFrameTimeout)); err != nil {
		return errControlUIWebSocketConnectFailed
	}
	messageType, payload, err := clientConn.ReadMessage()
	_ = clientConn.SetReadDeadline(time.Time{})
	if err != nil {
		return errControlUIWebSocketConnectFailed
	}

	rewrittenPayload, err := rewriteControlUIWebSocketConnectPayload(messageType, payload, token)
	if err != nil {
		return err
	}
	if err := upstreamConn.WriteMessage(websocket.TextMessage, rewrittenPayload); err != nil {
		return errControlUIWebSocketConnectFailed
	}

	return nil
}

func rewriteControlUIWebSocketConnectPayload(messageType int, payload []byte, token string) ([]byte, error) {
	if messageType != websocket.TextMessage {
		return nil, errControlUIWebSocketConnectFailed
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errControlUIWebSocketConnectFailed
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, errControlUIWebSocketConnectFailed
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errControlUIWebSocketConnectFailed
	}

	request, ok := decoded.(map[string]any)
	if !ok {
		return nil, errControlUIWebSocketConnectFailed
	}
	method, ok := request["method"].(string)
	if !ok || method != "connect" {
		return nil, errControlUIWebSocketConnectFailed
	}
	params, ok := request["params"].(map[string]any)
	if !ok {
		return nil, errControlUIWebSocketConnectFailed
	}

	rewrittenParams := make(map[string]any, len(params)+1)
	for key, value := range params {
		if key == "auth" {
			continue
		}
		rewrittenParams[key] = value
	}
	rewrittenParams["auth"] = map[string]any{"token": token}
	request["params"] = rewrittenParams

	rewrittenPayload, err := json.Marshal(request)
	if err != nil {
		return nil, errControlUIWebSocketConnectFailed
	}
	return rewrittenPayload, nil
}

func closeControlUIWebSocketConnectFailure(clientConn, upstreamConn *websocket.Conn) {
	closeMessage := websocket.FormatCloseMessage(websocket.ClosePolicyViolation, errControlUIWebSocketConnectFailed.Error())
	deadline := time.Now().Add(time.Second)
	_ = clientConn.WriteControl(websocket.CloseMessage, closeMessage, deadline)
	_ = upstreamConn.WriteControl(websocket.CloseMessage, closeMessage, deadline)
}

func upstreamRawQueryForScope(scope InstanceAccessScope, source url.Values) string {
	query := url.Values{}
	for key, values := range source {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	query.Del("token")
	if scope.AccessMode == AccessModeControlUI {
		query.Del("password")
	}
	return query.Encode()
}

func (s *InstanceProxyService) applyControlUIUpstreamAuth(header http.Header, scope InstanceAccessScope, upstreamAuth InstanceProxyUpstreamAuth) error {
	if scope.AccessMode != AccessModeControlUI {
		return nil
	}

	token := strings.TrimSpace(upstreamAuth.OpenClawGatewayToken)
	if token == "" {
		return fmt.Errorf("control-ui upstream token is not configured")
	}

	header.Del("Authorization")
	header.Del("Cookie")
	header.Del("X-OpenClaw-Token")
	header.Set("Authorization", "Bearer "+token)
	return nil
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
	if s.serviceResolver != nil {
		return s.serviceResolver(ctx, userID, instanceID, targetPort)
	}

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
	scope := InstanceAccessScope{
		InstanceID:  instanceID,
		AccessMode:  AccessModeDesktop,
		RoutePrefix: fmt.Sprintf("/api/v1/instances/%d/proxy", instanceID),
	}
	return s.extractTargetPathForScope(requestPath, scope, instanceType)
}

func (s *InstanceProxyService) extractTargetPathForScope(requestPath string, scope InstanceAccessScope, instanceType string) string {
	prefix := strings.TrimRight(scope.RoutePrefix, "/")
	if scope.AccessMode == AccessModeDesktop && usesWebtopImage(instanceType) {
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
	if targetPort == DefaultControlUITargetPort {
		return []int32{DefaultDesktopTargetPort}
	}

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

func (s *InstanceProxyService) resolveTargetSchemeForScope(instanceType string, scope InstanceAccessScope, websocket bool) string {
	if scope.AccessMode == AccessModeControlUI {
		if websocket {
			return "ws"
		}
		return "http"
	}

	return s.resolveTargetScheme(instanceType, websocket)
}

func usesHTTPSUpstream(instanceType string) bool {
	switch instanceType {
	case "ubuntu", "webtop", "openclaw":
		return true
	default:
		return false
	}
}

func (s *InstanceProxyService) resolveProxyHost(ctx context.Context, userID, instanceID int, serviceInfo *k8s.ServiceInfo) string {
	servicePort := serviceInfo.ServicePort
	if servicePort == 0 {
		servicePort = serviceInfo.TargetPort
	}
	return fmt.Sprintf("%s:%d", serviceInfo.ClusterIP, servicePort)
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

var (
	htmlLinkTagPattern     = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	htmlLinkIconRelPattern = regexp.MustCompile(`(?is)\brel\s*=\s*("[^"]*icon[^"]*"|'[^']*icon[^']*')`)
	htmlFaviconHrefPattern = regexp.MustCompile(`(?is)\bhref\s*=\s*("((?:\./)?favicon\.svg|/favicon\.svg)"|'((?:\./)?favicon\.svg|/favicon\.svg)')`)
)

func rewriteControlUIIconHrefs(html, proxyBase string) string {
	scopedIconHref := fmt.Sprintf(`href="%sfavicon.svg"`, proxyBase)
	return htmlLinkTagPattern.ReplaceAllStringFunc(html, func(tag string) string {
		if !htmlLinkIconRelPattern.MatchString(tag) {
			return tag
		}
		return htmlFaviconHrefPattern.ReplaceAllString(tag, scopedIconHref)
	})
}

func (s *InstanceProxyService) rewriteRedirectLocation(scope InstanceAccessScope, location string) string {
	if strings.HasPrefix(location, "/") && !strings.HasPrefix(location, "/api/v1/instances/") {
		return strings.TrimRight(scope.RoutePrefix, "/") + location
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
