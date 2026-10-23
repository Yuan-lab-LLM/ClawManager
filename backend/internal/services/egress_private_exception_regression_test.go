package services

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestEgressPrivateExceptionRejectsBroadCIDRAndSkipsHistoricalRules(t *testing.T) {
	repo := &stubEgressPrivateExceptionRepo{items: []models.EgressPrivateException{
		{ID: 1, ScopeType: "instance", ScopeID: 8, CIDR: "10.0.0.1/0", Port: 18080, Enabled: true},
		{ID: 2, ScopeType: "instance", ScopeID: 8, CIDR: "10.255.25.3/32", Port: 18080, Enabled: true},
	}}
	svc := NewEgressPrivateExceptionService(repo, &stubEgressInstanceLookup{instance: &models.Instance{ID: 8}}, nil)
	_, err := svc.Create(SaveEgressPrivateExceptionRequest{ScopeType: "instance", ScopeID: 8, CIDR: "10.0.0.1/0", Port: 18080, Enabled: true})
	if err != errEgressPrivateCIDRNotPrivate {
		t.Fatalf("broad CIDR error=%v", err)
	}
	if rules := svc.SnapshotRules(); len(rules) != 1 || rules[0].Prefix.String() != "10.255.25.3/32" {
		t.Fatalf("unsafe persisted rule was loaded: %+v", rules)
	}
	id := 8
	if svc.Allows(&id, nil, netip.MustParseAddr("127.0.0.1"), 18080) {
		t.Fatal("loopback was allowed")
	}
	if !svc.Allows(&id, nil, netip.MustParseAddr("10.255.25.3"), 18080) {
		t.Fatal("valid private exception was lost")
	}
}

func TestEgressPrivateExceptionMappedCIDRStoredAndLoadedCanonically(t *testing.T) {
	for _, test := range []struct{ cidr, want string }{
		{"::ffff:10.255.25.3/128", "10.255.25.3/32"},
		{"::ffff:10.255.25.3/120", "10.255.25.0/24"},
		{"::ffff:10.255.25.3/112", "10.255.0.0/16"},
	} {
		t.Run(test.cidr, func(t *testing.T) {
			repo := &stubEgressPrivateExceptionRepo{}
			svc := NewEgressPrivateExceptionService(repo, &stubEgressInstanceLookup{instance: &models.Instance{ID: 8}}, nil)
			item, err := svc.Create(SaveEgressPrivateExceptionRequest{ScopeType: "instance", ScopeID: 8, CIDR: test.cidr, Port: 18080, Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			if item.CIDR != test.want || repo.items[0].CIDR != test.want {
				t.Fatalf("CIDR was not canonicalized: %s", item.CIDR)
			}
			// Reload an old database row written before mapped CIDRs were normalized.
			repo.items[0].CIDR = test.cidr
			if rules := svc.SnapshotRules(); len(rules) != 1 || rules[0].Prefix.String() != test.want {
				t.Fatalf("historical mapped rule was not normalized: %+v", rules)
			}
			id := 8
			if !svc.Allows(&id, nil, netip.MustParseAddr("10.255.25.3"), 18080) {
				t.Fatal("historical mapped rule failed to allow native IPv4")
			}
		})
	}
}

type unavailableEgressExceptionRepo struct {
	*stubEgressPrivateExceptionRepo
	unavailable bool
}

func (r *unavailableEgressExceptionRepo) ListEnabled(ctx context.Context) ([]models.EgressPrivateException, error) {
	if r.unavailable {
		return nil, errors.New("database unavailable")
	}
	return r.stubEgressPrivateExceptionRepo.ListEnabled(ctx)
}

func TestEgressPrivateExceptionCacheExpiresClosedAndRecovers(t *testing.T) {
	repo := &unavailableEgressExceptionRepo{stubEgressPrivateExceptionRepo: &stubEgressPrivateExceptionRepo{items: []models.EgressPrivateException{
		{ID: 1, ScopeType: "instance", ScopeID: 8, CIDR: "10.255.25.3/32", Port: 18080, Enabled: true},
	}}}
	svc := NewEgressPrivateExceptionService(repo, nil, nil).(*egressPrivateExceptionService)
	id := 8
	ip := netip.MustParseAddr("10.255.25.3")
	if !svc.Allows(&id, nil, ip, 18080) {
		t.Fatal("failed to prime cache")
	}
	// Simulate revocation by another replica and a failed refresh after expiry.
	repo.items[0].Enabled = false
	repo.unavailable = true
	svc.loadedAt = time.Now().Add(-egressPrivateExceptionCacheTTL - time.Second)
	if svc.Allows(&id, nil, ip, 18080) {
		t.Fatal("expired cache allowed a revoked exception")
	}
	if svc.Allows(&id, nil, ip, 18080) {
		t.Fatal("repeated refresh failure reused a stale exception")
	}
	repo.unavailable = false
	if svc.Allows(&id, nil, ip, 18080) {
		t.Fatal("recovered refresh ignored revocation")
	}
	repo.items[0].Enabled = true
	svc.markStale()
	if !svc.Allows(&id, nil, ip, 18080) {
		t.Fatal("valid rules did not recover after the database recovered")
	}
}

func TestEgressPrivateExceptionLocalRevocationDoesNotReuseFreshCache(t *testing.T) {
	repo := &unavailableEgressExceptionRepo{stubEgressPrivateExceptionRepo: &stubEgressPrivateExceptionRepo{items: []models.EgressPrivateException{
		{ID: 1, ScopeType: "instance", ScopeID: 8, CIDR: "10.255.25.3/32", Port: 18080, Enabled: true},
	}}}
	svc := NewEgressPrivateExceptionService(repo, &stubEgressInstanceLookup{instance: &models.Instance{ID: 8}}, nil)
	id := 8
	ip := netip.MustParseAddr("10.255.25.3")
	if !svc.Allows(&id, nil, ip, 18080) {
		t.Fatal("failed to prime cache")
	}
	_, err := svc.Update(1, SaveEgressPrivateExceptionRequest{ScopeType: "instance", ScopeID: 8, CIDR: "10.255.25.3/32", Port: 18080, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	repo.unavailable = true
	if svc.Allows(&id, nil, ip, 18080) {
		t.Fatal("invalidated cache was reused despite a successful local revocation")
	}
	repo.unavailable = false
	if svc.Allows(&id, nil, ip, 18080) {
		t.Fatal("revoked rule returned after recovery")
	}
}
