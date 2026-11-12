package runtimeinventory

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"net/netip"
	"testing"
	"time"
)

func TestExplicitLegacyServiceRangesNeverBypassOtherFreshness(t *testing.T) {
	i := readyInventory(t)
	k := kubefake.NewSimpleClientset()
	if err := i.configureLegacyServiceRanges(k, []string{"10.96.0.0/12"}); err != nil {
		t.Fatal(err)
	}
	i.services.unhealthy()
	// The topology is immutable site configuration, not a synthetic watch receipt.
	if err := i.checkHealthy(context.Background()); err != nil {
		t.Fatal(err)
	}
	i.nodes.unhealthy()
	if err := i.checkHealthy(context.Background()); err == nil {
		t.Fatal("stale Node proof admitted")
	}
	i.nodes.healthy(time.Now())
	if i.legacyServiceRanges[0] != netip.MustParsePrefix("10.96.0.0/12") {
		t.Fatal("wrong range")
	}
	for _, v := range []string{"0.0.0.0/0", "10.96.0.1/12", "garbage"} {
		if err := i.configureLegacyServiceRanges(k, []string{v}); err == nil {
			t.Fatal(v)
		}
	}
	k.Discovery().(*fake.FakeDiscovery).Resources = []*metav1.APIResourceList{{GroupVersion: "networking.k8s.io/v1", APIResources: []metav1.APIResource{{Name: "servicecidrs"}}}}
	if err := i.configureLegacyServiceRanges(k, []string{"10.96.0.0/12"}); err == nil {
		t.Fatal("replaced available authority")
	}
}
