package services

import (
	"clawreef/internal/models"
	"context"
	"errors"
	"io"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestSlotCoexistenceKeepsLegacyAndRefusesUnknownDowngrade(t *testing.T) {
	calls := 0
	legacy := NewRuntimeAgentClientWithHTTPClient("test", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"gateway_id":"gw-7-1"}`))}, nil
	})})
	client := NewSlotRuntimeAgentClientWithHTTPClient("test", fake.NewSimpleClientset(), "runtime", nil)
	protected := false
	lookupErr := error(nil)
	if err := ConfigureSlotLegacyCoexistence(client, legacy, func(context.Context, int) (bool, error) { return protected, lookupErr }); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteGateway(context.Background(), "http://old-runtime:19090", "gw-7-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateGateway(context.Background(), "http://old-runtime:19090", RuntimeAgentCreateGatewayRequest{InstanceID: 7, Generation: 1}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
	idle := client.(RuntimeIdleStopClient)
	if _, err := idle.IdlePreflight(context.Background(), "http://old-runtime:19090", "gw-7-1", RuntimeIdleStopRequest{InstanceID: 7, Generation: 1, PodUID: "old"}); !errors.Is(err, ErrRuntimeAgentUnsupported) {
		t.Fatalf("legacy auto stop admitted: %v", err)
	}
	lookupErr = errors.New("database unavailable")
	if err := client.DeleteGateway(context.Background(), "http://old-runtime:19090", "gw-7-1"); err == nil {
		t.Fatal("lookup failure downgraded")
	}
	lookupErr = nil
	protected = true
	if err := client.DeleteGateway(context.Background(), "http://old-runtime:19090", "gw-7-1"); err == nil {
		t.Fatal("protected gateway downgraded")
	}
	if calls != 2 {
		t.Fatal("legacy call on protected or unknown identity")
	}
}

func TestSlotLegacyHTTPIsLimitedToExistingTenantPods(t *testing.T) {
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "runtime", Labels: map[string]string{"clawmanager.io/runtime-type": "openclaw"}}, Status: corev1.PodStatus{PodIP: "10.0.0.7"}}
	kube := fake.NewSimpleClientset(p)
	client := NewSlotRuntimeAgentClient("test", kube, "runtime")
	legacy := NewRuntimeAgentClient("test")
	if err := ConfigureSlotLegacyCoexistence(client, legacy, func(context.Context, int) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	c := client.(*slotRuntimeAgentClient)
	u, _ := url.Parse("http://10.0.0.7:19090/readyz")
	endpoint := u.String()
	pid := "old-pod"
	scheduler := &RuntimeScheduler{agentClient: client}
	uid, gid, profile, err := scheduler.gatewayLinuxIDs(context.Background(), 7, nil, models.RuntimePod{PodUID: &pid, AgentEndpoint: &endpoint})
	if err != nil || uid != 200007 || gid != 200007 || profile != "" {
		t.Fatalf("legacy identity contract changed %d/%d %q %v", uid, gid, profile, err)
	}
	if !c.allowLegacyHTTPEndpoint(context.Background(), u) {
		t.Fatal("legacy Pod refused")
	}
	p.Labels["clawmanager.io/runtime-protocol"] = "container-slots-v1"
	if _, err := kube.CoreV1().Pods("runtime").Update(context.Background(), p, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if c.allowLegacyHTTPEndpoint(context.Background(), u) {
		t.Fatal("slot allowed HTTP downgrade")
	}
	u.Host = "10.0.0.8:19090"
	if c.allowLegacyHTTPEndpoint(context.Background(), u) {
		t.Fatal("unowned endpoint admitted")
	}
}

func TestMixedTransportKeepsOldCredentialOnlyForVerifiedLegacyHTTP(t *testing.T) {
	tpt := slotTLSTransport{base: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		want := "new"
		if r.URL.Scheme == "http" {
			want = "old"
		}
		if r.Header.Get("X-ClawManager-Control-Token") != want {
			t.Fatal("cross-protocol credential")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	}), legacyHTTP: func(context.Context, *url.URL) bool { return true }, legacyToken: func() string { return "old" }}
	for _, scheme := range []string{"http", "https"} {
		r, _ := http.NewRequest("GET", scheme+"://10.0.0.7:19090/readyz", nil)
		r.Header.Set("X-ClawManager-Control-Token", "new")
		if _, err := tpt.RoundTrip(r); err != nil {
			t.Fatal(err)
		}
		if r.Header.Get("X-ClawManager-Control-Token") != "new" {
			t.Fatal("mutated caller request")
		}
	}
}
