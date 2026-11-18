package services

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"clawreef/internal/runtimeauth"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
)

func TestRuntimeSecurityLeaseRejectsWrongPeerAndIdentity(t *testing.T) {
	// Existing identity fixture is shared with capability authority tests.
	a, _, _, _, _ := securityFixture(t)
	identity, err := a.CurrentIdentity(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.IssueLease(context.Background(), identity, netip.MustParseAddr("10.2.3.99")); err == nil {
		t.Fatal("accepted another source Pod")
	}
	wrong := identity
	wrong.Generation++
	if _, err = a.IssueLease(context.Background(), wrong, netip.MustParseAddr("10.2.0.3")); err == nil {
		t.Fatal("accepted wrong generation")
	}
}

func TestRuntimeSecurityLeasePeerNeverUsesUncheckedAssignmentRead(t *testing.T) {
	a, _, kube, cm, _ := securityFixture(t)
	reads := 0
	kube.PrependReactor("get", "configmaps", func(ktesting.Action) (bool, runtime.Object, error) {
		reads++
		observed := cm.DeepCopy()
		if reads == 2 {
			observed.Data["endpoint"] = "https://10.2.0.99:19090"
		}
		return true, observed, nil
	})
	identity := runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod-7"}
	lease, err := a.IssueLease(context.Background(), identity, netip.MustParseAddr("10.2.0.99"))
	if err == nil || lease.ModelToken != "" || lease.EgressToken != "" || lease.ServicesToken != "" {
		t.Fatalf("unchecked endpoint authorized wrong peer after %d reads: %v", reads, err)
	}
}

func TestRuntimeSecurityLeaseIssuesSeparatedCapabilities(t *testing.T) {
	a, _, _, _, _ := securityFixture(t)
	identity, err := a.CurrentIdentity(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := a.IssueLease(context.Background(), identity, netip.MustParseAddr("10.2.0.3"))
	if err != nil {
		t.Fatal(err)
	}
	if lease.Identity() != identity || !lease.ExpiresAt.After(time.Now().Add(14*time.Minute)) || lease.ExpiresAt.After(time.Now().Add(15*time.Minute)) {
		t.Fatal("invalid lease identity or expiry")
	}
	for audience, token := range map[string]string{runtimeauth.AudienceModel: lease.ModelToken, runtimeauth.AudienceEgress: lease.EgressToken, runtimeauth.AudienceServices: lease.ServicesToken} {
		got, err := a.Capabilities().Verify(context.Background(), token, audience)
		if err != nil || got != identity {
			t.Fatalf("%s: %v", audience, err)
		}
	}
}
