package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

type RuntimeBrowserBinding interface {
	TargetURL() *url.URL
	Authenticate(*http.Request)
	Check(context.Context) error
}

func NewRuntimeBrowserOperation(binding RuntimeBrowserBinding) (RuntimeServiceOperation, error) {
	if binding == nil {
		return RuntimeServiceOperation{}, errors.New("managed browser unavailable")
	}
	target := binding.TargetURL()
	if target == nil || target.Scheme != "http" || target.Host == "" || target.User != nil || target.Opaque != "" || target.Path != "" || target.RawQuery != "" || target.Fragment != "" {
		return RuntimeServiceOperation{}, errors.New("managed browser target unavailable")
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, DisableKeepAlives: true, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 32 << 10}
	return RuntimeServiceOperation{Check: binding.Check, Close: func() error { transport.CloseIdleConnections(); return nil }, Serve: func(w http.ResponseWriter, r *http.Request) {
		route := strings.TrimPrefix(r.URL.Path, "/api/v1/runtime/services")
		if !runtimeBrowserRoute(r.Method, route) || r.URL.RawPath != "" || path.Clean(route) != route || strings.ContainsAny(route, "\\\x00") || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			rejectRuntimeService(w, r, 400)
			return
		}
		upgrade := strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
		if r.Header.Get("Upgrade") != "" && (!upgrade || !strings.HasPrefix(route, "/devtools/")) {
			rejectRuntimeService(w, r, 400)
			return
		}
		request := r.Clone(r.Context())
		request.RequestURI = ""
		request.URL.Scheme, request.URL.Host, request.Host = target.Scheme, target.Host, target.Host
		request.URL.Path, request.URL.RawPath = route, ""
		stripRuntimeBrowserHeaders(request.Header, upgrade)
		request.Header.Set("Accept-Encoding", "identity")
		binding.Authenticate(request)
		response, err := transport.RoundTrip(request)
		if err != nil {
			http.Error(w, "managed browser unavailable", 502)
			return
		}
		defer response.Body.Close()
		if encoding := response.Header.Get("Content-Encoding"); encoding != "" && !strings.EqualFold(encoding, "identity") {
			http.Error(w, "managed browser unavailable", 502)
			return
		}
		if upgrade {
			serveRuntimeBrowserUpgrade(w, r, response)
			return
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			http.Error(w, "managed browser unavailable", 502)
			return
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
		if err != nil || len(body) > 4<<20 {
			http.Error(w, "managed browser unavailable", 502)
			return
		}
		if strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "application/json") {
			body, err = rewriteRuntimeBrowserDiscovery(body, target.Host)
			if err != nil {
				http.Error(w, "managed browser unavailable", 502)
				return
			}
		}
		stripRuntimeBrowserHeaders(response.Header, false)
		response.Header.Del("Location")
		response.Header.Del("Content-Encoding")
		response.Header.Del("ETag")
		response.Header.Set("Content-Length", strconv.Itoa(len(body)))
		copyHeaders(w.Header(), response.Header)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(response.StatusCode)
		_, _ = w.Write(body)
	}}, nil
}

func runtimeBrowserRoute(method, p string) bool {
	if p == "/json/new" {
		return method == http.MethodGet || method == http.MethodPut
	}
	if method != http.MethodGet {
		return false
	}
	if p == "/json" || p == "/json/version" || p == "/json/list" || p == "/json/protocol" {
		return true
	}
	return strings.HasPrefix(p, "/json/activate/") || strings.HasPrefix(p, "/json/close/") || strings.HasPrefix(p, "/devtools/")
}

func stripRuntimeBrowserHeaders(headers http.Header, upgrade bool) {
	for _, line := range headers.Values("Connection") {
		for _, name := range strings.Split(line, ",") {
			headers.Del(strings.TrimSpace(name))
		}
	}
	for name := range headers {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "x-clawmanager-") || strings.HasPrefix(lower, "x-forwarded-") {
			headers.Del(name)
		}
	}
	for _, name := range []string{"Authorization", "Proxy-Authorization", "Proxy-Authenticate", "WWW-Authenticate", "Cookie", "Set-Cookie", "Connection", "Proxy-Connection", "Keep-Alive", "Forwarded", "X-Real-IP", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		headers.Del(name)
	}
	if upgrade {
		headers.Set("Connection", "Upgrade")
		headers.Set("Upgrade", "websocket")
	}
}

func rewriteRuntimeBrowserDiscovery(body []byte, target string) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("invalid discovery")
	}
	var rewrite func(any) error
	rewrite = func(value any) error {
		switch item := value.(type) {
		case map[string]any:
			for key, child := range item {
				if key == "webSocketDebuggerUrl" {
					raw, ok := child.(string)
					if !ok {
						return errors.New("invalid discovery")
					}
					upstream, err := url.Parse(raw)
					if err != nil || (upstream.Scheme != "ws" && upstream.Scheme != "wss") || upstream.Opaque != "" || upstream.Host != target || upstream.RawPath != "" || !strings.HasPrefix(upstream.Path, "/devtools/") || path.Clean(upstream.Path) != upstream.Path || strings.ContainsAny(upstream.Path, "\\\x00") {
						return errors.New("unexpected discovery target")
					}
					local := url.URL{Scheme: "ws", Host: "clawmanager-local-services.invalid:31282", Path: upstream.Path}
					item[key] = local.String()
				} else if err := rewrite(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range item {
				if err := rewrite(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := rewrite(value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func serveRuntimeBrowserUpgrade(w http.ResponseWriter, r *http.Request, response *http.Response) {
	backend, ok := response.Body.(io.ReadWriteCloser)
	if response.StatusCode != 101 || !strings.EqualFold(response.Header.Get("Upgrade"), "websocket") || !ok {
		http.Error(w, "managed browser unavailable", 502)
		return
	}
	client, buffered, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	doneIO := make(chan struct{})
	stop := context.AfterFunc(r.Context(), func() { defer close(doneIO); client.Close(); backend.Close() })
	defer func() {
		if !stop() {
			<-doneIO
		}
	}()
	stripRuntimeBrowserHeaders(response.Header, true)
	response.Header.Set("Cache-Control", "no-store")
	response.Body = nil
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	if response.Write(buffered) != nil || buffered.Flush() != nil {
		return
	}
	if client.SetDeadline(time.Time{}) != nil {
		return
	}
	done := make(chan struct{})
	go func() { defer close(done); _, _ = io.Copy(backend, buffered.Reader); client.Close(); backend.Close() }()
	_, _ = io.Copy(client, backend)
	client.Close()
	backend.Close()
	<-done
}
