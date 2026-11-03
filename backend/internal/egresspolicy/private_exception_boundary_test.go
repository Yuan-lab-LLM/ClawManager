package egresspolicy

import (
	"net/netip"
	"testing"
)

func TestPrivateExceptionPrefixRequiresWholePrivateNetwork(t *testing.T) {
	for _, test := range []struct {
		cidr string
		want bool
	}{
		{"10.0.0.1/0", false}, {"10.0.0.1/7", false},
		{"172.16.0.1/11", false}, {"192.168.0.1/15", false},
		{"169.254.0.1/15", false}, {"fd00::1/0", false},
		{"fd00::1/6", false}, {"fe80::1/9", false},
		{"127.0.0.1/32", false}, {"8.8.8.8/32", false},
		{"10.1.2.3/8", true}, {"172.16.0.1/12", true},
		{"192.168.0.1/16", true}, {"169.254.0.1/16", true},
		{"fd00::1/7", true}, {"fe80::1/10", true},
	} {
		t.Run(test.cidr, func(t *testing.T) {
			if got := IsEligiblePrivateExceptionPrefix(netip.MustParsePrefix(test.cidr)); got != test.want {
				t.Fatalf("eligibility=%v, want %v", got, test.want)
			}
		})
	}
	id := 8
	rules := []PrivateExceptionRule{{ScopeType: ScopeInstance, ScopeID: id, Prefix: netip.MustParsePrefix("10.0.0.1/0"), Port: 18080}}
	if MatchPrivateException(rules, &id, nil, netip.MustParseAddr("127.0.0.1"), 18080) {
		t.Fatal("unsafe historical rule granted loopback access")
	}
}

func TestPrivateExceptionMappedIPv4CIDRMatchesBothAddressForms(t *testing.T) {
	id := 8
	for _, cidr := range []string{"::ffff:10.255.25.3/128", "::ffff:10.255.25.3/120", "::ffff:10.255.25.3/112"} {
		t.Run(cidr, func(t *testing.T) {
			prefix := netip.MustParsePrefix(cidr)
			if !IsEligiblePrivateExceptionPrefix(prefix) {
				t.Fatal("valid mapped private network was rejected")
			}
			rules := []PrivateExceptionRule{{ScopeType: ScopeInstance, ScopeID: id, Prefix: prefix, Port: 18080}}
			for _, target := range []string{"10.255.25.3", "::ffff:10.255.25.3"} {
				if !MatchPrivateException(rules, &id, nil, netip.MustParseAddr(target), 18080) {
					t.Fatalf("mapped network failed to match %s", target)
				}
			}
			if MatchPrivateException(rules, &id, nil, netip.MustParseAddr("127.0.0.1"), 18080) {
				t.Fatal("mapped private prefix matched loopback")
			}
		})
	}
	for _, cidr := range []string{"::ffff:10.0.0.1/96", "::ffff:10.0.0.1/95", "::ffff:172.16.0.1/107", "::ffff:127.0.0.1/128", "::ffff:8.8.8.8/128"} {
		if IsEligiblePrivateExceptionPrefix(netip.MustParsePrefix(cidr)) {
			t.Fatalf("unsafe mapped network accepted: %s", cidr)
		}
	}
}
