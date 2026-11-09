package models

import "testing"

func TestRuntimeCapabilityReportValidation(t *testing.T) {
	valid := RuntimeCapabilityReport{Protocol: 1, Generation: 0, RuntimeVersion: "2.0.27", AdapterVersion: "cm.1",
		Capabilities: []string{"task_identity", "turn_cancel", "cancel_receipt", "live_catalog"}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*RuntimeCapabilityReport){
		"downgrade":       func(r *RuntimeCapabilityReport) { r.Protocol = 0 },
		"future":          func(r *RuntimeCapabilityReport) { r.Protocol = 2 },
		"stale":           func(r *RuntimeCapabilityReport) { r.Generation = -1 },
		"missing":         func(r *RuntimeCapabilityReport) { r.Capabilities = []string{"task_identity"} },
		"duplicate":       func(r *RuntimeCapabilityReport) { r.Capabilities = append(r.Capabilities, "task_identity") },
		"unknown":         func(r *RuntimeCapabilityReport) { r.Capabilities = append(r.Capabilities, "magic") },
		"invalid version": func(r *RuntimeCapabilityReport) { r.AdapterVersion = "bad\nversion" },
	} {
		t.Run(name, func(t *testing.T) {
			r := valid
			r.Capabilities = append([]string{}, valid.Capabilities...)
			change(&r)
			if r.Validate() == nil {
				t.Fatal("invalid report accepted")
			}
		})
	}
}
