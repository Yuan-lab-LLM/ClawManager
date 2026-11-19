package main

import "testing"

func TestHistoryCLIRejectsUnboundedOrUnspecifiedMutation(t *testing.T) {
	for _, args := range [][]string{{}, {"-mode", "delete"}, {"-mode", "prove", "-limit", "501"}, {"-mode", "verify", "-after", "-1"}, {"-mode", "backfill", "-since", "2026-01-01T00:00:00Z", "-until", "2026-11-01T00:00:00Z"}, {"-mode", "backfill", "-since", "2026-11-03T00:00:00Z", "-until", "2026-11-01T00:00:00Z"}} {
		if err := run(args); err == nil {
			t.Fatalf("unsafe maintenance accepted: %v", args)
		}
	}
}
