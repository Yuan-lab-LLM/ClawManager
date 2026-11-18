package models

import (
	"strings"
	"testing"
	"time"
)

func TestEnrollmentRevisionBindsContractNotHeartbeat(t *testing.T) {
	s := RuntimeProtocolState{InstanceID: 1, OwnerUserID: 2, Generation: 3, Protocol: 1, RuntimeVersion: "native.1", AdapterVersion: "adapter.1", CapabilitiesJSON: `["task_identity","turn_cancel","cancel_receipt","live_catalog"]`}
	original, err := s.EnrollmentRevision()
	if err != nil || ValidateEnrollmentRevision(&original) != nil {
		t.Fatalf("revision: %q %v", original, err)
	}
	s.ReportedAt = time.Now()
	s.CapabilitiesJSON = `["live_catalog","cancel_receipt","turn_cancel","task_identity"]`
	if got, err := s.EnrollmentRevision(); err != nil || got != original {
		t.Fatal("freshness or capability ordering changed revision")
	}
	for _, change := range []func(*RuntimeProtocolState){
		func(s *RuntimeProtocolState) { s.InstanceID++ }, func(s *RuntimeProtocolState) { s.OwnerUserID++ },
		func(s *RuntimeProtocolState) { s.Generation++ }, func(s *RuntimeProtocolState) { s.RuntimeVersion += "x" },
		func(s *RuntimeProtocolState) { s.AdapterVersion += "x" },
		func(s *RuntimeProtocolState) {
			s.CapabilitiesJSON = `["live_catalog","cancel_receipt","turn_cancel","task_identity","maintenance_drain"]`
		},
	} {
		changed := s
		change(&changed)
		if got, err := changed.EnrollmentRevision(); err != nil || got == original {
			t.Fatal("changed contract retained revision")
		}
	}
	for _, change := range []func(*RuntimeProtocolState){
		func(s *RuntimeProtocolState) { s.Protocol = 2 }, func(s *RuntimeProtocolState) { s.Generation = -1 },
		func(s *RuntimeProtocolState) { s.OwnerUserID = 0 }, func(s *RuntimeProtocolState) { s.InstanceID = 0 },
		func(s *RuntimeProtocolState) { s.CapabilitiesJSON = `null` }, func(s *RuntimeProtocolState) { s.CapabilitiesJSON = `invalid` },
	} {
		changed := s
		change(&changed)
		if _, err := changed.EnrollmentRevision(); err == nil {
			t.Fatal("invalid contract has revision")
		}
	}
	if _, err := (*RuntimeProtocolState)(nil).EnrollmentRevision(); err == nil {
		t.Fatal("nil state has revision")
	}
	for _, value := range []string{"", strings.Repeat("A", 64), strings.Repeat("g", 64), strings.Repeat("a", 63), strings.Repeat("a", 65)} {
		if ValidateEnrollmentRevision(&value) == nil {
			t.Fatal("invalid revision accepted")
		}
	}
	if ValidateEnrollmentRevision(nil) != nil {
		t.Fatal("explicit first enrollment rejected")
	}
}
