package models

import (
	"encoding/json"
	"testing"
)

func TestRuntimeCapabilityRenewalMatchesExactContract(t *testing.T) {
	report := RuntimeCapabilityReport{Protocol: 1, Generation: 3, RuntimeVersion: "native.1", AdapterVersion: "adapter.1", Capabilities: RequiredRuntimeCapabilities()}
	raw, _ := json.Marshal(report.Capabilities)
	state := RuntimeProtocolState{OwnerUserID: 7, Generation: 3, Protocol: 1, RuntimeVersion: report.RuntimeVersion, AdapterVersion: report.AdapterVersion, CapabilitiesJSON: string(raw)}
	if !state.MatchesReport(7, report) {
		t.Fatal("identical report rejected")
	}
	reordered := report
	reordered.Capabilities = []string{"live_catalog", "cancel_receipt", "turn_cancel", "task_identity"}
	if !state.MatchesReport(7, reordered) {
		t.Fatal("capability order changed contract")
	}
	for _, name := range []string{"owner", "generation", "protocol", "runtime", "adapter", "capabilities", "corrupt", "duplicate", "missing"} {
		t.Run(name, func(t *testing.T) {
			s := state
			switch name {
			case "owner":
				s.OwnerUserID++
			case "generation":
				s.Generation++
			case "protocol":
				s.Protocol++
			case "runtime":
				s.RuntimeVersion += ".new"
			case "adapter":
				s.AdapterVersion += ".new"
			case "capabilities":
				s.CapabilitiesJSON = `["task_identity","turn_cancel","cancel_receipt","live_catalog","maintenance_drain"]`
			case "corrupt":
				s.CapabilitiesJSON = "invalid"
			case "duplicate":
				s.CapabilitiesJSON = `["task_identity","turn_cancel","cancel_receipt","live_catalog","live_catalog"]`
			case "missing":
				s.CapabilitiesJSON = "[]"
			}
			if s.MatchesReport(7, report) {
				t.Fatal("changed contract accepted")
			}
		})
	}
	var legacy *RuntimeProtocolState
	if legacy.MatchesReport(7, report) {
		t.Fatal("legacy instance renewed")
	}
	large := int64(2147483648)
	report.Generation = int(large)
	if report.Validate() == nil {
		t.Fatal("generation exceeds database INT range")
	}
}
