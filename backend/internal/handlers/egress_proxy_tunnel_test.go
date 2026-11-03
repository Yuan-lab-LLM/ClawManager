package handlers

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"clawreef/internal/egresspolicy"
	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
)

type bufferedEgressHijackRecorder struct {
	*hijackableResponseRecorder
	reader *bufio.Reader
}

func (r *bufferedEgressHijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return r.serverConn, bufio.NewReadWriter(r.reader, bufio.NewWriter(r.serverConn)), nil
}

func TestEgressProxyConnectPreservesBufferedTunnelBytes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := &bufferedEgressHijackRecorder{hijackableResponseRecorder: newHijackableResponseRecorder(t)}
	r.reader = bufio.NewReader(io.MultiReader(strings.NewReader("FIRST"), r.serverConn))
	if _, err := r.reader.Peek(5); err != nil {
		t.Fatal(err)
	}
	upstream, peer := net.Pipe()
	t.Cleanup(func() { _ = upstream.Close(); _ = peer.Close() })
	h := NewEgressProxyHandler(nil)
	h.policy = egresspolicy.Policy{Mode: egresspolicy.ModeOpen}
	h.baseDialContext = func(context.Context, string, string) (net.Conn, error) { return upstream, nil }
	c, _ := gin.CreateTestContext(r)
	c.Request = httptest.NewRequest(http.MethodConnect, "http://8.8.8.8:443", nil)
	c.Request.Host = "8.8.8.8:443"
	done := make(chan struct{})
	go func() { defer close(done); h.handleConnect(c) }()
	_ = r.clientConn.SetDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(r.clientConn)
	status, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(status, "200") {
		t.Fatalf("CONNECT response: %q %v", status, err)
	}
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	_ = peer.SetReadDeadline(time.Now().Add(2 * time.Second))
	first := make([]byte, 5)
	if _, err := io.ReadFull(peer, first); err != nil || string(first) != "FIRST" {
		t.Fatalf("buffered bytes lost: %q %v", first, err)
	}
	if _, err := io.WriteString(r.clientConn, "SECOND"); err != nil {
		t.Fatal(err)
	}
	second := make([]byte, 6)
	if _, err := io.ReadFull(peer, second); err != nil || string(second) != "SECOND" {
		t.Fatalf("subsequent bytes lost: %q %v", second, err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("CONNECT handler did not return")
	}
}

func TestEgressProxyRecycledLegacyIPCannotUsePrivateException(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewEgressProxyHandler(nil, WithEgressPrivateExceptions(
		&stubPrivateExceptionSource{rules: []egresspolicy.PrivateExceptionRule{{ScopeType: "instance", ScopeID: 8, Prefix: netip.MustParsePrefix("10.255.25.3/32"), Port: 18080}}},
		&stubEgressInstanceResolver{byPodIP: map[string]*models.Instance{"10.42.0.130": {ID: 8, UserID: 1, Status: "error"}}},
	))
	h.baseDialContext = func(context.Context, string, string) (net.Conn, error) {
		t.Error("recycled IP triggered private dial")
		return nil, io.EOF
	}
	for _, method := range []string{http.MethodGet, http.MethodConnect} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(method, "http://10.255.25.3:18080/", nil)
		c.Request.Host = "10.255.25.3:18080"
		c.Request.RemoteAddr = "10.42.0.130:48800"
		c.Request.Header.Set("X-ClawManager-Instance-Id", "8")
		h.Handle(c)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s recycled IP got %d", method, w.Code)
		}
	}
}

type unavailableEgressRuleSource struct{ calls int }

func (s *unavailableEgressRuleSource) SnapshotRulesContext(ctx context.Context) []egresspolicy.PrivateExceptionRule {
	s.calls++
	<-ctx.Done()
	return nil
}

func TestEgressProxyPublicDialDoesNotWaitForRuleRefresh(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		source := &unavailableEgressRuleSource{}
		h := NewEgressProxyHandler(nil, WithEgressPrivateExceptions(source, nil))
		h.baseDialContext = func(context.Context, string, string) (net.Conn, error) {
			a, b := net.Pipe()
			_ = a.Close()
			return b, nil
		}
		h.lookupNetIP = func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		if authenticated {
			id, user := 8, 1
			ctx = context.WithValue(ctx, egressIdentityContextKey{}, egressRequestIdentity{InstanceID: &id, UserID: &user})
		}
		for _, address := range []string{"8.8.8.8:443", "public.example:443"} {
			conn, err := h.proxyDialContext(ctx, "tcp", address)
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			_ = conn.Close()
		}
		cancel()
		if source.calls != 0 {
			t.Fatalf("public request waited for private rules: authenticated=%v", authenticated)
		}
	}
}
