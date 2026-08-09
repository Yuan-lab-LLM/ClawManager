package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/egresspolicy"
	"clawreef/internal/models"
	"clawreef/internal/services"
	"clawreef/internal/services/k8s"

	"github.com/gin-gonic/gin"
)

const (
	teamPreviewHost            = "clawmanager-team-preview.invalid"
	teamPreviewPathPrefix      = "/v1/"
	teamPreviewV2Prefix        = "/v2/"
	teamPreviewInteractiveMode = "interactive"
	teamPreviewMaxSize         = 64 << 20
	teamTokenSecretKey         = "CLAWMANAGER_TEAM_TOKEN"
)

type teamPreviewRepository interface {
	GetTeamByID(id int) (*models.Team, error)
}

type teamPreviewSecretReader interface {
	GetSecretValue(ctx context.Context, namespace, name, key string) (string, error)
}

type egressPrivateExceptionSource interface {
	SnapshotRules() []egresspolicy.PrivateExceptionRule
}

type egressInstanceResolver interface {
	GetByID(id int) (*models.Instance, error)
	FindByPodIP(podIP string) (*models.Instance, error)
}

type egressIdentityContextKey struct{}

type egressRequestIdentity struct {
	InstanceID *int
	UserID     *int
}

// EgressProxyHandler provides a minimal forward proxy for ordinary HTTP/HTTPS traffic.
type EgressProxyHandler struct {
	transport          *http.Transport
	dialContext        func(context.Context, string, string) (net.Conn, error)
	baseDialContext    func(context.Context, string, string) (net.Conn, error)
	lookupNetIP        egresspolicy.LookupNetIPFunc
	privateExceptions  egressPrivateExceptionSource
	instances          egressInstanceResolver
	policy             egresspolicy.Policy
	audit              services.AuditEventService
	previewRepo        teamPreviewRepository
	previewSecrets     teamPreviewSecretReader
	workspaceRoot      string
	namespaceForUser   func(int) string
	previewHosts       map[string]struct{}
}

// NewEgressProxyHandler creates a new egress proxy handler.
func NewEgressProxyHandler(audit services.AuditEventService, options ...EgressProxyOption) *EgressProxyHandler {
	baseDialer := net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	handler := &EgressProxyHandler{
		baseDialContext: baseDialer.DialContext,
		policy:          egresspolicy.LoadFromEnv(),
		audit:           audit,
		previewHosts:    map[string]struct{}{teamPreviewHost: {}},
	}
	handler.transport = &http.Transport{
		Proxy:                 nil,
		DialContext:           handler.proxyDialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	handler.dialContext = handler.proxyDialContext
	for _, option := range options {
		if option != nil {
			option(handler)
		}
	}
	return handler
}

// WithTeamArtifactPreviewOrigin registers the exact managed service host that
// may serve signed Team previews. The legacy .invalid host remains accepted so
// previously issued links remain compatible during rolling upgrades.
func WithTeamArtifactPreviewOrigin(originValue string) EgressProxyOption {
	return func(handler *EgressProxyHandler) {
		origin, err := url.Parse(strings.TrimSpace(originValue))
		if err != nil || !strings.EqualFold(origin.Scheme, "http") || strings.TrimSpace(origin.Hostname()) == "" {
			return
		}
		if handler.previewHosts == nil {
			handler.previewHosts = map[string]struct{}{}
		}
		handler.previewHosts[strings.ToLower(origin.Hostname())] = struct{}{}
	}
}

type EgressProxyOption func(*EgressProxyHandler)

func WithTeamArtifactPreview(
	repo teamPreviewRepository,
	secrets teamPreviewSecretReader,
	workspaceRoot string,
	namespaceForUser func(int) string,
) EgressProxyOption {
	return func(handler *EgressProxyHandler) {
		handler.previewRepo = repo
		handler.previewSecrets = secrets
		handler.workspaceRoot = strings.TrimSpace(workspaceRoot)
		handler.namespaceForUser = namespaceForUser
	}
}

// WithEgressPrivateExceptions wires private CIDR exception matching and instance identity lookup.
func WithEgressPrivateExceptions(source egressPrivateExceptionSource, instances egressInstanceResolver) EgressProxyOption {
	return func(handler *EgressProxyHandler) {
		handler.privateExceptions = source
		handler.instances = instances
	}
}

// Handle proxies ordinary HTTP or HTTPS CONNECT traffic.
func (h *EgressProxyHandler) Handle(c *gin.Context) {
	if strings.EqualFold(c.Request.Method, http.MethodConnect) {
		h.handleConnect(c)
		return
	}

	if h.isTeamPreviewRequest(c.Request) {
		h.handleTeamPreview(c)
		return
	}

	if c.Request.URL == nil || c.Request.URL.Scheme == "" || c.Request.URL.Host == "" {
		c.Status(http.StatusNotFound)
		return
	}

	if allowed, reason := h.policy.AllowHost(c.Request.URL.Host); !allowed {
		h.recordBlockedEgress(c, c.Request.URL.Host, reason)
		c.String(http.StatusForbidden, "egress blocked: %s (%s)", c.Request.URL.Host, reason)
		return
	}

	identity := h.resolveEgressIdentity(c)
	reqCtx := context.WithValue(c.Request.Context(), egressIdentityContextKey{}, identity)
	outReq := c.Request.Clone(reqCtx)
	outReq.RequestURI = ""
	removeHopHeaders(outReq.Header)

	transport := h.transport
	if transport == nil {
		transport = &http.Transport{
			Proxy:       nil,
			DialContext: h.proxyDialContext,
		}
	}
	resp, err := transport.RoundTrip(outReq)
	if err != nil {
		if errors.Is(err, egresspolicy.ErrUnsafeTarget) {
			h.recordBlockedEgress(c, c.Request.URL.Host, err.Error())
			c.String(http.StatusForbidden, "egress blocked: %s (%s)", c.Request.URL.Host, err)
			return
		}
		c.String(http.StatusBadGateway, "proxy upstream error: %v", err)
		return
	}
	defer resp.Body.Close()

	removeHopHeaders(resp.Header)
	copyHeaders(c.Writer.Header(), resp.Header)
	c.Status(resp.StatusCode)
	_, _ = io.Copy(c.Writer, resp.Body)
}

func (h *EgressProxyHandler) handleConnect(c *gin.Context) {
	target := strings.TrimSpace(c.Request.Host)
	if target == "" {
		c.String(http.StatusBadRequest, "missing CONNECT target")
		return
	}

	if allowed, reason := h.policy.AllowHost(target); !allowed {
		h.recordBlockedEgress(c, target, reason)
		c.String(http.StatusForbidden, "egress blocked: %s (%s)", target, reason)
		return
	}

	identity := h.resolveEgressIdentity(c)
	reqCtx := context.WithValue(c.Request.Context(), egressIdentityContextKey{}, identity)
	upstreamConn, err := h.proxyDialContext(reqCtx, "tcp", target)
	if err != nil {
		if errors.Is(err, egresspolicy.ErrUnsafeTarget) {
			h.recordBlockedEgress(c, target, err.Error())
			c.String(http.StatusForbidden, "egress blocked: %s (%s)", target, err)
			return
		}
		c.String(http.StatusBadGateway, "proxy connect error: %v", err)
		return
	}

	hijacker, ok := c.Writer.(http.Hijacker)
	if !ok {
		_ = upstreamConn.Close()
		c.String(http.StatusInternalServerError, "proxy hijacking not supported")
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		_ = upstreamConn.Close()
		return
	}

	_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	go tunnelConns(upstreamConn, clientConn)
	go tunnelConns(clientConn, upstreamConn)
}

func (h *EgressProxyHandler) isTeamPreviewRequest(request *http.Request) bool {
	if request == nil {
		return false
	}
	host := request.Host
	if request.URL != nil && strings.TrimSpace(request.URL.Host) != "" {
		host = request.URL.Host
	}
	normalized, _, err := net.SplitHostPort(host)
	if err == nil {
		host = normalized
	}
	host = strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
	if strings.HasSuffix(host, "."+teamPreviewHost) {
		return true
	}
	_, ok := h.previewHosts[host]
	return ok
}

func (h *EgressProxyHandler) handleTeamPreview(c *gin.Context) {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		c.Header("Allow", "GET, HEAD")
		c.Status(http.StatusMethodNotAllowed)
		return
	}
	if h.previewRepo == nil || h.previewSecrets == nil || h.namespaceForUser == nil {
		c.String(http.StatusServiceUnavailable, "Team artifact preview is unavailable")
		return
	}

	teamID, signedPrefix, signature, requestedPath, mode, err := parseTeamPreviewPath(c.Request.URL.Path)
	if err != nil {
		c.String(http.StatusBadRequest, "invalid Team artifact preview link")
		return
	}
	team, err := h.previewRepo.GetTeamByID(teamID)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to resolve Team artifact preview")
		return
	}
	if team == nil || team.TeamTokenSecretName == nil || strings.TrimSpace(*team.TeamTokenSecretName) == "" {
		c.String(http.StatusNotFound, "Team artifact preview not found")
		return
	}
	namespace := strings.TrimSpace(h.namespaceForUser(team.UserID))
	token, err := h.previewSecrets.GetSecretValue(
		c.Request.Context(),
		namespace,
		strings.TrimSpace(*team.TeamTokenSecretName),
		teamTokenSecretKey,
	)
	if err != nil || strings.TrimSpace(token) == "" {
		c.String(http.StatusForbidden, "Team artifact preview authorization failed")
		return
	}
	if !verifyTeamPreviewSignatureForMode(token, teamID, signedPrefix, mode, signature) {
		c.String(http.StatusForbidden, "Team artifact preview authorization failed")
		return
	}
	if mode == teamPreviewInteractiveMode {
		if h.isManagedInteractivePreviewBootstrapHost(c.Request) {
			c.Header("Cache-Control", "private, no-store, max-age=0")
			c.Header("Referrer-Policy", "no-referrer")
			c.Redirect(
				http.StatusTemporaryRedirect,
				"http://"+isolatedInteractivePreviewHost(signature)+c.Request.URL.EscapedPath(),
			)
			return
		}
		if !isIsolatedInteractivePreviewHost(c.Request, signature) {
			c.String(http.StatusForbidden, "Team artifact preview authorization failed")
			return
		}
	}

	relativePath, err := cleanTeamPreviewRelativePath(joinTeamPreviewPath(signedPrefix, requestedPath))
	if err != nil || relativePath == "" {
		c.String(http.StatusBadRequest, "invalid Team artifact preview path")
		return
	}
	rootPath := k8s.TeamSharedWorkspacePath(h.workspaceRoot, team.UserID, team.ID)
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.String(http.StatusNotFound, "Team artifact preview not found")
			return
		}
		c.String(http.StatusInternalServerError, "failed to open Team artifact preview")
		return
	}
	defer root.Close()
	file, err := root.Open(filepath.FromSlash(relativePath))
	if err != nil {
		if os.IsNotExist(err) {
			c.String(http.StatusNotFound, "Team artifact preview not found")
			return
		}
		c.String(http.StatusForbidden, "Team artifact preview path is not accessible")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		c.String(http.StatusNotFound, "Team artifact preview not found")
		return
	}
	if info.Size() > teamPreviewMaxSize {
		c.String(http.StatusRequestEntityTooLarge, "Team artifact is too large to preview")
		return
	}

	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(info.Name())))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	setTeamPreviewHeaders(c.Writer.Header(), contentType, mode)
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), file)
}

func parseTeamPreviewPath(rawPath string) (int, string, string, string, string, error) {
	mode := ""
	pathPrefix := teamPreviewPathPrefix
	if strings.HasPrefix(rawPath, teamPreviewV2Prefix) {
		pathPrefix = teamPreviewV2Prefix
	} else if !strings.HasPrefix(rawPath, teamPreviewPathPrefix) {
		return 0, "", "", "", "", fmt.Errorf("unexpected preview path")
	}
	parts := strings.Split(strings.TrimPrefix(rawPath, pathPrefix), "/")
	if pathPrefix == teamPreviewV2Prefix {
		if len(parts) < 5 || parts[0] != teamPreviewInteractiveMode {
			return 0, "", "", "", "", fmt.Errorf("invalid preview mode")
		}
		mode = parts[0]
		parts = parts[1:]
	}
	if len(parts) < 4 {
		return 0, "", "", "", "", fmt.Errorf("incomplete preview path")
	}
	teamID, err := strconv.Atoi(parts[0])
	if err != nil || teamID <= 0 {
		return 0, "", "", "", "", fmt.Errorf("invalid team id")
	}
	prefix, err := decodeTeamPreviewPrefix(parts[1])
	if err != nil {
		return 0, "", "", "", "", err
	}
	signature := strings.TrimSpace(parts[2])
	if signature == "" {
		return 0, "", "", "", "", fmt.Errorf("missing signature")
	}
	requestedPath, err := cleanTeamPreviewRelativePath(strings.Join(parts[3:], "/"))
	if err != nil {
		return 0, "", "", "", "", err
	}
	return teamID, prefix, signature, requestedPath, mode, nil
}

func decodeTeamPreviewPrefix(encoded string) (string, error) {
	if encoded == "_" {
		return "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("invalid signed prefix")
	}
	return cleanTeamPreviewRelativePath(string(decoded))
}

func cleanTeamPreviewRelativePath(raw string) (string, error) {
	value := filepath.ToSlash(filepath.Clean(strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))))
	value = strings.TrimPrefix(value, "/")
	if value == "" || value == "." {
		return "", nil
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.ContainsRune(segment, '\x00') {
			return "", fmt.Errorf("invalid preview path")
		}
	}
	return value, nil
}

func joinTeamPreviewPath(prefix, requestedPath string) string {
	if prefix == "" {
		return requestedPath
	}
	if requestedPath == "" {
		return prefix
	}
	return prefix + "/" + requestedPath
}

func teamPreviewSignaturePayload(teamID int, prefix string) string {
	return fmt.Sprintf("team-preview-v1\n%d\n%s", teamID, prefix)
}

func verifyTeamPreviewSignature(token string, teamID int, prefix, signature string) bool {
	return verifyTeamPreviewSignatureForMode(token, teamID, prefix, "", signature)
}

func teamPreviewSignaturePayloadForMode(teamID int, prefix, mode string) string {
	if mode == "" {
		return teamPreviewSignaturePayload(teamID, prefix)
	}
	return fmt.Sprintf("team-preview-v2\n%s\n%d\n%s", mode, teamID, prefix)
}

func verifyTeamPreviewSignatureForMode(token string, teamID int, prefix, mode, signature string) bool {
	provided, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte(teamPreviewSignaturePayloadForMode(teamID, prefix, mode)))
	return hmac.Equal(provided, mac.Sum(nil))
}

func isolatedInteractivePreviewHost(signature string) string {
	value := strings.ToLower(strings.TrimSpace(signature))
	if len(value) > 16 {
		value = value[:16]
	}
	return "p-" + value + "." + teamPreviewHost
}

func (h *EgressProxyHandler) isManagedInteractivePreviewBootstrapHost(request *http.Request) bool {
	host := normalizedTeamPreviewRequestHost(request)
	if host == "" || host == teamPreviewHost || strings.HasSuffix(host, "."+teamPreviewHost) {
		return false
	}
	_, ok := h.previewHosts[host]
	return ok
}

func normalizedTeamPreviewRequestHost(request *http.Request) string {
	if request == nil {
		return ""
	}
	host := request.Host
	if request.URL != nil && strings.TrimSpace(request.URL.Host) != "" {
		host = request.URL.Host
	}
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	return strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
}

func isIsolatedInteractivePreviewHost(request *http.Request, signature string) bool {
	return normalizedTeamPreviewRequestHost(request) == isolatedInteractivePreviewHost(signature)
}

func setTeamPreviewHeaders(headers http.Header, contentType, mode string) {
	headers.Set("Content-Type", contentType)
	headers.Set("Cache-Control", "private, no-store, max-age=0")
	headers.Set("Referrer-Policy", "no-referrer")
	headers.Set("X-Content-Type-Options", "nosniff")
	headers.Set("X-Frame-Options", "DENY")
	headers.Set("Cross-Origin-Opener-Policy", "same-origin")
	if mode == teamPreviewInteractiveMode {
		headers.Set("Origin-Agent-Cluster", "?1")
		headers.Set(
			"Content-Security-Policy",
			"sandbox allow-scripts allow-same-origin allow-modals; "+
				"default-src 'none'; script-src 'self' 'unsafe-inline' blob:; "+
				"style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; "+
				"font-src 'self' data:; media-src 'self' data: blob:; worker-src blob:; "+
				"connect-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
		)
		return
	}
	headers.Set(
		"Content-Security-Policy",
		"sandbox allow-scripts allow-same-origin allow-forms allow-modals allow-popups; "+
			"default-src 'self' https: data: blob:; connect-src 'self' https: wss:; "+
			"object-src 'none'; base-uri 'none'; frame-ancestors 'none'",
	)
}

func (h *EgressProxyHandler) proxyDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	identity, _ := ctx.Value(egressIdentityContextKey{}).(egressRequestIdentity)
	var rules []egresspolicy.PrivateExceptionRule
	if h.privateExceptions != nil {
		rules = h.privateExceptions.SnapshotRules()
	}
	dial := h.baseDialContext
	if dial == nil {
		base := net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		dial = base.DialContext
	}
	return egresspolicy.DialContextAllowingPrivateExceptions(
		ctx,
		network,
		address,
		rules,
		identity.InstanceID,
		identity.UserID,
		h.lookupNetIP,
		dial,
	)
}

func (h *EgressProxyHandler) resolveEgressIdentity(c *gin.Context) egressRequestIdentity {
	if instanceID := resolveEgressInstanceID(c); instanceID != nil {
		identity := egressRequestIdentity{InstanceID: instanceID}
		if h.instances != nil {
			if instance, err := h.instances.GetByID(*instanceID); err == nil && instance != nil {
				userID := instance.UserID
				identity.UserID = &userID
			}
		}
		return identity
	}
	if h.instances == nil || c == nil || c.Request == nil {
		return egressRequestIdentity{}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(c.Request.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(c.Request.RemoteAddr)
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return egressRequestIdentity{}
	}
	instance, err := h.instances.FindByPodIP(host)
	if err != nil || instance == nil {
		return egressRequestIdentity{}
	}
	instanceID := instance.ID
	userID := instance.UserID
	return egressRequestIdentity{InstanceID: &instanceID, UserID: &userID}
}

func (h *EgressProxyHandler) recordBlockedEgress(c *gin.Context, host, reason string) {
	if h.audit == nil {
		return
	}
	identity := h.resolveEgressIdentity(c)
	remoteAddr := strings.TrimSpace(c.Request.RemoteAddr)
	message := fmt.Sprintf("Blocked egress to %s (%s) from %s", host, reason, remoteAddr)
	if err := h.audit.RecordEvent(&models.AuditEvent{
		TraceID:      fmt.Sprintf("egress_%d", time.Now().UnixNano()),
		InstanceID:   identity.InstanceID,
		EventType:    "egress.llm.blocked",
		TrafficClass: models.TrafficClassGenericEgress,
		Severity:     models.AuditSeverityWarn,
		Message:      message,
	}); err != nil {
		log.Printf("failed to record egress block audit event: %v", err)
	}
}

func resolveEgressInstanceID(c *gin.Context) *int {
	for _, headerName := range []string{
		"X-ClawManager-Instance-Id",
		"X-ClawManager-Egress-Instance-Id",
	} {
		raw := strings.TrimSpace(c.GetHeader(headerName))
		if raw == "" {
			continue
		}
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			return &parsed
		}
	}
	return nil
}

func tunnelConns(dst net.Conn, src net.Conn) {
	defer dst.Close()
	defer src.Close()
	_, _ = io.Copy(dst, src)
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func removeHopHeaders(headers http.Header) {
	for _, key := range []string{
		"Connection",
		"Proxy-Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	} {
		headers.Del(key)
	}
}
