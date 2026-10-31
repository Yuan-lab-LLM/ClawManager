package services

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
)

func TestProtectedSlotTeamPreviewProtectionPrecedesLaunch(t *testing.T) {
	for _, scenario := range []string{"success", "unavailable", "missing-provider", "request-mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			a, instances, kube, _, pod := securityFixture(t)
			// This fixture isolates preview publication ordering. Real Team network
			// authorization and missing-provider denial have their own tests.
			a.teamNetwork = func(context.Context, *models.Instance) error { return nil }
			env := `{"CLAWMANAGER_TEAM_ID":"12"}`
			instances.instance.EnvironmentOverridesJSON = &env
			client := NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "test", &http.Client{}).(*slotRuntimeAgentClient)
			client.security = a
			calls := 0
			if scenario != "missing-provider" {
				client.protectTeamPreview = func(ctx context.Context, id runtimeauth.Identity) error {
					calls++
					if id != (runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: string(pod.UID)}) || instances.instance.RuntimeSecurityProfile != RuntimeSecurityProfile {
						t.Error("preview protection used unpublished identity")
					}
					if scenario == "unavailable" {
						return errors.New("Team provisioning incomplete")
					}
					return nil
				}
			}
			req := RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 3, Generation: 2, PodUID: string(pod.UID), SecurityProfile: RuntimeSecurityProfile, Environment: map[string]string{"CLAWMANAGER_TEAM_ID": "12"}}
			if scenario == "request-mismatch" {
				req.Environment["CLAWMANAGER_TEAM_ID"] = "13"
			}
			err := client.protectPublishedGeneration(context.Background(), req)
			if (err == nil) != (scenario == "success") {
				t.Fatalf("incorrect protection result: %v", err)
			}
			wantCalls := 1
			if scenario == "missing-provider" || scenario == "request-mismatch" {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatalf("already protected retry skipped Team protection: %d", calls)
			}
		})
	}
}
