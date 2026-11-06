package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"clawreef/internal/browserorigin"
	"clawreef/internal/workbench"
)

const workbenchPreviewHeader = "X-ClawManager-Workbench-Preview"

type workbenchPreviewRoute struct {
	workbench.PreviewScope
	Scheme   string `json:"scheme"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Path     string `json:"path"`
	RawPath  string `json:"raw_path,omitempty"`
	RawQuery string `json:"raw_query,omitempty"`
}
type workbenchPreviewTransport interface {
	workbenchTransport() (http.RoundTripper, string)
}

func (c *runtimeAgentHTTPClient) workbenchTransport() (http.RoundTripper, string) {
	transport := c.httpClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	return transport, c.controlToken
}
func (s *InstanceWorkbenchService) IsPreviewHost(host string) bool {
	return s.previews != nil && s.previews.IsPreviewHost(host)
}
func (s *InstanceWorkbenchService) ServePreview(w http.ResponseWriter, r *http.Request) {
	// App requests never enter the CM API router, authentication middleware or CORS.
	w.Header().Set("Referrer-Policy", "no-referrer")
	lease, target, err := s.previewRequestTarget(r.Context(), r)
	if err != nil {
		http.Error(w, "Preview expired or instance environment changed. Reopen the preview from the workbench.", 404)
		return
	}
	if s.prepareBrowserPreview(w, r, lease) {
		return
	}
	if r.URL.Path == "/__clawmanager_cert_check" || r.URL.Path == "/__clawmanager_cert_trust" {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
		} else if r.URL.Path == "/__clawmanager_cert_check" {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'")
			w.Header().Set("X-Frame-Options", "DENY")
			if r.Method == http.MethodGet {
				_, _ = io.WriteString(w, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Certificate confirmed</title></head><body><h1>证书确认完成 / Certificate confirmed</h1><p>请关闭此标签页并返回 ClawManager。</p><p>Close this tab and return to ClawManager.</p></body></html>`)
			}
		}
		return
	}
	provider, ok := s.agent.(workbenchPreviewTransport)
	if !ok {
		http.Error(w, "Preview requires an updated runtime", 503)
		return
	}
	endpoint, err := url.Parse(target.endpoint)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		http.Error(w, "Preview unavailable", 503)
		return
	}
	publicURL, _ := url.Parse(s.previews.URL(lease))
	if s.browserOrigins != nil && s.browserOrigins.Matches(r.Host) {
		binding, err := s.browserOrigins.Lookup(r.Context(), r.Host)
		if err != nil {
			http.Error(w, "Preview unavailable", http.StatusNotFound)
			return
		}
		publicURL, _ = url.Parse(s.browserOrigins.URL(binding))
	}
	transport, controlToken := provider.workbenchTransport()
	ctx, cancel := context.WithDeadline(r.Context(), lease.ExpiresAt)
	defer cancel()
	// Long-lived streams must also terminate when ownership/generation changes.
	go func() {
		interval := s.previewRecheckInterval
		if interval <= 0 {
			interval = 5 * time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, _, err := s.previewRequestTarget(ctx, r); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	route := workbenchPreviewRoute{PreviewScope: lease.Scope, Scheme: lease.Target.Scheme, Host: lease.Target.Host, Port: lease.Target.Port, Path: r.URL.Path, RawPath: r.URL.RawPath, RawQuery: r.URL.RawQuery}
	encoded, _ := json.Marshal(route)
	if len(encoded) > 16<<10 {
		http.Error(w, "Request target too large", 414)
		return
	}
	proxy := httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.SetURL(endpoint)
			p.Out.URL.Path = "/v1/workbench/preview"
			p.Out.URL.RawPath = ""
			p.Out.URL.RawQuery = ""
			p.Out.Host = endpoint.Host
			for key := range p.Out.Header {
				if strings.HasPrefix(strings.ToLower(key), "x-clawmanager-") {
					p.Out.Header.Del(key)
				}
			}
			// Application auth belongs to its isolated origin; CM credentials are never injected.
			p.Out.Header.Set("X-ClawManager-Control-Token", controlToken)
			p.Out.Header.Set(workbenchPreviewHeader, base64.RawURLEncoding.EncodeToString(encoded))
			if s.browserOrigins != nil && s.browserOrigins.Matches(r.Host) {
				browserorigin.FilterPlatformCookies(p.Out.Header)
			}
			p.Out.Header.Del("Referer")
			p.Out.Header.Del("X-Forwarded-Host")
			p.Out.Header.Del("X-Forwarded-For")
			p.Out.Header.Del("X-Forwarded-Proto")
		},
		Transport: previewCancelTransport{base: transport}, FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "Service unavailable. Check its address and start it in the instance.", 502)
		},
		ModifyResponse: func(resp *http.Response) error {
			for key := range resp.Header {
				if strings.HasPrefix(strings.ToLower(key), "x-clawmanager-") {
					resp.Header.Del(key)
				}
			}
			if location := resp.Header.Get("Location"); location != "" {
				if u, err := url.Parse(location); err == nil && u.Host != "" {
					if u.Scheme == "" {
						u.Scheme = lease.Target.Scheme
					}
					if parsed, err := workbench.ParsePreviewURL(u.String()); err == nil && parsed.Port == lease.Target.Port {
						u.Scheme = publicURL.Scheme
						u.Host = r.Host
						resp.Header.Set("Location", u.String())
					}
				}
			}
			cookies := resp.Header.Values("Set-Cookie")
			resp.Header.Del("Set-Cookie")
			for _, raw := range cookies { // A localhost Domain cannot apply to a capability origin.
				if s.browserOrigins != nil && s.browserOrigins.Matches(r.Host) {
					name, _, _ := strings.Cut(raw, "=")
					if browserorigin.PlatformCookie(strings.TrimSpace(name)) {
						continue
					}
				}
				parts := strings.Split(raw, ";")
				kept := parts[:0]
				for _, part := range parts {
					if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(part)), "domain=") {
						kept = append(kept, part)
					}
				}
				resp.Header.Add("Set-Cookie", strings.Join(kept, ";"))
			}
			resp.Header.Set("Referrer-Policy", "no-referrer")
			return nil
		},
	}
	proxy.ServeHTTP(w, r.WithContext(ctx))
}

// Closing the upstream explicitly also fences upgraded WebSockets when a binding
// changes. Preserve io.ReadWriteCloser so ReverseProxy can tunnel the upgrade.
type previewCancelTransport struct{ base http.RoundTripper }

func (t previewCancelTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusSwitchingProtocols {
		if body, ok := response.Body.(io.ReadWriteCloser); ok {
			wrapped := &previewUpgradeBody{ReadWriteCloser: body, done: make(chan struct{})}
			response.Body = wrapped
			go func() {
				select {
				case <-r.Context().Done():
					_ = wrapped.Close()
				case <-wrapped.done:
				}
			}()
		}
	}
	return response, nil
}

type previewUpgradeBody struct {
	io.ReadWriteCloser
	once sync.Once
	done chan struct{}
}

func (b *previewUpgradeBody) Close() error {
	var err error
	b.once.Do(func() { close(b.done); err = b.ReadWriteCloser.Close() })
	return err
}
