package utils

import "testing"

func TestFormatOpenClawSessionKey(t *testing.T) {
	if got := FormatOpenClawSessionKey("agent:openclaw:main"); got != "main" {
		t.Fatalf("expected main, got %q", got)
	}
	if got := FormatOpenClawSessionKey("agent:hermes:work"); got != "work" {
		t.Fatalf("expected work, got %q", got)
	}
	if got := FormatOpenClawSessionKey("sess_trc_123"); got != "sess_trc_123" {
		t.Fatalf("expected passthrough, got %q", got)
	}
}

func TestNormalizeOpenClawSessionID(t *testing.T) {
	if got := NormalizeOpenClawSessionID("main", "openclaw"); got != "agent:openclaw:main" {
		t.Fatalf("expected agent:openclaw:main, got %q", got)
	}
	if got := NormalizeOpenClawSessionID("main", "hermes"); got != "agent:hermes:main" {
		t.Fatalf("expected agent:hermes:main, got %q", got)
	}
	if got := NormalizeOpenClawSessionID("agent:openclaw:main", "openclaw"); got != "agent:openclaw:main" {
		t.Fatalf("expected unchanged, got %q", got)
	}
}

func TestIsTraceFallbackSessionID(t *testing.T) {
	if !IsTraceFallbackSessionID("sess_trc_abc") {
		t.Fatal("expected trace fallback session")
	}
	if IsTraceFallbackSessionID("agent:openclaw:main") {
		t.Fatal("expected stable session")
	}
}
