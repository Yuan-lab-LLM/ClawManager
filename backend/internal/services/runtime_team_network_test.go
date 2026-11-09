package services

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestProtectedTeamCannotAcquireAuthorityWithoutNetworkProof(t *testing.T) {
	a, instances, _, _, _ := securityFixture(t)
	env := `{"CLAWMANAGER_TEAM_ID":"12"}`
	instances.instance.EnvironmentOverridesJSON = &env
	if _, err := a.CurrentIdentity(context.Background(), 7); err == nil {
		t.Fatal("Team with unknown upstream network safety acquired execution and egress authority")
	}
}

type protectedTeamFixture struct {
	*managedTeamFixture
	associationErr error
}

func (f *protectedTeamFixture) GetRuntimeMemberByInstanceContext(ctx context.Context, id int) (*models.TeamMember, error) {
	if f.associationErr != nil {
		return nil, f.associationErr
	}
	return f.member, ctx.Err()
}

func TestProtectedTeamNetworkAuthorityAndBindingCannotDowngrade(t *testing.T) {
	for _, scenario := range []string{"allowed", "missing-env", "changed-env", "public-literal", "public-dns", "lost-network-proof", "lost-sql", "foreign-member", "no-member", "no-team"} {
		t.Run(scenario, func(t *testing.T) {
			a, instances, kube, _, _ := securityFixture(t)
			env := `{"CLAWMANAGER_TEAM_ID":"12"}`
			instances.instance.EnvironmentOverridesJSON = &env
			id := 7
			teams := &protectedTeamFixture{managedTeamFixture: &managedTeamFixture{team: &models.Team{ID: 12, UserID: 3, Status: models.TeamStatusRunning}, member: &models.TeamMember{TeamID: 12, UserID: 3, InstanceID: &id, Status: models.TeamMemberStatusIdle}}}
			bus, err := newRedisBus("redis://fake-old-master@redis.test.svc.cluster.local:6379/3")
			if err != nil {
				t.Fatal(err)
			}
			_, err = kube.CoreV1().Services("test").Create(context.Background(), &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "redis", Namespace: "test"}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: "10.96.0.10"}}, metav1.CreateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			proof := true
			m := &ManagedTeamRedis{teams: teams, resolve: func(context.Context, *models.Team) (*redisBus, error) { return bus, nil }, networkProtection: func(ctx context.Context, ip netip.Addr) error {
				if !proof || ip != netip.MustParseAddr("10.96.0.10") {
					return errManagedRedis
				}
				return ctx.Err()
			}}
			if err := ConfigureManagedTeamRedisNetwork(m, a); err != nil {
				t.Fatal(err)
			}
			identity, err := a.CurrentIdentity(context.Background(), 7)
			if err != nil {
				t.Fatal(err)
			}
			token, err := a.Capabilities().Issue(context.Background(), identity, runtimeauth.AudienceEgress)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "missing-env":
				env = `{}`
			case "changed-env":
				env = `{"CLAWMANAGER_TEAM_ID":"13"}`
			case "public-literal":
				bus.address = "203.0.113.10:6379"
			case "public-dns":
				bus.address = "redis.example.com:6379"
			case "lost-network-proof":
				proof = false
			case "lost-sql":
				teams.associationErr = errors.New("SQL unavailable")
			case "foreign-member":
				teams.member.UserID = 9
			case "no-member":
				teams.member = nil
			case "no-team":
				teams.member = nil
				env = `{}`
				m.resolve = func(context.Context, *models.Team) (*redisBus, error) {
					t.Error("non-Team resolved Redis")
					return nil, errManagedRedis
				}
			}
			_, err = a.Capabilities().Verify(context.Background(), token, runtimeauth.AudienceEgress)
			allowed := scenario == "allowed" || scenario == "no-team"
			if (err == nil) != allowed {
				t.Fatalf("authority after %s: %v", scenario, err)
			}
			if _, err := a.NativeGatewayCredential(context.Background(), 7); (err == nil) != allowed {
				t.Fatalf("native authority bypass after %s: %v", scenario, err)
			}
		})
	}
}

func TestProtectedRedisServiceTargetsArePinnedWithoutDNSFallback(t *testing.T) {
	for _, scenario := range []string{"short", "qualified", "svc", "fqdn", "literal", "headless", "external-name", "node-port", "load-balancer", "external-ip", "deleting", "wrong-suffix", "missing-service", "unavailable", "unproved"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "redis", Namespace: "test"}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: "10.96.0.10"}}
			host := "redis.test.svc.cluster.local"
			switch scenario {
			case "short":
				host = "redis"
			case "qualified":
				host = "redis.test"
			case "svc":
				host = "redis.test.svc"
			case "literal":
				host = "10.96.0.10"
			case "headless":
				service.Spec.ClusterIP = "None"
			case "external-name":
				service.Spec.Type = corev1.ServiceTypeExternalName
				service.Spec.ExternalName = "public.example"
			case "node-port":
				service.Spec.Type = corev1.ServiceTypeNodePort
			case "load-balancer":
				service.Spec.Type = corev1.ServiceTypeLoadBalancer
			case "external-ip":
				service.Spec.ExternalIPs = []string{"203.0.113.10"}
			case "deleting":
				now := metav1.Now()
				service.DeletionTimestamp = &now
			case "wrong-suffix":
				host = "redis.test.svc.attacker.example"
			case "missing-service":
				host = "missing"
			case "unavailable":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			client := fake.NewSimpleClientset(service)
			bus, _ := newRedisBus("rediss://fake-master@" + host + ":6379/3")
			target, err := managedRedisProtectedTarget(ctx, client, "test", bus, func(ctx context.Context, ip netip.Addr) error {
				if scenario == "unproved" || ip != netip.MustParseAddr("10.96.0.10") {
					return errManagedRedis
				}
				return ctx.Err()
			})
			allowed := scenario == "short" || scenario == "qualified" || scenario == "svc" || scenario == "fqdn" || scenario == "literal"
			if (err == nil) != allowed || (allowed && target != "10.96.0.10:6379") {
				t.Fatalf("target=%q err=%v", target, err)
			}
			if allowed && bus.address != host+":6379" {
				t.Fatal("original TLS server name lost")
			}
		})
	}
}

func TestProtectedTeamNetworkRefusalPrecedesNativeLaunch(t *testing.T) {
	a, instances, kube, _, pod := securityFixture(t)
	env := `{"CLAWMANAGER_TEAM_ID":"12"}`
	instances.instance.EnvironmentOverridesJSON = &env
	a.teamNetwork = func(context.Context, *models.Instance) error { return errManagedRedis }
	c := NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "test", nil).(*slotRuntimeAgentClient)
	c.security = a
	c.protectTeamPreview = func(context.Context, runtimeauth.Identity) error {
		t.Error("unsafe Team reached later preview/launch stage")
		return nil
	}
	req := RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 3, Generation: 2, PodUID: string(pod.UID), SecurityProfile: RuntimeSecurityProfile, Environment: map[string]string{"CLAWMANAGER_TEAM_ID": "12"}}
	if err := c.protectPublishedGeneration(context.Background(), req); err == nil {
		t.Fatal("unsafe Team permitted native launch")
	}
}

func TestProtectedRedisOpenUsesPinnedTargetAndNeverAuthenticationFallback(t *testing.T) {
	for _, scenario := range []string{"pinned-target", "target-refused", "pinned-proof-lost"} {
		t.Run(scenario, func(t *testing.T) {
			m, id, _ := managedACLFixture(t, []string{"user default on #fake-password ~* +@all"})
			team, err := m.authorize(context.Background(), id, 0)
			if err != nil {
				t.Fatal(err)
			}
			original, err := m.resolve(context.Background(), team)
			if err != nil {
				t.Fatal(err)
			}
			pinned := original.address
			m.networkProtection = func(context.Context, netip.Addr) error {
				if scenario == "pinned-proof-lost" {
					return errManagedRedis
				}
				return nil
			}
			m.protectedTarget = func(context.Context, *redisBus) (string, error) {
				if scenario == "target-refused" {
					return "", errManagedRedis
				}
				return pinned, nil
			}
			if scenario == "pinned-target" {
				copy := *original
				copy.address = "must-not-resolve.invalid:6379"
				m.resolve = func(context.Context, *models.Team) (*redisBus, error) { return &copy, nil }
			}
			session, err := m.Open(context.Background(), id)
			if session != nil {
				defer session.Close()
			}
			if (err == nil) != (scenario == "pinned-target") {
				t.Fatalf("strict Open %s: %v", scenario, err)
			}
		})
	}
}
