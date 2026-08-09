package egresspolicy

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
)

func TestIsEligiblePrivateExceptionPrefix(t *testing.T) {
	private := netip.MustParsePrefix("10.255.25.3/32")
	docker := netip.MustParsePrefix("172.19.0.0/16")
	public := netip.MustParsePrefix("8.8.8.0/24")
	linkLocal := netip.MustParsePrefix("169.254.1.1/32")

	if !IsEligiblePrivateExceptionPrefix(private) {
		t.Fatal("expected private /32 to be eligible")
	}
	if !IsEligiblePrivateExceptionPrefix(docker) {
		t.Fatal("expected docker CIDR to be eligible")
	}
	if IsEligiblePrivateExceptionPrefix(public) {
		t.Fatal("expected public CIDR to be rejected")
	}
	if !IsEligiblePrivateExceptionPrefix(linkLocal) {
		t.Fatal("expected link-local to be eligible")
	}
}

func TestMatchPrivateExceptionInstanceAndUserUnion(t *testing.T) {
	rules := []PrivateExceptionRule{
		{ScopeType: ScopeInstance, ScopeID: 8, Prefix: netip.MustParsePrefix("10.255.25.3/32"), Port: 18080},
		{ScopeType: ScopeUser, ScopeID: 1, Prefix: netip.MustParsePrefix("172.19.0.0/16"), Port: 9380},
	}
	instanceID := 8
	userID := 1
	ipHost := netip.MustParseAddr("10.255.25.3")
	ipDocker := netip.MustParseAddr("172.19.0.5")

	if !MatchPrivateException(rules, &instanceID, &userID, ipHost, 18080) {
		t.Fatal("expected instance exception to match")
	}
	if !MatchPrivateException(rules, &instanceID, &userID, ipDocker, 9380) {
		t.Fatal("expected user exception to match")
	}
	if MatchPrivateException(rules, &instanceID, &userID, ipHost, 18081) {
		t.Fatal("expected port mismatch to fail")
	}
	otherInstance := 9
	if MatchPrivateException(rules, &otherInstance, nil, ipHost, 18080) {
		t.Fatal("expected unrelated instance to fail")
	}
}

func TestDialContextAllowingPrivateExceptions(t *testing.T) {
	rules := []PrivateExceptionRule{
		{ScopeType: ScopeInstance, ScopeID: 8, Prefix: netip.MustParsePrefix("10.255.25.3/32"), Port: 18080},
	}
	instanceID := 8
	dialed := ""
	conn, err := DialContextAllowingPrivateExceptions(
		context.Background(),
		"tcp",
		"10.255.25.3:18080",
		rules,
		&instanceID,
		nil,
		nil,
		func(ctx context.Context, network, address string) (net.Conn, error) {
			dialed = address
			return nil, errors.New("stub dial stop")
		},
	)
	if err == nil || conn != nil {
		t.Fatalf("expected stub dial error, got conn=%v err=%v", conn, err)
	}
	if dialed != "10.255.25.3:18080" {
		t.Fatalf("expected private exception dial, got %q", dialed)
	}

	_, err = DialContextAllowingPrivateExceptions(
		context.Background(),
		"tcp",
		"10.255.25.3:18080",
		rules,
		nil,
		nil,
		nil,
		func(ctx context.Context, network, address string) (net.Conn, error) {
			t.Fatal("should not dial without identity")
			return nil, nil
		},
	)
	if !errors.Is(err, ErrUnsafeTarget) {
		t.Fatalf("expected ErrUnsafeTarget, got %v", err)
	}
}
