package services

import (
	"errors"
	"fmt"
	"testing"
)

func TestPendingStartDiagnosticNeverIncludesUpstreamText(t *testing.T) {
	for _, test := range []struct{ message, want string }{
		{"secret-token-and-dsn", "pending_unknown"},
		{"pending_placement: secret-token-and-dsn", "placement"},
		{"pending_authority: pending generation business Secret already exists; secret", "already_published"},
		{"managed Team preview unavailable", "preview_admission"},
	} {
		if got := pendingStartReason(fmt.Errorf("wrapped: %w", errors.New(test.message))); got != test.want {
			t.Fatalf("reason=%q", got)
		}
	}
}
