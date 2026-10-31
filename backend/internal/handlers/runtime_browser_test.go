package handlers

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
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

	"clawreef/internal/runtimeauth"
)

func TestManagedBrowserCompressedDiscoveryFailsClosed(t *testing.T) {
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Encoding", "gzip")
		compressed := gzip.NewWriter(w)
		_, _ = io.WriteString(compressed, `{"webSocketDebuggerUrl":"ws://worker-secret-sentinel@unexpected/devtools/browser/own"}`)
		_ = compressed.Close()
	}))
	defer worker.Close()
	target, _ := url.Parse(worker.URL)
	operation, err := NewRuntimeBrowserOperation(runtimeBrowserBindingFake{target})
	if err != nil {
		t.Fatal(err)
	}
	defer operation.Close()
	r := httptest.NewRequest("GET", "/api/v1/runtime/services/json/version", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	operation.Serve(w, r)
	if w.Code != 502 {
		t.Fatalf("compressed discovery escaped validation: %d", w.Code)
	}
}

type runtimeBrowserBindingFake struct{ target *url.URL }

func (b runtimeBrowserBindingFake) TargetURL() *url.URL         { copy := *b.target; return &copy }
func (b runtimeBrowserBindingFake) Check(context.Context) error { return nil }
func (b runtimeBrowserBindingFake) Authenticate(r *http.Request) {
	r.SetBasicAuth("openclaw", "worker-secret-sentinel")
}

type revocableRuntimeBrowser struct {
	runtimeBrowserBindingFake
	revoked *atomic.Bool
}

func (b revocableRuntimeBrowser) Check(context.Context) error {
	if b.revoked.Load() {
		return errors.New("disabled")
	}
	return nil
}

func TestManagedBrowserBufferedUpgradeAndServiceRevocation(t *testing.T) {
	workerDone := make(chan struct{})
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(workerDone)
		_, password, ok := r.BasicAuth()
		if !ok || password != "worker-secret-sentinel" {
			t.Error("CDP worker authentication lost")
		}
		c, buffered, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\nworker-hello")
		payload := make([]byte, len("client-hello"))
		if _, err := io.ReadFull(buffered.Reader, payload); err != nil {
			return
		}
		_, _ = c.Write(payload)
		_, _ = io.Copy(io.Discard, buffered.Reader)
	}))
	defer worker.Close()
	target, _ := url.Parse(worker.URL)
	var revoked atomic.Bool
	binding := revocableRuntimeBrowser{runtimeBrowserBindingFake{target}, &revoked}
	verifier := managedServiceVerifier(func(context.Context, string, string) (runtimeauth.Identity, error) {
		return runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod"}, nil
	})
	h, err := NewRuntimeServicesHandler(verifier, func(context.Context, runtimeauth.Identity, *http.Request) (RuntimeServiceOperation, error) {
		return NewRuntimeBrowserOperation(binding)
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer func() { h.Close(); server.Close() }()
	c, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(9 * time.Second))
	_, _ = io.WriteString(c, "GET /api/v1/runtime/services/devtools/browser/own HTTP/1.1\r\nHost: local\r\nAuthorization: Bearer v1.services\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\nclient-hello")
	reader := bufio.NewReader(c)
	response, err := http.ReadResponse(reader, nil)
	if err != nil || response.StatusCode != 101 {
		t.Fatal("CDP upgrade failed")
	}
	payload := make([]byte, len("worker-helloclient-hello"))
	if _, err := io.ReadFull(reader, payload); err != nil || string(payload) != "worker-helloclient-hello" {
		t.Fatal("buffered CDP traffic lost")
	}
	revoked.Store(true)
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("revoked CDP service retained stream")
	} else if e, ok := err.(net.Error); ok && e.Timeout() {
		t.Fatal("service revocation did not interrupt socket")
	}
	select {
	case <-workerDone:
	case <-time.After(time.Second):
		t.Fatal("revoked CDP upstream not joined")
	}
}

func TestManagedBrowserDiscoveryHidesWorkerCredential(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "openclaw" || password != "worker-secret-sentinel" || r.Header.Get("X-ClawManager-Control-Token") != "" || r.Header.Get("Cookie") != "" {
			t.Error("worker credential/header boundary")
		}
		if r.URL.Path != "/json/version" {
			t.Error("arbitrary browser target")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"nested":[{"webSocketDebuggerUrl":"ws://openclaw:worker-secret-sentinel@%s/devtools/browser/own?token=worker-secret-sentinel"}]}`, strings.TrimPrefix(server.URL, "http://"))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	operation, err := NewRuntimeBrowserOperation(runtimeBrowserBindingFake{target})
	if err != nil {
		t.Fatal(err)
	}
	defer operation.Close()
	r := httptest.NewRequest("GET", "/api/v1/runtime/services/json/version", nil)
	r.Header.Set("Authorization", "Bearer v1.services")
	r.Header.Set("Cookie", "browser-key-sentinel")
	r.Header.Set("X-ClawManager-Control-Token", "control-key-sentinel")
	w := httptest.NewRecorder()
	operation.Serve(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "sentinel") || !strings.Contains(w.Body.String(), "ws://clawmanager-local-services.invalid:31282/devtools/browser/own") {
		t.Fatalf("discovery leaked key or lost native route: status%d", w.Code)
	}
}

func TestManagedBrowserRejectsRedirectAndUnexpectedDiscoveryHost(t *testing.T) {
	for _, mode := range []string{"redirect", "foreign-discovery"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if mode == "redirect" {
				w.Header().Set("Location", "http://openclaw:worker-secret-sentinel@evil.invalid")
				w.WriteHeader(302)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"webSocketDebuggerUrl":"ws://openclaw:worker-secret-sentinel@evil.invalid/devtools/browser/own"}`)
		}))
		target, _ := url.Parse(server.URL)
		operation, err := NewRuntimeBrowserOperation(runtimeBrowserBindingFake{target})
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		operation.Serve(w, httptest.NewRequest("GET", "/api/v1/runtime/services/json/version", nil))
		_ = operation.Close()
		server.Close()
		if w.Code < 400 || strings.Contains(w.Body.String(), "sentinel") || w.Header().Get("Location") != "" {
			t.Fatal("worker redirect/discovery escaped fixed target")
		}
	}
}
