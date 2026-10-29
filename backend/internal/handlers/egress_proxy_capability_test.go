package handlers

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/egresspolicy"
	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	"github.com/gin-gonic/gin"
)

type egressVerifierFunc func(context.Context, string, string) (runtimeauth.Identity, error)

func (f egressVerifierFunc) Verify(ctx context.Context, token, audience string) (runtimeauth.Identity, error) {
	return f(ctx, token, audience)
}

var egressCapabilityIdentity = runtimeauth.Identity{InstanceID: 8, UserID: 1, Generation: 3, PodUID: "pod-test"}

func newEgressCapabilityService(t *testing.T) (*runtimeauth.Service, string, string) {
	t.Helper()
	s, err := runtimeauth.New([]byte(strings.Repeat("k", 32)), func(context.Context, int) (runtimeauth.Identity, error) { return egressCapabilityIdentity, nil })
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.Issue(context.Background(), egressCapabilityIdentity, runtimeauth.AudienceEgress)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Issue(context.Background(), egressCapabilityIdentity, runtimeauth.AudienceModel)
	if err != nil {
		t.Fatal(err)
	}
	return s, e, m
}

func TestEgressCapabilityRejectsBeforeDial(t *testing.T) {
	s, good, model := newEgressCapabilityService(t)
	for _, method := range []string{http.MethodGet, http.MethodConnect} {
		for _, tc := range []struct {
			name, auth string
			status     int
		}{{"missing", "", 407}, {"basic", "Basic abc", 407}, {"invalid", "Bearer invalid", 403}, {"model", "Bearer " + model, 403}} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				h := NewEgressProxyHandler(nil, WithEgressRuntimeCapabilities(s))
				calls := 0
				h.baseDialContext = func(context.Context, string, string) (net.Conn, error) {
					calls++
					return nil, errors.New("unexpected dial")
				}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(method, "http://8.8.8.8:80/", nil)
				c.Request.Host = "8.8.8.8:80"
				c.Request.Header.Set("Proxy-Authorization", tc.auth)
				c.Request.Header.Set("X-ClawManager-Instance-Id", "8")
				h.Handle(c)
				if rec.Code != tc.status || calls != 0 {
					t.Fatalf("status=%d calls=%d body=%s", rec.Code, calls, rec.Body.String())
				}
				if strings.Contains(rec.Body.String(), good) || strings.Contains(rec.Body.String(), model) {
					t.Fatal("token exposed")
				}
			})
		}
	}
}

func TestEgressSpoofedIdentityCannotAuthorizePrivateTarget(t *testing.T) {
	for _, header := range []string{"X-ClawManager-Instance-Id", "X-ClawManager-Egress-Instance-Id"} {
		t.Run(header, func(t *testing.T) {
			h := NewEgressProxyHandler(nil, WithEgressPrivateExceptions(&stubPrivateExceptionSource{rules: []egresspolicy.PrivateExceptionRule{{ScopeType: egresspolicy.ScopeInstance, ScopeID: 8, Prefix: netip.MustParsePrefix("10.255.25.3/32"), Port: 18080}}}, &stubEgressInstanceResolver{byID: map[int]*models.Instance{8: {ID: 8, UserID: 1}}, byPodIP: map[string]*models.Instance{"10.42.0.130": {ID: 8, UserID: 1}}}))
			calls := 0
			h.baseDialContext = func(context.Context, string, string) (net.Conn, error) {
				calls++
				return nil, errors.New("unexpected dial")
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "http://10.255.25.3:18080/", nil)
			c.Request.Header.Set(header, "8")
			c.Request.RemoteAddr = "10.42.0.130:43000"
			h.Handle(c)
			if rec.Code != 403 || calls != 0 {
				t.Fatalf("status=%d calls=%d", rec.Code, calls)
			}
		})
	}
}

func TestEgressCapabilityIdentityAndHeaders(t *testing.T) {
	s, token, _ := newEgressCapabilityService(t)
	seen := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen <- r.Header.Clone(); fmt.Fprint(w, "ok") }))
	defer upstream.Close()
	h := NewEgressProxyHandler(nil, WithEgressRuntimeCapabilities(s), WithEgressPrivateExceptions(&stubPrivateExceptionSource{rules: []egresspolicy.PrivateExceptionRule{{ScopeType: egresspolicy.ScopeUser, ScopeID: 1, Prefix: netip.MustParsePrefix("10.255.25.3/32"), Port: 18080}}}, nil))
	h.baseDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		id, _ := ctx.Value(egressIdentityContextKey{}).(egressRequestIdentity)
		if id.InstanceID == nil || *id.InstanceID != 8 || id.UserID == nil || *id.UserID != 1 {
			return nil, errors.New("wrong verified identity")
		}
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(upstream.URL, "http://"))
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "http://10.255.25.3:18080/", nil)
	for k, v := range map[string]string{"Proxy-Authorization": "Bearer " + token, "X-ClawManager-Instance-Id": "999", "X-ClawManager-Egress-Instance-Id": "999", "X-ClawManager-User-Id": "999", "X-ClawManager-Runtime-Generation": "999", "X-ClawManager-Pod-Uid": "forged", "Proxy-Connection": "keep-alive", "Connection": "X-Proxy-Secret", "X-Proxy-Secret": "sentinel", "X-Forwarded-For": "127.0.0.1"} {
		c.Request.Header.Set(k, v)
	}
	h.Handle(c)
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	headers := <-seen
	for _, k := range []string{"Proxy-Authorization", "X-ClawManager-Instance-Id", "X-ClawManager-Egress-Instance-Id", "X-ClawManager-User-Id", "X-ClawManager-Runtime-Generation", "X-ClawManager-Pod-Uid", "X-Proxy-Secret", "X-Forwarded-For"} {
		if headers.Get(k) != "" {
			t.Errorf("credential or forged identity forwarded: %s", k)
		}
	}
}

func TestEgressConnectRevocationAndBufferedHandshake(t *testing.T) {
	for _, failure := range []string{"revoked", "unavailable", "assignment"} {
		t.Run(failure, func(t *testing.T) {
			var revoked atomic.Bool
			verifier := egressVerifierFunc(func(ctx context.Context, token, aud string) (runtimeauth.Identity, error) {
				if token != "v1.test" || aud != runtimeauth.AudienceEgress {
					return runtimeauth.Identity{}, errors.New("bad purpose")
				}
				if revoked.Load() {
					if failure == "unavailable" {
						<-ctx.Done()
						return runtimeauth.Identity{}, ctx.Err()
					}
					if failure == "assignment" {
						changed := egressCapabilityIdentity
						changed.Generation++
						return changed, nil
					}
					return runtimeauth.Identity{}, errors.New("revoked")
				}
				return egressCapabilityIdentity, nil
			})
			h := NewEgressProxyHandler(nil, WithEgressRuntimeCapabilities(verifier))
			upstream, peer := net.Pipe()
			defer peer.Close()
			h.baseDialContext = func(context.Context, string, string) (net.Conn, error) { return upstream, nil }
			// A real HTTP server exercises bytes already buffered by net/http at Hijack.
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, _ := gin.CreateTestContext(w)
				c.Request = r
				h.Handle(c)
			}))
			defer server.Close()
			conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(11 * time.Second))
			fmt.Fprint(conn, "CONNECT 8.8.8.8:443 HTTP/1.1\r\nHost: 8.8.8.8:443\r\nProxy-Authorization: Bearer v1.test\r\n\r\nclient-handshake")
			reader := bufio.NewReader(conn)
			resp, err := http.ReadResponse(reader, nil)
			if err != nil || resp.StatusCode != 200 {
				t.Fatalf("CONNECT response=%v err=%v", resp, err)
			}
			peer.SetReadDeadline(time.Now().Add(time.Second))
			payload := make([]byte, len("client-handshake"))
			if _, err = io.ReadFull(peer, payload); err != nil || string(payload) != "client-handshake" {
				t.Fatalf("lost buffered handshake: %q %v", payload, err)
			}
			// Request handler has returned, so its request context is canceled; tunnel must remain alive.
			go func() { peer.Write([]byte("alive")) }()
			payload = make([]byte, 5)
			if _, err = io.ReadFull(reader, payload); err != nil || string(payload) != "alive" {
				t.Fatalf("tunnel tied to request: %q %v", payload, err)
			}
			start := time.Now()
			revoked.Store(true)
			conn.SetReadDeadline(start.Add(10 * time.Second))
			_, err = reader.ReadByte()
			if err == nil {
				t.Fatal("revoked tunnel remained open")
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				t.Fatal("revoked/unavailable tunnel exceeded 10-second bound")
			}
		})
	}
}

func TestEgressHTTPStreamCanceledOnRevocation(t *testing.T) {
	var revoked atomic.Bool
	verifier := egressVerifierFunc(func(ctx context.Context, token, aud string) (runtimeauth.Identity, error) {
		if revoked.Load() {
			return runtimeauth.Identity{}, errors.New("revoked")
		}
		return egressCapabilityIdentity, nil
	})
	started := make(chan struct{})
	stopped := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		fmt.Fprint(w, "stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(stopped)
	}))
	defer upstream.Close()
	h := NewEgressProxyHandler(nil, WithEgressRuntimeCapabilities(verifier))
	h.baseDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(upstream.URL, "http://"))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := gin.CreateTestContext(w)
		r.URL.Scheme = "http"
		r.URL.Host = "8.8.8.8:80"
		c.Request = r
		h.Handle(c)
	}))
	defer server.Close()
	request, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	request.Header.Set("Proxy-Authorization", "Bearer v1.test")
	done := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(request)
		if err == nil {
			_, err = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upstream never started")
	}
	revoked.Store(true)
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("upstream stream survived revocation")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("HTTP handler did not finish")
	}
}

func TestEgressPrivateConnectionIsNotReusedAcrossIdentities(t *testing.T) {
	verifier := egressVerifierFunc(func(ctx context.Context, token, audience string) (runtimeauth.Identity, error) {
		id := egressCapabilityIdentity
		if token == "v1.other" {
			id.InstanceID++
			id.UserID++
		}
		return id, nil
	})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	defer upstream.Close()
	h := NewEgressProxyHandler(nil, WithEgressRuntimeCapabilities(verifier), WithEgressPrivateExceptions(&stubPrivateExceptionSource{rules: []egresspolicy.PrivateExceptionRule{{ScopeType: egresspolicy.ScopeInstance, ScopeID: 8, Prefix: netip.MustParsePrefix("10.255.25.3/32"), Port: 18080}}}, nil))
	dials := 0
	h.baseDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dials++
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(upstream.URL, "http://"))
	}
	for i, token := range []string{"v1.allowed", "v1.other"} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "http://10.255.25.3:18080/", nil)
		c.Request.Header.Set("Proxy-Authorization", "Bearer "+token)
		h.Handle(c)
		want := 200
		if i == 1 {
			want = 403
		}
		if rec.Code != want {
			t.Fatalf("request %d reused private authorization: status=%d want=%d", i, rec.Code, want)
		}
	}
	if dials != 1 {
		t.Fatalf("unauthorized request reached dialing: %d", dials)
	}
}

type blockedEgressWriter struct {
	*httptest.ResponseRecorder
	writing         chan struct{}
	release         chan struct{}
	started, closed atomic.Bool
}

func (w *blockedEgressWriter) Write(p []byte) (int, error) {
	if w.started.CompareAndSwap(false, true) {
		close(w.writing)
	}
	<-w.release
	return 0, errors.New("write deadline exceeded")
}
func (w *blockedEgressWriter) SetWriteDeadline(time.Time) error {
	if w.closed.CompareAndSwap(false, true) {
		close(w.release)
	}
	return nil
}

func TestEgressRevocationUnblocksSlowHTTPClient(t *testing.T) {
	var revoked atomic.Bool
	verifier := egressVerifierFunc(func(context.Context, string, string) (runtimeauth.Identity, error) {
		if revoked.Load() {
			return runtimeauth.Identity{}, errors.New("revoked")
		}
		return egressCapabilityIdentity, nil
	})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "stream") }))
	defer upstream.Close()
	h := NewEgressProxyHandler(nil, WithEgressRuntimeCapabilities(verifier))
	h.baseDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(upstream.URL, "http://"))
	}
	writer := &blockedEgressWriter{ResponseRecorder: httptest.NewRecorder(), writing: make(chan struct{}), release: make(chan struct{})}
	defer writer.SetWriteDeadline(time.Now())
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodGet, "http://8.8.8.8/", nil)
	c.Request.Header.Set("Proxy-Authorization", "Bearer v1.test")
	done := make(chan struct{})
	go func() { defer close(done); h.Handle(c) }()
	select {
	case <-writer.writing:
	case <-time.After(time.Second):
		t.Fatal("stream did not reach client")
	}
	revoked.Store(true)
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("revocation leaked handler blocked writing to slow client")
	}
}

// Closing this body after its read unblocks proves the transport released the
// incoming upload. RoundTrip's error path also joins its writeLoop before return.
type observedEgressUploadBody struct {
	io.ReadCloser
	finished chan struct{}
	closed   atomic.Bool
}

func (b *observedEgressUploadBody) Close() error {
	err := b.ReadCloser.Close()
	if b.closed.CompareAndSwap(false, true) {
		close(b.finished)
	}
	return err
}

func TestEgressRevocationUnblocksSlowHTTPUpload(t *testing.T) {
	var revoked atomic.Bool
	verifier := egressVerifierFunc(func(context.Context, string, string) (runtimeauth.Identity, error) {
		if revoked.Load() {
			return runtimeauth.Identity{}, errors.New("revoked")
		}
		return egressCapabilityIdentity, nil
	})
	const partialBytes = 64 << 10
	arrived := make(chan error, 1)
	upstreamDone := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(upstreamDone)
		_, err := io.CopyN(io.Discard, r.Body, partialBytes)
		arrived <- err
		if err == nil {
			_, _ = io.Copy(io.Discard, r.Body)
		}
	}))
	defer upstream.Close()
	h := NewEgressProxyHandler(nil, WithEgressRuntimeCapabilities(verifier))
	h.baseDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(upstream.URL, "http://"))
	}
	handlerDone := make(chan struct{})
	bodyClosed := make(chan struct{})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		r.Body = &observedEgressUploadBody{ReadCloser: r.Body, finished: bodyClosed}
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		h.Handle(c)
	}))
	defer proxy.Close()
	client, err := net.Dial("tcp", strings.TrimPrefix(proxy.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	// Keep the TCP client open without sending the rest of its declared body.
	_, err = fmt.Fprintf(client, "POST http://8.8.8.8/upload HTTP/1.1\r\nHost: 8.8.8.8\r\nProxy-Authorization: Bearer v1.test\r\nContent-Length: 1048576\r\n\r\n%s", strings.Repeat("x", partialBytes))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-arrived:
		if err != nil {
			t.Fatalf("partial upload did not reach upstream: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("upstream did not receive partial upload")
	}
	revoked.Store(true)
	deadline := time.Now().Add(10 * time.Second)
	for _, step := range []struct {
		name string
		done <-chan struct{}
	}{{"handler and transport writeLoop", handlerDone}, {"upstream upload", upstreamDone}, {"transport request body", bodyClosed}} {
		select {
		case <-step.done:
		case <-time.After(time.Until(deadline)):
			t.Fatalf("revocation left %s blocked; client has sent no more bytes or closed", step.name)
		}
	}
}
