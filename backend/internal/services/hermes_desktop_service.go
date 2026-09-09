package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"

	"github.com/golang-jwt/jwt/v5"
)

const (
	HermesDesktopRef        = "v2026.8.31"
	HermesDesktopCommit     = "29112bef099274229cadff79cdff7bf7b99c4b77"
	HermesDesktopSessionTTL = 10 * time.Minute
	hermesDesktopTicketTTL  = 30 * time.Second
	hermesDesktopMaxBody    = 4 << 20
)

var (
	ErrHermesDesktopUnauthorized = errors.New("desktop_session_required")
	ErrHermesDesktopForbidden    = errors.New("desktop_access_denied")
	ErrHermesDesktopUnavailable  = errors.New("desktop_unavailable")
	ErrHermesDesktopUpstream     = errors.New("desktop_upstream_unavailable")
)

type HermesDesktopConfig struct {
	Enabled         bool
	ControlUIOrigin string
	Secret          string
	Instances       repository.InstanceRepository
	Users           repository.UserRepository
	Bindings        repository.InstanceRuntimeBindingRepository
	Pods            repository.RuntimePodRepository
	Teams           repository.HermesDesktopTeamGuard
	Agent           RuntimeAgentClient
	Redis           PlatformRedisClient
}

type HermesDesktopDescriptor struct {
	Available    bool              `json:"available"`
	Reason       string            `json:"reason,omitempty"`
	InstanceID   int               `json:"instance_id"`
	RendererURL  string            `json:"renderer_url,omitempty"`
	APIBase      string            `json:"api_base,omitempty"`
	ExpiresAt    *time.Time        `json:"expires_at,omitempty"`
	Versions     map[string]string `json:"versions,omitempty"`
	Capabilities []string          `json:"capabilities"`
}

type HermesDesktopClaims struct {
	UserID     int    `json:"uid"`
	InstanceID int    `json:"iid"`
	Generation int    `json:"gen"`
	PodID      int64  `json:"pod"`
	Port       int    `json:"port"`
	SessionID  string `json:"sid"`
	Epoch      string `json:"epoch"`
	jwt.RegisteredClaims
}

type hermesDesktopTarget struct {
	instance *models.Instance
	binding  *models.InstanceRuntimeBinding
	pod      *models.RuntimePod
	url      *url.URL
}

type HermesDesktopService struct {
	config      HermesDesktopConfig
	gatewayAuth *HermesGatewayAuth
	key         []byte
	client      *http.Client
	mu          sync.Mutex
	health      map[string]time.Time
	catalog     map[hermesGatewayCacheKey]hermesDesktopCatalogEntry
}

func NewHermesDesktopService(config HermesDesktopConfig) *HermesDesktopService {
	key := sha256.Sum256([]byte("clawmanager/hermes-desktop/v1/" + config.Secret))
	auth, _ := NewHermesGatewayAuth(config.ControlUIOrigin)
	return &HermesDesktopService{config: config, gatewayAuth: auth, key: key[:], client: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, MaxIdleConns: 128, MaxIdleConnsPerHost: 8, IdleConnTimeout: 60 * time.Second, ResponseHeaderTimeout: 10 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, health: make(map[string]time.Time), catalog: make(map[hermesGatewayCacheKey]hermesDesktopCatalogEntry)}
}

func HermesDesktopBase(instanceID int) string {
	return fmt.Sprintf("/api/v1/instances/%d/hermes-desktop", instanceID)
}
func HermesDesktopCookieName(instanceID int) string {
	return fmt.Sprintf("cm_hermes_desktop_%d", instanceID)
}

func (s *HermesDesktopService) resolve(ctx context.Context, userID, instanceID int) (*hermesDesktopTarget, string, error) {
	return s.resolveRuntime(ctx, userID, instanceID, true)
}

func (s *HermesDesktopService) resolveRuntime(ctx context.Context, userID, instanceID int, desktop bool) (*hermesDesktopTarget, string, error) {
	user, err := s.config.Users.GetByID(userID)
	if err != nil || user == nil || !user.IsActive {
		return nil, "", ErrHermesDesktopUnauthorized
	}
	instance, err := s.config.Instances.GetByID(instanceID)
	if err != nil || instance == nil {
		return nil, "", ErrHermesDesktopForbidden
	}
	if instance.UserID != userID && user.Role != "admin" {
		return nil, "", ErrHermesDesktopForbidden
	}
	if desktop && !s.config.Enabled {
		return nil, "feature_disabled", nil
	}
	if strings.TrimSpace(s.config.Secret) == "" {
		return nil, "runtime_auth_unavailable", nil
	}
	if s.gatewayAuth == nil {
		return nil, "runtime_origin_unavailable", nil
	}
	if s.config.Redis == nil {
		return nil, "ticket_store_unavailable", nil
	}
	if instance.Type != "hermes" || instance.InstanceMode != InstanceModeLite || instance.RuntimeType != RuntimeBackendGateway {
		return nil, "unsupported_instance", nil
	}
	if s.config.Teams == nil {
		return nil, "runtime_unavailable", nil
	}
	team, err := s.config.Teams.IsTeamInstance(instanceID)
	if err != nil {
		return nil, "runtime_unavailable", nil
	}
	if team {
		return nil, "team_not_supported", nil
	}
	if instance.Status != "running" {
		return nil, "instance_not_running", nil
	}
	binding, err := s.config.Bindings.GetRunningByInstanceID(ctx, instanceID)
	if err != nil || binding == nil || binding.InstanceID != instanceID || binding.RuntimeType != "hermes" || binding.State != "running" || binding.Generation != instance.RuntimeGeneration || binding.GatewayPort <= 0 || binding.GatewayPort > 65535 {
		return nil, "runtime_not_ready", nil
	}
	pod, err := s.config.Pods.GetByID(ctx, binding.RuntimePodID)
	if err != nil || pod == nil || pod.RuntimeType != "hermes" || pod.PodIP == nil || net.ParseIP(strings.TrimSpace(*pod.PodIP)) == nil || pod.AgentEndpoint == nil || (pod.State != "ready" && pod.State != "draining") {
		return nil, "runtime_not_ready", nil
	}
	if instance.AccessToken == nil || strings.TrimSpace(*instance.AccessToken) == "" {
		return nil, "runtime_auth_unavailable", nil
	}
	target := &hermesDesktopTarget{instance: instance, binding: binding, pod: pod, url: &url.URL{Scheme: "http", Host: net.JoinHostPort(strings.TrimSpace(*pod.PodIP), strconv.Itoa(binding.GatewayPort))}}
	return target, "", nil
}

func (s *HermesDesktopService) compatible(ctx context.Context, target *hermesDesktopTarget) bool {
	client, ok := s.config.Agent.(HermesDesktopCapabilityClient)
	if !ok {
		return false
	}
	key := fmt.Sprintf("%d/%d/%s/%d", target.pod.ID, target.instance.ID, *target.pod.AgentEndpoint, target.binding.Generation)
	s.mu.Lock()
	expires := s.health[key]
	s.mu.Unlock()
	if time.Now().Before(expires) {
		return true
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	health, err := client.HealthCapabilities(ctx, *target.pod.AgentEndpoint)
	if err != nil || health == nil || health.Capabilities.HermesDesktopWeb == nil {
		return false
	}
	c := health.Capabilities.HermesDesktopWeb
	if !c.compatible() {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.health) >= 512 {
		clear(s.health)
	}
	s.health[key] = time.Now().Add(30 * time.Second)
	return true
}

func (s *HermesDesktopService) Describe(ctx context.Context, userID, instanceID int) (*HermesDesktopDescriptor, error) {
	d := &HermesDesktopDescriptor{InstanceID: instanceID, Capabilities: []string{}}
	target, reason, err := s.resolve(ctx, userID, instanceID)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		d.Reason = reason
		return d, nil
	}
	if !s.redisReady(ctx) {
		d.Reason = "ticket_store_unavailable"
		return d, nil
	}
	if !s.compatible(ctx, target) {
		d.Reason = "runtime_capability_unsupported"
		return d, nil
	}
	d.Available = true
	d.APIBase = HermesDesktopBase(instanceID)
	d.RendererURL = fmt.Sprintf("/hermes-desktop-web/?instance_id=%d", instanceID)
	d.Versions = map[string]string{"hermes_ref": HermesDesktopRef, "hermes_commit": HermesDesktopCommit, "bridge_version": "1"}
	d.Capabilities = []string{"chat", "sessions"}
	return d, nil
}

func (s *HermesDesktopService) redisReady(ctx context.Context) bool {
	if _, ok := s.config.Redis.(hermesDesktopLeaseStore); !ok {
		return false
	}
	probe, ok := s.config.Redis.(hermesDesktopRedisReadiness)
	if !ok {
		return false
	}
	s.mu.Lock()
	expires := s.health["redis-readiness"]
	s.mu.Unlock()
	if time.Now().Before(expires) {
		return true
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if probe.Ping(ctx) != nil {
		return false
	}
	s.mu.Lock()
	s.health["redis-readiness"] = time.Now().Add(30 * time.Second)
	s.mu.Unlock()
	return true
}

func (s *HermesDesktopService) Activate(ctx context.Context, userID, instanceID int) (*HermesDesktopDescriptor, string, error) {
	// Capture the authorization epoch before health/login network calls. A
	// logout racing a slow activation must not be adopted as its new epoch.
	if _, reason, err := s.resolve(ctx, userID, instanceID); err != nil {
		return nil, "", err
	} else if reason != "" {
		return &HermesDesktopDescriptor{InstanceID: instanceID, Reason: reason, Capabilities: []string{}}, "", nil
	}
	epoch, err := s.ensureSessionEpoch(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	d, err := s.Describe(ctx, userID, instanceID)
	if err != nil || !d.Available {
		return d, "", err
	}
	target, reason, err := s.resolve(ctx, userID, instanceID)
	if err != nil {
		return nil, "", err
	}
	if reason != "" {
		return nil, "", ErrHermesDesktopUnavailable
	}
	// Validate upstream login before advertising a usable renderer. The cookies
	// are cached only in the server process and are never serialized to clients.
	if _, err = s.upstreamCookies(ctx, target); err != nil {
		d.Available = false
		d.Reason = "runtime_auth_unavailable"
		return d, "", nil
	}
	id, err := hermesDesktopRandomID()
	if err != nil {
		return nil, "", err
	}
	currentEpoch, err := s.sessionEpoch(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	if currentEpoch != epoch {
		return nil, "", ErrHermesDesktopUnauthorized
	}
	claims := HermesDesktopClaims{UserID: userID, InstanceID: instanceID, Generation: target.binding.Generation, PodID: target.pod.ID, Port: target.binding.GatewayPort, SessionID: id, Epoch: epoch}
	token, err := s.sign(&claims, "session", HermesDesktopSessionTTL)
	if err != nil {
		return nil, "", err
	}
	expiry := claims.ExpiresAt.Time
	d.ExpiresAt = &expiry
	return d, token, nil
}

func hermesDesktopRandomID() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *HermesDesktopService) sign(c *HermesDesktopClaims, audience string, ttl time.Duration) (string, error) {
	id, err := hermesDesktopRandomID()
	if err != nil {
		return "", err
	}
	c.RegisteredClaims = jwt.RegisteredClaims{Issuer: "clawmanager-hermes-desktop", Audience: jwt.ClaimStrings{audience}, ID: id, IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl))}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.key)
}

func (s *HermesDesktopService) parse(raw, audience string) (*HermesDesktopClaims, error) {
	if len(raw) > 4096 {
		return nil, ErrHermesDesktopUnauthorized
	}
	c := &HermesDesktopClaims{}
	_, err := jwt.ParseWithClaims(raw, c, func(*jwt.Token) (any, error) { return s.key, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("clawmanager-hermes-desktop"), jwt.WithAudience(audience), jwt.WithExpirationRequired())
	if err != nil || c.ID == "" || c.SessionID == "" || c.Epoch == "" || c.UserID <= 0 {
		return nil, ErrHermesDesktopUnauthorized
	}
	return c, nil
}

func (s *HermesDesktopService) Authenticate(ctx context.Context, raw string, instanceID int) (*HermesDesktopClaims, error) {
	c, err := s.parse(raw, "session")
	if err != nil {
		return nil, err
	}
	if c.InstanceID != instanceID {
		return nil, ErrHermesDesktopForbidden
	}
	if _, err = s.authorizeClaims(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *HermesDesktopService) authorizeClaims(ctx context.Context, c *HermesDesktopClaims) (*hermesDesktopTarget, error) {
	if c.ExpiresAt == nil || !time.Now().Before(c.ExpiresAt.Time) {
		return nil, ErrHermesDesktopUnauthorized
	}
	epoch, err := s.sessionEpoch(ctx, c.UserID)
	if err != nil {
		return nil, err
	}
	if c.Epoch != epoch {
		return nil, ErrHermesDesktopUnauthorized
	}
	target, reason, err := s.resolveRuntime(ctx, c.UserID, c.InstanceID, true)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		return nil, ErrHermesDesktopUnavailable
	}
	if c.Generation != target.binding.Generation || c.PodID != target.pod.ID || c.Port != target.binding.GatewayPort {
		return nil, ErrHermesDesktopUnauthorized
	}
	if !s.compatible(ctx, target) {
		return nil, ErrHermesDesktopUnavailable
	}
	return target, nil
}

func (s *HermesDesktopService) MintTicket(c *HermesDesktopClaims) (string, time.Time, error) {
	copy := *c
	if c.ExpiresAt == nil || !time.Now().Before(c.ExpiresAt.Time) {
		return "", time.Time{}, ErrHermesDesktopUnauthorized
	}
	ttl := hermesDesktopTicketTTL
	if remaining := time.Until(c.ExpiresAt.Time); remaining < ttl {
		ttl = remaining
	}
	token, err := s.sign(&copy, "websocket", ttl)
	if err != nil {
		return "", time.Time{}, err
	}
	return HermesDesktopBase(c.InstanceID) + "/ws?ticket=" + url.QueryEscape(token), copy.ExpiresAt.Time, nil
}

func (s *HermesDesktopService) redeemTicket(ctx context.Context, raw string, c *HermesDesktopClaims) error {
	ticket, err := s.parse(raw, "websocket")
	if err != nil {
		return err
	}
	if ticket.UserID != c.UserID || ticket.InstanceID != c.InstanceID || ticket.Generation != c.Generation || ticket.PodID != c.PodID || ticket.Port != c.Port || ticket.SessionID != c.SessionID || ticket.Epoch != c.Epoch {
		return ErrHermesDesktopForbidden
	}
	if s.config.Redis == nil {
		return ErrHermesDesktopUnavailable
	}
	ok, err := s.config.Redis.SetNX(ctx, "hermes-desktop:ws-used:"+ticket.ID, "1", hermesDesktopTicketTTL+time.Second)
	if err != nil {
		return ErrHermesDesktopUnavailable
	}
	if !ok {
		return ErrHermesDesktopUnauthorized
	}
	return nil
}

func (s *HermesDesktopService) sessionEpoch(ctx context.Context, userID int) (string, error) {
	store, ok := s.config.Redis.(hermesDesktopLeaseStore)
	if !ok {
		return "", ErrHermesDesktopUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	epoch, found, err := store.Get(ctx, fmt.Sprintf("hermes-desktop:user-epoch:%d", userID))
	if err != nil {
		return "", ErrHermesDesktopUnavailable
	}
	if !found {
		return "", ErrHermesDesktopUnauthorized
	}
	return epoch, nil
}

func (s *HermesDesktopService) ensureSessionEpoch(ctx context.Context, userID int) (string, error) {
	store, ok := s.config.Redis.(hermesDesktopLeaseStore)
	if !ok {
		return "", ErrHermesDesktopUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	key := fmt.Sprintf("hermes-desktop:user-epoch:%d", userID)
	if epoch, found, err := store.Get(ctx, key); err != nil {
		return "", ErrHermesDesktopUnavailable
	} else if found {
		return epoch, nil
	}
	epoch, err := hermesDesktopRandomID()
	if err != nil {
		return "", err
	}
	if _, err := store.SetPersistentNX(ctx, key, epoch); err != nil {
		return "", ErrHermesDesktopUnavailable
	}
	return s.sessionEpoch(ctx, userID)
}

// RevokeUserSessions invalidates every Desktop Web cookie for this account,
// including cookies on other instances/replicas whose Path keeps them out of
// the /auth/logout request. The ordinary stateless bearer JWT is unchanged.
func (s *HermesDesktopService) RevokeUserSessions(ctx context.Context, userID int) error {
	if !s.config.Enabled && s.gatewayAuth == nil {
		return nil
	}
	store, ok := s.config.Redis.(hermesDesktopLeaseStore)
	if !ok || userID <= 0 {
		return ErrHermesDesktopUnavailable
	}
	epoch, err := hermesDesktopRandomID()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// One small persistent key per account avoids revoking a fresh lease when
	// an earlier logout marker expires. Missing state also fails closed; only
	// a new bearer-authorized bootstrap can establish a fresh epoch.
	if err := store.Set(ctx, fmt.Sprintf("hermes-desktop:user-epoch:%d", userID), epoch, 0); err != nil {
		return ErrHermesDesktopUnavailable
	}
	return nil
}

func (s *HermesDesktopService) upstreamCookies(ctx context.Context, t *hermesDesktopTarget) ([]*http.Cookie, error) {
	if s.gatewayAuth == nil {
		return nil, ErrHermesDesktopUnavailable
	}
	cookies, err := s.gatewayAuth.Cookie(ctx, t.authKey(), t.url, *t.instance.AccessToken)
	if err != nil {
		return nil, ErrHermesDesktopUpstream
	}
	return cookies, nil
}

func (t *hermesDesktopTarget) authKey() HermesGatewayAuthKey {
	return HermesGatewayAuthKey{InstanceID: t.instance.ID, Generation: t.binding.Generation, RuntimePodID: t.pod.ID, GatewayPort: t.binding.GatewayPort}
}

func (s *HermesDesktopService) upstreamRequest(ctx context.Context, t *hermesDesktopTarget, method, path, query string) ([]byte, int, []*http.Cookie, error) {
	return s.upstreamRequestBody(ctx, t, method, path, query, nil)
}

func (s *HermesDesktopService) upstreamRequestBody(ctx context.Context, t *hermesDesktopTarget, method, path, query string, body []byte) ([]byte, int, []*http.Cookie, error) {
	return s.upstreamRequestAttempt(ctx, t, method, path, query, body, true)
}

func (s *HermesDesktopService) upstreamRequestAttempt(ctx context.Context, t *hermesDesktopTarget, method, path, query string, body []byte, retry bool) ([]byte, int, []*http.Cookie, error) {
	cookies, err := s.upstreamCookies(ctx, t)
	if err != nil {
		return nil, 0, nil, err
	}
	u := *t.url
	u.Path = path
	u.RawQuery = query
	req, _ := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	req.Header.Set("Accept", "application/json")
	s.gatewayAuth.SetHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, 0, nil, ErrHermesDesktopUpstream
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || (resp.StatusCode >= 300 && resp.StatusCode < 400) {
		// A runtime restart invalidates its cookie store without necessarily
		// changing the binding. Re-authenticate once; never replay an RPC.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		s.gatewayAuth.Invalidate(t.authKey())
		if retry && (method == http.MethodGet || method == http.MethodHead) {
			return s.upstreamRequestAttempt(ctx, t, method, path, query, body, false)
		}
		return nil, resp.StatusCode, nil, ErrHermesDesktopUpstream
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, hermesDesktopMaxBody+1))
	if err != nil || len(responseBody) > hermesDesktopMaxBody {
		return nil, 0, nil, ErrHermesDesktopUpstream
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, nil, ErrHermesDesktopUpstream
	}
	return responseBody, resp.StatusCode, cookies, nil
}

func (s *HermesDesktopService) ProxyAPI(ctx context.Context, c *HermesDesktopClaims, method, path string, query url.Values) ([]byte, error) {
	return s.ProxyAPIRequest(ctx, c, method, path, query, nil)
}

func (s *HermesDesktopService) ProxyAPIRequest(ctx context.Context, c *HermesDesktopClaims, method, path string, query url.Values, requestBody []byte) ([]byte, error) {
	if !hermesDesktopHTTPAllowed(method, path, query) {
		return nil, ErrHermesDesktopForbidden
	}
	target, err := s.authorizeClaims(ctx, c)
	if err != nil {
		return nil, err
	}
	if path == "/profiles/sessions/sidebar" {
		return s.desktopSidebar(ctx, target, query)
	}
	if method == http.MethodPut && path == "/config" {
		var envelope struct {
			Config map[string]json.RawMessage `json:"config"`
		}
		decoder := json.NewDecoder(bytes.NewReader(requestBody))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&envelope) != nil || envelope.Config == nil || decoder.Decode(new(any)) != io.EOF {
			return nil, ErrHermesDesktopForbidden
		}
		body, _, cookies, err := s.upstreamRequestBody(ctx, target, method, "/api/config", "", requestBody)
		if err != nil {
			return nil, err
		}
		return hermesDesktopSanitize(body, *target.instance.AccessToken, cookies)
	}
	if method == http.MethodGet && path == "/hermes/update/check" {
		status, err := s.desktopRead(ctx, target, "/status", nil)
		if err != nil {
			return nil, err
		}
		var current struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(status, &current) != nil {
			return nil, ErrHermesDesktopUpstream
		}
		return json.Marshal(map[string]any{
			"install_method": "clawmanager", "current_version": current.Version,
			"behind": nil, "update_available": false, "can_apply": false,
			"update_command": nil, "message": "Runtime updates are managed by ClawManager.",
		})
	}
	if path == "/profiles/sessions" {
		body, err := s.desktopRead(ctx, target, "/sessions", query)
		if err != nil {
			return nil, err
		}
		var page map[string]json.RawMessage
		if json.Unmarshal(body, &page) != nil {
			return nil, ErrHermesDesktopUpstream
		}
		page["profile_totals"], _ = json.Marshal(map[string]json.RawMessage{"default": page["total"]})
		return json.Marshal(page)
	}
	if method != http.MethodGet {
		q := url.Values{}
		for key, values := range query {
			if key != "profile" {
				q[key] = append([]string(nil), values...)
			}
		}
		body, _, cookies, err := s.upstreamRequestBody(ctx, target, method, "/api"+path, q.Encode(), requestBody)
		if err != nil {
			return nil, err
		}
		secrets := []string{*target.instance.AccessToken}
		for _, cookie := range cookies {
			secrets = append(secrets, cookie.Value)
		}
		return hermesDesktopRedactStringValues(body, secrets...)
	}
	return s.desktopRead(ctx, target, path, query)
}
