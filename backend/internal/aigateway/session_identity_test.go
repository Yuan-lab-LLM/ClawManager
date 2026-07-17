package aigateway

import "testing"

func TestApplyManagedInstanceSessionDefaultsUsesMainForOpenClawInstanceToken(t *testing.T) {
	req := ChatCompletionRequest{
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}
	ApplyManagedInstanceSessionDefaults(&req, "instance", "openclaw")
	if req.OpenClawSessionKey == nil || *req.OpenClawSessionKey != "main" {
		t.Fatalf("expected default session key main, got %+v", req.OpenClawSessionKey)
	}
	if req.ManagedAgentType == nil || *req.ManagedAgentType != "openclaw" {
		t.Fatalf("expected managed agent type openclaw, got %+v", req.ManagedAgentType)
	}
	if got := resolveSessionID(req); got != "agent:openclaw:main" {
		t.Fatalf("expected normalized session id agent:openclaw:main, got %q", got)
	}
}

func TestApplyManagedInstanceSessionDefaultsUsesMainForHermesInstanceToken(t *testing.T) {
	req := ChatCompletionRequest{}
	ApplyManagedInstanceSessionDefaults(&req, "instance", "hermes")
	if got := resolveSessionID(req); got != "agent:hermes:main" {
		t.Fatalf("expected normalized session id agent:hermes:main, got %q", got)
	}
}

func TestApplyManagedInstanceSessionDefaultsSkipsUserJWTCalls(t *testing.T) {
	req := ChatCompletionRequest{}
	ApplyManagedInstanceSessionDefaults(&req, "user", "openclaw")
	if req.OpenClawSessionKey != nil {
		t.Fatalf("expected no default session key for user auth, got %+v", req.OpenClawSessionKey)
	}
}

func TestApplyManagedInstanceSessionDefaultsRespectsExplicitHeader(t *testing.T) {
	explicit := "work"
	req := ChatCompletionRequest{
		OpenClawSessionKey: &explicit,
		RuntimeType:        stringPtr("desktop"),
	}
	ApplyManagedInstanceSessionDefaults(&req, "instance", "openclaw")
	if got := resolveSessionID(req); got != "agent:openclaw:work" {
		t.Fatalf("expected explicit session key to win, got %q", got)
	}
}

func TestApplyManagedInstanceSessionDefaultsRespectsExplicitSessionID(t *testing.T) {
	explicit := "agent:openclaw:custom"
	req := ChatCompletionRequest{
		SessionID: &explicit,
	}
	ApplyManagedInstanceSessionDefaults(&req, "instance", "openclaw")
	if req.OpenClawSessionKey != nil {
		t.Fatalf("expected body session id to prevent default header injection, got %+v", req.OpenClawSessionKey)
	}
	if got := resolveSessionID(req); got != explicit {
		t.Fatalf("expected explicit session id %q, got %q", explicit, got)
	}
}
