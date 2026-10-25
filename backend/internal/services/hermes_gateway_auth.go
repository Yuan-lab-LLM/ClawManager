package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	ErrHermesGatewayOrigin = errors.New("Hermes control UI origin is invalid or missing")
	ErrHermesGatewayAuth   = errors.New("Hermes upstream authentication unavailable")
)

const (
	hermesGatewayCookieTTL = 5 * time.Minute
	hermesGatewayCacheMax  = 512
	hermesGatewayBodyMax   = 64 << 10
	hermesGatewayProtocol  = "hermes-gateway-v1"
)

// HermesGatewayAuthKey binds server-only credentials to a runtime allocation.
// The actual target origin and password digest are also part of the cache key.
type HermesGatewayAuthKey struct {
	InstanceID   int
	Generation   int
	RuntimePodID int64
	GatewayPort  int
}

type hermesGatewayCacheKey struct {
	allocation HermesGatewayAuthKey
	target     string
	credential [sha256.Size]byte
}

type hermesGatewayCookies struct {
	cookies []*http.Cookie
	expires time.Time
}

type hermesGatewayLogin struct {
	done    chan struct{}
	cookies []*http.Cookie
	err     error
}

// HermesGatewayWebSocket is internal authentication metadata for filtering
// upstream messages. It must never be used as a browser response DTO.
type HermesGatewayWebSocket struct {
	Conn    *websocket.Conn `json:"-"`
	Cookies []*http.Cookie  `json:"-"`
	Ticket  string          `json:"-"`
}

func (*HermesGatewayWebSocket) String() string   { return "Hermes gateway WebSocket (server-only)" }
func (*HermesGatewayWebSocket) GoString() string { return "Hermes gateway WebSocket (server-only)" }

type HermesGatewayAuth struct {
	origin    *url.URL
	client    *http.Client
	transport *http.Transport
	dialer    *websocket.Dialer
	now       func() time.Time
	mu        sync.Mutex
	cache     map[hermesGatewayCacheKey]hermesGatewayCookies
	logins    map[hermesGatewayCacheKey]*hermesGatewayLogin
}

// NewHermesGatewayAuth requires an explicit trusted control-plane origin. It
// never derives Origin from the Runtime Pod address or from browser headers.
func NewHermesGatewayAuth(origin string) (*HermesGatewayAuth, error) {
	parsed, err := url.Parse(origin)
	if err != nil || origin == "" || origin != strings.TrimSpace(origin) || parsed == nil ||
		parsed.User != nil || parsed.Opaque != "" || parsed.Path != "" || parsed.RawPath != "" ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" ||
		strings.Contains(origin, "#") || !hermesGatewayValidAuthority(parsed) {
		return nil, ErrHermesGatewayOrigin
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	transport := &http.Transport{
		Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		MaxIdleConns: 128, MaxIdleConnsPerHost: 8, IdleConnTimeout: time.Minute,
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second,
	}
	return &HermesGatewayAuth{
		origin: parsed, transport: transport,
		client: &http.Client{Transport: transport, Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		dialer: &websocket.Dialer{Proxy: nil, HandshakeTimeout: 10 * time.Second},
		now:    time.Now, cache: make(map[hermesGatewayCacheKey]hermesGatewayCookies),
		logins: make(map[hermesGatewayCacheKey]*hermesGatewayLogin),
	}, nil
}

func hermesGatewayValidAuthority(u *url.URL) bool {
	if u == nil || (strings.ToLower(u.Scheme) != "http" && strings.ToLower(u.Scheme) != "https") ||
		u.Host == "" || u.Hostname() == "" || strings.HasSuffix(u.Host, ":") {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n <= 0 || n > 65535 {
			return false
		}
	}
	host := u.Hostname()
	if net.ParseIP(host) != nil {
		return !strings.Contains(host, ":") || strings.HasPrefix(u.Host, "[")
	}
	if len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}

func hermesGatewayTarget(target *url.URL) (*url.URL, error) {
	if !hermesGatewayValidAuthority(target) || target.User != nil || target.Opaque != "" {
		return nil, ErrHermesGatewayAuth
	}
	// Callers may pass a legacy proxy target with a page path. Authentication
	// always uses the fixed endpoint below, never that path/query/fragment.
	return &url.URL{Scheme: strings.ToLower(target.Scheme), Host: target.Host}, nil
}

// SetHeaders standardizes the trusted forwarding boundary while retaining the
// Runtime target Host. Callers attach only server-held cookies after this step.
func (a *HermesGatewayAuth) SetHeaders(req *http.Request) {
	if req == nil {
		return
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	for _, name := range []string{"Origin", "Forwarded", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Forwarded-For", "X-Forwarded-Prefix", "Host", "Referer"} {
		req.Header.Del(name)
	}
	if req.URL != nil {
		req.Host = req.URL.Host
	}
	if a == nil || a.origin == nil {
		return
	}
	req.Header.Set("Origin", a.origin.String())
	req.Header.Set("X-Forwarded-Host", a.origin.Host)
	req.Header.Set("X-Forwarded-Proto", a.origin.Scheme)
}

func (a *HermesGatewayAuth) Cookie(ctx context.Context, key HermesGatewayAuthKey, target *url.URL, password string) ([]*http.Cookie, error) {
	if a == nil || a.origin == nil || a.client == nil || key.InstanceID <= 0 || strings.TrimSpace(password) == "" || ctx.Err() != nil {
		return nil, ErrHermesGatewayAuth
	}
	runtimeOrigin, err := hermesGatewayTarget(target)
	if err != nil {
		return nil, err
	}
	cacheKey := hermesGatewayCacheKey{allocation: key, target: runtimeOrigin.String(), credential: sha256.Sum256([]byte(password))}
	a.mu.Lock()
	if entry, exists := a.cache[cacheKey]; exists {
		if a.now().Before(entry.expires) {
			a.mu.Unlock()
			return hermesGatewayCloneCookies(entry.cookies), nil
		}
		delete(a.cache, cacheKey)
	}
	if login, exists := a.logins[cacheKey]; exists {
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ErrHermesGatewayAuth
		case <-login.done:
			return hermesGatewayCloneCookies(login.cookies), login.err
		}
	}
	if len(a.logins) >= hermesGatewayCacheMax {
		a.mu.Unlock()
		return nil, ErrHermesGatewayAuth
	}
	login := &hermesGatewayLogin{done: make(chan struct{})}
	a.logins[cacheKey] = login
	a.mu.Unlock()
	entry, err := a.login(ctx, runtimeOrigin, password)
	a.mu.Lock()
	if a.logins[cacheKey] != login {
		// Invalidate detached this in-flight login. Its result cannot resurrect
		// the old cookie or overwrite a newer successful login.
		err = ErrHermesGatewayAuth
	} else {
		delete(a.logins, cacheKey)
		if err == nil {
			if len(a.cache) >= hermesGatewayCacheMax {
				var oldest hermesGatewayCacheKey
				var expires time.Time
				for candidate, cached := range a.cache {
					if expires.IsZero() || cached.expires.Before(expires) {
						oldest, expires = candidate, cached.expires
					}
				}
				delete(a.cache, oldest)
			}
			a.cache[cacheKey] = entry
		}
	}
	if err == nil {
		login.cookies = entry.cookies
	}
	login.err = err
	close(login.done)
	a.mu.Unlock()
	return hermesGatewayCloneCookies(login.cookies), login.err
}

func (a *HermesGatewayAuth) Invalidate(key HermesGatewayAuthKey) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for candidate := range a.cache {
		if candidate.allocation == key {
			delete(a.cache, candidate)
		}
	}
	for candidate := range a.logins {
		if candidate.allocation == key {
			delete(a.logins, candidate)
		}
	}
}

func (a *HermesGatewayAuth) login(ctx context.Context, target *url.URL, password string) (hermesGatewayCookies, error) {
	payload, _ := json.Marshal(map[string]string{"provider": "basic", "username": "clawmanager", "password": password, "next": "/chat"})
	u := *target
	u.Path = "/auth/password-login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
	if err != nil {
		return hermesGatewayCookies{}, ErrHermesGatewayAuth
	}
	a.SetHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	response, err := a.do(req)
	if err != nil {
		return hermesGatewayCookies{}, err
	}
	receivedAt := a.now()
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return hermesGatewayCookies{}, ErrHermesGatewayAuth
	}
	body, err := hermesGatewayReadBody(response)
	if err != nil {
		return hermesGatewayCookies{}, err
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(body, &result) != nil || result == nil {
		return hermesGatewayCookies{}, ErrHermesGatewayAuth
	}
	now := receivedAt
	entry := hermesGatewayCookies{expires: now.Add(hermesGatewayCookieTTL)}
	for _, cookie := range response.Cookies() {
		if !hermesGatewaySessionCookie(cookie.Name) || cookie.Value == "" || cookie.MaxAge < 0 || (!cookie.Expires.IsZero() && !now.Before(cookie.Expires)) {
			continue
		}
		if cookie.MaxAge > 0 && cookie.MaxAge < int(hermesGatewayCookieTTL/time.Second) {
			if expires := now.Add(time.Duration(cookie.MaxAge) * time.Second); expires.Before(entry.expires) {
				entry.expires = expires
			}
		}
		if !cookie.Expires.IsZero() && cookie.Expires.Before(entry.expires) {
			entry.expires = cookie.Expires
		}
		entry.cookies = append(entry.cookies, cookie)
	}
	if len(entry.cookies) == 0 || !a.now().Before(entry.expires) {
		return hermesGatewayCookies{}, ErrHermesGatewayAuth
	}
	return entry, nil
}

func hermesGatewaySessionCookie(name string) bool {
	return name == "hermes_session_at" || name == "__Host-hermes_session_at" || name == "__Secure-hermes_session_at"
}

func (a *HermesGatewayAuth) do(req *http.Request) (*http.Response, error) {
	client := *a.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Jar = nil // Never mix implicit cookies between runtime allocations.
	if client.Timeout <= 0 || client.Timeout > 15*time.Second {
		client.Timeout = 15 * time.Second
	}
	if client.Transport == nil {
		client.Transport = a.transport
	} else if transport, ok := client.Transport.(*http.Transport); ok && transport.Proxy != nil {
		// Test/runtime HTTP clients may be injected, but the internal target
		// and password must never use an ambient or inherited forward proxy.
		copy := transport.Clone()
		copy.Proxy = nil
		defer copy.CloseIdleConnections()
		client.Transport = copy
	}
	response, err := client.Do(req)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, ErrHermesGatewayAuth
	}
	return response, nil
}

func hermesGatewayReadBody(response *http.Response) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(response.Body, hermesGatewayBodyMax+1))
	if err != nil || len(data) > hermesGatewayBodyMax {
		return nil, ErrHermesGatewayAuth
	}
	return data, nil
}

func hermesGatewayCloneCookies(cookies []*http.Cookie) []*http.Cookie {
	copy := make([]*http.Cookie, len(cookies))
	for i, cookie := range cookies {
		value := *cookie
		value.Unparsed = append([]string(nil), cookie.Unparsed...)
		copy[i] = &value
	}
	return copy
}

var hermesGatewayTicket = regexp.MustCompile(`^[A-Za-z0-9_.~-]+$`)
var hermesGatewayQueryKey = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func hermesGatewayWSQuery(path string, query url.Values) (string, error) {
	if path != "/api/ws" && path != "/api/pty" && path != "/api/events" {
		return "", ErrHermesGatewayAuth
	}
	if path == "/api/ws" && len(query) != 0 {
		return "", ErrHermesGatewayAuth
	}
	for name, values := range query {
		if !hermesGatewayQueryKey.MatchString(name) || len(values) != 1 || len(values[0]) > 4096 {
			return "", ErrHermesGatewayAuth
		}
		compact := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(name))
		for _, forbidden := range []string{"token", "ticket", "internal", "password", "secret", "apikey", "authorization", "cookie", "credential"} {
			if strings.Contains(compact, forbidden) {
				return "", ErrHermesGatewayAuth
			}
		}
	}
	encoded := query.Encode()
	if len(encoded) > 8192 {
		return "", ErrHermesGatewayAuth
	}
	return encoded, nil
}

// DialWebSocket authenticates once, mints one upstream ticket, and sends it
// only as a WebSocket subprotocol. It never retries ticket POSTs or WS dials.
// PTY/events application query fields must already be allowlisted by callers.
func (a *HermesGatewayAuth) DialWebSocket(ctx context.Context, key HermesGatewayAuthKey, target *url.URL, password, path string, query url.Values) (*HermesGatewayWebSocket, error) {
	encoded, err := hermesGatewayWSQuery(path, query)
	if err != nil || a == nil || a.dialer == nil {
		return nil, ErrHermesGatewayAuth
	}
	cookies, err := a.Cookie(ctx, key, target, password)
	if err != nil {
		return nil, err
	}
	runtimeOrigin, err := hermesGatewayTarget(target)
	if err != nil {
		return nil, err
	}
	ticketURL := *runtimeOrigin
	ticketURL.Path = "/api/auth/ws-ticket"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ticketURL.String(), nil)
	if err != nil {
		return nil, ErrHermesGatewayAuth
	}
	a.SetHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	response, err := a.do(req)
	if err != nil {
		return nil, err
	}
	body, readErr := hermesGatewayReadBody(response)
	_ = response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || (response.StatusCode >= 300 && response.StatusCode < 400) {
		a.Invalidate(key)
	}
	var ticket struct {
		Ticket string `json:"ticket"`
	}
	if response.StatusCode != http.StatusOK || readErr != nil || json.Unmarshal(body, &ticket) != nil || len(ticket.Ticket) > 4096 || !hermesGatewayTicket.MatchString(ticket.Ticket) {
		return nil, ErrHermesGatewayAuth
	}
	wsURL := *runtimeOrigin
	wsURL.Path, wsURL.RawQuery = path, encoded
	wsURL.Scheme = "ws"
	if runtimeOrigin.Scheme == "https" {
		wsURL.Scheme = "wss"
	}
	handshake, err := http.NewRequestWithContext(ctx, http.MethodGet, wsURL.String(), nil)
	if err != nil {
		return nil, ErrHermesGatewayAuth
	}
	a.SetHeaders(handshake)
	for _, cookie := range cookies {
		handshake.AddCookie(cookie)
	}
	dialer := *a.dialer
	dialer.Proxy = nil
	dialer.Subprotocols = []string{hermesGatewayProtocol, "hermes-gateway-ticket." + ticket.Ticket}
	if dialer.HandshakeTimeout <= 0 || dialer.HandshakeTimeout > 10*time.Second {
		dialer.HandshakeTimeout = 10 * time.Second
	}
	conn, response, err := dialer.DialContext(ctx, wsURL.String(), handshake.Header)
	if response != nil && (response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden) {
		a.Invalidate(key)
	}
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return nil, ErrHermesGatewayAuth
	}
	if conn.Subprotocol() != hermesGatewayProtocol {
		_ = conn.Close()
		return nil, ErrHermesGatewayAuth
	}
	return &HermesGatewayWebSocket{Conn: conn, Cookies: hermesGatewayCloneCookies(cookies), Ticket: ticket.Ticket}, nil
}
