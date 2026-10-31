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

var privateExceptionPrefixes = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
}

// NormalizePrivateExceptionPrefix canonicalizes host bits and converts mapped
// IPv4 CIDRs to IPv4. Prefixes broader than the mapped IPv4 region are invalid.
func NormalizePrivateExceptionPrefix(prefix netip.Prefix) netip.Prefix {
	if !prefix.IsValid() {
		return netip.Prefix{}
	}
	if prefix.Addr().Is4In6() {
		if prefix.Bits() < 96 {
			return netip.Prefix{}
		}
		prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
	}
	return prefix.Masked()
}

// IsEligiblePrivateExceptionPrefix requires the entire CIDR to fit within
// a private or link-local network, rather than just its supplied first address.
func IsEligiblePrivateExceptionPrefix(prefix netip.Prefix) bool {
	prefix = NormalizePrivateExceptionPrefix(prefix)
	if !prefix.IsValid() {
		return false
	}
	for _, allowed := range privateExceptionPrefixes {
		if prefix.Bits() >= allowed.Bits() && allowed.Contains(prefix.Addr()) {
			return true
		}
	}
	return false
}

// MatchPrivateException returns true when ip:port is allowed for the given identity.
func MatchPrivateException(rules []PrivateExceptionRule, instanceID, userID *int, ip netip.Addr, port int) bool {
	if !ip.IsValid() || port <= 0 || port > 65535 || len(rules) == 0 {
		return false
	}
	ip = ip.Unmap()
	for _, rule := range rules {
		prefix := NormalizePrivateExceptionPrefix(rule.Prefix)
		if rule.Port != port || !IsEligiblePrivateExceptionPrefix(prefix) || !prefix.Contains(ip) || !privateExceptionMatchesScope(rule, instanceID, userID) {
			continue
		}
		return true
	}
	return false
}

func privateExceptionMatchesScope(rule PrivateExceptionRule, instanceID, userID *int) bool {
	switch strings.ToLower(strings.TrimSpace(rule.ScopeType)) {
	case ScopeInstance:
		return instanceID != nil && *instanceID == rule.ScopeID
	case ScopeUser:
		return userID != nil && *userID == rule.ScopeID
	default:
		return false
	}
}

func hasApplicablePrivateException(rules []PrivateExceptionRule, instanceID, userID *int, port int) bool {
	for _, rule := range rules {
		if rule.Port == port && privateExceptionMatchesScope(rule, instanceID, userID) && IsEligiblePrivateExceptionPrefix(rule.Prefix) {
			return true
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
	return DialContextWithPolicyAndPrivateExceptions(ctx, network, address, Policy{Mode: ModeOpen}, rules, instanceID, userID, lookup, dial)
}

// DialContextWithPolicyAndPrivateExceptions applies domain policy to public
// destinations and scoped CIDR exceptions to private destinations. A private
// exception can satisfy allowlist mode, but never override an explicit denylist.
func DialContextWithPolicyAndPrivateExceptions(
	ctx context.Context,
	network, address string,
	policy Policy,
	rules []PrivateExceptionRule,
	instanceID, userID *int,
	lookup LookupNetIPFunc,
	dial func(ctx context.Context, network, address string) (net.Conn, error),
) (net.Conn, error) {
	return DialContextWithPolicyAndPrivateRuleSource(ctx, network, address, policy,
		func(context.Context) []PrivateExceptionRule { return rules }, instanceID, userID, lookup, dial)
}

// DialContextWithPolicyAndPrivateRuleSource only loads scoped rules when the
// domain policy or a resolved private address requires them. Public-only
// destinations do not wait for the private-rule database to refresh.
func DialContextWithPolicyAndPrivateRuleSource(
	ctx context.Context,
	network, address string,
	policy Policy,
	source func(context.Context) []PrivateExceptionRule,
	instanceID, userID *int,
	lookup LookupNetIPFunc,
	dial func(context.Context, string, string) (net.Conn, error),
) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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

	allowPublic, reason := policy.AllowHost(host)
	var rules []PrivateExceptionRule
	loaded := false
	loadRules := func() []PrivateExceptionRule {
		if !loaded {
			loaded = true
			if source != nil && ctx.Err() == nil {
				rules = source(ctx)
			}
		}
		return rules
	}
	// DNS cannot make this target permissible without a matching scope+port
	// exception. Reject it first so NXDOMAIN remains a policy block, not a 502.
	if !allowPublic && (policy.Mode != ModeAllowlist || !hasApplicablePrivateException(loadRules(), instanceID, userID, portNum)) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s (%s)", ErrHostBlocked, host, reason)
	}
	resolved, err := resolveDialCandidates(ctx, network, host, port, portNum, loadRules, instanceID, userID, lookup, allowPublic)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		if !allowPublic && errors.Is(err, ErrUnsafeTarget) {
			return nil, fmt.Errorf("%w: %s (%s)", ErrHostBlocked, host, reason)
		}
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
	loadRules func() []PrivateExceptionRule,
	instanceID, userID *int,
	lookup LookupNetIPFunc,
	allowPublic bool,
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
			if allowPublic {
				public = append(public, target)
			}
			continue
		}
		sawPrivate = true
		if MatchPrivateException(loadRules(), instanceID, userID, address, portNum) {
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
