package egresspolicy

import "testing"

func TestPolicyDenylistBlocksOpenAI(t *testing.T) {
	policy := Policy{
		Mode:               ModeDenylist,
		DeniedHostSuffixes: defaultDeniedHostSuffixes(),
	}
	allowed, reason := policy.AllowHost("api.openai.com")
	if allowed || reason == "" {
		t.Fatalf("expected openai host to be blocked, allowed=%v reason=%q", allowed, reason)
	}
}

func TestPolicyDenylistAllowsGitHub(t *testing.T) {
	policy := Policy{
		Mode:               ModeDenylist,
		DeniedHostSuffixes: defaultDeniedHostSuffixes(),
	}
	allowed, reason := policy.AllowHost("github.com")
	if !allowed || reason != "" {
		t.Fatalf("expected github host to be allowed, allowed=%v reason=%q", allowed, reason)
	}
}

func TestPolicyOpenAllowsEverything(t *testing.T) {
	policy := Policy{Mode: ModeOpen}
	allowed, reason := policy.AllowHost("api.openai.com")
	if !allowed || reason != "" {
		t.Fatalf("expected open mode to allow host, allowed=%v reason=%q", allowed, reason)
	}
}
