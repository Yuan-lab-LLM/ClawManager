package services

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"

	"github.com/gorilla/websocket"
)

// InstanceProxyService handles proxying requests to instance pods
type InstanceProxyService struct {
	serviceService       *k8s.ServiceService
	accessService        *InstanceAccessService
	instanceRepo         repository.InstanceRepository
	runtimePodRepo       repository.RuntimePodRepository
	bindingRepo          repository.InstanceRuntimeBindingRepository
	httpClient           *http.Client
	openClawGatewayToken string
	openClawProxyOrigin  string
	serviceCache         map[serviceCacheKey]serviceCacheEntry
	serviceLookups       map[serviceCacheKey]*serviceLookupCall
	cacheMu              sync.RWMutex
	lookupMu             sync.Mutex
	serviceTTL           time.Duration
}

type serviceCacheKey struct {
	userID     int
	instanceID int
	targetPort int32
}

type serviceCacheEntry struct {
	serviceInfo *k8s.ServiceInfo
	expiresAt   time.Time
}

type serviceLookupCall struct {
	done        chan struct{}
	serviceInfo *k8s.ServiceInfo
	err         error
}

const defaultServiceCacheTTL = 30 * time.Second

var ErrInstanceGatewayUnavailable = errors.New("instance gateway is not available")

type InstanceProxyServiceOption func(*InstanceProxyService)

func WithInstanceProxyRuntimeRepositories(instanceRepo repository.InstanceRepository, runtimePodRepo repository.RuntimePodRepository, bindingRepo repository.InstanceRuntimeBindingRepository) InstanceProxyServiceOption {
	return func(s *InstanceProxyService) {
		s.instanceRepo = instanceRepo
		s.runtimePodRepo = runtimePodRepo
		s.bindingRepo = bindingRepo
	}
}

// NewInstanceProxyService creates a new instance proxy service
func NewInstanceProxyService(accessService *InstanceAccessService, options ...InstanceProxyServiceOption) *InstanceProxyService {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   128,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}

	service := &InstanceProxyService{
		serviceService: k8s.NewServiceService(),
		accessService:  accessService,
		httpClient: &http.Client{
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Don't follow redirects automatically, let the client handle them
				return http.ErrUseLastResponse
			},
		},
		openClawGatewayToken: strings.TrimSpace(os.Getenv("OPENCLAW_GATEWAY_TOKEN")),
		openClawProxyOrigin:  resolveOpenClawProxyOriginFromEnv(),
		serviceCache:         make(map[serviceCacheKey]serviceCacheEntry),
		serviceLookups:       make(map[serviceCacheKey]*serviceLookupCall),
		serviceTTL:           defaultServiceCacheTTL,
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
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

	effectiveRequestPath := canonicalProxyEntryRequestPath(r.URL.Path, accessToken, instanceID)

	// Extract the actual path from the request (remove the proxy prefix)
	targetPath := s.extractTargetPath(effectiveRequestPath, instanceID, accessToken.InstanceType)
	targetPort := s.resolveTargetPort(accessToken.InstanceType, accessToken.TargetPort, targetPath)
	shouldRewriteHTML := s.shouldRewriteHTMLForProxy(instanceID, accessToken.InstanceType)

	// Build target URL
	targetURL, err := s.resolveHTTPProxyTarget(ctx, accessToken, instanceID, targetPort, targetPath, effectiveRequestPath)
	if err != nil {
		return err
	}

	managedGatewayToken := s.managedRuntimeGatewayBearerToken(ctx, instanceID, accessToken.InstanceType)
	proxyPrefix := hermesProxyPrefix(instanceID)
	hermesLite := s.isHermesLiteProxyInstance(instanceID, accessToken.InstanceType)
	bootstrapPath := stripInstanceProxyPrefix(targetPath, instanceID)

	// Copy query parameters, excluding ClawManager-owned proxy/gateway tokens.
	queryParams := r.URL.Query()
	s.removeProxyAccessTokenQuery(queryParams, token, managedGatewayToken)
	if len(queryParams) > 0 {
		targetURL.RawQuery = queryParams.Encode()
	}

	// Create new request with longer timeout for streaming
	proxyCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var bootstrapSetCookies []string
	if hermesLite && shouldBootstrapHermesDashboardSession(r, bootstrapPath) && strings.TrimSpace(managedGatewayToken) != "" {
		if cookies, bootErr := s.bootstrapHermesDashboardSession(proxyCtx, targetURL, instanceID, managedGatewayToken, r); bootErr == nil {
			bootstrapSetCookies = cookies
		}
	}

	// After a successful bootstrap on non-chat entry points, send the browser
	// to /chat with session cookies instead of serving the login HTML.
	if len(bootstrapSetCookies) > 0 && shouldRedirectHermesBootstrapToChat(bootstrapPath) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		for _, cookie := range bootstrapSetCookies {
			if strings.TrimSpace(cookie) == "" {
				continue
			}
			w.Header().Add("Set-Cookie", cookie)
		}
		w.Header().Set("Location", hermesChatProxyLocation(instanceID, token))
		w.Header().Del("X-Frame-Options")
		w.Header().Del("Content-Security-Policy")
		w.WriteHeader(http.StatusFound)
		return nil
	}

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
	attachCookiesToRequest(proxyReq, bootstrapSetCookies)

	// Set X-Forwarded headers
	proxyReq.Header.Set("X-Forwarded-For", r.RemoteAddr)
	proxyReq.Header.Set("X-Forwarded-Host", r.Host)
	proxyReq.Header.Set("X-Forwarded-Proto", requestScheme(r))
	proxyReq.Header.Set("X-Forwarded-Prefix", proxyPrefix)
	if !isHermesDashboardPublicAuthPath(bootstrapPath) {
		setManagedRuntimeGatewayAuthHeaders(proxyReq.Header, managedGatewayToken)
	}
	if shouldRewriteHTML {
		proxyReq.Header.Del("Accept-Encoding")
	}

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

	if shouldRewriteHTML && strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("failed to read upstream html: %w", readErr)
		}
		if closeErr := resp.Body.Close(); closeErr != nil {
			return fmt.Errorf("failed to close upstream html body: %w", closeErr)
		}

		modifiedBody := injectProxyBase(string(body), proxyBaseForRequestPath(effectiveRequestPath, instanceID))
		if hermesLite {
			modifiedBody = injectHermesAbsolutePathPatch(modifiedBody, proxyPrefix)
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
	for _, cookie := range bootstrapSetCookies {
		if strings.TrimSpace(cookie) == "" {
			continue
		}
		w.Header().Add("Set-Cookie", cookie)
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

	targetURL, err := s.resolveWebSocketProxyTarget(ctx, accessToken, instanceID, targetPort, targetPath, r.URL.Path)
	if err != nil {
		return err
	}

	managedGatewayToken := s.managedRuntimeGatewayBearerToken(ctx, instanceID, accessToken.InstanceType)
	upstreamPath := stripInstanceProxyPrefix(targetPath, instanceID)
	hermesLite := s.isHermesLiteProxyInstance(instanceID, accessToken.InstanceType)
	skipManagedWSAuth := hermesLite && isHermesDashboardTicketWebSocket(upstreamPath, r.URL.Query())

	// Copy query parameters, excluding ClawManager-owned proxy/gateway tokens.
	queryParams := r.URL.Query()
	s.removeProxyAccessTokenQuery(queryParams, token, managedGatewayToken)
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
	upstreamHeader.Set("X-Forwarded-For", r.RemoteAddr)
	upstreamHeader.Set("X-Forwarded-Host", r.Host)
	upstreamHeader.Set("X-Forwarded-Proto", requestScheme(r))
	upstreamHeader.Set("X-Forwarded-Prefix", fmt.Sprintf("/api/v1/instances/%d/proxy", instanceID))
	// Hermes dashboard chat uses cookie + ticket query auth. Do not inject
	// managed Bearer/API-Key headers or rewrite Origin for those sockets.
	if skipManagedWSAuth {
		upstreamHeader.Del("Authorization")
		upstreamHeader.Del("X-Api-Key")
		upstreamHeader.Del("X-OpenAI-Api-Key")
		upstreamHeader.Del("OpenAI-Api-Key")
		upstreamHeader.Del("X-ClawManager-Instance-Token")
		upstreamHeader.Del("X-ClawManager-LLM-API-Key")
	} else {
		setManagedRuntimeGatewayAuthHeaders(upstreamHeader, managedGatewayToken)
		if managedGatewayToken != "" {
			upstreamHeader.Set("Origin", s.openClawWebSocketOrigin(targetURL))
		}
	}

	// Keep the pipe alive for the full WebSocket lifetime; do not inherit any
	// short deadlines that may be attached to the inbound request context.
	proxyCtx := ctx
	if ctx != nil {
		proxyCtx = context.WithoutCancel(ctx)
	}

	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 30 * time.Second,
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
		ReadBufferSize:   1024 * 1024,
		WriteBufferSize:  1024 * 1024,
	}

	upstreamConn, resp, err := dialer.DialContext(proxyCtx, targetURL.String(), upstreamHeader)
	if err != nil {
		if resp != nil {
			defer resp.Body.Close()
		}
		return fmt.Errorf("failed to connect upstream websocket: %w", err)
	}
	defer upstreamConn.Close()
	upstreamConn.SetReadLimit(hermesWebSocketMaxMessageBytes)

	upgrader := websocket.Upgrader{
		CheckOrigin:     func(r *http.Request) bool { return true },
		ReadBufferSize:  1024 * 1024,
		WriteBufferSize: 1024 * 1024,
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
	clientConn.SetReadLimit(hermesWebSocketMaxMessageBytes)

	errCh := make(chan error, 2)
	pipe := func(dst, src *websocket.Conn) {
		for {
			messageType, reader, readErr := src.NextReader()
			if readErr != nil {
				errCh <- readErr
				return
			}
			writer, writeErr := dst.NextWriter(messageType)
			if writeErr != nil {
				errCh <- writeErr
				return
			}
			if _, copyErr := io.Copy(writer, reader); copyErr != nil {
				_ = writer.Close()
				errCh <- copyErr
				return
			}
			if closeErr := writer.Close(); closeErr != nil {
				errCh <- closeErr
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

func setManagedRuntimeGatewayAuthHeaders(header http.Header, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	header.Set("Authorization", "Bearer "+token)
	header.Set("X-Api-Key", token)
	header.Set("X-OpenAI-Api-Key", token)
	header.Set("OpenAI-Api-Key", token)
	header.Set("X-ClawManager-Instance-Token", token)
	header.Set("X-ClawManager-LLM-API-Key", token)
}

func hermesProxyPrefix(instanceID int) string {
	return fmt.Sprintf("/api/v1/instances/%d/proxy", instanceID)
}

func (s *InstanceProxyService) isHermesLiteProxyInstance(instanceID int, instanceType string) bool {
	if s == nil || s.instanceRepo == nil || !strings.EqualFold(strings.TrimSpace(instanceType), RuntimeTypeHermes) {
		return false
	}
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil || instance == nil {
		return false
	}
	runtimeType, ok := v2RuntimeTypeForInstance(instance)
	return ok && runtimeType == RuntimeTypeHermes
}

func isHermesDashboardPublicAuthPath(targetPath string) bool {
	path := strings.TrimSpace(targetPath)
	if path == "" {
		return false
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	switch {
	case path == "/login", strings.HasPrefix(path, "/login?"):
		return true
	case path == "/auth", strings.HasPrefix(path, "/auth/"):
		return true
	default:
		return false
	}
}

const hermesWebSocketMaxMessageBytes int64 = 8 << 20 // 8 MiB PTY snapshots

func isHermesDashboardTicketWebSocket(targetPath string, query url.Values) bool {
	if query != nil && strings.TrimSpace(query.Get("ticket")) != "" {
		return true
	}
	path := normalizeHermesBootstrapPath(targetPath)
	switch path {
	case "/api/pty", "/api/events":
		return true
	default:
		return strings.HasPrefix(path, "/api/pty/") || strings.HasPrefix(path, "/api/events/")
	}
}

func isHermesSessionCookieName(name string) bool {
	bare := strings.TrimSpace(name)
	for _, prefix := range []string{"__Host-", "__Secure-"} {
		if strings.HasPrefix(bare, prefix) {
			bare = strings.TrimPrefix(bare, prefix)
			break
		}
	}
	return bare == "hermes_session_at"
}

func requestHasHermesSessionCookie(r *http.Request) bool {
	if r == nil {
		return false
	}
	for _, cookie := range r.Cookies() {
		if isHermesSessionCookieName(cookie.Name) && strings.TrimSpace(cookie.Value) != "" {
			return true
		}
	}
	return false
}

func shouldBootstrapHermesDashboardSession(r *http.Request, targetPath string) bool {
	if r == nil {
		return false
	}
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method != http.MethodGet && method != http.MethodHead {
		return false
	}
	if requestHasHermesSessionCookie(r) {
		return false
	}
	path := normalizeHermesBootstrapPath(targetPath)
	switch path {
	case "/", "/chat", "/login":
		return true
	}
	if strings.HasPrefix(path, "/login") {
		return true
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	if strings.Contains(accept, "text/html") &&
		!strings.HasPrefix(path, "/api") &&
		!strings.HasPrefix(path, "/auth") &&
		!strings.HasPrefix(path, "/assets") {
		return true
	}
	return false
}

func normalizeHermesBootstrapPath(targetPath string) string {
	path := strings.TrimSpace(targetPath)
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	trimmed := strings.TrimSuffix(path, "/")
	if trimmed == "" {
		return "/"
	}
	return trimmed
}

func shouldRedirectHermesBootstrapToChat(bootstrapPath string) bool {
	return normalizeHermesBootstrapPath(bootstrapPath) != "/chat"
}

func hermesChatProxyLocation(instanceID int, proxyToken string) string {
	location := fmt.Sprintf("/api/v1/instances/%d/proxy/chat/", instanceID)
	if token := strings.TrimSpace(proxyToken); token != "" {
		return location + "?token=" + url.QueryEscape(token)
	}
	return location
}

func (s *InstanceProxyService) bootstrapHermesDashboardSession(
	ctx context.Context,
	upstreamTarget *url.URL,
	instanceID int,
	password string,
	clientReq *http.Request,
) ([]string, error) {
	if s == nil || s.httpClient == nil || upstreamTarget == nil {
		return nil, fmt.Errorf("hermes bootstrap unavailable")
	}
	password = strings.TrimSpace(password)
	if password == "" {
		return nil, fmt.Errorf("hermes bootstrap password missing")
	}

	loginURL := &url.URL{
		Scheme: upstreamTarget.Scheme,
		Host:   upstreamTarget.Host,
		Path:   "/auth/password-login",
	}
	body, err := json.Marshal(map[string]string{
		"provider": "basic",
		"username": "clawmanager",
		"password": password,
		"next":     "/chat",
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if clientReq != nil {
		req.Header.Set("X-Forwarded-For", clientReq.RemoteAddr)
		req.Header.Set("X-Forwarded-Host", clientReq.Host)
		req.Header.Set("X-Forwarded-Proto", requestScheme(clientReq))
	}
	req.Header.Set("X-Forwarded-Prefix", hermesProxyPrefix(instanceID))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("hermes password-login status %d", resp.StatusCode)
	}
	cookies := collectUpstreamSetCookies(resp)
	if len(cookies) == 0 {
		return nil, fmt.Errorf("hermes password-login returned no cookies")
	}
	return cookies, nil
}

func collectUpstreamSetCookies(resp *http.Response) []string {
	if resp == nil {
		return nil
	}
	if values := resp.Header.Values("Set-Cookie"); len(values) > 0 {
		return append([]string(nil), values...)
	}
	if values := resp.Header["Set-Cookie"]; len(values) > 0 {
		return append([]string(nil), values...)
	}
	out := make([]string, 0, len(resp.Cookies()))
	for _, cookie := range resp.Cookies() {
		if cookie == nil || strings.TrimSpace(cookie.Name) == "" {
			continue
		}
		out = append(out, cookie.String())
	}
	return out
}

func attachCookiesToRequest(req *http.Request, setCookies []string) {
	if req == nil || len(setCookies) == 0 {
		return
	}
	existing := req.Header.Get("Cookie")
	parts := make([]string, 0, len(setCookies)+1)
	if strings.TrimSpace(existing) != "" {
		parts = append(parts, existing)
	}
	for _, raw := range setCookies {
		name, value, ok := parseSetCookiePair(raw)
		if !ok {
			continue
		}
		parts = append(parts, name+"="+value)
	}
	if len(parts) == 0 {
		return
	}
	req.Header.Set("Cookie", strings.Join(parts, "; "))
}

func parseSetCookiePair(raw string) (name, value string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	pair := strings.SplitN(raw, ";", 2)[0]
	name, value, found := strings.Cut(pair, "=")
	name = strings.TrimSpace(name)
	if !found || name == "" {
		return "", "", false
	}
	return name, value, true
}

func injectHermesAbsolutePathPatch(html, proxyPrefix string) string {
	prefix := strings.TrimRight(strings.TrimSpace(proxyPrefix), "/")
	if prefix == "" || html == "" {
		return html
	}
	// Keep the script brace-safe for fmt; prefix is JSON-quoted for JS.
	prefixJSON, err := json.Marshal(prefix)
	if err != nil {
		return html
	}
	script := `<script>(function(p){if(!p)return;function fix(u){if(typeof u!=="string")return u;if(!u||u.charAt(0)!=="/"||u.indexOf("//")===0)return u;if(u===p||u.indexOf(p+"/")===0)return u;return p+u;}var of=window.fetch;if(typeof of==="function"){window.fetch=function(input,init){if(typeof input==="string"){input=fix(input);}else if(input&&typeof input.url==="string"){try{input=new Request(fix(input.url),input);}catch(e){}}return of.call(this,input,init);};}if(window.XMLHttpRequest&&XMLHttpRequest.prototype){var oo=XMLHttpRequest.prototype.open;XMLHttpRequest.prototype.open=function(method,url){if(typeof url==="string"){arguments[1]=fix(url);}return oo.apply(this,arguments);};}function wrap(fn){return function(url){if(typeof url==="string"){url=fix(url);}return fn.call(this,url);};}try{var la=window.location.assign.bind(window.location);window.location.assign=wrap(la);}catch(e){}try{var lr=window.location.replace.bind(window.location);window.location.replace=wrap(lr);}catch(e){}})(` + string(prefixJSON) + `);</script>`

	for _, tag := range []string{"<head>", "<Head>", "<HEAD>"} {
		if idx := strings.Index(html, tag); idx != -1 {
			insertAt := idx + len(tag)
			return html[:insertAt] + script + html[insertAt:]
		}
	}
	return script + html
}

func (s *InstanceProxyService) managedRuntimeGatewayBearerToken(ctx context.Context, instanceID int, instanceType string) string {
	if s == nil || s.instanceRepo == nil {
		return ""
	}
	normalizedType, managedType := NormalizeV2RuntimeType(instanceType)
	if !managedType {
		return ""
	}
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil || instance == nil {
		return ""
	}
	if runtimeType, ok := v2RuntimeTypeForInstance(instance); ok && runtimeType == normalizedType {
		if instance.AccessToken != nil && strings.TrimSpace(*instance.AccessToken) != "" {
			return strings.TrimSpace(*instance.AccessToken)
		}
	}
	if normalizedType == RuntimeTypeOpenClaw && s.openClawGatewayToken != "" {
		return s.openClawGatewayToken
	}
	return ""
}

func (s *InstanceProxyService) resolveHTTPProxyTarget(ctx context.Context, accessToken *AccessToken, instanceID int, targetPort int32, targetPath, requestPath string) (*url.URL, error) {
	if targetURL, ok, err := s.resolveV2ProxyTarget(ctx, accessToken, instanceID, targetPath, requestPath, false); ok || err != nil {
		return targetURL, err
	}
	serviceInfo, err := s.getOrCreateService(ctx, accessToken.UserID, instanceID, targetPort)
	if err != nil {
		return nil, fmt.Errorf("failed to get or create service: %w", err)
	}
	return &url.URL{
		Scheme: s.resolveTargetScheme(accessToken.InstanceType, false),
		Host:   s.resolveProxyHost(ctx, accessToken.UserID, instanceID, serviceInfo),
		Path:   targetPath,
	}, nil
}

func (s *InstanceProxyService) resolveWebSocketProxyTarget(ctx context.Context, accessToken *AccessToken, instanceID int, targetPort int32, targetPath, requestPath string) (*url.URL, error) {
	if targetURL, ok, err := s.resolveV2ProxyTarget(ctx, accessToken, instanceID, targetPath, requestPath, true); ok || err != nil {
		return targetURL, err
	}
	serviceInfo, err := s.getOrCreateService(ctx, accessToken.UserID, instanceID, targetPort)
	if err != nil {
		return nil, fmt.Errorf("failed to get or create service: %w", err)
	}
	return &url.URL{
		Scheme: s.resolveTargetScheme(accessToken.InstanceType, true),
		Host:   s.resolveProxyHost(ctx, accessToken.UserID, instanceID, serviceInfo),
		Path:   targetPath,
	}, nil
}

func (s *InstanceProxyService) resolveV2ProxyTarget(ctx context.Context, accessToken *AccessToken, instanceID int, targetPath, requestPath string, websocket bool) (*url.URL, bool, error) {
	if s.instanceRepo == nil || s.bindingRepo == nil || s.runtimePodRepo == nil {
		return nil, false, nil
	}
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get instance for proxy: %w", err)
	}
	if instance == nil {
		return nil, false, ErrInstanceGatewayUnavailable
	}
	if instance.UserID != accessToken.UserID {
		return nil, false, fmt.Errorf("token does not match instance owner")
	}
	if _, ok := v2RuntimeTypeForInstance(instance); !ok {
		return nil, false, nil
	}
	if !strings.EqualFold(strings.TrimSpace(instance.Status), "running") {
		return nil, true, ErrInstanceGatewayUnavailable
	}

	binding, err := s.bindingRepo.GetRunningByInstanceID(ctx, instanceID)
	if err != nil {
		return nil, true, fmt.Errorf("%w: %v", ErrInstanceGatewayUnavailable, err)
	}
	if binding == nil {
		return nil, true, ErrInstanceGatewayUnavailable
	}
	if binding.Generation != instance.RuntimeGeneration {
		return nil, true, ErrInstanceGatewayUnavailable
	}
	pod, err := s.runtimePodRepo.GetByID(ctx, binding.RuntimePodID)
	if err != nil {
		return nil, true, fmt.Errorf("%w: %v", ErrInstanceGatewayUnavailable, err)
	}
	if pod == nil || pod.PodIP == nil || strings.TrimSpace(*pod.PodIP) == "" || binding.GatewayPort <= 0 {
		return nil, true, ErrInstanceGatewayUnavailable
	}
	scheme := "http"
	if websocket {
		scheme = "ws"
	}
	upstreamPath := stripInstanceProxyPrefix(targetPath, instanceID)
	if shouldPreserveOpenClawControlUIPath(instance) {
		upstreamPath = openClawControlUIRequestPath(requestPath, instanceID)
	}
	return &url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(strings.TrimSpace(*pod.PodIP), strconv.Itoa(binding.GatewayPort)),
		Path:   upstreamPath,
	}, true, nil
}

// getOrCreateService gets service info or creates the service if it doesn't exist
func (s *InstanceProxyService) getOrCreateService(ctx context.Context, userID, instanceID int, targetPort int32) (*k8s.ServiceInfo, error) {
	cacheKey := serviceCacheKey{
		userID:     userID,
		instanceID: instanceID,
		targetPort: targetPort,
	}
	if cached := s.getCachedService(cacheKey); cached != nil {
		return cached, nil
	}

	call, leader := s.getOrCreateLookup(cacheKey)
	if !leader {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("service lookup canceled: %w", ctx.Err())
		case <-call.done:
			if call.err != nil {
				return nil, call.err
			}
			return cloneServiceInfo(call.serviceInfo), nil
		}
	}

	defer s.finishLookup(cacheKey, call)

	serviceInfo, err := s.serviceService.GetServiceInfo(ctx, userID, instanceID, targetPort)
	if err == nil {
		s.storeCachedService(cacheKey, serviceInfo)
		call.serviceInfo = cloneServiceInfo(serviceInfo)
		return cloneServiceInfo(serviceInfo), nil
	}

	// Try to get existing service
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
		call.err = fmt.Errorf("failed to create service: %w", err)
		return nil, call.err
	}

	s.storeCachedService(cacheKey, serviceInfo)
	call.serviceInfo = cloneServiceInfo(serviceInfo)
	fmt.Printf("Service created successfully for instance %d (ClusterIP: %s)\n", instanceID, serviceInfo.ClusterIP)
	return cloneServiceInfo(serviceInfo), nil
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

func stripInstanceProxyPrefix(requestPath string, instanceID int) string {
	prefix := fmt.Sprintf("/api/v1/instances/%d/proxy", instanceID)
	if strings.HasPrefix(requestPath, prefix) {
		path := strings.TrimPrefix(requestPath, prefix)
		if path == "" {
			return "/"
		}
		return path
	}
	return requestPath
}

func canonicalProxyEntryRequestPath(requestPath string, accessToken *AccessToken, instanceID int) string {
	prefix := fmt.Sprintf("/api/v1/instances/%d/proxy", instanceID)
	path := strings.TrimSpace(requestPath)
	if path != prefix && path != prefix+"/" {
		return requestPath
	}
	if accessToken == nil || strings.TrimSpace(accessToken.AccessURL) == "" {
		return requestPath
	}
	parsed, err := url.Parse(accessToken.AccessURL)
	if err != nil {
		return requestPath
	}
	entryPath := strings.TrimSpace(parsed.Path)
	if entryPath == "" || entryPath == prefix || entryPath == prefix+"/" {
		return requestPath
	}
	if strings.HasPrefix(entryPath, prefix+"/") {
		return entryPath
	}
	return requestPath
}

func shouldPreserveOpenClawControlUIPath(instance *models.Instance) bool {
	if instance == nil || !strings.EqualFold(strings.TrimSpace(instance.Type), RuntimeTypeOpenClaw) {
		return false
	}
	_, ok := v2RuntimeTypeForInstance(instance)
	return ok
}

func openClawControlUIRequestPath(requestPath string, instanceID int) string {
	prefix := fmt.Sprintf("/api/v1/instances/%d/proxy", instanceID)
	path := strings.TrimSpace(requestPath)
	if path == "" || path == prefix {
		return prefix + "/"
	}
	if strings.HasPrefix(path, prefix) {
		return path
	}
	if strings.HasPrefix(path, "/") {
		return prefix + path
	}
	return prefix + "/" + path
}

// GetProxyURL generates a proxy URL for frontend
func (s *InstanceProxyService) GetProxyURL(instanceID int, token string) string {
	return proxyURLWithPath(instanceID, "/", token)
}

// GetProxyURLForInstance generates the best frontend entry URL for an instance.
func (s *InstanceProxyService) GetProxyURLForInstance(instance *models.Instance, token string) string {
	if instance == nil {
		return ""
	}
	if runtimeType, ok := v2RuntimeTypeForInstance(instance); ok && runtimeType == RuntimeTypeHermes {
		return proxyURLWithPath(instance.ID, "/chat", token)
	}
	return proxyURLWithPath(instance.ID, "/", token)
}

// GetTargetPortForInstance returns the service target port used by the instance type.
func (s *InstanceProxyService) GetTargetPortForInstance(instance *models.Instance) int32 {
	if instance == nil {
		return 3001
	}

	return buildRuntimeConfig(instance.Type, instance.OSType, instance.OSVersion, instance.ImageRegistry, instance.ImageTag).Port
}

// ResolveUpstreamHostPort ensures the instance Service exists and returns its
// cluster-internal "host:port" target so the edge gateway can proxy directly to
// the instance without routing pixel traffic through this control-plane process.
func (s *InstanceProxyService) ResolveUpstreamHostPort(ctx context.Context, userID, instanceID int, targetPort int32) (string, error) {
	serviceInfo, err := s.getOrCreateService(ctx, userID, instanceID, targetPort)
	if err != nil {
		return "", fmt.Errorf("failed to resolve upstream service: %w", err)
	}

	return fmt.Sprintf("%s.%s.svc.cluster.local:%d", serviceInfo.Name, serviceInfo.Namespace, serviceInfo.TargetPort), nil
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
	case "ubuntu", "webtop", "hermes", "openclaw", "workbuddy":
		return true
	default:
		return false
	}
}

func (s *InstanceProxyService) resolveProxyHost(ctx context.Context, userID, instanceID int, serviceInfo *k8s.ServiceInfo) string {
	return fmt.Sprintf("%s:%d", serviceInfo.ClusterIP, serviceInfo.TargetPort)
}

func (s *InstanceProxyService) shouldRewriteHTML(instanceType string) bool {
	return !usesWebtopImage(instanceType)
}

func (s *InstanceProxyService) shouldRewriteHTMLForProxy(instanceID int, instanceType string) bool {
	if s != nil && s.instanceRepo != nil && strings.EqualFold(strings.TrimSpace(instanceType), RuntimeTypeHermes) {
		instance, err := s.instanceRepo.GetByID(instanceID)
		if err == nil && instance != nil {
			if runtimeType, ok := v2RuntimeTypeForInstance(instance); ok && runtimeType == RuntimeTypeHermes {
				return true
			}
		}
	}
	return s.shouldRewriteHTML(instanceType)
}

// IsWebtopInstanceType reports whether the instance type is served by a
// Webtop/KasmVNC desktop image (and therefore eligible for direct gateway
// proxying via SUBFOLDER-prefixed paths).
func (s *InstanceProxyService) IsWebtopInstanceType(instanceType string) bool {
	return usesWebtopImage(instanceType)
}

func (s *InstanceProxyService) getCachedService(key serviceCacheKey) *k8s.ServiceInfo {
	s.cacheMu.RLock()
	entry, ok := s.serviceCache[key]
	s.cacheMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		if ok {
			s.cacheMu.Lock()
			delete(s.serviceCache, key)
			s.cacheMu.Unlock()
		}
		return nil
	}

	return cloneServiceInfo(entry.serviceInfo)
}

func (s *InstanceProxyService) storeCachedService(key serviceCacheKey, serviceInfo *k8s.ServiceInfo) {
	s.cacheMu.Lock()
	s.serviceCache[key] = serviceCacheEntry{
		serviceInfo: cloneServiceInfo(serviceInfo),
		expiresAt:   time.Now().Add(s.serviceTTL),
	}
	s.cacheMu.Unlock()
}

func (s *InstanceProxyService) getOrCreateLookup(key serviceCacheKey) (*serviceLookupCall, bool) {
	s.lookupMu.Lock()
	defer s.lookupMu.Unlock()

	if existing, ok := s.serviceLookups[key]; ok {
		return existing, false
	}

	call := &serviceLookupCall{
		done: make(chan struct{}),
	}
	s.serviceLookups[key] = call
	return call, true
}

func (s *InstanceProxyService) finishLookup(key serviceCacheKey, call *serviceLookupCall) {
	s.lookupMu.Lock()
	delete(s.serviceLookups, key)
	close(call.done)
	s.lookupMu.Unlock()
}

func cloneServiceInfo(serviceInfo *k8s.ServiceInfo) *k8s.ServiceInfo {
	if serviceInfo == nil {
		return nil
	}

	cloned := *serviceInfo
	return &cloned
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

func proxyURLWithPath(instanceID int, targetPath, token string) string {
	path := strings.TrimSpace(targetPath)
	if path == "" || path == "/" {
		path = "/"
	} else {
		path = "/" + strings.TrimLeft(path, "/")
		if !strings.HasSuffix(path, "/") {
			path += "/"
		}
	}

	raw := fmt.Sprintf("/api/v1/instances/%d/proxy%s", instanceID, path)
	if token == "" {
		return raw
	}
	return fmt.Sprintf("%s?token=%s", raw, url.QueryEscape(token))
}

func (s *InstanceProxyService) removeProxyAccessTokenQuery(query url.Values, accessToken, managedGatewayToken string) {
	values := query["token"]
	if len(values) == 0 {
		return
	}
	filtered := values[:0]
	for _, value := range values {
		if !s.shouldRemoveProxyTokenQueryValue(value, accessToken, managedGatewayToken) {
			filtered = append(filtered, value)
		}
	}
	if len(filtered) == 0 {
		query.Del("token")
		return
	}
	query["token"] = filtered
}

func (s *InstanceProxyService) shouldRemoveProxyTokenQueryValue(value, accessToken, managedGatewayToken string) bool {
	candidate := strings.TrimSpace(value)
	if candidate == "" {
		return false
	}
	if candidate == accessToken || (managedGatewayToken != "" && candidate == managedGatewayToken) {
		return true
	}
	if s != nil && s.accessService != nil && s.accessService.IsInstanceAccessToken(candidate) {
		return true
	}
	return managedGatewayToken != "" && looksLikeManagedInstanceGatewayToken(candidate)
}

func looksLikeManagedInstanceGatewayToken(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "igt_")
}
func proxyBaseForRequestPath(requestPath string, instanceID int) string {
	prefix := fmt.Sprintf("/api/v1/instances/%d/proxy", instanceID)
	path := strings.TrimSpace(requestPath)
	if strings.HasPrefix(path, prefix) {
		path = strings.TrimPrefix(path, prefix)
	}
	if path == "" || path == "/" {
		return fmt.Sprintf("%s/", prefix)
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasSuffix(path, "/") {
		lastSlash := strings.LastIndex(path, "/")
		if lastSlash >= 0 {
			path = path[:lastSlash+1]
		} else {
			path = "/"
		}
	}
	return prefix + path
}

func websocketUpstreamOrigin(targetURL *url.URL) string {
	if targetURL == nil {
		return ""
	}
	scheme := targetURL.Scheme
	switch scheme {
	case "ws":
		scheme = "http"
	case "wss":
		scheme = "https"
	}
	if scheme == "" || targetURL.Host == "" {
		return ""
	}
	return scheme + "://" + targetURL.Host
}

func (s *InstanceProxyService) openClawWebSocketOrigin(targetURL *url.URL) string {
	if s != nil && s.openClawProxyOrigin != "" {
		return s.openClawProxyOrigin
	}
	return websocketUpstreamOrigin(targetURL)
}

func resolveOpenClawProxyOriginFromEnv() string {
	for _, key := range []string{
		"OPENCLAW_PROXY_ORIGIN",
		"CLAWMANAGER_TEAM_MANAGER_BASE_URL",
		"CLAWMANAGER_BACKEND_URL",
	} {
		if origin := originFromURLString(os.Getenv(key)); origin != "" {
			return origin
		}
	}
	return ""
}

func originFromURLString(rawURL string) string {
	value := strings.TrimSpace(rawURL)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
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
