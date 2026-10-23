package egresspolicy

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
)

func TestPrivateExceptionAndDomainPolicySelectOnlyAuthorizedAddresses(t *testing.T) {
	instanceID, userID := 8, 1
	private := netip.MustParseAddr("10.255.25.3")
	public := netip.MustParseAddr("8.8.8.8")
	rules := []PrivateExceptionRule{{ScopeType: ScopeInstance, ScopeID: 8, Prefix: netip.MustParsePrefix("10.255.25.3/32"), Port: 18080}}
	for _, test := range []struct {
		name       string
		policy     Policy
		address    string
		resolved   []netip.Addr
		rules      []PrivateExceptionRule
		wantTarget string
		wantError  error
	}{
		{"private literal satisfies allowlist", Policy{Mode: ModeAllowlist}, "10.255.25.3:18080", nil, rules, "10.255.25.3:18080", nil},
		{"private DNS satisfies allowlist", Policy{Mode: ModeAllowlist}, "internal.example:18080", []netip.Addr{private}, rules, "10.255.25.3:18080", nil},
		{"mixed DNS cannot bypass public allowlist", Policy{Mode: ModeAllowlist}, "internal.example:18080", []netip.Addr{public, private}, rules, "10.255.25.3:18080", nil},
		{"public target cannot use CIDR exception", Policy{Mode: ModeAllowlist}, "8.8.8.8:18080", nil, rules, "", ErrHostBlocked},
		{"wrong port", Policy{Mode: ModeAllowlist}, "10.255.25.3:18081", nil, rules, "", ErrHostBlocked},
		{"wrong instance", Policy{Mode: ModeAllowlist}, "10.255.25.3:18080", nil, []PrivateExceptionRule{{ScopeType: ScopeInstance, ScopeID: 9, Prefix: rules[0].Prefix, Port: 18080}}, "", ErrHostBlocked},
		{"domain allowlist does not grant private access", Policy{Mode: ModeAllowlist, AllowedHostSuffixes: []string{"internal.example"}}, "internal.example:18080", []netip.Addr{private}, nil, "", ErrUnsafeTarget},
		{"explicit denylist wins", Policy{Mode: ModeDenylist, DeniedHostSuffixes: []string{"internal.example"}}, "internal.example:18080", []netip.Addr{private}, rules, "", ErrHostBlocked},
		{"user exception", Policy{Mode: ModeAllowlist}, "10.255.25.3:18080", nil, []PrivateExceptionRule{{ScopeType: ScopeUser, ScopeID: 1, Prefix: rules[0].Prefix, Port: 18080}}, "10.255.25.3:18080", nil},
		{"IPv6 exception", Policy{Mode: ModeAllowlist}, "[fd00::3]:18080", nil, []PrivateExceptionRule{{ScopeType: ScopeInstance, ScopeID: 8, Prefix: netip.MustParsePrefix("fd00::3/128"), Port: 18080}}, "[fd00::3]:18080", nil},
		{"mapped IPv4 exception", Policy{Mode: ModeAllowlist}, "[::ffff:10.255.25.3]:18080", nil, []PrivateExceptionRule{{ScopeType: ScopeInstance, ScopeID: 8, Prefix: netip.MustParsePrefix("::ffff:10.255.25.3/128"), Port: 18080}}, "10.255.25.3:18080", nil},
		{"normal public allowlist", Policy{Mode: ModeAllowlist, AllowedHostSuffixes: []string{"public.example"}}, "public.example:443", []netip.Addr{public}, nil, "8.8.8.8:443", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			dialed := ""
			conn, err := DialContextWithPolicyAndPrivateExceptions(context.Background(), "tcp", test.address, test.policy, test.rules, &instanceID, &userID,
				func(context.Context, string, string) ([]netip.Addr, error) { return test.resolved, nil },
				func(_ context.Context, _, address string) (net.Conn, error) {
					dialed = address
					left, right := net.Pipe()
					_ = left.Close()
					return right, nil
				})
			if conn != nil {
				_ = conn.Close()
			}
			if dialed != test.wantTarget {
				t.Fatalf("dialed %q, want %q", dialed, test.wantTarget)
			}
			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("error=%v, want %v", err, test.wantError)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPrivateExceptionPolicyRejectsInapplicableRulesBeforeDNS(t *testing.T) {
	instanceID, userID, otherID := 8, 1, 9
	valid := PrivateExceptionRule{ScopeType: ScopeInstance, ScopeID: 8, Prefix: netip.MustParsePrefix("10.255.25.3/32"), Port: 18080}
	dnsErr := &net.DNSError{Err: "no such host", Name: "unlisted.invalid", IsNotFound: true}
	for _, test := range []struct {
		name        string
		rule        *PrivateExceptionRule
		instance    *int
		wantDNS     bool
		allowedHost bool
	}{
		{"no rules", nil, &instanceID, false, false},
		{"missing identity", &valid, nil, false, false},
		{"wrong instance", &valid, &otherID, false, false},
		{"wrong user", &PrivateExceptionRule{ScopeType: ScopeUser, ScopeID: 2, Prefix: valid.Prefix, Port: 18080}, &instanceID, false, false},
		{"wrong port", &PrivateExceptionRule{ScopeType: ScopeInstance, ScopeID: 8, Prefix: valid.Prefix, Port: 18081}, &instanceID, false, false},
		{"unsafe rule", &PrivateExceptionRule{ScopeType: ScopeInstance, ScopeID: 8, Prefix: netip.MustParsePrefix("10.0.0.1/0"), Port: 18080}, &instanceID, false, false},
		{"applicable exception retains DNS error", &valid, &instanceID, true, false},
		{"allowed domain retains DNS error", nil, nil, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var rules []PrivateExceptionRule
			if test.rule != nil {
				rules = append(rules, *test.rule)
			}
			policy := Policy{Mode: ModeAllowlist}
			if test.allowedHost {
				policy.AllowedHostSuffixes = []string{"unlisted.invalid"}
			}
			lookups := 0
			_, err := DialContextWithPolicyAndPrivateExceptions(context.Background(), "tcp", "unlisted.invalid:18080", policy, rules, test.instance, &userID,
				func(context.Context, string, string) ([]netip.Addr, error) { lookups++; return nil, dnsErr }, nil)
			if test.wantDNS {
				if lookups != 1 || !errors.Is(err, dnsErr) || errors.Is(err, ErrHostBlocked) {
					t.Fatalf("genuine DNS failure was lost: lookups=%d err=%v", lookups, err)
				}
			} else if lookups != 0 || !errors.Is(err, ErrHostBlocked) {
				t.Fatalf("request should be denied before DNS: lookups=%d err=%v", lookups, err)
			}
		})
	}
}
