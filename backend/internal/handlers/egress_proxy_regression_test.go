package handlers

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/egresspolicy"
	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
)

func TestEgressProxyHTTPChecksEveryRequestAndDoesNotLeakIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" || r.Header.Get("X-ClawManager-Instance-Id") != "" || r.Header.Get("X-ClawManager-Egress-Instance-Id") != "" {
			t.Error("proxy identity leaked to upstream")
		}
		if r.Header.Get("Authorization") != "Bearer origin-token" {
			t.Error("origin authorization was changed")
		}
		_, _ = io.WriteString(w, "private service")
	}))
	defer upstream.Close()
	tokenA, tokenB := "instance-a-token", "instance-b-token"
	rules := &stubPrivateExceptionSource{rules: []egresspolicy.PrivateExceptionRule{{ScopeType: "instance", ScopeID: 8, Prefix: netip.MustParsePrefix("10.255.25.3/32"), Port: 18080}}}
	h := NewEgressProxyHandler(nil, WithEgressPrivateExceptions(rules, &stubEgressInstanceResolver{byID: map[int]*models.Instance{
		8: {ID: 8, UserID: 1, AccessToken: &tokenA},
		9: {ID: 9, UserID: 2, AccessToken: &tokenB},
	}}))
	h.policy = egresspolicy.Policy{Mode: egresspolicy.ModeAllowlist, AllowedHostSuffixes: []string{"github.com"}}
	var dials atomic.Int32
	h.baseDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "10.255.25.3:18080" {
			t.Errorf("unexpected target: %s", address)
		}
		dials.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(upstream.URL, "http://"))
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		h.Handle(c)
	}))
	defer proxy.Close()
	defer h.transport.CloseIdleConnections()
	clientA := egressProxyTestClient(t, proxy.URL, 8, tokenA)
	clientB := egressProxyTestClient(t, proxy.URL, 9, tokenB)
	anonymous := egressProxyTestClient(t, proxy.URL, 0, "")
	for i, test := range []struct {
		client *http.Client
		want   int
		revoke bool
	}{
		{clientB, 403, false},
		{clientA, 200, false},
		{clientB, 403, false},
		{anonymous, 403, false},
		{clientA, 200, false},
		{clientA, 403, true},
	} {
		if test.revoke {
			rules.rules = nil
		}
		req, err := http.NewRequest(http.MethodGet, "http://10.255.25.3:18080/", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-ClawManager-Instance-Id", "8")
		req.Header.Set("X-ClawManager-Egress-Instance-Id", "8")
		req.Header.Set("Authorization", "Bearer origin-token")
		resp, err := test.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != test.want {
			t.Fatalf("request %d: got %d (%s), want %d", i, resp.StatusCode, body, test.want)
		}
	}
	if dials.Load() != 2 {
		t.Fatalf("authorized requests should each dial once: got %d", dials.Load())
	}
}

func TestEgressProxyUnlistedHostWithoutApplicableExceptionBlocksBeforeDNSAndAudits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, method := range []string{http.MethodGet, http.MethodConnect} {
		t.Run(method, func(t *testing.T) {
			audit := &stubEgressAuditService{}
			h := NewEgressProxyHandler(audit)
			h.policy = egresspolicy.Policy{Mode: egresspolicy.ModeAllowlist, AllowedHostSuffixes: []string{"github.com"}}
			var lookups atomic.Int32
			h.lookupNetIP = func(context.Context, string, string) ([]netip.Addr, error) {
				lookups.Add(1)
				return nil, &net.DNSError{Err: "no such host", Name: "unlisted.invalid", IsNotFound: true}
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(method, "http://unlisted.invalid:443/", nil)
			c.Request.Host = "unlisted.invalid:443"
			h.Handle(c)
			if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "host not in allowlist") {
				t.Fatalf("expected policy block, got %d: %s", w.Code, w.Body.String())
			}
			if lookups.Load() != 0 {
				t.Fatal("inapplicable exception triggered DNS lookup")
			}
			if len(audit.events) != 1 || audit.events[0].EventType != "egress.llm.blocked" {
				t.Fatalf("missing block audit: %+v", audit.events)
			}
		})
	}
}

func TestEgressProxyHTTPSConnectUsesAuthenticatedUserException(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "private TLS service") }))
	defer upstream.Close()
	token := "gateway-token"
	rules := &stubPrivateExceptionSource{rules: []egresspolicy.PrivateExceptionRule{{ScopeType: "user", ScopeID: 1, Prefix: netip.MustParsePrefix("10.255.25.3/32"), Port: 18443}}}
	h := NewEgressProxyHandler(nil, WithEgressPrivateExceptions(rules, &stubEgressInstanceResolver{byID: map[int]*models.Instance{8: {ID: 8, UserID: 1, AccessToken: &token}}}))
	h.policy = egresspolicy.Policy{Mode: egresspolicy.ModeAllowlist}
	h.baseDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "10.255.25.3:18443" {
			t.Errorf("unexpected CONNECT target: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(upstream.URL, "https://"))
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		h.Handle(c)
	}))
	defer proxy.Close()
	client := egressProxyTestClient(t, proxy.URL, 8, token)
	resp, err := client.Get("https://10.255.25.3:18443/")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || resp.StatusCode != 200 || string(body) != "private TLS service" {
		t.Fatalf("CONNECT response: %d %q %v", resp.StatusCode, body, err)
	}
	// A new tunnel must check the current rules. Established tunnels are not revoked.
	client.Transport.(*http.Transport).CloseIdleConnections()
	rules.rules = nil
	if resp, err := client.Get("https://10.255.25.3:18443/"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("new CONNECT succeeded after revocation")
	}
}

func egressProxyTestClient(t *testing.T, proxyURL string, instanceID int, token string) *http.Client {
	t.Helper()
	u, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	if instanceID > 0 {
		u.User = url.UserPassword(fmt.Sprint(instanceID), egresspolicy.InstanceProxyPassword(instanceID, token))
	}
	transport := &http.Transport{Proxy: http.ProxyURL(u), TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 3 * time.Second}
}

func TestEgressProxyIdentityRequiresCredentialsEvenForLegacyPod(t *testing.T) {
	gin.SetMode(gin.TestMode)
	token := "gateway-token"
	h := NewEgressProxyHandler(nil, WithEgressPrivateExceptions(&stubPrivateExceptionSource{}, &stubEgressInstanceResolver{
		byID:    map[int]*models.Instance{8: {ID: 8, UserID: 1, AccessToken: &token}},
		byPodIP: map[string]*models.Instance{"10.42.0.130": {ID: 9, UserID: 2}},
	}))
	password := egresspolicy.InstanceProxyPassword(8, token)
	for _, test := range []struct {
		name, source, claim, auth string
		want                      int
	}{
		{"shared Pod authenticated", "10.42.0.200:48800", "9", "Basic " + base64.StdEncoding.EncodeToString([]byte("8:"+password)), 8},
		{"header alone", "10.42.0.200:48800", "8", "", 0},
		{"legacy Pod without header", "10.42.0.130:48800", "", "", 0},
		{"legacy Pod with matching hint", "10.42.0.130:48800", "9", "", 0},
		{"forged header", "10.42.0.130:48800", "8", "", 0},
		{"bad credentials cannot fall back", "10.42.0.130:48800", "9", "Basic " + base64.StdEncoding.EncodeToString([]byte("8:wrong")), 0},
		{"unknown instance", "10.42.0.130:48800", "9", "Basic " + base64.StdEncoding.EncodeToString([]byte("10:"+password)), 0},
		{"raw gateway token rejected", "10.42.0.200:48800", "8", "Basic " + base64.StdEncoding.EncodeToString([]byte("8:"+token)), 0},
		{"malformed auth", "10.42.0.130:48800", "9", "Basic ???", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
			c.Request.RemoteAddr = test.source
			c.Request.Header.Set("X-ClawManager-Instance-Id", test.claim)
			c.Request.Header.Set("Proxy-Authorization", test.auth)
			identity := h.resolveEgressIdentity(c)
			if test.want == 0 {
				if identity.InstanceID != nil || identity.UserID != nil {
					t.Fatalf("unexpected identity: %+v", identity)
				}
				return
			}
			if identity.InstanceID == nil || *identity.InstanceID != test.want || identity.UserID == nil {
				t.Fatalf("identity=%+v, want instance %d", identity, test.want)
			}
		})
	}
}
