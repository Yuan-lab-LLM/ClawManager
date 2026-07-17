package handlers

import (
	"testing"
	"time"
)

func TestValidateSessionUsageTimeRange(t *testing.T) {
	since := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	if err := validateSessionUsageTimeRange(&since, &until); err != nil {
		t.Fatalf("expected valid range, got %v", err)
	}
	if err := validateSessionUsageTimeRange(&since, &since); err == nil {
		t.Fatalf("expected equal since/until to be rejected")
	}
	if err := validateSessionUsageTimeRange(nil, &until); err != nil {
		t.Fatalf("expected open-ended range, got %v", err)
	}
}

func TestParseOptionalRFC3339(t *testing.T) {
	if _, err := parseOptionalRFC3339(""); err != nil {
		t.Fatalf("empty value should be allowed: %v", err)
	}
	parsed, err := parseOptionalRFC3339("2026-07-01T00:00:00Z")
	if err != nil || parsed == nil {
		t.Fatalf("expected parsed timestamp, got %v err=%v", parsed, err)
	}
	if _, err := parseOptionalRFC3339("not-a-date"); err == nil {
		t.Fatalf("expected invalid timestamp error")
	}
}
