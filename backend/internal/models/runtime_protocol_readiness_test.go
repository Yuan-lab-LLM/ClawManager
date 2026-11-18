package models

import (
	"testing"
	"time"
)

func TestRuntimeProtocolReadinessRequiresCompleteCurrentReport(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	valid := RuntimeProtocolState{Protocol: 1, OwnerUserID: 7, Generation: 3, RuntimeVersion: "2026.10.2", AdapterVersion: "cm.1", ReportedAt: now}
	instance := Instance{UserID: 7, RuntimeGeneration: 3, Status: "running"}
	caps := []string{"task_identity", "turn_cancel", "cancel_receipt", "live_catalog"}
	for _, tc := range []struct {
		name   string
		change func(*RuntimeProtocolState, *Instance, *[]string)
		reason string
	}{
		{"valid", func(*RuntimeProtocolState, *Instance, *[]string) {}, "connected"},
		{"future protocol", func(s *RuntimeProtocolState, _ *Instance, _ *[]string) { s.Protocol = 2 }, "unsupported_protocol"},
		{"owner changed", func(s *RuntimeProtocolState, _ *Instance, _ *[]string) { s.OwnerUserID = 8 }, "owner_changed"},
		{"generation changed", func(s *RuntimeProtocolState, _ *Instance, _ *[]string) { s.Generation = 2 }, "generation_changed"},
		{"stopped", func(_ *RuntimeProtocolState, i *Instance, _ *[]string) { i.Status = "stopped" }, "runtime_not_running"},
		{"clock skew", func(s *RuntimeProtocolState, _ *Instance, _ *[]string) { s.ReportedAt = now.Add(time.Nanosecond) }, "report_clock_skew"},
		{"expiry boundary", func(s *RuntimeProtocolState, _ *Instance, _ *[]string) { s.ReportedAt = now.Add(-30 * time.Second) }, "report_expired"},
		{"before expiry", func(s *RuntimeProtocolState, _ *Instance, _ *[]string) {
			s.ReportedAt = now.Add(-30*time.Second + time.Nanosecond)
		}, "connected"},
		{"empty capabilities", func(_ *RuntimeProtocolState, _ *Instance, c *[]string) { *c = nil }, "invalid_report"},
		{"missing cancellation", func(_ *RuntimeProtocolState, _ *Instance, c *[]string) {
			*c = []string{"task_identity", "cancel_receipt", "live_catalog"}
		}, "invalid_report"},
		{"unknown capability", func(_ *RuntimeProtocolState, _ *Instance, c *[]string) { *c = append(*c, "unverified") }, "invalid_report"},
		{"duplicate capability", func(_ *RuntimeProtocolState, _ *Instance, c *[]string) { *c = append(*c, "turn_cancel") }, "invalid_report"},
		{"missing version", func(s *RuntimeProtocolState, _ *Instance, _ *[]string) { s.RuntimeVersion = "" }, "invalid_report"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, i, c := valid, instance, append([]string(nil), caps...)
			tc.change(&s, &i, &c)
			mode, reason := s.Readiness(&i, c, now)
			want := "awaiting_runtime"
			if tc.reason == "connected" {
				want = "governed"
			}
			if mode != want || reason != tc.reason {
				t.Fatalf("got %s/%s want %s/%s", mode, reason, want, tc.reason)
			}
		})
	}
	var legacy *RuntimeProtocolState
	if mode, reason := legacy.Readiness(&instance, nil, now); mode != "legacy" || reason != "not_enrolled" {
		t.Fatalf("legacy: %s/%s", mode, reason)
	}
}
