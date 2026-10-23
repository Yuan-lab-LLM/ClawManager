package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func stopClientFixture() (RuntimeAgentStopRequest, RuntimeAgentStopReceipt) {
	r := RuntimeAgentStopRequest{InstanceID: 12, UserID: 4, Generation: 7, GatewayID: "gw-12-7", OperationID: "0123456789abcdef0123456789abcdef",
		PodUID: "pod-uid", AgentEpoch: "11111111111111111111111111111111", TimeoutSeconds: 1, ExpectedPID: 1234}
	return r, RuntimeAgentStopReceipt{InstanceID: r.InstanceID, Generation: r.Generation, GatewayID: r.GatewayID, OperationID: r.OperationID,
		PodUID: r.PodUID, AgentEpoch: r.AgentEpoch, PID: 1234, Confirmed: true, ConfirmationScope: "process_group", ConfirmedAt: time.Now().UTC()}
}

func TestRuntimeAgentStopValidatesReceipt(t *testing.T) {
	for _, test := range []struct {
		name  string
		alter func(*RuntimeAgentStopReceipt)
	}{
		{"valid", func(*RuntimeAgentStopReceipt) {}},
		{"wrong generation", func(r *RuntimeAgentStopReceipt) { r.Generation++ }},
		{"wrong instance", func(r *RuntimeAgentStopReceipt) { r.InstanceID++ }},
		{"wrong gateway", func(r *RuntimeAgentStopReceipt) { r.GatewayID = "gw-12-6" }},
		{"wrong operation", func(r *RuntimeAgentStopReceipt) { r.OperationID = "old" }},
		{"old pod", func(r *RuntimeAgentStopReceipt) { r.PodUID = "old" }},
		{"restarted agent", func(r *RuntimeAgentStopReceipt) { r.AgentEpoch = "old" }},
		{"not confirmed", func(r *RuntimeAgentStopReceipt) { r.Confirmed = false }},
		{"leader only", func(r *RuntimeAgentStopReceipt) { r.ConfirmationScope = "leader" }},
		{"missing timestamp", func(r *RuntimeAgentStopReceipt) { r.ConfirmedAt = time.Time{} }},
		{"missing pid", func(r *RuntimeAgentStopReceipt) { r.PID = 0 }},
		{"different pid", func(r *RuntimeAgentStopReceipt) { r.PID = 9999 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			req, receipt := stopClientFixture()
			test.alter(&receipt)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/gateway-stop" || r.Method != "POST" || r.Header.Get("X-ClawManager-Control-Token") != "control" {
					t.Error("incorrect stop transport")
				}
				var got RuntimeAgentStopRequest
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil || got != req {
					t.Error("lost request identity")
				}
				_ = json.NewEncoder(w).Encode(receipt)
			}))
			defer srv.Close()
			client := NewRuntimeAgentClient("control").(RuntimeAgentStopClient)
			got, err := client.ConfirmGatewayStop(context.Background(), srv.URL, req)
			if test.name == "valid" {
				if err != nil || got == nil {
					t.Fatalf("%v %v", got, err)
				}
				return
			}
			if got != nil || !errors.Is(err, ErrRuntimeStopUnconfirmed) {
				t.Fatalf("unsafe receipt accepted: %v %v", got, err)
			}
		})
	}
}

func TestRuntimeAgentStopNeverFallsBackToDelete(t *testing.T) {
	for _, status := range []int{404, 501, 409, 504, 204} {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Method != "POST" || r.URL.Path != "/v1/gateway-stop" {
				t.Error("destructive fallback")
			}
			w.WriteHeader(status)
		}))
		req, _ := stopClientFixture()
		_, err := NewRuntimeAgentClient("control").(RuntimeAgentStopClient).ConfirmGatewayStop(context.Background(), srv.URL, req)
		srv.Close()
		if !errors.Is(err, ErrRuntimeStopUnconfirmed) || calls != 1 {
			t.Fatalf("status=%d calls=%d error=%v", status, calls, err)
		}
	}
}

func TestRuntimeAgentStopCapabilitiesAndInvalidInput(t *testing.T) {
	req, _ := stopClientFixture()
	caps := RuntimeAgentStopCapabilities{Protocol: 1, PodUID: req.PodUID, AgentEpoch: req.AgentEpoch, GracefulStop: true, ConfirmationScope: "process_group"}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; _ = json.NewEncoder(w).Encode(caps) }))
	defer srv.Close()
	client := NewRuntimeAgentClient("control").(RuntimeAgentStopClient)
	if _, err := client.StopCapabilities(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	caps.GracefulStop = false
	if _, err := client.StopCapabilities(context.Background(), srv.URL); !errors.Is(err, ErrRuntimeStopUnconfirmed) {
		t.Fatal(err)
	}
	req.OperationID = "invalid"
	if _, err := client.ConfirmGatewayStop(context.Background(), srv.URL, req); !errors.Is(err, ErrRuntimeStopUnconfirmed) {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("invalid request sent to runtime")
	}
}
