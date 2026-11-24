package services

import (
	"clawreef/internal/models"
	"context"
	"testing"
)

func TestRuntimeSchedulerUsesPinnedHermesGatewayRequest(t *testing.T) {
	const podUID = "fixture-pod-uid"
	for _, tc := range []struct {
		name, runtimeType, imageRef, upgradeID, securityProfile, wantUID string
	}{
		{name: "pinned Hermes", runtimeType: RuntimeTypeHermes, imageRef: hermesLegacyGatewayImage},
		{name: "mirrored digest", runtimeType: RuntimeTypeHermes, imageRef: "mirror.example/hermes@" + imageDigestFromReference(hermesLegacyGatewayImage)},
		{name: "another Hermes image", runtimeType: RuntimeTypeHermes, imageRef: "example.com/hermes:current", wantUID: podUID},
		{name: "upgrade request", runtimeType: RuntimeTypeHermes, imageRef: hermesLegacyGatewayImage, upgradeID: "fixture-upgrade", wantUID: podUID},
		{name: "security profile stays bound", runtimeType: RuntimeTypeHermes, imageRef: hermesLegacyGatewayImage, securityProfile: "fixture-profile", wantUID: podUID},
		{name: "another runtime", runtimeType: RuntimeTypeOpenClaw, imageRef: hermesLegacyGatewayImage, wantUID: podUID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uid := podUID
			pod := models.RuntimePod{ID: 10, RuntimeType: tc.runtimeType, ImageRef: tc.imageRef, PodUID: &uid}
			instance := models.Instance{ID: 17, UserID: 45, RuntimeGeneration: 1, RuntimeSecurityProfile: tc.securityProfile}
			binding := &models.InstanceRuntimeBinding{InstanceID: instance.ID, RuntimePodID: pod.ID, GatewayPort: RuntimeGatewayPortStart}
			bindingRepo := newFakeRuntimeBindingRepo()
			bindingRepo.bindings[instance.ID] = binding
			agent := &fakeRuntimeAgentClient{createResponse: &RuntimeAgentCreateGatewayResponse{GatewayID: "fixture", Status: "starting"}}
			scheduler := &RuntimeScheduler{instanceRepo: newFakeRuntimeInstanceRepo(), bindingRepo: bindingRepo, agentClient: agent}
			start := &runtimeGatewayStart{
				endpoint: "http://agent.runtime", workspacePath: "/workspaces/fixture", upgradeID: tc.upgradeID, reservedBinding: binding,
			}
			if err := scheduler.createGatewayOnPod(context.Background(), instance, tc.runtimeType, pod, start); err != nil {
				t.Fatal(err)
			}
			if len(agent.createRequests) != 1 || agent.createRequests[0].req.PodUID != tc.wantUID || agent.createRequests[0].req.SecurityProfile != tc.securityProfile {
				t.Fatalf("gateway request did not use the expected Pod UID %q", tc.wantUID)
			}
		})
	}
}
