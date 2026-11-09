package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSlotClientCancelsPublishedButUnclaimedGateway(t *testing.T) {
	for _, scenario := range []string{"confirmed", "unsupported", "uncertain", "wrong-receipt", "wrong-pod", "changed-request"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			kube := fake.NewSimpleClientset(slotTestPod())
			cancelCalls := 0
			httpClient := &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				status, body := 409, `unknown slot writer`
				if r.Method == http.MethodPost && r.URL.Path == "/v1/gateways" {
					return nil, io.ErrUnexpectedEOF
				}
				if r.Method == http.MethodPost && r.URL.Path == "/v1/gateways/cancel-pending" {
					cancelCalls++
					cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
					if cm.Data["desired_state"] != "draining" {
						t.Fatal("cancel preceded durable draining intent")
					}
					var scope map[string]any
					if json.NewDecoder(r.Body).Decode(&scope) != nil || len(scope) != 6 || scope["instance_id"] != float64(7) || scope["user_id"] != float64(2) || scope["generation"] != float64(1) || scope["pod_uid"] != "pod-uid" || scope["agent_type"] != "openclaw" || scope["gateway_port"] != float64(20000) {
						t.Fatalf("cancel scope changed: %v", scope)
					}
					status, body = 200, `{"gateway_id":"gw-7-1","stopped":true,"confirmed":true,"unmounted":true}`
					switch scenario {
					case "unsupported":
						status, body = 404, "unsupported"
					case "uncertain":
						status, body = 409, "writer remains active"
					case "wrong-receipt":
						body = `{"gateway_id":"gw-8-1","stopped":true,"confirmed":true,"unmounted":true}`
					}
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			client := NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "runtime", httpClient)
			_, _ = client.CreateGateway(ctx, "https://10.0.0.2:19090", RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw", GatewayPort: 20000, Environment: map[string]string{"SECRET": "never-send-on-cancel"}})
			if scenario == "wrong-pod" {
				p, _ := kube.CoreV1().Pods("runtime").Get(ctx, "pool", metav1.GetOptions{})
				p.UID = "replacement"
				_, _ = kube.CoreV1().Pods("runtime").Update(ctx, p, metav1.UpdateOptions{})
			}
			if scenario == "changed-request" {
				cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
				cm.Data["request"] = strings.Replace(cm.Data["request"], `"user_id":2`, `"user_id":3`, 1)
				_, _ = kube.CoreV1().ConfigMaps("runtime").Update(ctx, cm, metav1.UpdateOptions{})
			}
			err := client.DeleteGateway(ctx, "https://10.0.0.2:19090", "pending-7-1")
			if (err == nil) != (scenario == "confirmed") {
				t.Fatalf("cancel outcome: %v", err)
			}
			wantCalls := 1
			if scenario == "wrong-pod" || scenario == "changed-request" {
				wantCalls = 0
			}
			if cancelCalls != wantCalls {
				t.Fatalf("cancel calls=%d want=%d", cancelCalls, wantCalls)
			}
			cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
			if (cm.Data["desired_state"] == "stopped") != (scenario == "confirmed") {
				t.Fatal("unconfirmed cancellation released assignment")
			}
		})
	}
}
