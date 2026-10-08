package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testToken = strings.Repeat("test", 16)

func TestRewriteCDPResponseUsesExternalHost(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://upstream/json/version", nil)
	if err != nil {
		t.Fatal(err)
	}
	req = req.WithContext(context.WithValue(req.Context(), externalHostKey, "clawbrowser-11:9222"))
	req = req.WithContext(context.WithValue(req.Context(), authTokenKey, testToken))
	resp := &http.Response{
		Request: req,
		Header:  http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"webSocketDebuggerUrl":"ws://127.0.0.1:9223/devtools/browser/test"}`,
		)),
	}

	if err := rewriteCDPResponse(resp); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(body); !strings.Contains(got, "ws://openclaw:"+testToken+"@clawbrowser-11:9222/devtools/browser/test") {
		t.Fatalf("rewritten body = %s", got)
	}
}

func TestWorkerRelayRequiresInstanceCredentialForCDPAndGUI(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("relay forwarded credentials upstream")
		}
		_, _ = w.Write([]byte("upstream"))
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	for _, cdp := range []bool{true, false} {
		for _, path := range []string{"/", "/json/version", "/json/list", "/devtools/browser/id", "/app/ui.js", "/websockify"} {
			for _, password := range []string{"", "other-instance-token", testToken} {
				req := httptest.NewRequest("GET", "http://relay"+path, nil)
				if password != "" {
					req.SetBasicAuth("openclaw", password)
				}
				req.Header.Set("Cookie", "untrusted=discard")
				if path == "/websockify" {
					req.Header.Set("Upgrade", "websocket")
					req.Header.Set("Connection", "Upgrade")
				}
				response := httptest.NewRecorder()
				before := hits.Load()
				newWorkerHandler(target, testToken, cdp).ServeHTTP(response, req)
				if password != testToken {
					if response.Code != 401 || hits.Load() != before {
						t.Fatalf("unauthorized request reached upstream: cdp=%v path=%s", cdp, path)
					}
				} else if response.Code != 200 || hits.Load() != before+1 {
					t.Fatalf("valid authentication rejected: cdp=%v path=%s code=%d", cdp, path, response.Code)
				}
			}
		}
	}
}

func TestWorkerRelayReadinessChecksUpstreamAndDoesNotBypassAuthOnUpgrade(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	for _, cdp := range []bool{true, false} {
		handler := newWorkerHandler(target, testToken, cdp)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "http://relay/healthz", nil))
		if response.Code != 503 {
			t.Fatal("unready upstream was advertised as ready")
		}
		request := httptest.NewRequest("GET", "http://relay/healthz", nil)
		request.Header.Set("Upgrade", "websocket")
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 401 {
			t.Fatal("health endpoint bypassed WebSocket authentication")
		}
	}
}

func TestAuthenticatedDiscoveryReturnsUsableWebSocketCredentials(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"webSocketDebuggerUrl":"ws://localhost:9223/devtools/page/one"}]`))
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	relay := httptest.NewServer(newWorkerHandler(target, testToken, true))
	defer relay.Close()
	request, _ := http.NewRequest("GET", relay.URL+"/json/list", nil)
	request.SetBasicAuth("openclaw", testToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	host := strings.TrimPrefix(relay.URL, "http://")
	if !strings.Contains(string(body), "ws://openclaw:"+testToken+"@"+host+"/devtools/page/one") {
		t.Fatal("discovery WebSocket did not preserve instance authentication")
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("credential-bearing discovery response may be cached")
	}
}

func TestWorkerRelayForwardsAuthenticatedWebSocketUpgradeAndFrames(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("authentication leaked upstream")
		}
		connection, stream, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_, _ = stream.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_ = stream.Flush()
		value, err := stream.ReadByte()
		if err != nil {
			return
		}
		_ = stream.WriteByte(value)
		_ = stream.Flush()
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	for _, cdp := range []bool{true, false} {
		relay := httptest.NewServer(newWorkerHandler(target, testToken, cdp))
		connection, err := net.Dial("tcp", strings.TrimPrefix(relay.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = fmt.Fprintf(connection, "GET /websockify HTTP/1.1\r\nHost: relay\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nAuthorization: Basic %s\r\n\r\n", base64.StdEncoding.EncodeToString([]byte("openclaw:"+testToken)))
		reader := bufio.NewReader(connection)
		response, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 101 {
			t.Fatalf("upgrade failed: %d", response.StatusCode)
		}
		_, _ = connection.Write([]byte("x"))
		received, err := reader.ReadByte()
		if err != nil || received != 'x' {
			t.Fatalf("upgraded data was not forwarded: %v", err)
		}
		_ = connection.Close()
		relay.Close()
	}
}

func TestRewriteCDPResponseLeavesNonJSONUntouched(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://upstream/", nil)
	if err != nil {
		t.Fatal(err)
	}
	body := `ws://127.0.0.1:9223/devtools/browser/test`
	resp := &http.Response{
		Request: req,
		Header:  http.Header{"Content-Type": []string{"text/plain"}},
		Body:    io.NopCloser(strings.NewReader(body)),
	}

	if err := rewriteCDPResponse(resp); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("body changed: %q", got)
	}
}
