package ownerrecords

import (
	"context"
	"testing"
	"time"
)

// Cancellation must preserve uncommitted activity, including when another
// analytics phase has consumed its budget. Replaying it is idempotent.
func TestTeamPortalCancelledFlushRetainsActivity(t *testing.T) {
	s := &Store{}
	s.perf.low.windows = map[string]activityWindow{"pending": {owner: "owner@example.test", start: time.Now(), mask: 3}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.flushActivity(ctx); err == nil {
		t.Fatal("cancelled flush reported success")
	}
	if w, ok := s.perf.low.windows["pending"]; !ok || w.mask != 3 {
		t.Fatal("uncommitted activity discarded on cancellation")
	}
}
