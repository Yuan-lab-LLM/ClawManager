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
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/egresspolicy"
	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
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
	SnapshotRulesContext(context.Context) []egresspolicy.PrivateExceptionRule
}

type egressInstanceResolver interface {
	GetByIDContext(context.Context, int) (*models.Instance, error)
}

// RuntimeCapabilityVerifier verifies a capability against the current assignment.
// Implementations must honor context cancellation and deadlines.
type RuntimeCapabilityVerifier interface {
	Verify(context.Context, string, string) (runtimeauth.Identity, error)
}

type egressIdentityContextKey struct{}

// Only trusted request setup can attach this non-joining cancellation hook.
// Transport dial contexts retain values even when their cancellation is detached.
type egressRevokeContextKey struct{}

type egressRequestIdentity struct {
	InstanceID *int
	UserID     *int
}

// EgressProxyHandler provides a minimal forward proxy for ordinary HTTP/HTTPS traffic.
type EgressProxyHandler struct {
	capabilities      RuntimeCapabilityVerifier
	hardDeny          func(context.Context, netip.Addr) (bool, error)
	transport         *http.Transport
	dialContext       func(context.Context, string, string) (net.Conn, error)
	baseDialContext   func(context.Context, string, string) (net.Conn, error)
	lookupNetIP       egresspolicy.LookupNetIPFunc
	privateExceptions egressPrivateExceptionSource
	instances         egressInstanceResolver
	policy            egresspolicy.Policy
	audit             services.AuditEventService
	managedPreview    *services.ManagedTeamPreview
	previewRepo       teamPreviewRepository
	previewSecrets    teamPreviewSecretReader
	workspaceRoot     string
	namespaceForUser  func(int) string
	previewHosts      map[string]struct{}
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
		Proxy:             nil,
		DialContext:       handler.proxyDialContext,
		ForceAttemptHTTP2: false,
		// Private-target authorization is per request; do not reuse another identity's connection.
		DisableKeepAlives:     true,
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

// WithEgressHardDeny protects cluster/control addresses even when private
// exceptions otherwise authorize the destination. It receives resolved IPs.
func WithEgressHardDeny(deny func(context.Context, netip.Addr) (bool, error)) EgressProxyOption {
	return func(handler *EgressProxyHandler) { handler.hardDeny = deny }
}

// WithEgressRuntimeCapabilities requires egress-purpose runtime capabilities.
func WithEgressRuntimeCapabilities(verifier RuntimeCapabilityVerifier) EgressProxyOption {
	return func(handler *EgressProxyHandler) { handler.capabilities = verifier }
}

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

// WithEgressPrivateExceptions wires private CIDR exception matching.
// Legacy identity requires derived proxy credentials; source IPs and ID headers never grant it.
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
		token, verified, ok := h.authenticateEgress(c)
		if !ok {
			return
		}
		ctx, finish := h.authorizedRequest(c, token, verified)
		defer finish()
		c.Request = c.Request.WithContext(ctx)
		h.handleTeamPreview(c)
		return
	}

	if c.Request.URL == nil || c.Request.URL.Scheme == "" || c.Request.URL.Host == "" {
		c.Status(http.StatusNotFound)
		return
	}

	token, verified, ok := h.authenticateEgress(c)
	if !ok {
		return
	}

	identity := h.resolveEgressIdentity(c)
	reqCtx, finish := h.authorizedRequest(c, token, verified)
	defer finish()
	reqCtx = context.WithValue(reqCtx, egressIdentityContextKey{}, identity)
	outReq := c.Request.Clone(reqCtx)
	outReq.RequestURI = ""
	removeHopHeaders(outReq.Header)
	outReq.Header.Del("X-ClawManager-Instance-Id")
	outReq.Header.Del("X-ClawManager-Egress-Instance-Id")

	transport := h.transport
	if transport == nil {
		transport = &http.Transport{
			Proxy:             nil,
			DisableKeepAlives: true,
			DialContext:       h.proxyDialContext,
		}
	}
	resp, err := transport.RoundTrip(outReq)
	if err != nil {
		if errors.Is(err, egresspolicy.ErrUnsafeTarget) || errors.Is(err, egresspolicy.ErrHostBlocked) {
			h.recordBlockedEgress(c, c.Request.URL.Host, err.Error(), identity)
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
	token, verified, ok := h.authenticateEgress(c)
	if !ok {
		return
	}
	target := strings.TrimSpace(c.Request.Host)
	if target == "" {
		c.String(http.StatusBadRequest, "missing CONNECT target")
		return
	}

	identity := h.resolveEgressIdentity(c)
	// Hijacking ends the HTTP request context. The tunnel owns its detached lifetime.
	reqCtx, cancel := h.authorizedContext(context.WithValue(context.WithoutCancel(c.Request.Context()), egressIdentityContextKey{}, identity), token, verified)
	handedOff := false
	defer func() {
		if !handedOff {
			cancel()
		}
	}()
	upstreamConn, err := h.proxyDialContext(reqCtx, "tcp", target)
	if err != nil {
		if errors.Is(err, egresspolicy.ErrUnsafeTarget) || errors.Is(err, egresspolicy.ErrHostBlocked) {
			h.recordBlockedEgress(c, target, err.Error(), identity)
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

	clientConn, buffered, err := hijacker.Hijack()
	if err != nil {
		_ = upstreamConn.Close()
		return
	}

	// Revocation also interrupts a blocked handshake write.
	stop := context.AfterFunc(reqCtx, func() { _ = clientConn.Close(); _ = upstreamConn.Close() })
	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		stop()
		_ = clientConn.Close()
		_ = upstreamConn.Close()
		return
	}
	// Preserve bytes already read during the CONNECT handshake.
	var clientReader io.Reader = clientConn
	if buffered != nil {
		clientReader = buffered.Reader
	}
	handedOff = true
	go func() {
		defer cancel()
		defer stop()
		defer clientConn.Close()
		defer upstreamConn.Close()
		done := make(chan struct{}, 2)
		go func() { _, _ = io.Copy(upstreamConn, clientReader); done <- struct{}{} }()
		go func() { _, _ = io.Copy(clientConn, upstreamConn); done <- struct{}{} }()
		<-done
		_ = clientConn.Close()
		_ = upstreamConn.Close()
		<-done
	}()
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
	if strings.HasPrefix(c.Request.URL.Path, "/v3/") {
		h.handleManagedTeamPreview(c)
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
	// Any nonempty sticky marker, including a future unknown value, permanently
	// retires no-expiry legacy grants even after issuers leave or services restart.
	if team != nil && team.PreviewSecurityProfile != "" {
		c.String(http.StatusForbidden, "Team artifact preview authorization failed")
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
	if h.managedPreview != nil {
		binding, err := h.managedPreview.OpenLegacy(c.Request.Context(), team.ID, team.UserID, signedPrefix, requestedPath)
		if err != nil {
			c.String(http.StatusForbidden, "Team artifact preview authorization failed")
			return
		}
		defer binding.Close()
		info, err := binding.File.Stat()
		if err != nil {
			c.Status(http.StatusForbidden)
			return
		}
		serveTeamPreviewContent(c, binding.File, info, mode, time.Time{}, binding.Check, binding.Interrupt)
		return
	}
	rootPath := k8s.TeamSharedWorkspacePath(h.workspaceRoot, team.UserID, team.ID)
	if c.Request.Context().Err() != nil {
		c.String(http.StatusForbidden, "Team artifact preview authorization failed")
		return
	}
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
	if c.Request.Context().Err() != nil {
		return
	}
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
	// A connection retains verified values, never mutable request-owned pointers.
	if identity.InstanceID != nil {
		id := *identity.InstanceID
		identity.InstanceID = &id
	}
	if identity.UserID != nil {
		id := *identity.UserID
		identity.UserID = &id
	}
	loadRules := func(ruleCtx context.Context) []egresspolicy.PrivateExceptionRule {
		if h.privateExceptions == nil || (identity.InstanceID == nil && identity.UserID == nil) {
			return nil
		}
		return h.privateExceptions.SnapshotRulesContext(ruleCtx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dial := h.baseDialContext
	if dial == nil {
		base := net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		dial = base.DialContext
	}
	baseDial := dial
	dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		// egresspolicy has resolved and selected this literal after scoped exceptions.
		destination, err := netip.ParseAddrPort(address)
		if err != nil {
			return nil, errors.New("egress destination must be resolved")
		}
		ip, port := destination.Addr().Unmap(), int(destination.Port())
		check := func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, egressVerifyTimeout)
			defer cancel()
			if h.hardDeny != nil {
				denied, err := h.hardDeny(ctx, ip)
				if err != nil || denied || ctx.Err() != nil {
					return errors.New("egress destination is unavailable")
				}
			}
			if !egresspolicy.IsPublicAddress(ip) {
				if h.privateExceptions == nil || !egresspolicy.MatchPrivateException(
					loadRules(ctx), identity.InstanceID, identity.UserID, ip, port,
				) {
					return errors.New("egress destination is unavailable")
				}
			}
			return nil
		}
		if err := check(ctx); err != nil {
			return nil, err
		}
		conn, err := baseDial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		if h.hardDeny == nil && egresspolicy.IsPublicAddress(ip) {
			return conn, nil
		}
		revoke, _ := ctx.Value(egressRevokeContextKey{}).(context.CancelFunc)
		return newEgressGuardedConn(conn, check, revoke), nil
	}
	return egresspolicy.DialContextWithPolicyAndPrivateRuleSource(
		ctx,
		network,
		address,
		h.policy,
		loadRules,
		identity.InstanceID,
		identity.UserID,
		h.lookupNetIP,
		dial,
	)
}

// Capabilities provide protected identity; legacy listeners also verify proxy credentials.
// Caller ID headers and source IPs never prove ownership.
func (h *EgressProxyHandler) resolveEgressIdentity(c *gin.Context) egressRequestIdentity {
	if c == nil || c.Request == nil {
		return egressRequestIdentity{}
	}
	identity, _ := c.Request.Context().Value(egressIdentityContextKey{}).(egressRequestIdentity)
	if identity.InstanceID != nil || identity.UserID != nil || h.capabilities != nil || h.instances == nil {
		return identity
	}
	if auth := c.GetHeader("Proxy-Authorization"); auth != "" {
		// Reuse net/http's Basic parser without touching origin Authorization.
		request := &http.Request{Header: http.Header{"Authorization": []string{auth}}}
		username, password, ok := request.BasicAuth()
		instanceID, err := strconv.Atoi(username)
		if !ok || err != nil || instanceID <= 0 {
			return egressRequestIdentity{}
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		instance, err := h.instances.GetByIDContext(ctx, instanceID)
		if err != nil || instance == nil || instance.ID != instanceID || instance.UserID <= 0 || instance.RuntimeSecurityProfile != "" || instance.AccessToken == nil || strings.TrimSpace(*instance.AccessToken) == "" {
			return egressRequestIdentity{}
		}
		expected := egresspolicy.InstanceProxyPassword(instanceID, strings.TrimSpace(*instance.AccessToken))
		if !hmac.Equal([]byte(password), []byte(expected)) {
			return egressRequestIdentity{}
		}
		userID := instance.UserID
		return egressRequestIdentity{InstanceID: &instanceID, UserID: &userID}
	}
	// Neither a recycled Pod IP nor a caller-supplied ID proves ownership.
	return egressRequestIdentity{}
}

const egressRecheckInterval = 5 * time.Second
const egressVerifyTimeout = 2 * time.Second

func (h *EgressProxyHandler) verifyEgress(ctx context.Context, token string) (runtimeauth.Identity, error) {
	ctx, cancel := context.WithTimeout(ctx, egressVerifyTimeout)
	defer cancel()
	return h.capabilities.Verify(ctx, token, runtimeauth.AudienceEgress)
}

func (h *EgressProxyHandler) authenticateEgress(c *gin.Context) (string, runtimeauth.Identity, bool) {
	if h.capabilities == nil {
		return "", runtimeauth.Identity{}, true
	}
	parts := strings.Fields(c.GetHeader("Proxy-Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		c.Header("Proxy-Authenticate", `Bearer realm="egress"`)
		c.String(http.StatusProxyAuthRequired, "egress capability required")
		return "", runtimeauth.Identity{}, false
	}
	identity, err := h.verifyEgress(c.Request.Context(), parts[1])
	if err != nil || identity.InstanceID <= 0 || identity.UserID <= 0 || identity.Generation <= 0 || identity.PodUID == "" {
		c.String(http.StatusForbidden, "invalid egress capability")
		return "", runtimeauth.Identity{}, false
	}
	trusted := egressRequestIdentity{InstanceID: &identity.InstanceID, UserID: &identity.UserID}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), egressIdentityContextKey{}, trusted))
	return parts[1], identity, true
}

// Every lookup is bounded: a rejected or unavailable verifier ends the lifetime
// within one five-second interval plus the two-second lookup deadline.
func (h *EgressProxyHandler) authorizedContext(parent context.Context, token string, identity runtimeauth.Identity) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	ctx = context.WithValue(ctx, egressRevokeContextKey{}, cancel)
	if h.capabilities == nil {
		return ctx, cancel
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(egressRecheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current, err := h.verifyEgress(ctx, token)
				if err != nil || current != identity {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, func() { cancel(); <-done }
}

// Both upstream responses and local previews must interrupt blocked client I/O
// on revocation, and join callbacks before net/http reuses their writer.
func (h *EgressProxyHandler) authorizedRequest(c *gin.Context, token string, identity runtimeauth.Identity) (context.Context, func()) {
	ctx, cancel := h.authorizedContext(c.Request.Context(), token, identity)
	controller := http.NewResponseController(c.Writer)
	finished := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(finished)
		_ = controller.SetWriteDeadline(time.Now())
		_ = controller.SetReadDeadline(time.Now())
	})
	return ctx, func() {
		if !stop() {
			<-finished
		}
		cancel()
	}
}

func (h *EgressProxyHandler) recordBlockedEgress(c *gin.Context, host, reason string, identity egressRequestIdentity) {
	if h.audit == nil {
		return
	}
	remoteAddr := strings.TrimSpace(c.Request.RemoteAddr)
	message := fmt.Sprintf("Blocked egress to %s (%s) from %s", host, reason, remoteAddr)
	writer, ok := h.audit.(interface {
		RecordEventContext(context.Context, *models.AuditEvent) error
	})
	if !ok {
		log.Printf("egress audit writer lacks context support: %s", message)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := writer.RecordEventContext(ctx, &models.AuditEvent{
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

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func removeHopHeaders(headers http.Header) {
	// RFC 9110: Connection may nominate additional hop-by-hop headers.
	for _, value := range headers.Values("Connection") {
		for _, key := range strings.Split(value, ",") {
			headers.Del(strings.TrimSpace(key))
		}
	}
	for key := range headers {
		if strings.HasPrefix(strings.ToLower(key), "x-clawmanager-") {
			headers.Del(key)
		}
	}
	for _, key := range []string{
		"Connection",
		"Proxy-Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Forwarded",
		"X-Forwarded-For",
		"X-Real-IP",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	} {
		headers.Del(key)
	}
}
