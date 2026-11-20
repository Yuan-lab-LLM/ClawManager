package handlers

import (
	"clawreef/internal/models"
	"encoding/json"
	"testing"
	"time"
)

func TestRuntimeInspectCurrentReportIsFreshAndIdentityBound(t *testing.T) {
	valid := models.RuntimeProtocolState{InstanceID: 8, OwnerUserID: 7, Generation: 3, Protocol: 1, RuntimeVersion: "2026.10.2", AdapterVersion: "cm-openclaw-native-1", ReportedAt: time.Now(), CapabilitiesJSON: `["task_identity","turn_cancel","cancel_receipt","live_catalog"]`}
	for _, kind := range []string{"valid", "expired", "owner", "generation", "corrupt"} {
		state := valid
		switch kind {
		case "expired":
			state.ReportedAt = time.Now().Add(-time.Minute)
		case "owner":
			state.OwnerUserID++
		case "generation":
			state.Generation++
		case "corrupt":
			state.CapabilitiesJSON = "null"
		}
		store := &protocolTestStore{state: &state}
		c, w := renewalContext("")
		NewRuntimeProtocolHandler(store, nil).Inspect(c)
		var body struct {
			Report *struct {
				Runtime string `json:"runtime_version"`
			} `json:"current_report"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil {
			t.Fatal(w.Body.String())
		}
		if (body.Report != nil) != (kind == "valid") {
			t.Fatalf("incorrect readiness %s", kind)
		}
		if store.instanceID != 0 || store.ownerID != 0 {
			t.Fatal("readiness mutated enrollment")
		}
	}
}
