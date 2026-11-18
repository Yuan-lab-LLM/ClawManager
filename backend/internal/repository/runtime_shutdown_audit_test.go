package repository

import (
	"clawreef/internal/models"
	"context"
	"strings"
	"testing"
)

func TestRuntimeShutdownAuditRejectsInvalidBeforeDatabaseAccess(t *testing.T) {
	r := &runtimeShutdownAuditRepository{}
	good := models.RuntimeShutdownObservation{Generation: 3, ClaimID: strings.Repeat("a", 64), Outcome: "cleanup_unconfirmed"}
	for _, source := range []string{"", "user", "pro_agent\n", "secret"} {
		if r.Record(context.Background(), 10, 1, source, good) == nil {
			t.Fatal(source)
		}
	}
	if r.Record(context.Background(), 0, 1, "pro_agent", good) == nil {
		t.Fatal("invalid instance")
	}
	bad := good
	bad.ClaimID = ""
	if r.Record(context.Background(), 10, 1, "pro_agent", bad) == nil {
		t.Fatal("invalid claim")
	}
	for _, before := range []int64{-1, -10} {
		if _, err := r.List(context.Background(), 10, 1, before); err == nil {
			t.Fatal(before)
		}
	}
}
