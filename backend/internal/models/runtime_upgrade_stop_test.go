package models

import (
	"strings"
	"testing"
	"time"
)

func TestUpgradeStopTargetIdentity(t *testing.T) {
	base := RuntimeUpgradeStop{InstanceID: 1, OwnerID: 2, Generation: 3, Operation: strings.Repeat("a", 32), RuntimePodID: 4, PodUID: "pod", AgentEpoch: strings.Repeat("b", 32), AgentEndpoint: "http://agent:8080", GatewayID: "gw-1-3", PID: 1234}
	if !base.ValidTarget() {
		t.Fatal("valid target rejected")
	}
	for _, change := range []func(*RuntimeUpgradeStop){
		func(s *RuntimeUpgradeStop) { s.InstanceID++ }, func(s *RuntimeUpgradeStop) { s.OwnerID++ },
		func(s *RuntimeUpgradeStop) { s.Generation++ }, func(s *RuntimeUpgradeStop) { s.Operation = strings.Repeat("c", 32) },
		func(s *RuntimeUpgradeStop) { s.RuntimePodID++ }, func(s *RuntimeUpgradeStop) { s.PodUID = "new" },
		func(s *RuntimeUpgradeStop) { s.AgentEpoch = strings.Repeat("d", 32) }, func(s *RuntimeUpgradeStop) { s.AgentEndpoint = "http://other:8080" },
		func(s *RuntimeUpgradeStop) { s.GatewayID = "gw-1-4" }, func(s *RuntimeUpgradeStop) { s.PID++ },
	} {
		other := base
		change(&other)
		if other.SameTarget(base) {
			t.Fatal("target change ignored", other)
		}
	}
	for _, change := range []func(*RuntimeUpgradeStop){
		func(s *RuntimeUpgradeStop) { s.PID = 0 }, func(s *RuntimeUpgradeStop) { s.Generation = -1 },
		func(s *RuntimeUpgradeStop) { s.GatewayID = "gw-1-2" }, func(s *RuntimeUpgradeStop) { s.AgentEpoch = "bad" },
		func(s *RuntimeUpgradeStop) { s.AgentEndpoint = "" }, func(s *RuntimeUpgradeStop) { s.Operation = strings.Repeat("A", 32) },
	} {
		other := base
		change(&other)
		if other.ValidTarget() {
			t.Fatal("invalid target accepted", other)
		}
	}
	now := time.Now()
	other := base
	other.ConfirmedAt = &now
	other.ConfirmationScope = "process_group"
	if !other.SameTarget(base) {
		t.Fatal("receipt changes target identity")
	}
}
