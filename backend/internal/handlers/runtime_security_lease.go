package handlers

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"time"

	"clawreef/internal/runtimeauth"
	"clawreef/internal/services"
)

type RuntimeLeaseIssuer interface {
	IssueLease(context.Context, runtimeauth.Identity, netip.Addr) (services.RuntimeSecurityLease, error)
}

type RuntimeSecurityLeaseHandler struct {
	controlToken string
	issuer       RuntimeLeaseIssuer
}

func NewRuntimeSecurityLeaseHandler(controlToken string, issuer RuntimeLeaseIssuer) (*RuntimeSecurityLeaseHandler, error) {
	if len(controlToken) < 32 || issuer == nil {
		return nil, errors.New("invalid runtime lease issuer configuration")
	}
	return &RuntimeSecurityLeaseHandler{controlToken: controlToken, issuer: issuer}, nil
}

func (h *RuntimeSecurityLeaseHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	// Bound uploads, and keep rejected connections from being reused.
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(2 * time.Second))
	rejected := false
	defer func() {
		if !rejected {
			_ = controller.SetReadDeadline(time.Time{})
		}
	}()
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	defer r.Body.Close()
	fail := func(status int) {
		rejected = true
		w.Header().Set("Connection", "close")
		_ = controller.SetReadDeadline(time.Now())
		_ = r.Body.Close()
		http.Error(w, "runtime lease unavailable", status)
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		fail(http.StatusMethodNotAllowed)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-ClawManager-Control-Token")), []byte(h.controlToken)) != 1 {
		fail(http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	// Bound stalled uploads as well as issuance; canceling a request context
	// alone does not interrupt reads from the incoming server request body.
	var request struct {
		InstanceID int    `json:"instance_id"`
		UserID     int    `json:"user_id"`
		Generation int    `json:"generation"`
		PodUID     string `json:"pod_uid"`
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&request) != nil || d.Decode(new(any)) != io.EOF || request.InstanceID < 1 || request.UserID < 1 || request.Generation < 1 || request.PodUID == "" {
		fail(http.StatusBadRequest)
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		fail(http.StatusForbidden)
		return
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || peer.Zone() != "" {
		fail(http.StatusForbidden)
		return
	}
	identity := runtimeauth.Identity{InstanceID: request.InstanceID, UserID: request.UserID, Generation: request.Generation, PodUID: request.PodUID}
	lease, err := h.issuer.IssueLease(ctx, identity, peer)
	if err != nil || ctx.Err() != nil {
		fail(http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(lease)
}
