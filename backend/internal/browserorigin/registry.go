// Package browserorigin assigns permanent same-host browser origins to instances.
package browserorigin

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

const (
	Native  = "native"
	Preview = "preview"
)

var (
	ErrCapacity = errors.New("browser origin capacity exhausted")
	ErrNotFound = errors.New("browser origin not found")
	ErrInvalid  = errors.New("invalid browser origin configuration or target")
)

type Config struct {
	ManagerOrigin       string
	FirstPort, LastPort int
}

type Binding struct {
	Port       int    `db:"port" json:"port"`
	InstanceID int    `db:"instance_id" json:"instance_id"`
	Kind       string `db:"kind" json:"kind"`
	Target     string `db:"target" json:"target"`
}

type Store interface {
	Bind(context.Context, int, string, string, int, int) (Binding, error)
	ByPort(context.Context, int) (Binding, error)
}

type Registry struct {
	config Config
	host   string
	store  Store
}

func New(managerOrigin, portRange string, store Store) (*Registry, error) {
	u, err := url.Parse(managerOrigin)
	if err != nil || store == nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.Host == "" ||
		(u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(managerOrigin, "#") {
		return nil, ErrInvalid
	}
	host := strings.ToLower(u.Hostname())
	if !validHost(host) || strings.HasSuffix(u.Host, ":") {
		return nil, ErrInvalid
	}
	managerPort := 443
	explicitPort := u.Port() != ""
	if explicitPort {
		managerPort, err = parsePort(u.Port())
		if err != nil {
			return nil, err
		}
	}
	firstText, lastText, ok := strings.Cut(portRange, "-")
	if !ok {
		return nil, ErrInvalid
	}
	first, err := parsePort(firstText)
	if err != nil {
		return nil, err
	}
	last, err := parsePort(lastText)
	if err != nil || first > last || last-first >= 512 || (managerPort >= first && managerPort <= last) {
		return nil, ErrInvalid
	}
	u.Host = host
	if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	}
	if explicitPort {
		u.Host = net.JoinHostPort(host, strconv.Itoa(managerPort))
	}
	u.Path = ""
	return &Registry{config: Config{ManagerOrigin: u.String(), FirstPort: first, LastPort: last}, host: host, store: store}, nil
}

func validHost(host string) bool {
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Zone() == ""
	}
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func parsePort(raw string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != raw {
		return 0, ErrInvalid
	}
	return n, nil
}

// CanonicalTarget applies the same validation at registry and persistence boundaries.
// Preview targets identify a service root; document paths never consume ports.
func CanonicalTarget(kind, target string) (string, error) {
	if kind == Native {
		if target == "opencode" || target == "deepseek-harness" {
			return target, nil
		}
		return "", ErrInvalid
	}
	if kind != Preview || target == "" || len(target) > 512 || strings.ContainsAny(target, "\\ \t\r\n\x00#") {
		return "", ErrInvalid
	}
	if !strings.Contains(target, "://") {
		target = "http://" + target
	}
	u, err := url.Parse(target)
	if err != nil || u.User != nil || u.Opaque != "" || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || strings.HasSuffix(u.Host, ":") ||
		(u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery {
		return "", ErrInvalid
	}
	host := strings.ToLower(u.Hostname())
	if host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host != "localhost" {
		ip, err := netip.ParseAddr(host)
		if err != nil || ip.Zone() != "" || !ip.Unmap().IsLoopback() {
			return "", ErrInvalid
		}
		host = ip.String()
	}
	port := 80
	if u.Scheme == "https" {
		port = 443
	}
	if u.Port() != "" {
		port, err = parsePort(u.Port())
		if err != nil {
			return "", err
		}
	}
	return (&url.URL{Scheme: u.Scheme, Host: net.JoinHostPort(host, strconv.Itoa(port)), Path: "/"}).String(), nil
}

func (r *Registry) Bind(ctx context.Context, instanceID int, kind, target string) (Binding, error) {
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	if instanceID <= 0 {
		return Binding{}, ErrInvalid
	}
	target, err := CanonicalTarget(kind, target)
	if err != nil {
		return Binding{}, err
	}
	return r.store.Bind(ctx, instanceID, kind, target, r.config.FirstPort, r.config.LastPort)
}

func (r *Registry) requestPort(requestHost string) (int, bool) {
	host, rawPort, err := net.SplitHostPort(requestHost)
	if err != nil || strings.ToLower(host) != r.host {
		return 0, false
	}
	port, err := parsePort(rawPort)
	return port, err == nil && port >= r.config.FirstPort && port <= r.config.LastPort
}

func (r *Registry) Lookup(ctx context.Context, requestHost string) (Binding, error) {
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	port, ok := r.requestPort(requestHost)
	if !ok {
		return Binding{}, ErrNotFound
	}
	return r.store.ByPort(ctx, port)
}
func (r *Registry) Matches(requestHost string) bool { _, ok := r.requestPort(requestHost); return ok }
func (r *Registry) URL(binding Binding) string {
	if binding.Port < r.config.FirstPort || binding.Port > r.config.LastPort {
		return ""
	}
	return "https://" + net.JoinHostPort(r.host, strconv.Itoa(binding.Port))
}
func (r *Registry) ManagerOrigin() string { return r.config.ManagerOrigin }
func (r *Registry) Range() (int, int)     { return r.config.FirstPort, r.config.LastPort }
