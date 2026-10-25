package services

import "testing"

func TestHermesWebManagedDefaults(t *testing.T) {
	if got := HermesControlUIOrigin("tenant-system", ""); got != "http://clawmanager-gateway.tenant-system.svc.cluster.local:9001" {
		t.Fatal(got)
	}
	if got := HermesControlUIOrigin("tenant-system", "https://explicit.internal"); got != "https://explicit.internal" {
		t.Fatal(got)
	}
	if HermesControlUIOrigin("", "") != "" {
		t.Fatal("invented namespace")
	}
	if !HermesWebEnabled("") || !HermesWebEnabled("true") || HermesWebEnabled("false") || HermesWebEnabled("invalid") {
		t.Fatal("incorrect enable policy")
	}
}
