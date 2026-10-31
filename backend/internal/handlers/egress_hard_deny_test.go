package handlers

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"

	"clawreef/internal/egresspolicy"
)

func TestProtectedEgressHardDenyRunsOnResolvedDestination(t *testing.T) {
	for _, target := range []string{"8.8.4.4:443", "public-node.example:443"} {
		calls, checks := 0, 0
		h := NewEgressProxyHandler(nil, WithEgressHardDeny(func(_ context.Context, address netip.Addr) (bool, error) {
			checks++
			return address == netip.MustParseAddr("8.8.4.4"), nil
		}))
		h.lookupNetIP = func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("8.8.4.4")}, nil
		}
		h.baseDialContext = func(context.Context, string, string) (net.Conn, error) {
			calls++
			return nil, errors.New("unexpected dial")
		}
		if _, err := h.proxyDialContext(context.Background(), "tcp", target); err == nil || calls != 0 || checks != 1 {
			t.Fatalf("hard deny bypass: target=%s checks=%d dials=%d", target, checks, calls)
		}
	}
}

func TestProtectedEgressPrivateExceptionCannotOverrideClusterDeny(t *testing.T) {
	id := 7
	for _, ip := range []string{"10.244.235.224", "169.254.169.254"} {
		calls, checks := 0, 0
		h := NewEgressProxyHandler(nil, WithEgressHardDeny(func(context.Context, netip.Addr) (bool, error) { checks++; return true, nil }))
		h.privateExceptions = &stubPrivateExceptionSource{rules: []egresspolicy.PrivateExceptionRule{{ScopeType: egresspolicy.ScopeInstance, ScopeID: id, Prefix: netip.MustParsePrefix(ip + "/32"), Port: 443}}}
		h.baseDialContext = func(context.Context, string, string) (net.Conn, error) {
			calls++
			return nil, errors.New("unexpected dial")
		}
		ctx := context.WithValue(context.Background(), egressIdentityContextKey{}, egressRequestIdentity{InstanceID: &id})
		if _, err := h.proxyDialContext(ctx, "tcp", ip+":443"); err == nil || calls != 0 || checks != 1 {
			t.Fatal("scoped private exception overrode cluster/metadata hard deny")
		}
	}
}

func TestProtectedEgressUnavailableInventoryNeverDials(t *testing.T) {
	calls := 0
	h := NewEgressProxyHandler(nil, WithEgressHardDeny(func(context.Context, netip.Addr) (bool, error) { return false, errors.New("watch disconnected") }))
	h.baseDialContext = func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, errors.New("unexpected dial")
	}
	if _, err := h.proxyDialContext(context.Background(), "tcp", "1.1.1.1:443"); err == nil || calls != 0 {
		t.Fatal("inventory outage permitted dial")
	}
}
