package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/browserorigin"
	"clawreef/internal/workbench"
)

const browserPreviewBootstrapPath = "/__clawmanager_preview/"
const BrowserOriginViewerPath = "/__clawmanager_browser/view"

func (s *InstanceWorkbenchService) SetBrowserOrigins(origins *browserorigin.Registry) {
	s.browserOrigins = origins
}
func browserPreviewTargetKey(t workbench.PreviewTarget) string {
	return (&url.URL{Scheme: t.Scheme, Host: net.JoinHostPort(t.Host, strconv.Itoa(t.Port)), Path: "/"}).String()
}
func browserPreviewCookie(port int) string { return "cm_preview_" + strconv.Itoa(port) }
func (s *InstanceWorkbenchService) browserPreviewURL(binding browserorigin.Binding, lease workbench.PreviewLease) string {
	return s.browserOrigins.URL(binding) + browserPreviewBootstrapPath + lease.ID
}
func (s *InstanceWorkbenchService) browserPreviewViewerURL(binding browserorigin.Binding, lease workbench.PreviewLease) string {
	return s.browserOrigins.ManagerOrigin() + BrowserOriginViewerPath + "#" + base64.RawURLEncoding.EncodeToString([]byte(s.browserPreviewURL(binding, lease)))
}

func (s *InstanceWorkbenchService) previewRequestTarget(ctx context.Context, r *http.Request) (workbench.PreviewLease, workbenchTarget, error) {
	if s.browserOrigins == nil || !s.browserOrigins.Matches(r.Host) {
		return s.previewTarget(ctx, r.Host)
	}
	binding, err := s.browserOrigins.Lookup(ctx, r.Host)
	if err != nil || binding.Kind != browserorigin.Preview {
		return workbench.PreviewLease{}, workbenchTarget{}, workbench.ErrPreviewNotFound
	}
	id := ""
	if strings.HasPrefix(r.URL.Path, browserPreviewBootstrapPath) {
		id = strings.TrimPrefix(r.URL.Path, browserPreviewBootstrapPath)
	} else if cookie, e := r.Cookie(browserPreviewCookie(binding.Port)); e == nil {
		id = cookie.Value
	}
	lease, err := s.previews.LookupCapability(ctx, id)
	if err != nil || lease.Scope.InstanceID != binding.InstanceID || browserPreviewTargetKey(lease.Target) != binding.Target {
		return lease, workbenchTarget{}, workbench.ErrPreviewNotFound
	}
	return s.resolvePreviewLease(ctx, lease)
}

// Browser ports only select routes. A secret capability is required both for
// the initial document and for subsequent root-relative assets/API/WS requests.
func (s *InstanceWorkbenchService) prepareBrowserPreview(w http.ResponseWriter, r *http.Request, lease workbench.PreviewLease) bool {
	if s.browserOrigins == nil || !s.browserOrigins.Matches(r.Host) {
		return false
	}
	binding, err := s.browserOrigins.Lookup(r.Context(), r.Host)
	if err != nil {
		http.Error(w, "Preview unavailable", 404)
		return true
	}
	bootstrap := strings.HasPrefix(r.URL.Path, browserPreviewBootstrapPath)
	if r.Header.Get("Sec-Fetch-Dest") == "document" {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, s.browserPreviewViewerURL(binding, lease), http.StatusSeeOther)
		return true
	}
	ownOrigin := strings.TrimSuffix(s.browserOrigins.URL(binding), "/")
	if !browserorigin.AllowNativeRequest(r, ownOrigin, s.browserOrigins.ManagerOrigin(), bootstrap) {
		http.Error(w, "Open the preview from its ClawManager workbench", http.StatusForbidden)
		return true
	}
	if bootstrap {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", 405)
			return true
		}
		maxAge := max(1, int(time.Until(lease.ExpiresAt).Seconds()))
		http.SetCookie(w, &http.Cookie{Name: browserPreviewCookie(binding.Port), Value: lease.ID, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteNoneMode, Partitioned: true, MaxAge: maxAge, Expires: lease.ExpiresAt})
		w.Header().Set("Cache-Control", "no-store")
		location := (&url.URL{Path: lease.Target.Path, RawPath: lease.Target.RawPath, RawQuery: lease.Target.RawQuery, Fragment: lease.Target.Fragment}).String()
		if browserorigin.ServeBootstrapDocument(w, r, location) {
			return true
		}
		http.Redirect(w, r, location, http.StatusSeeOther)
		return true
	}
	return false
}

// The viewer is trusted CM HTML, never application HTML. An external preview
// creates a fresh top-level document and a credentialless child on another port.
func (s *InstanceWorkbenchService) ServeBrowserViewer(w http.ResponseWriter, r *http.Request) {
	if s.browserOrigins == nil {
		http.NotFound(w, r)
		return
	}
	manager, _ := url.Parse(s.browserOrigins.ManagerOrigin())
	if r.Host != manager.Host || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		http.NotFound(w, r)
		return
	}
	first, last := s.browserOrigins.Range()
	cfg, _ := json.Marshal(map[string]any{"host": manager.Hostname(), "first": first, "last": last})
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		http.Error(w, "Viewer unavailable", 503)
		return
	}
	nonce := base64.RawStdEncoding.EncodeToString(nonceBytes)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", fmt.Sprintf("default-src 'none'; script-src 'nonce-%s'; style-src 'unsafe-inline'; frame-src https://%s; base-uri 'none'; frame-ancestors 'none'", nonce, net.JoinHostPort(manager.Hostname(), "*")))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>ClawManager preview</title><style>html,body,iframe{margin:0;width:100%;height:100%;border:0}body{font-family:system-ui}p{padding:24px}</style></head><body><p id="status">Loading preview…</p><script nonce="`+nonce+`">(function(){"use strict";window.addEventListener("hashchange",function(){location.reload()});const c=`+string(cfg)+`;try{if(!("credentialless" in HTMLIFrameElement.prototype))throw Error("This preview requires Chrome.");let v=location.hash.slice(1).replace(/-/g,"+").replace(/_/g,"/");v+="=".repeat((4-v.length%4)%4);const u=new URL(atob(v)),p=Number(u.port);if(u.protocol!=="https:"||u.hostname!==c.host||p<c.first||p>c.last||u.username||u.password||u.search||!/^\/__clawmanager_preview\/[a-f0-9]{48}$/.test(u.pathname))throw Error("Invalid or expired preview. Reopen it from ClawManager.");const f=document.createElement("iframe");f.title="Service preview";f.credentialless=true;f.referrerPolicy="no-referrer";f.setAttribute("sandbox","allow-scripts allow-forms allow-same-origin allow-modals allow-downloads");f.src=u.href;document.body.replaceChildren(f);}catch(e){document.getElementById("status").textContent=e.message;}})();</script></body></html>`)
}
