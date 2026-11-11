// Package workbench holds the transport-independent owner workbench contracts.
package workbench

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidPreviewURL = errors.New("preview address must be an HTTP or HTTPS service in the current instance")
	ErrPreviewNotFound   = errors.New("preview is unavailable or expired")
	ErrPreviewScope      = errors.New("preview requires the current instance generation")
	ErrPreviewLimit      = errors.New("too many active previews for this instance")
)

// PreviewTarget is a logical business-loopback address, never a Pod or host IP.
type PreviewTarget struct {
	Scheme   string `json:"scheme"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Path     string `json:"path"`
	RawPath  string `json:"raw_path,omitempty"`
	RawQuery string `json:"raw_query,omitempty"`
	Fragment string `json:"fragment,omitempty"`
}

func ParsePreviewURL(raw string) (PreviewTarget, error) {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > 8192 || strings.ContainsAny(value, "\\\r\n\x00") {
		return PreviewTarget{}, ErrInvalidPreviewURL
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	u, err := url.Parse(value)
	if err != nil || u.Opaque != "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || strings.HasSuffix(u.Host, ":") {
		return PreviewTarget{}, ErrInvalidPreviewURL
	}
	host := strings.ToLower(u.Hostname())
	if host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host != "localhost" {
		ip, err := netip.ParseAddr(host)
		if err != nil || ip.Zone() != "" || !ip.Unmap().IsLoopback() {
			return PreviewTarget{}, ErrInvalidPreviewURL
		}
		host = ip.String()
	}
	port := 80
	if u.Scheme == "https" {
		port = 443
	}
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return PreviewTarget{}, ErrInvalidPreviewURL
		}
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	return PreviewTarget{Scheme: u.Scheme, Host: host, Port: port, Path: path, RawPath: u.RawPath, RawQuery: u.RawQuery, Fragment: u.Fragment}, nil
}

func (t PreviewTarget) URL() string {
	return (&url.URL{Scheme: t.Scheme, Host: net.JoinHostPort(t.Host, strconv.Itoa(t.Port)), Path: t.Path, RawPath: t.RawPath, RawQuery: t.RawQuery, Fragment: t.Fragment}).String()
}

type PreviewScope struct {
	InstanceID int    `json:"instance_id"`
	UserID     int    `json:"user_id"`
	Generation int    `json:"generation"`
	GatewayID  string `json:"gateway_id"`
}

func (s PreviewScope) Valid() bool {
	return s.InstanceID > 0 && s.UserID > 0 && s.Generation > 0 && s.GatewayID == fmt.Sprintf("gw-%d-%d", s.InstanceID, s.Generation)
}

// SharedPreviewAccess remains control-plane metadata and is never embedded in
// PreviewScope or sent to the runtime agent.
type SharedPreviewAccess struct {
	Code            string    `json:"code"`
	SessionBinding  string    `json:"session_binding"`
	WorkspaceAccess string    `json:"workspace_access"`
	ExpiresAt       time.Time `json:"expires_at"`
}

func (a SharedPreviewAccess) Valid() bool {
	return strings.TrimSpace(a.Code) != "" && len(a.Code) <= 256 && len(a.SessionBinding) == 64 && (a.WorkspaceAccess == "read" || a.WorkspaceAccess == "write") && !a.ExpiresAt.IsZero()
}

type PreviewLease struct {
	// ID is the bearer capability returned to the caller, and is never persisted.
	ID        string               `json:"id,omitempty"`
	Shared    *SharedPreviewAccess `json:"shared,omitempty"`
	Scope     PreviewScope         `json:"scope"`
	Target    PreviewTarget        `json:"target"`
	CreatedAt time.Time            `json:"created_at"`
	ExpiresAt time.Time            `json:"expires_at"`
}

// PreviewStore must be shared between control-plane replicas. Implementations
// persist only the SHA-256 capability key, the target and its ownership scope.
type PreviewStore interface {
	Put(context.Context, string, PreviewLease) error
	Get(context.Context, string) (PreviewLease, error)
}

type Previews struct {
	origin *url.URL
	domain string
	store  PreviewStore
	ttl    time.Duration
	now    func() time.Time
}

func NewPreviews(originTemplate string, store PreviewStore, ttl time.Duration) (*Previews, error) {
	if store == nil || ttl <= 0 || ttl > 24*time.Hour || strings.Count(originTemplate, "{token}") != 1 {
		return nil, errors.New("preview origin, shared store and an expiry up to 24 hours are required")
	}
	marker := strings.Repeat("a", 48)
	u, err := url.Parse(strings.Replace(originTemplate, "{token}", marker, 1))
	if err != nil || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "http" && u.Scheme != "https") || !strings.HasPrefix(u.Hostname(), "p-"+marker+".") {
		return nil, errors.New("preview origin must use http(s)://p-{token}.<preview-domain> without a path or credentials")
	}
	domain := strings.TrimPrefix(strings.ToLower(u.Hostname()), "p-"+marker+".")
	if domain == "" || strings.ContainsAny(domain, "{}:/\\ \t\r\n") {
		return nil, errors.New("invalid preview domain")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, errors.New("invalid preview origin port")
		}
	}
	return &Previews{origin: u, domain: domain, store: store, ttl: ttl, now: time.Now}, nil
}

func previewKey(id string) string {
	hash := sha256.Sum256([]byte(id))
	return hex.EncodeToString(hash[:])
}

func (p *Previews) Create(ctx context.Context, scope PreviewScope, rawURL string) (PreviewLease, error) {
	return p.create(ctx, scope, rawURL, nil)
}
func (p *Previews) CreateShared(ctx context.Context, scope PreviewScope, rawURL string, shared SharedPreviewAccess) (PreviewLease, error) {
	if !shared.Valid() || !p.now().Before(shared.ExpiresAt) {
		return PreviewLease{}, ErrPreviewScope
	}
	return p.create(ctx, scope, rawURL, &shared)
}
func (p *Previews) create(ctx context.Context, scope PreviewScope, rawURL string, shared *SharedPreviewAccess) (PreviewLease, error) {
	if err := ctx.Err(); err != nil {
		return PreviewLease{}, err
	}
	if !scope.Valid() {
		return PreviewLease{}, ErrPreviewScope
	}
	target, err := ParsePreviewURL(rawURL)
	if err != nil {
		return PreviewLease{}, err
	}
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return PreviewLease{}, errors.New("could not create preview capability")
	}
	id, now := hex.EncodeToString(random), p.now()
	lease := PreviewLease{Scope: scope, Target: target, CreatedAt: now, ExpiresAt: now.Add(p.ttl), Shared: shared}
	if shared != nil && shared.ExpiresAt.Before(lease.ExpiresAt) {
		lease.ExpiresAt = shared.ExpiresAt
	}
	if err := p.store.Put(ctx, previewKey(id), lease); err != nil {
		return PreviewLease{}, err
	}
	lease.ID = id
	return lease, nil
}

func (p *Previews) URL(lease PreviewLease) string {
	u := *p.origin
	host := "p-" + lease.ID + "." + p.domain
	if port := u.Port(); port != "" {
		host = net.JoinHostPort(host, port)
	}
	u.Host, u.Path, u.RawPath = host, lease.Target.Path, lease.Target.RawPath
	u.RawQuery, u.Fragment = lease.Target.RawQuery, lease.Target.Fragment
	return u.String()
}

func (p *Previews) IsPreviewHost(host string) bool {
	u, err := url.Parse("//" + host)
	if err != nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return strings.HasPrefix(h, "p-") && strings.HasSuffix(h, "."+p.domain)
}

func (p *Previews) LookupHost(ctx context.Context, host string) (PreviewLease, error) {
	if !p.IsPreviewHost(host) {
		return PreviewLease{}, ErrPreviewNotFound
	}
	u, _ := url.Parse("//" + host)
	hostname := strings.ToLower(u.Hostname())
	id := strings.TrimSuffix(strings.TrimPrefix(hostname, "p-"), "."+p.domain)
	return p.LookupCapability(ctx, id)
}

// LookupCapability authenticates the same opaque capability when a deployment
// routes by a fixed browser origin rather than its bearer subdomain.
func (p *Previews) LookupCapability(ctx context.Context, id string) (PreviewLease, error) {
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != 24 {
		return PreviewLease{}, ErrPreviewNotFound
	}
	lease, err := p.store.Get(ctx, previewKey(id))
	if err != nil {
		return PreviewLease{}, err
	}
	if !lease.Scope.Valid() || !p.now().Before(lease.ExpiresAt) || (lease.Shared != nil && (!lease.Shared.Valid() || !p.now().Before(lease.Shared.ExpiresAt))) {
		return PreviewLease{}, ErrPreviewNotFound
	}
	// Persistent records are metadata, not authority to bypass URL validation.
	if _, err := ParsePreviewURL(lease.Target.URL()); err != nil {
		return PreviewLease{}, ErrPreviewNotFound
	}
	lease.ID = id
	return lease, nil
}
