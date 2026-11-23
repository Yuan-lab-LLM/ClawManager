package services

import (
	"context"
	"encoding/json"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"net/http"
	"strings"
	"testing"
)

func TestIdlePreflightObservesExactPublishedPendingIdentity(t *testing.T) {
	for _, scenario := range []string{"published", "unclaimed", "preflight-conflict-live", "missing", "wrong-generation", "wrong-endpoint", "wrong-pod", "wrong-request", "wrong-receipt"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			kube := fake.NewSimpleClientset(slotTestPod())
			calls := 0
			client := NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/v1/gateways" && r.Method == http.MethodPost {
					return nil, io.ErrUnexpectedEOF
				}
				calls++
				if r.Method == http.MethodGet && r.URL.Path == "/v1/gateways/gw-7-1" {
					status, body := 404, `not found`
					if scenario == "preflight-conflict-live" {
						status, body = 200, `{"gateway_id":"gw-7-1","instance_id":7,"generation":1,"state":"running"}`
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
				}
				if r.Method != http.MethodPost || r.URL.Path != "/v1/gateways/gw-7-1/idle-preflight" {
					t.Fatalf("preflight changed into a lifecycle action: %s %s", r.Method, r.URL.Path)
				}
				var req RuntimeIdleStopRequest
				if json.NewDecoder(r.Body).Decode(&req) != nil || req.InstanceID != 7 || req.Generation != 1 || req.PodUID != "pod-uid" {
					t.Fatalf("observation identity changed: %+v", req)
				}
				body := `{"gateway_id":"gw-7-1","instance_id":7,"generation":1,"pod_uid":"pod-uid","execution_state":"idle","execution_proof_supported":true}`
				if scenario == "wrong-receipt" {
					body = strings.Replace(body, `"generation":1`, `"generation":2`, 1)
				}
				if scenario == "unclaimed" || scenario == "preflight-conflict-live" {
					return &http.Response{StatusCode: 409, Body: io.NopCloser(strings.NewReader("idle-stop source is unknown"))}, nil
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})})
			_, _ = client.CreateGateway(ctx, "https://10.0.0.2:19090", RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw", GatewayPort: 20000})
			endpoint, pending := "https://10.0.0.2:19090", "pending-7-1"
			switch scenario {
			case "missing":
				_ = kube.CoreV1().ConfigMaps("runtime").Delete(ctx, "lite-instance-7", metav1.DeleteOptions{})
			case "wrong-generation":
				pending = "pending-7-2"
			case "wrong-endpoint":
				endpoint = "https://10.0.0.3:19090"
			case "wrong-pod":
				p, _ := kube.CoreV1().Pods("runtime").Get(ctx, "pool", metav1.GetOptions{})
				p.UID = "replacement"
				_, _ = kube.CoreV1().Pods("runtime").Update(ctx, p, metav1.UpdateOptions{})
			case "wrong-request":
				cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
				cm.Data["request"] = strings.Replace(cm.Data["request"], `"user_id":2`, `"user_id":3`, 1)
				_, _ = kube.CoreV1().ConfigMaps("runtime").Update(ctx, cm, metav1.UpdateOptions{})
			}
			proof, err := client.(RuntimeIdleStopClient).IdlePreflight(ctx, endpoint, pending, RuntimeIdleStopRequest{InstanceID: 7, Generation: 1, PodUID: "pod-uid"})
			if (err == nil) != (scenario == "published" || scenario == "unclaimed") {
				t.Fatalf("proof=%+v err=%v", proof, err)
			}
			wantCalls := 0
			if scenario == "published" || scenario == "wrong-receipt" {
				wantCalls = 1
			}
			if scenario == "unclaimed" || scenario == "preflight-conflict-live" {
				wantCalls = 2
			}
			if scenario == "unclaimed" && (proof.ExecutionState != "unknown" || proof.ExecutionProofSupported || teamStopPreflightAllows("idle_timeout", "creating", proof) || teamStopPreflightAllows("manual", "running", proof) || !teamStopPreflightAllows("manual", "creating", proof)) {
				t.Fatal("unclaimed observation granted execution/stop proof")
			}
			if calls != wantCalls {
				t.Fatalf("calls=%d want=%d", calls, wantCalls)
			}
			if cm, e := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{}); e == nil && cm.Data["desired_state"] != "running" {
				t.Fatal("read-only preflight changed assignment intent")
			}
		})
	}
}

func TestIdleStopRejectsUnconfirmedOrWrongIdentityWithoutDeleteFallback(t *testing.T) {
	for _, body := range []string{
		`{"gateway_id":"gw-7-1","instance_id":7,"generation":1,"pod_uid":"pod-uid","stopped":true,"confirmed":true,"unmounted":true,"graceful":false}`,
		`{"gateway_id":"gw-7-1","instance_id":7,"generation":2,"pod_uid":"pod-uid","stopped":true,"confirmed":true,"unmounted":true,"graceful":true}`,
	} {
		calls := 0
		client := NewSlotRuntimeAgentClientWithHTTPClient("token", fake.NewSimpleClientset(slotTestPod()), "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.Method != "POST" || r.URL.Path != "/v1/gateways/gw-7-1/idle-stop" {
				t.Fatalf("unsafe fallback %s %s", r.Method, r.URL.Path)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}).(RuntimeIdleStopClient)
		if err := client.IdleStop(context.Background(), "https://10.0.0.2:19090", "gw-7-1", RuntimeIdleStopRequest{InstanceID: 7, Generation: 1, PodUID: "pod-uid"}); err == nil {
			t.Fatal("accepted invalid receipt")
		}
		if calls != 1 {
			t.Fatalf("calls=%d", calls)
		}
	}
}
