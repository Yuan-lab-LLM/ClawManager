package handlers

import (
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
	"clawreef/internal/runtimeauth"
	"github.com/gin-gonic/gin"
)

type observedEgressTCPWriter struct {
	http.ResponseWriter
	writing   atomic.Bool
	completed atomic.Int64
}

func (w *observedEgressTCPWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *observedEgressTCPWriter) Write(p []byte) (int, error) {
	w.writing.Store(true)
	defer w.writing.Store(false)
	n, err := w.ResponseWriter.Write(p)
	w.completed.Add(1)
	return n, err
}

func TestEgressDestinationRevocationUnblocksHTTPClient(t *testing.T) {
	for _, reason := range []string{"denied", "unavailable", "private exception removed"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			var revoked atomic.Bool
			upstreamDone := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(upstreamDone)
				chunk := make([]byte, 64<<10)
				for {
					if _, err := w.Write(chunk); err != nil {
						return
					}
				}
			}))
			defer upstream.Close()
			verifier := egressVerifierFunc(func(context.Context, string, string) (runtimeauth.Identity, error) {
				return egressCapabilityIdentity, nil
			})
			h := NewEgressProxyHandler(nil, WithEgressRuntimeCapabilities(verifier), WithEgressHardDeny(func(context.Context, netip.Addr) (bool, error) {
				if revoked.Load() {
					if reason == "denied" {
						return true, nil
					}
					if reason == "unavailable" {
						return false, errors.New("inventory stale")
					}
				}
				return false, nil
			}))
			target := "8.8.8.8"
			rules := &liveEgressRules{rules: []egresspolicy.PrivateExceptionRule{{ScopeType: egresspolicy.ScopeInstance, ScopeID: egressCapabilityIdentity.InstanceID, Prefix: netip.MustParsePrefix("10.80.0.8/32"), Port: 80}}}
			if reason == "private exception removed" {
				target = "10.80.0.8"
				h.privateExceptions = rules
			}
			h.baseDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(upstream.URL, "http://"))
			}
			handlerDone := make(chan struct{})
			writerReady := make(chan *observedEgressTCPWriter, 1)
			proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(handlerDone)
				writer := &observedEgressTCPWriter{ResponseWriter: w}
				writerReady <- writer
				c, _ := gin.CreateTestContext(writer)
				c.Request = r
				h.Handle(c)
			}))
			proxy.Config.ConnState = func(conn net.Conn, state http.ConnState) {
				if state == http.StateNew {
					_ = conn.(*net.TCPConn).SetWriteBuffer(4096)
				}
			}
			proxy.Start()
			defer proxy.Close()
			client, err := net.Dial("tcp", strings.TrimPrefix(proxy.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			_ = client.(*net.TCPConn).SetReadBuffer(1024)
			if _, err := fmt.Fprintf(client, "GET http://%s/stream HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: Bearer v1.test\r\n\r\n", target, target); err != nil {
				t.Fatal(err)
			}
			var writer *observedEgressTCPWriter
			select {
			case writer = <-writerReady:
			case <-time.After(2 * time.Second):
				t.Fatal("request did not arrive")
			}
			// Observe a real socket write that makes no progress while the client reads nothing.
			blocked := false
			until := time.Now().Add(3 * time.Second)
			for time.Now().Before(until) {
				count := writer.completed.Load()
				time.Sleep(100 * time.Millisecond)
				if writer.writing.Load() && writer.completed.Load() == count {
					blocked = true
					break
				}
			}
			if !blocked {
				t.Fatal("fixture did not block a downstream TCP write")
			}
			revoked.Store(true)
			rules.mu.Lock()
			rules.rules = nil
			rules.mu.Unlock()
			deadline := time.Now().Add(9 * time.Second)
			for _, step := range []struct {
				name string
				done <-chan struct{}
			}{{"HTTP handler", handlerDone}, {"upstream stream", upstreamDone}} {
				select {
				case <-step.done:
				case <-time.After(time.Until(deadline)):
					t.Fatalf("destination revocation left %s blocked while client remains nonreading", step.name)
				}
			}
		})
	}
}

func TestEgressDestinationRevocationUnblocksHTTPUpload(t *testing.T) {
	t.Parallel()
	var revoked atomic.Bool
	arrived := make(chan struct{})
	upstreamDone := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(upstreamDone)
		if _, err := io.CopyN(io.Discard, r.Body, 64<<10); err != nil {
			return
		}
		close(arrived)
		_, _ = io.Copy(io.Discard, r.Body)
	}))
	defer upstream.Close()
	h := NewEgressProxyHandler(nil, WithEgressRuntimeCapabilities(egressVerifierFunc(func(context.Context, string, string) (runtimeauth.Identity, error) {
		return egressCapabilityIdentity, nil
	})), WithEgressHardDeny(func(context.Context, netip.Addr) (bool, error) { return revoked.Load(), nil }))
	h.baseDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(upstream.URL, "http://"))
	}
	handlerDone, bodyClosed := make(chan struct{}), make(chan struct{})
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
	if _, err := fmt.Fprintf(client, "POST http://8.8.8.8/upload HTTP/1.1\r\nHost: 8.8.8.8\r\nProxy-Authorization: Bearer v1.test\r\nContent-Length: 1048576\r\n\r\n%s", strings.Repeat("x", 64<<10)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-arrived:
	case <-time.After(2 * time.Second):
		t.Fatal("partial upload did not arrive")
	}
	revoked.Store(true)
	deadline := time.Now().Add(9 * time.Second)
	for _, step := range []struct {
		name string
		done <-chan struct{}
	}{{"handler and transport writeLoop", handlerDone}, {"upstream upload", upstreamDone}, {"incoming body", bodyClosed}} {
		select {
		case <-step.done:
		case <-time.After(time.Until(deadline)):
			t.Fatalf("destination revocation left %s blocked while client holds upload open", step.name)
		}
	}
}
