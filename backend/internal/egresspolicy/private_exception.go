package egresspolicy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

const (
	ScopeInstance = "instance"
	ScopeUser     = "user"
)

// PrivateExceptionRule is a compiled CIDR+port allowance for a user or instance.
type PrivateExceptionRule struct {
	ScopeType string
	ScopeID   int
	Prefix    netip.Prefix
	Port      int
}

// IsEligiblePrivateExceptionPrefix reports whether a CIDR may be stored as a private exception.
func IsEligiblePrivateExceptionPrefix(prefix netip.Prefix) bool {
	if !prefix.IsValid() {
		return false
	}
	addr := prefix.Addr().Unmap()
	return addr.IsPrivate() || addr.IsLinkLocalUnicast()
}

// MatchPrivateException returns true when ip:port is allowed for the given identity.
func MatchPrivateException(rules []PrivateExceptionRule, instanceID, userID *int, ip netip.Addr, port int) bool {
	if !ip.IsValid() || port <= 0 || port > 65535 || len(rules) == 0 {
		return false
	}
	ip = ip.Unmap()
	for _, rule := range rules {
		if rule.Port != port || !rule.Prefix.IsValid() || !rule.Prefix.Contains(ip) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(rule.ScopeType)) {
		case ScopeInstance:
			if instanceID != nil && *instanceID == rule.ScopeID {
				return true
			}
		case ScopeUser:
			if userID != nil && *userID == rule.ScopeID {
				return true
			}
		}
	}
	return false
}

// DialContextAllowingPrivateExceptions dials public targets normally and private
// targets only when they match an exception for the request identity.
func DialContextAllowingPrivateExceptions(
	ctx context.Context,
	network, address string,
	rules []PrivateExceptionRule,
	instanceID, userID *int,
	lookup LookupNetIPFunc,
	dial func(ctx context.Context, network, address string) (net.Conn, error),
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return nil, fmt.Errorf("invalid target address %q: %w", address, err)
	}
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum <= 0 || portNum > 65535 {
		return nil, fmt.Errorf("invalid target port %q", port)
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return nil, fmt.Errorf("target host is required")
	}
	if dial == nil {
		base := net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		dial = base.DialContext
	}

	resolved, err := resolveDialCandidates(ctx, network, host, port, portNum, rules, instanceID, userID, lookup)
	if err != nil {
		return nil, err
	}

	var errs []error
	for _, target := range resolved {
		attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		conn, dialErr := dial(attemptCtx, network, target)
		cancel()
		if dialErr == nil {
			return conn, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", target, dialErr))
	}
	return nil, fmt.Errorf("dial target %q: %w", address, errors.Join(errs...))
}

func resolveDialCandidates(
	ctx context.Context,
	network, host, port string,
	portNum int,
	rules []PrivateExceptionRule,
	instanceID, userID *int,
	lookup LookupNetIPFunc,
) ([]string, error) {
	var addrs []netip.Addr
	if literal, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{literal.Unmap()}
	} else {
		if lookup == nil {
			lookup = net.DefaultResolver.LookupNetIP
		}
		resolved, err := lookup(ctx, lookupNetwork(network), host)
		if err != nil {
			return nil, fmt.Errorf("resolve target %q: %w", host, err)
		}
		addrs = resolved
	}

	public := make([]string, 0, len(addrs))
	privateAllowed := make([]string, 0, len(addrs))
	seen := map[netip.Addr]struct{}{}
	sawPrivate := false
	for _, address := range addrs {
		address = address.Unmap()
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		target := net.JoinHostPort(address.String(), port)
		if IsPublicAddress(address) {
			public = append(public, target)
			continue
		}
		sawPrivate = true
		if MatchPrivateException(rules, instanceID, userID, address, portNum) {
			privateAllowed = append(privateAllowed, target)
		}
	}
	candidates := append(public, privateAllowed...)
	if len(candidates) == 0 {
		if sawPrivate {
			return nil, fmt.Errorf("%w: %s", ErrUnsafeTarget, host)
		}
		return nil, fmt.Errorf("%w: %s", ErrUnsafeTarget, host)
	}
	return candidates, nil
}
