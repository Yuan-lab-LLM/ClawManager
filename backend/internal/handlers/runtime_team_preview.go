package handlers

import (
	"bytes"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/services"
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

type managedPreviewSigner interface {
	Sign(context.Context, runtimeauth.Identity, string, string) (services.ManagedTeamPreviewGrant, error)
}

// Resolve only the fixed operation here. Body I/O belongs in Serve, where a real
// ResponseController can bound a slow upload before calling the signer.
func NewRuntimeTeamPreviewOperation(provider managedPreviewSigner, id runtimeauth.Identity, r *http.Request) (RuntimeServiceOperation, error) {
	if provider == nil || r == nil || r.URL == nil || r.Method != http.MethodPost || r.URL.Path != "/api/v1/runtime/services/team/preview-sign" || r.URL.RawQuery != "" || r.ContentLength < 1 || r.ContentLength > 4096 || len(r.TransferEncoding) != 0 || r.Header.Get("Content-Encoding") != "" {
		return RuntimeServiceOperation{}, errors.New("invalid managed preview operation")
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return RuntimeServiceOperation{}, errors.New("invalid managed preview operation")
	}
	return RuntimeServiceOperation{Serve: func(w http.ResponseWriter, r *http.Request) {
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(time.Now().Add(2 * time.Second))
		rejected := false
		defer func() {
			if !rejected {
				_ = controller.SetReadDeadline(time.Time{})
			}
		}()
		reject := func(status int) {
			rejected = true
			w.Header().Set("Connection", "close")
			_ = controller.SetReadDeadline(time.Now())
			_ = r.Body.Close()
			http.Error(w, "managed Team preview unavailable", status)
		}
		var input struct {
			Path string `json:"path"`
			Mode string `json:"mode"`
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 4097))
		if err != nil || len(body) > 4096 {
			reject(http.StatusBadRequest)
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil {
			reject(http.StatusBadRequest)
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			reject(http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		grant, err := provider.Sign(ctx, id, input.Path, input.Mode)
		if err != nil || ctx.Err() != nil {
			reject(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(grant)
	}}, nil
}

func WithManagedTeamPreview(provider *services.ManagedTeamPreview) EgressProxyOption {
	return func(h *EgressProxyHandler) { h.managedPreview = provider }
}
func (h *EgressProxyHandler) handleManagedTeamPreview(c *gin.Context) {
	if h.managedPreview == nil {
		c.String(503, "Team artifact preview is unavailable")
		return
	}
	binding, err := h.managedPreview.Open(c.Request.Context(), c.Request.URL.EscapedPath())
	if err != nil {
		c.String(403, "Team artifact preview authorization failed")
		return
	}
	defer binding.Close()
	if binding.Mode == "interactive" {
		if h.isManagedInteractivePreviewBootstrapHost(c.Request) {
			c.Header("Cache-Control", "private, no-store, max-age=0")
			c.Header("Referrer-Policy", "no-referrer")
			c.Redirect(http.StatusTemporaryRedirect, "http://"+binding.IsolatedHost+c.Request.URL.EscapedPath())
			return
		}
		if !strings.EqualFold(normalizedTeamPreviewRequestHost(c.Request), binding.IsolatedHost) {
			c.String(403, "Team artifact preview authorization failed")
			return
		}
	}
	info, err := binding.File.Stat()
	if err != nil {
		c.String(403, "Team artifact preview unavailable")
		return
	}
	serveTeamPreviewContent(c, binding.File, info, binding.Mode, binding.ExpiresAt, binding.Check, binding.Interrupt)
}
