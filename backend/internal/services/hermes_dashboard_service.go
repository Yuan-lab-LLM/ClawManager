package services

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

func HermesDashboardCookieName(id int) string {
	return "cm_hermes_dashboard_" + strings.TrimPrefix(HermesDesktopCookieName(id), "cm_hermes_desktop_")
}
func HermesDashboardBase(id int) string { return hermesProxyPrefix(id) }

// The classic dashboard is a separate surface, not a bypass for the optional
// Desktop renderer capability gate. Both share authorization and upstream auth.
func (s *HermesDesktopService) IsDashboardInstance(id int) bool {
	if s == nil || s.config.Instances == nil {
		return false
	}
	i, err := s.config.Instances.GetByID(id)
	return err == nil && i != nil && i.Type == "hermes" && i.InstanceMode == InstanceModeLite && i.RuntimeType == RuntimeBackendGateway
}

func (s *HermesDesktopService) ActivateDashboard(ctx context.Context, userID, instanceID int) (*HermesDesktopDescriptor, string, error) {
	t, reason, err := s.resolveRuntime(ctx, userID, instanceID, false)
	if err != nil {
		return nil, "", err
	}
	if reason != "" || !s.redisReady(ctx) {
		return nil, "", ErrHermesDesktopUnavailable
	}
	epoch, err := s.ensureSessionEpoch(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	if _, err = s.upstreamCookies(ctx, t); err != nil {
		return nil, "", err
	}
	id, err := hermesDesktopRandomID()
	if err != nil {
		return nil, "", ErrHermesDesktopUnavailable
	}
	c := HermesDesktopClaims{Surface: "dashboard", UserID: userID, InstanceID: instanceID, Generation: t.binding.Generation, PodID: t.pod.ID, Port: t.binding.GatewayPort, SessionID: id, Epoch: epoch}
	raw, err := s.sign(&c, "dashboard-session", HermesDesktopSessionTTL)
	if err != nil {
		return nil, "", ErrHermesDesktopUnavailable
	}
	// Recheck after login: logout, revocation and rebinding may race a slow login.
	if _, err = s.authorizeClaims(ctx, &c); err != nil {
		return nil, "", err
	}
	expires := c.ExpiresAt.Time
	return &HermesDesktopDescriptor{Available: true, InstanceID: instanceID, RendererURL: HermesDashboardBase(instanceID) + "/chat/", ExpiresAt: &expires, Capabilities: []string{"classic-dashboard"}}, raw, nil
}

func (s *HermesDesktopService) AuthenticateDashboard(ctx context.Context, raw string, id int) (*HermesDesktopClaims, error) {
	c, err := s.parse(raw, "dashboard-session")
	if err != nil {
		return nil, err
	}
	if c.Surface != "dashboard" || c.InstanceID != id {
		return nil, ErrHermesDesktopForbidden
	}
	if _, err = s.authorizeClaims(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *HermesDesktopService) ProxyDashboardWebSocket(ctx context.Context, c *HermesDesktopClaims, ticket, path string, query url.Values, w http.ResponseWriter, r *http.Request) error {
	if c.Surface != "dashboard" {
		return ErrHermesDesktopForbidden
	}
	q, err := hermesDashboardWebSocketPolicy(path, query)
	if err != nil {
		return err
	}
	return s.proxyHermesWebSocket(ctx, c, ticket, path, q, w, r)
}

func (s *HermesDesktopService) ProxyDashboardHTTP(ctx context.Context, c *HermesDesktopClaims, method, path string, query url.Values, w http.ResponseWriter) error {
	if c.Surface != "dashboard" {
		return ErrHermesDesktopForbidden
	}
	rule, err := hermesDashboardHTTPPolicy(method, path, query)
	if err != nil {
		return err
	}
	t, err := s.authorizeClaims(ctx, c)
	if err != nil {
		return err
	}
	if rule.Kind == "local_json" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if method != http.MethodHead {
			_, err = w.Write(rule.LocalJSON)
		}
		return err
	}
	if rule.Kind == "ws_ticket" {
		wsURL, expiry, err := s.MintTicket(c)
		if err != nil {
			return err
		}
		u, _ := url.Parse(wsURL)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		return json.NewEncoder(w).Encode(map[string]any{"ticket": u.Query().Get("ticket"), "ttl_seconds": max(0, int(time.Until(expiry).Seconds())), "expires_at": expiry})
	}
	if rule.Kind != "upstream" {
		return ErrHermesDesktopForbidden
	}
	return s.dashboardRead(ctx, t, method, rule.Path, rule.Query.Encode(), w, true)
}

// Only allowlisted GET/HEAD requests reach here. A consumed write or PTY frame
// is never replayed. An upstream redirect is an auth failure, never a login page.
func (s *HermesDesktopService) dashboardRead(ctx context.Context, t *hermesDesktopTarget, method, path, query string, w http.ResponseWriter, retry bool) error {
	if method != http.MethodGet && method != http.MethodHead {
		return ErrHermesDesktopForbidden
	}
	cookies, err := s.upstreamCookies(ctx, t)
	if err != nil {
		return err
	}
	u := *t.url
	u.Path, u.RawQuery = path, query
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return ErrHermesDesktopUpstream
	}
	s.gatewayAuth.SetHeaders(req)
	req.Header.Set("X-Forwarded-Prefix", HermesDashboardBase(t.instance.ID))
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return ErrHermesDesktopUpstream
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || (resp.StatusCode >= 300 && resp.StatusCode < 400) {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		s.gatewayAuth.Invalidate(t.authKey())
		if retry {
			return s.dashboardRead(ctx, t, method, path, query, w, false)
		}
		return ErrHermesDesktopUpstream
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrHermesDesktopUpstream
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err != nil || len(body) > 16<<20 {
		return ErrHermesDesktopUpstream
	}
	contentType := resp.Header.Get("Content-Type")
	if method != http.MethodHead && strings.HasPrefix(path, "/api/") {
		if !strings.Contains(contentType, "application/json") {
			return ErrHermesDesktopUpstream
		}
		if path == "/api/status" {
			body, err = hermesDashboardSafeStatus(body)
		}
		if err != nil {
			return err
		}
		body, err = hermesDesktopSanitize(body, *t.instance.AccessToken, cookies)
		if err != nil {
			return err
		}
	} else if strings.Contains(contentType, "text/html") {
		if path != "/chat" && path != "/chat/" {
			return ErrHermesDesktopUpstream
		}
		if !hermesDashboardAuthenticatedHTML(body, HermesDashboardBase(t.instance.ID)) {
			return ErrHermesDesktopUpstream
		}
		body = []byte(injectHermesAbsolutePathPatch(injectProxyBase(string(body), HermesDashboardBase(t.instance.ID)+"/"), HermesDashboardBase(t.instance.ID)))
		body = []byte(strings.Replace(string(body), "</head>", hermesDashboardManagedIdentityScript+"</head>", 1))
		body = []byte(rewriteHermesDashboardHTMLAssets(string(body), HermesDashboardBase(t.instance.ID)))
	} else if strings.HasSuffix(path, ".css") && strings.HasPrefix(strings.ToLower(contentType), "text/css") {
		body = []byte(rewriteHermesDashboardCSSAssets(string(body), HermesDashboardBase(t.instance.ID)))
	}
	// No Set-Cookie/Location/CORS/upstream security headers or browser auth
	// material is forwarded in either direction.
	for _, secret := range append([]string{*t.instance.AccessToken}, hermesCookieValues(cookies)...) {
		if secret != "" {
			body = bytes.ReplaceAll(body, []byte(secret), []byte("[redacted]"))
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	if method != http.MethodHead {
		_, err = w.Write(body)
	}
	return err
}

var hermesDashboardAuthFlag = regexp.MustCompile(`window\.__HERMES_AUTH_REQUIRED__\s*=\s*true\s*;`)
var hermesDashboardBaseFlag = regexp.MustCompile(`window\.__HERMES_BASE_PATH__\s*=\s*("[^"\r\n]*")\s*;`)

func hermesDashboardAuthenticatedHTML(body []byte, prefix string) bool {
	if bytes.Contains(body, []byte("__HERMES_SESSION_TOKEN__")) || !hermesDashboardAuthFlag.Match(body) {
		return false
	}
	match := hermesDashboardBaseFlag.FindSubmatch(body)
	var actual string
	return len(match) == 2 && json.Unmarshal(match[1], &actual) == nil && actual == prefix
}

// Authentication has already succeeded server-side. The Runtime identity
// widget is informational: its logout must not redirect an SSO user to a
// second login screen. The user signs out through ClawManager instead.
const hermesDashboardManagedIdentityScript = `<script>(function(){function managed(){document.querySelectorAll('[role="status"][aria-label^="Logged in as "] button[aria-label="Log out"]').forEach(function(b){b.disabled=true;b.title='Sign out through ClawManager / 请在 ClawManager 退出登录';b.setAttribute('aria-label',b.title);});}new MutationObserver(managed).observe(document.documentElement,{childList:true,subtree:true});managed();})();</script>`

func hermesDashboardSafeStatus(body []byte) ([]byte, error) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil {
		return nil, ErrHermesDesktopUpstream
	}
	safe := map[string]json.RawMessage{}
	for _, key := range []string{"version", "model", "provider", "active_sessions", "gateway_state", "gateway_running", "uptime", "agent_running"} {
		if value, ok := raw[key]; ok {
			safe[key] = value
		}
	}
	return json.Marshal(safe)
}

func hermesCookieValues(cookies []*http.Cookie) []string {
	values := make([]string, 0, len(cookies))
	for _, c := range cookies {
		values = append(values, c.Value)
	}
	return values
}

// PTY is a byte stream, unlike the Desktop JSON-RPC channel. Hold only a suffix
// that might be the start of a known secret, so split-frame credentials cannot
// escape while ordinary terminal output remains immediate. Pending prefixes are
// discarded on close, not flushed to the browser.
type hermesStreamRedactor struct {
	secrets [][]byte
	pending []byte
}

func newHermesStreamRedactor(password string, cookies []*http.Cookie, ticket string) *hermesStreamRedactor {
	r := &hermesStreamRedactor{}
	for _, secret := range append([]string{password, ticket}, hermesCookieValues(cookies)...) {
		if secret != "" {
			r.secrets = append(r.secrets, []byte(secret))
		}
	}
	return r
}
func (r *hermesStreamRedactor) Filter(frame []byte) []byte {
	body := append(r.pending, frame...)
	for _, secret := range r.secrets {
		body = bytes.ReplaceAll(body, secret, []byte("[redacted]"))
	}
	hold := 0
	for _, secret := range r.secrets {
		for n := min(len(secret)-1, len(body)); n > hold; n-- {
			if bytes.Equal(body[len(body)-n:], secret[:n]) {
				hold = n
				break
			}
		}
	}
	end := len(body) - hold
	r.pending = bytes.Clone(body[end:])
	return body[:end]
}
