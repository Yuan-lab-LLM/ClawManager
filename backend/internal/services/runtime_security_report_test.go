package services

import (
	"clawreef/internal/runtimeauth"
	"context"
	"encoding/json"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestVerifyReportedGatewayFreshExactAssignment(t *testing.T) {
	for _, scenario := range []string{"valid", "pod-uid", "port", "workspace", "generation", "stopped", "draining", "gateway-id", "runtime", "unready", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			a, instances, kube, cm, pod := securityFixture(t)
			instances.instance.Status = "creating"
			var request RuntimeAgentCreateGatewayRequest
			_ = json.Unmarshal([]byte(cm.Data["request"]), &request)
			request.WorkspacePath = "/workspaces/opencode/user-3/instance-7"
			cm.Data["gateway_id"] = "gw-7-2"
			id := runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod-7"}
			workspace := request.WorkspacePath
			port := request.GatewayPort
			kind := "opencode"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "pod-uid":
				id.PodUID = "other"
			case "port":
				port++
			case "workspace":
				workspace += "/other"
			case "generation":
				id.Generation++
			case "stopped":
				instances.instance.Status = "stopped"
			case "draining":
				cm.Data["desired_state"] = "draining"
			case "gateway-id":
				cm.Data["gateway_id"] = "pending-7-2"
			case "runtime":
				kind = "hermes"
			case "unready":
				pod.Status.Conditions = nil
			case "canceled":
				cancel()
			}
			raw, _ := json.Marshal(request)
			cm.Data["request"] = string(raw)
			_, _ = kube.CoreV1().ConfigMaps("test").Update(context.Background(), cm, metav1.UpdateOptions{})
			_, _ = kube.CoreV1().Pods("test").Update(context.Background(), pod, metav1.UpdateOptions{})
			err := a.VerifyReportedGateway(ctx, id, kind, workspace, port)
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("authorization=%v", err)
			}
		})
	}
}
