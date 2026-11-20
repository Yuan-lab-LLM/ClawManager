package models

import (
	"testing"
	"time"
)

func TestRuntimeCurrentReportBootstrap(t *testing.T) {
	now := time.Now()
	valid := RuntimeProtocolState{InstanceID: 8, OwnerUserID: 7, Generation: 3, Protocol: 1, RuntimeVersion: "native.1", AdapterVersion: "adapter.1", ReportedAt: now, CapabilitiesJSON: `["task_identity","turn_cancel","cancel_receipt","live_catalog"]`}
	if !valid.CurrentReport(8, 7, 3, now) {
		t.Fatal("valid report rejected")
	}
	if (*RuntimeProtocolState)(nil).CurrentReport(8, 7, 3, now) {
		t.Fatal("missing report accepted")
	}
	for _, change := range []func(*RuntimeProtocolState){
		func(s *RuntimeProtocolState) { s.InstanceID++ }, func(s *RuntimeProtocolState) { s.OwnerUserID++ }, func(s *RuntimeProtocolState) { s.Generation++ },
		func(s *RuntimeProtocolState) { s.Protocol = 2 }, func(s *RuntimeProtocolState) { s.CapabilitiesJSON = "null" }, func(s *RuntimeProtocolState) { s.CapabilitiesJSON = "invalid" },
		func(s *RuntimeProtocolState) { s.ReportedAt = now.Add(time.Second) }, func(s *RuntimeProtocolState) { s.ReportedAt = now.Add(-RuntimeCapabilityReportTTL) },
	} {
		s := valid
		change(&s)
		if s.CurrentReport(8, 7, 3, now) {
			t.Fatal("invalid report accepted")
		}
	}
	if !valid.CurrentReport(8, 7, 3, now.Add(RuntimeCapabilityReportTTL-time.Nanosecond)) {
		t.Fatal("TTL boundary incorrect")
	}
}
