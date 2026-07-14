package services

import "testing"

func TestNormalizeV2RuntimeTypeAcceptsManagedRuntimeTypes(t *testing.T) {
	for _, input := range []string{"openclaw", " OpenClaw ", "hermes", "Hermes"} {
		got, ok := NormalizeV2RuntimeType(input)
		if !ok {
			t.Fatalf("expected %q to be accepted", input)
		}
		if got != RuntimeTypeOpenClaw && got != RuntimeTypeHermes {
			t.Fatalf("expected normalized managed runtime type, got %q", got)
		}
	}
}

func TestNormalizeV2RuntimeTypeRejectsLegacyRuntimeTypes(t *testing.T) {
	for _, input := range []string{"webtop", "ubuntu", "", "desktop", "shell"} {
		if got, ok := NormalizeV2RuntimeType(input); ok {
			t.Fatalf("expected %q to be rejected, got %q", input, got)
		}
	}
}

func TestRuntimeWorkspacePath(t *testing.T) {
	got := RuntimeWorkspacePath("openclaw", 45, 123)
	want := "/workspaces/openclaw/user-45/instance-123"
	if got != want {
		t.Fatalf("expected workspace path %q, got %q", want, got)
	}
}

func TestRuntimeLinuxID(t *testing.T) {
	if got, want := RuntimeLinuxID(123), 200123; got != want {
		t.Fatalf("expected linux id %d, got %d", want, got)
	}
}

func TestRuntimeGatewayPortRangeMatchesInstanceCapacity(t *testing.T) {
	if got, want := RuntimeGatewayPortsPerInstance, 3; got != want {
		t.Fatalf("expected %d ports per gateway instance, got %d", want, got)
	}
	wantEnd := RuntimeGatewayPortStart + RuntimePodCapacity*RuntimeGatewayPortsPerInstance - 1
	if RuntimeGatewayPortEnd != wantEnd {
		t.Fatalf("gateway port end = %d, want %d", RuntimeGatewayPortEnd, wantEnd)
	}
	if got := (RuntimeGatewayPortEnd - RuntimeGatewayPortStart + 1) / RuntimeGatewayPortsPerInstance; got != RuntimePodCapacity {
		t.Fatalf("gateway port range supports %d instances, want %d", got, RuntimePodCapacity)
	}
}

func TestInstanceModeRuntimeTypeMapping(t *testing.T) {
	if got, ok := RuntimeTypeForInstanceMode(" lite "); !ok || got != RuntimeBackendGateway {
		t.Fatalf("lite runtime type = %q/%v, want gateway/true", got, ok)
	}
	if got, ok := RuntimeTypeForInstanceMode("Pro"); !ok || got != RuntimeBackendDesktop {
		t.Fatalf("pro runtime type = %q/%v, want desktop/true", got, ok)
	}
	if got := InstanceModeForRuntimeType(RuntimeBackendGateway); got != InstanceModeLite {
		t.Fatalf("gateway mode = %q, want lite", got)
	}
	if got := InstanceModeForRuntimeType(RuntimeBackendDesktop); got != InstanceModePro {
		t.Fatalf("desktop mode = %q, want pro", got)
	}
}
