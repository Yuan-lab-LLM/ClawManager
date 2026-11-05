package services

import (
	"context"
	"io"
	"k8s.io/client-go/kubernetes/fake"
	"net/http"
	"strings"
	"testing"
)

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
