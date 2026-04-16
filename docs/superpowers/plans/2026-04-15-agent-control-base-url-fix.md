> Superseded by `docs/superpowers/specs/2026-04-15-p1-internal-release-design.md` and `docs/superpowers/plans/2026-04-15-r3-a-u3-f2-p1-internal-rc.md`.
> Do not execute this plan: the `https://...:8443` URL-builder hypothesis was disproven by the `9001 -> 9001` gateway-service evidence chain.

# Agent Control Base URL Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the in-cluster default control-plane URL generation so OpenClaw agent registration targets the live K3S gateway scheme and port, allowing agent/runtime status propagation to start working.

**Architecture:** Keep the change minimal and source-of-truth oriented. Add focused unit tests around default URL generation in `backend/internal/services/instance_runtime.go`, then update the default URL builders to derive the service port from `SERVER_ADDRESS` when explicit service-port env vars are absent and prefer `https` for the in-cluster gateway/control endpoint used by the runtime agent.

**Tech Stack:** Go 1.21+, standard library testing, existing `backend/internal/services` package.

---

### Task 1: Lock current failing URL behavior in unit tests

**Files:**
- Create: `backend/internal/services/instance_runtime_test.go`
- Modify: none
- Test: `backend/internal/services/instance_runtime_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package services

import "testing"

func TestDefaultAgentControlBaseURLUsesHTTPSAndServerAddressPort(t *testing.T) {
	t.Setenv("K8S_NAMESPACE", "clawmanager")
	t.Setenv("SERVER_ADDRESS", ":8443")
	t.Setenv("CLAWMANAGER_AGENT_CONTROL_BASE_URL", "")
	t.Setenv("CLAWMANAGER_AGENT_CONTROL_SERVICE_PORT", "")
	t.Setenv("CLAWMANAGER_LLM_GATEWAY_SERVICE_PORT", "")
	t.Setenv("CLAWMANAGER_LLM_GATEWAY_PORT", "")

	got, ok := defaultAgentControlBaseURL()
	if !ok {
		t.Fatal("expected URL to resolve")
	}
	want := "https://clawmanager-gateway.clawmanager-system.svc.cluster.local:8443"
	if got != want {
		t.Fatalf("defaultAgentControlBaseURL() = %q, want %q", got, want)
	}
}

func TestDefaultGatewayBaseURLUsesHTTPSAndServerAddressPort(t *testing.T) {
	t.Setenv("K8S_NAMESPACE", "clawmanager")
	t.Setenv("SERVER_ADDRESS", ":8443")
	t.Setenv("CLAWMANAGER_LLM_GATEWAY_SERVICE_PORT", "")
	t.Setenv("CLAWMANAGER_LLM_GATEWAY_PORT", "")

	got, ok := defaultGatewayBaseURL()
	if !ok {
		t.Fatal("expected URL to resolve")
	}
	want := "https://clawmanager-gateway.clawmanager-system.svc.cluster.local:8443/api/v1/gateway/llm"
	if got != want {
		t.Fatalf("defaultGatewayBaseURL() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/services -run 'TestDefault(AgentControl|Gateway)BaseURL'`
Expected: FAIL because current implementation falls back to `http://...:9001`.

### Task 2: Implement the minimal URL resolution fix

**Files:**
- Modify: `backend/internal/services/instance_runtime.go`
- Test: `backend/internal/services/instance_runtime_test.go`

- [ ] **Step 1: Write the minimal implementation**

```go
func defaultGatewayBaseURL() (string, bool) {
	// keep override handling and namespace/service name logic
	// when explicit gateway service port env vars are absent,
	// fall back to normalized SERVER_ADDRESS port
	// use https for the in-cluster gateway endpoint
}

func defaultAgentControlBaseURL() (string, bool) {
	// keep override handling and namespace/service name logic
	// when explicit agent/gateway service port env vars are absent,
	// fall back to normalized SERVER_ADDRESS port
	// use https for the in-cluster control endpoint
}
```

- [ ] **Step 2: Run targeted tests to verify they pass**

Run: `cd backend && go test ./internal/services -run 'TestDefault(AgentControl|Gateway)BaseURL'`
Expected: PASS.

### Task 3: Regression-check adjacent behavior

**Files:**
- Modify: none
- Test: existing backend service package tests

- [ ] **Step 1: Run focused package tests**

Run: `cd backend && go test ./internal/services`
Expected: PASS.

- [ ] **Step 2: Capture summary for the next runtime verification packet**

Record that the code fix targets source-of-truth URL generation only; runtime replay verification remains a separate packet.
