package services

import (
	"clawreef/internal/models"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestModelCatalogSyncRequiresCurrentIdentityAndRuntimeReceipt(t *testing.T) {
	instance := &models.Instance{ID: 7, UserID: 2, RuntimeGeneration: 3, Status: "running"}
	state := &RuntimeAgentGatewayState{GatewayID: "gw-7", InstanceID: 7, Generation: 3}
	if got := validatedModelCatalogSync(instance, state, "gw-7"); got.State != "unknown" || got.Reason != "not_reported" {
		t.Fatal("legacy runtime falsely marked synced", got)
	}
	state.ModelCatalogSync = &RuntimeModelCatalogSyncReport{State: "applied", OwnerUserID: 2, Generation: 3, Revision: strings.Repeat("a", 64), ModelCount: 4, ObservedAt: time.Now().UTC()}
	if got := validatedModelCatalogSync(instance, state, "gw-7"); got.State != "applied" || got.ModelCount != 4 {
		t.Fatal("valid receipt rejected", got)
	}
	for _, mutate := range []func(*RuntimeAgentGatewayState){func(s *RuntimeAgentGatewayState) { s.Generation = 2 }, func(s *RuntimeAgentGatewayState) { s.GatewayID = "another" }, func(s *RuntimeAgentGatewayState) { s.InstanceID = 8 }, func(s *RuntimeAgentGatewayState) { s.ModelCatalogSync.OwnerUserID = 5 }, func(s *RuntimeAgentGatewayState) { s.ModelCatalogSync.Generation = 2 }} {
		clone := *state
		report := *state.ModelCatalogSync
		clone.ModelCatalogSync = &report
		mutate(&clone)
		if got := validatedModelCatalogSync(instance, &clone, "gw-7"); got.State != "unknown" || got.Reason != "identity_mismatch" {
			t.Fatal("stale identity accepted", got)
		}
	}
}

func TestModelCatalogSyncDoesNotExposePrivateFailureMetadata(t *testing.T) {
	instance := &models.Instance{ID: 7, UserID: 2, RuntimeGeneration: 3}
	state := &RuntimeAgentGatewayState{GatewayID: "gw", InstanceID: 7, Generation: 3, ModelCatalogSync: &RuntimeModelCatalogSyncReport{State: "failed", OwnerUserID: 2, Generation: 3, Revision: "private-token-or-runtime-error", ModelCount: 3, ObservedAt: time.Now()}}
	got := validatedModelCatalogSync(instance, state, "gw")
	if got.State != "failed" || got.Revision != "" || got.ModelCount != 0 {
		t.Fatal("private failure details leaked", got)
	}
	state.ModelCatalogSync.State = "empty"
	state.ModelCatalogSync.Revision = strings.Repeat("a", 64)
	if got := validatedModelCatalogSync(instance, state, "gw"); got.State != "unknown" {
		t.Fatal("empty report contained models", got)
	}
}

func TestRuntimeGatewayStateDoesNotMarshalPrivateCatalogMetadata(t *testing.T) {
	var state RuntimeAgentGatewayState
	if err := json.Unmarshal([]byte(`{"gateway_id":"gw","model_catalog_sync":{"state":"failed","revision":"private-secret","model_count":4}}`), &state); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil || strings.Contains(string(raw), "private-secret") {
		t.Fatal("private catalog metadata entered inspection JSON", err)
	}
}
