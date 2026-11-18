package services

import (
	"context"
	"strings"
	"testing"
)

func TestOrphanRecoveryPublicRoutesRequireExactSourceUID(t *testing.T) {
	s := &RuntimeManagementService{}
	req := RuntimeSchedulingRequest{Mode: "orphan-recovery", SourcePodID: 1}
	if _, e := s.Preflight(context.Background(), req); e == nil || !strings.Contains(e.Error(), "source_pod_uid") {
		t.Fatalf("preflight did not enforce stale-card UID: %v", e)
	}
	if _, e := s.Evict(context.Background(), 1, req, nil); e == nil || !strings.Contains(e.Error(), "source_pod_uid") {
		t.Fatalf("eviction did not enforce stale-card UID: %v", e)
	}
}
