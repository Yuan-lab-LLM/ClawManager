package services

import "testing"

func TestNormalizeRuntimeGatewayLifecycle(t *testing.T) {
	tests := []struct {
		name           string
		raw            string
		bindingState   string
		instanceState  string
		running        bool
		recognized     bool
		createAccepted bool
	}{
		{name: "running", raw: "running", bindingState: "running", instanceState: "running", running: true, recognized: true, createAccepted: true},
		{name: "ready", raw: "ready", bindingState: "running", instanceState: "running", running: true, recognized: true, createAccepted: true},
		{name: "healthy", raw: "healthy", bindingState: "running", instanceState: "running", running: true, recognized: true, createAccepted: true},
		{name: "starting", raw: "starting", bindingState: "creating", instanceState: "creating", recognized: true, createAccepted: true},
		{name: "creating", raw: "creating", bindingState: "creating", instanceState: "creating", recognized: true, createAccepted: true},
		{name: "pending", raw: "pending", bindingState: "creating", instanceState: "creating", recognized: true, createAccepted: true},
		{name: "error", raw: "error", bindingState: "error", instanceState: "error", recognized: true},
		{name: "stopped", raw: "stopped", bindingState: "stopped", instanceState: "stopped", recognized: true},
		{name: "empty", raw: "", bindingState: "creating", instanceState: "creating"},
		{name: "unknown", raw: "mystery", bindingState: "creating", instanceState: "creating"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeRuntimeGatewayLifecycle(tt.raw, nil)
			if got.BindingState != tt.bindingState || got.InstanceState != tt.instanceState || got.Running != tt.running || got.Recognized != tt.recognized {
				t.Fatalf("state = %+v", got)
			}
			if got.CreateAccepted() != tt.createAccepted {
				t.Fatalf("CreateAccepted = %t, want %t", got.CreateAccepted(), tt.createAccepted)
			}
			if !tt.running && got.Message == nil && tt.instanceState != "stopped" {
				t.Fatal("non-running state is missing a diagnostic message")
			}
		})
	}
}

func TestNormalizeRuntimeGatewayLifecyclePreservesReportedError(t *testing.T) {
	message := "gateway port is not listening"
	got := NormalizeRuntimeGatewayLifecycle("failed", &message)
	if got.Message == nil || *got.Message != message {
		t.Fatalf("message = %#v, want %q", got.Message, message)
	}
}
