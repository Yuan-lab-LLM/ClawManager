package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSlotCrossPodHandoverRequiresConfirmedSourceAndRejectsDelayedRequests(t *testing.T) {
	source, target := slotTestPod(), slotTestPod()
	target.Name, target.UID, target.Status.PodIP = "target", types.UID("target-uid"), "10.0.0.3"
	kube := fake.NewSimpleClientset(source, target)
	stopConfirmed, targetCreates := false, 0
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		body := `{"gateway_id":"gw-7-1","state":"starting","gateway_port":20000}`
		if r.Method == "DELETE" {
			if !stopConfirmed {
				return nil, fmt.Errorf("source writer stop is unavailable")
			}
			body = `{"gateway_id":"gw-7-1","stopped":true,"confirmed":true,"unmounted":true}`
		} else if r.URL.Hostname() == "10.0.0.3" {
			targetCreates++
			var req RuntimeAgentCreateGatewayRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.PodUID != "target-uid" || req.Generation != 2 {
				t.Fatal("handover lost target identity")
			}
			body = `{"gateway_id":"gw-7-2","state":"starting","gateway_port":20000}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	ctx := context.Background()
	a := RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw", GatewayPort: 20000}
	b := a
	b.Generation = 2
	if _, err := client.CreateGateway(ctx, "https://10.0.0.2:19090", a); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateGateway(ctx, "https://10.0.0.3:19090", b); err == nil {
		t.Fatal("target started before source stopped")
	}
	if err := client.DeleteGateway(ctx, "https://10.0.0.2:19090", "gw-7-1"); err == nil {
		t.Fatal("unreachable source was treated as stopped")
	}
	if _, err := client.CreateGateway(ctx, "https://10.0.0.3:19090", b); err == nil {
		t.Fatal("draining source authorized handover")
	}
	if targetCreates != 0 {
		t.Fatal("unsafe target reached HTTP")
	}
	stopConfirmed = true
	if err := client.DeleteGateway(ctx, "https://10.0.0.2:19090", "gw-7-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateGateway(ctx, "https://10.0.0.3:19090", b); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateGateway(ctx, "https://10.0.0.2:19090", a); err == nil {
		t.Fatal("delayed old create resurrected source")
	}
	if err := client.DeleteGateway(ctx, "https://10.0.0.2:19090", "gw-7-1"); err == nil {
		t.Fatal("delayed old stop drained target generation")
	}
	cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
	if cm.Data["generation"] != "2" || cm.Data["desired_state"] != "running" || cm.Data["pod_uid"] != "target-uid" || targetCreates != 1 {
		t.Fatalf("handover corrupted target: %+v creates=%d", cm.Data, targetCreates)
	}
}

func TestSlotClientPreservesUpgradeTimeoutWithAuthenticatedTransport(t *testing.T) {
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", fake.NewSimpleClientset(), "runtime", &http.Client{Timeout: runtimeAgentControlTimeout}).(*slotRuntimeAgentClient)
	if client.httpClient.Timeout != runtimeAgentControlTimeout || client.upgradeHTTPClient.Timeout != runtimeAgentUpgradeTimeout {
		t.Fatal("long native upgrade deadline lost when injecting TLS transport")
	}
	custom := NewSlotRuntimeAgentClientWithHTTPClient("token", fake.NewSimpleClientset(), "runtime", &http.Client{Timeout: time.Second}).(*slotRuntimeAgentClient)
	if custom.upgradeHTTPClient.Timeout != time.Second {
		t.Fatal("explicit custom deadline overridden")
	}
}

func TestSlotManagementNeverSendsCredentialsOverPlaintextOrRedirects(t *testing.T) {
	calls := 0
	client := NewSlotRuntimeAgentClientWithHTTPClient("private-token", fake.NewSimpleClientset(slotTestPod()), "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://10.0.0.9:19090/v1/health"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})})
	if err := client.Health(context.Background(), "http://10.0.0.2:19090"); err == nil {
		t.Fatal("plaintext management allowed")
	}
	if err := client.Drain(context.Background(), "http://10.0.0.2:19090"); err == nil {
		t.Fatal("inherited control operation allowed plaintext")
	}
	if calls != 0 {
		t.Fatal("control credential reached plaintext transport")
	}
	if err := client.Health(context.Background(), "https://10.0.0.2:19090"); err == nil {
		t.Fatal("management redirect accepted")
	}
	if calls != 1 {
		t.Fatal("control credential forwarded to redirect target")
	}
}
