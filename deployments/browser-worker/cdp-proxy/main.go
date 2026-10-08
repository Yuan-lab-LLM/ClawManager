package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type contextKey string

const externalHostKey contextKey = "external-host"
const authTokenKey contextKey = "auth-token"

// Only authenticated discovery requests receive credential-bearing WebSocket URLs.
func rewriteCDPResponse(resp *http.Response) error {
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
		return nil
	}
	host, _ := resp.Request.Context().Value(externalHostKey).(string)
	token, _ := resp.Request.Context().Value(authTokenKey).(string)
	if host == "" || token == "" {
		return fmt.Errorf("missing authenticated CDP context")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	_ = resp.Body.Close()
	if err != nil || len(body) > 1<<20 {
		return fmt.Errorf("invalid CDP discovery response size")
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return fmt.Errorf("invalid CDP discovery JSON")
	}
	var rewrite func(any)
	rewrite = func(value any) {
		switch item := value.(type) {
		case map[string]any:
			for key, child := range item {
				if key == "webSocketDebuggerUrl" {
					if raw, ok := child.(string); ok {
						if parsed, err := url.Parse(raw); err == nil && parsed.Scheme == "ws" {
							parsed.Host = host
							parsed.User = url.UserPassword("openclaw", token)
							item[key] = parsed.String()
						}
					}
				} else {
					rewrite(child)
				}
			}
		case []any:
			for _, child := range item {
				rewrite(child)
			}
		}
	}
	rewrite(value)
	body, err = json.Marshal(value)
	if err != nil {
		return err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	resp.Header.Set("Cache-Control", "no-store")
	return nil
}

func newWorkerHandler(upstream *url.URL, token string, cdp bool) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.Transport = &http.Transport{Proxy: nil}
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		ctx := context.WithValue(req.Context(), externalHostKey, req.Host)
		ctx = context.WithValue(ctx, authTokenKey, token)
		*req = *req.WithContext(ctx)
		director(req)
		req.Host = upstream.Host
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
	}
	if cdp {
		proxy.ModifyResponse = rewriteCDPResponse
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		// Never log credential-bearing URLs or request headers.
		http.Error(w, "browser upstream unavailable", http.StatusBadGateway)
	}
	proxy.FlushInterval = -1
	probeClient := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" && r.Method == http.MethodGet && r.Header.Get("Upgrade") == "" {
			path := "/"
			if cdp {
				path = "/json/version"
			}
			response, err := probeClient.Get(upstream.String() + path)
			if err != nil {
				http.Error(w, "upstream not ready", http.StatusServiceUnavailable)
				return
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				http.Error(w, "upstream not ready", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		user, password, ok := r.BasicAuth()
		if !ok || token == "" || user != "openclaw" || subtle.ConstantTimeCompare([]byte(password), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Basic realm=\"browser-worker\"")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		proxy.ServeHTTP(w, r)
	})
}

func loopbackUpstream(name, fallback string) *url.URL {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		raw = fallback
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil || parsed.RawQuery != "" || (parsed.Path != "" && parsed.Path != "/") {
		log.Fatalf("%s must be an HTTP loopback endpoint", name)
	}
	parsed.Path = ""
	return parsed
}

func main() {
	token := strings.TrimSpace(os.Getenv("BROWSER_WORKER_AUTH_TOKEN"))
	if len(token) < 32 {
		log.Fatal("BROWSER_WORKER_AUTH_TOKEN must contain at least 32 characters")
	}
	cdp := loopbackUpstream("CDP_UPSTREAM", "http://127.0.0.1:9223")
	gui := loopbackUpstream("GUI_UPSTREAM", "http://127.0.0.1:5800")
	newServer := func(addr string, handler http.Handler) *http.Server {
		return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
	}
	go func() { log.Fatal(newServer(":5801", newWorkerHandler(gui, token, false)).ListenAndServe()) }()
	log.Fatal(newServer(":9222", newWorkerHandler(cdp, token, true)).ListenAndServe())
}
