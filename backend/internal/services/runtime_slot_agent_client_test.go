package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

type slotRoundTripper func(*http.Request) (*http.Response, error)

func (f slotRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func slotTestPod() *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "runtime", UID: types.UID("pod-uid"), Labels: map[string]string{"clawmanager.io/runtime-protocol": "container-slots-v1"}}, Status: corev1.PodStatus{PodIP: "10.0.0.2", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
}

func TestSlotClientStopsPublishedAssignmentAfterLostCreateResponse(t *testing.T) {
	for _, test := range []struct {
		name, endpoint, id  string
		unmounted, wantStop bool
	}{
		{"matching receipt", "https://10.0.0.2:19090", "pending-7-1", true, true},
		{"missing unmount", "https://10.0.0.2:19090", "pending-7-1", false, false},
		{"wrong endpoint", "https://10.0.0.3:19090", "pending-7-1", true, false},
		{"wrong generation", "https://10.0.0.2:19090", "pending-7-2", true, false},
		{"missing assignment", "https://10.0.0.2:19090", "pending-8-1", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			kube := fake.NewSimpleClientset(slotTestPod())
			httpClient := &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodPost {
					return nil, io.ErrUnexpectedEOF
				}
				if r.URL.Path != "/v1/gateways/gw-7-1" {
					t.Fatalf("wrong stop identity: %s", r.URL.Path)
				}
				body, _ := json.Marshal(map[string]any{"gateway_id": "gw-7-1", "stopped": true, "confirmed": true, "unmounted": test.unmounted})
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})}
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", httpClient)
			if _, err := client.CreateGateway(ctx, "https://10.0.0.2:19090", RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw", GatewayPort: 20000}); err == nil {
				t.Fatal("create response must be uncertain")
			}
			err := client.DeleteGateway(ctx, test.endpoint, test.id)
			if (err == nil) != test.wantStop {
				t.Fatalf("stop=%v want success=%v", err, test.wantStop)
			}
			cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
			if (cm.Data["desired_state"] == "stopped") != test.wantStop {
				t.Fatal("assignment released without matching receipt")
			}
		})
	}
}

func TestSlotClientPersistsSecretAndPinsPodIdentity(t *testing.T) {
	kube := fake.NewSimpleClientset(slotTestPod())
	httpClient := &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		var req RuntimeAgentCreateGatewayRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.PodUID != "pod-uid" {
			t.Fatalf("missing target identity: %+v", req)
		}
		if req.Environment["API_KEY"] != "business-secret" || r.Header.Get("X-ClawManager-Control-Token") != "token" {
			t.Fatal("business environment or control authentication was not forwarded")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"gateway_id":"gw-7-1","state":"starting","gateway_port":20000}`))}, nil
	})}
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", httpClient)
	_, err := client.CreateGateway(context.Background(), "https://10.0.0.2:19090", RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw", GatewayPort: 20000, Environment: map[string]string{"API_KEY": "business-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	cm, err := kube.CoreV1().ConfigMaps("runtime").Get(context.Background(), "lite-instance-7", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(cm.Data)
	if strings.Contains(string(raw), "business-secret") {
		t.Fatal("business credentials leaked into ConfigMap")
	}
	if cm.Data["pod_uid"] != "pod-uid" || !strings.Contains(cm.Data["request"], `"pod_uid":"pod-uid"`) {
		t.Fatal("resolved Pod identity was not persisted")
	}
	secret, err := kube.CoreV1().Secrets("runtime").Get(context.Background(), cm.Data["secret_ref"], metav1.GetOptions{})
	if err != nil || string(secret.Data["API_KEY"]) != "business-secret" {
		t.Fatalf("business Secret missing: %v", err)
	}
	if _, ok := client.(RuntimeUpgradeAgentClient); !ok {
		t.Fatal("upgrade interface was hidden by decorator")
	}
}

func TestSlotClientRejectsStopWithoutUnmountEvidence(t *testing.T) {
	kube := fake.NewSimpleClientset(slotTestPod())
	httpClient := &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		body := `{"gateway_id":"gw-7-1","state":"starting","gateway_port":20000}`
		if r.Method == http.MethodDelete {
			body = `{"gateway_id":"gw-7-1","stopped":true,"confirmed":true,"unmounted":false}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", httpClient)
	req := RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw", GatewayPort: 20000}
	if _, err := client.CreateGateway(context.Background(), "https://10.0.0.2:19090", req); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteGateway(context.Background(), "https://10.0.0.2:19090", "gw-7-1"); err == nil {
		t.Fatal("unconfirmed unmount accepted")
	}
	cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(context.Background(), "lite-instance-7", metav1.GetOptions{})
	if cm.Data["desired_state"] != "draining" {
		t.Fatalf("failed stop relinquished writer: %v", cm.Data)
	}
}

func TestSlotClientConfirmedStopAllowsHandover(t *testing.T) {
	ctx := context.Background()
	other := slotTestPod()
	other.Name, other.UID, other.Status.PodIP = "other-pool", types.UID("other-uid"), "10.0.0.3"
	kube := fake.NewSimpleClientset(slotTestPod(), other)
	requests := 0
	httpClient := &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		requests++
		body := `{"gateway_id":"gw-7-1","stopped":true,"confirmed":true,"unmounted":true}`
		if r.Method == http.MethodPost {
			var req RuntimeAgentCreateGatewayRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			body = `{"gateway_id":"gw-7-1","port":20000,"status":"starting"}`
			if req.Generation == 2 {
				body = `{"gateway_id":"gw-7-2","port":20000,"status":"starting"}`
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", httpClient)
	a := RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw", GatewayPort: 20000}
	if _, err := client.CreateGateway(ctx, "https://10.0.0.2:19090", a); err != nil {
		t.Fatal(err)
	}
	next := a
	next.Generation++
	if _, err := client.CreateGateway(ctx, "https://10.0.0.3:19090", next); err == nil {
		t.Fatal("handover accepted without confirmed source stop")
	}
	if requests != 1 {
		t.Fatal("blocked handover reached HTTP")
	}
	if err := client.DeleteGateway(ctx, "https://10.0.0.2:19090", "gw-7-1"); err != nil {
		t.Fatal(err)
	}
	cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
	if cm.Data["desired_state"] != "stopped" {
		t.Fatalf("genuine receipt did not confirm stop: %#v", cm.Data)
	}
	if _, err := client.CreateGateway(ctx, "https://10.0.0.2:19090", a); err == nil {
		t.Fatal("stopped retry authorized restart")
	}
	if requests != 2 {
		t.Fatal("stopped retry reached HTTP")
	}
	if _, err := client.CreateGateway(ctx, "https://10.0.0.3:19090", next); err != nil {
		t.Fatal(err)
	}
	cm, _ = kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
	if cm.Data["generation"] != "2" || cm.Data["pod_uid"] != "other-uid" || cm.Data["desired_state"] != "running" {
		t.Fatalf("bad handover: %#v", cm.Data)
	}
	if err := client.DeleteGateway(ctx, "https://10.0.0.2:19090", "gw-7-1"); err == nil {
		t.Fatal("stale delete accepted after handover")
	}
	if requests != 3 {
		t.Fatal("stale delete reached HTTP")
	}
}

func TestSlotClientRejectsStalePodUIDAtReusedIP(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset(slotTestPod())
	requests := 0
	httpClient := &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"gateway_id":"gw-7-1"}`))}, nil
	})}
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", httpClient)
	a := RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw", GatewayPort: 20000, PodUID: "previous-pod"}
	if _, err := client.CreateGateway(ctx, "https://10.0.0.2:19090", a); err == nil {
		t.Fatal("provided stale UID accepted")
	}
	a.PodUID = ""
	if _, err := client.CreateGateway(ctx, "https://10.0.0.2:19090", a); err != nil {
		t.Fatal(err)
	}
	pod := slotTestPod()
	pod.UID = types.UID("replacement-uid")
	if _, err := kube.CoreV1().Pods("runtime").Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateGateway(ctx, "https://10.0.0.2:19090", a); err == nil {
		t.Fatal("implicit UID retry accepted replacement Pod")
	}
	a.PodUID = "pod-uid"
	if _, err := client.CreateGateway(ctx, "https://10.0.0.2:19090", a); err == nil {
		t.Fatal("explicit previous UID accepted replacement Pod")
	}
	a.PodUID = ""
	a.Generation++
	if _, err := client.CreateGateway(ctx, "https://10.0.0.2:19090", a); err == nil {
		t.Fatal("new generation bypassed stop at reused PodIP")
	}
	if requests != 1 {
		t.Fatalf("stale UID or unsafe handover reached HTTP: %d", requests)
	}
}

func TestSlotClientRejectsIneligibleTargetsBeforeHTTP(t *testing.T) {
	tests := []struct {
		name, endpoint string
		change         func(*corev1.Pod)
	}{
		{"different IP", "https://10.0.0.9:19090", func(*corev1.Pod) {}},
		{"DNS", "https://pool.runtime:19090", func(*corev1.Pod) {}},
		{"userinfo", "https://user@10.0.0.2:19090", func(*corev1.Pod) {}},
		{"path", "https://10.0.0.2:19090/other", func(*corev1.Pod) {}},
		{"empty query", "https://10.0.0.2:19090?", func(*corev1.Pod) {}},
		{"empty fragment", "https://10.0.0.2:19090#", func(*corev1.Pod) {}},
		{"legacy protocol", "https://10.0.0.2:19090", func(p *corev1.Pod) { p.Labels = nil }},
		{"missing UID", "https://10.0.0.2:19090", func(p *corev1.Pod) { p.UID = "" }},
		{"not ready", "https://10.0.0.2:19090", func(p *corev1.Pod) { p.Status.Conditions = nil }},
		{"terminating", "https://10.0.0.2:19090", func(p *corev1.Pod) { now := metav1.Now(); p.DeletionTimestamp = &now }},
		{"other namespace", "https://10.0.0.2:19090", func(p *corev1.Pod) { p.Namespace = "other" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pod := slotTestPod()
			test.change(pod)
			kube := fake.NewSimpleClientset(pod)
			httpClient := &http.Client{Transport: slotRoundTripper(func(*http.Request) (*http.Response, error) {
				t.Fatal("ineligible target reached HTTP")
				return nil, nil
			})}
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", httpClient)
			if _, err := client.CreateGateway(context.Background(), test.endpoint, RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw"}); err == nil {
				t.Fatal("ineligible target accepted")
			}
			configmaps, _ := kube.CoreV1().ConfigMaps("runtime").List(context.Background(), metav1.ListOptions{})
			if len(configmaps.Items) != 0 {
				t.Fatal("ineligible target persisted startup authority")
			}
		})
	}
}

func TestSlotClientRequiresEveryTeardownReceiptField(t *testing.T) {
	for name, receipt := range map[string]string{
		"missing gateway": `{"stopped":true,"confirmed":true,"unmounted":true}`,
		"wrong gateway":   `{"gateway_id":"gw-7-2","stopped":true,"confirmed":true,"unmounted":true}`,
		"not stopped":     `{"gateway_id":"gw-7-1","stopped":false,"confirmed":true,"unmounted":true}`,
		"not confirmed":   `{"gateway_id":"gw-7-1","stopped":true,"confirmed":false,"unmounted":true}`,
		"not unmounted":   `{"gateway_id":"gw-7-1","stopped":true,"confirmed":true,"unmounted":false}`,
		"empty":           `{}`,
		"invalid JSON":    `{`,
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			kube := fake.NewSimpleClientset(slotTestPod())
			requests := 0
			httpClient := &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				requests++
				body := `{"gateway_id":"gw-7-1"}`
				if r.Method == http.MethodDelete {
					body = receipt
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", httpClient)
			req := RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw"}
			if _, err := client.CreateGateway(ctx, "https://10.0.0.2:19090", req); err != nil {
				t.Fatal(err)
			}
			if err := client.DeleteGateway(ctx, "https://10.0.0.2:19090", "gw-7-1"); err == nil {
				t.Fatal("invalid teardown receipt accepted")
			}
			cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
			if cm.Data["desired_state"] != "draining" {
				t.Fatal("invalid teardown relinquished writer")
			}
			if _, err := client.CreateGateway(ctx, "https://10.0.0.2:19090", req); err == nil {
				t.Fatal("draining retry authorized startup")
			}
			if requests != 2 {
				t.Fatal("draining retry reached HTTP")
			}
		})
	}
}

func TestSlotClientRejectsAmbiguousPodIP(t *testing.T) {
	other := slotTestPod()
	other.Name, other.UID = "duplicate-ip", types.UID("other-uid")
	kube := fake.NewSimpleClientset(slotTestPod(), other)
	httpClient := &http.Client{Transport: slotRoundTripper(func(*http.Request) (*http.Response, error) { t.Fatal("ambiguous target reached HTTP"); return nil, nil })}
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", httpClient)
	if _, err := client.CreateGateway(context.Background(), "https://10.0.0.2:19090", RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw"}); err == nil {
		t.Fatal("ambiguous ready PodIP accepted")
	}
}
