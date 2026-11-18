package repository

import (
	"clawreef/internal/models"
	"context"
	"os"
	"strings"
	"testing"
)

func TestRuntimeShutdownAuditMySQLIsolationAndDeduplication(t *testing.T) {
	fixture, exec := runtimeProtocolMySQLFixture(t)
	r := &runtimeShutdownAuditRepository{sess: fixture.sess}
	exec("ALTER TABLE instances ADD COLUMN type VARCHAR(40) NOT NULL DEFAULT 'openclaw'")
	raw, err := os.ReadFile("../db/migrations/080_add_runtime_shutdown_audits.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(raw))
	t.Cleanup(func() { _, _ = fixture.sess.SQL().Exec("DROP TABLE IF EXISTS runtime_shutdown_audits") })
	ctx := context.Background()
	o := models.RuntimeShutdownObservation{Generation: 3, ClaimID: strings.Repeat("a", 64), Outcome: "cleanup_unconfirmed"}
	for i := 0; i < 3; i++ {
		if err := r.Record(ctx, 10, 1, "pro_agent", o); err != nil {
			t.Fatal(err)
		}
	}
	items, err := r.List(ctx, 10, 1, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("%v %v", items, err)
	}
	first := items[0]
	o.Outcome = "cleanup_confirmed_release_unconfirmed"
	if err := r.Record(ctx, 10, 1, "pro_agent", o); err != nil {
		t.Fatal(err)
	}
	items, err = r.List(ctx, 10, 1, 0)
	if err != nil || len(items) != 2 || items[0].ID <= first.ID {
		t.Fatalf("%v %v", items, err)
	}
	older, err := r.List(ctx, 10, 1, items[0].ID)
	if err != nil || len(older) != 1 || older[0].ID != first.ID {
		t.Fatalf("cursor %v %v", older, err)
	}
	o.Generation = 2
	if r.Record(ctx, 10, 1, "pro_agent", o) == nil {
		t.Fatal("stale generation accepted")
	}
	o.Generation = 3
	exec("UPDATE instances SET runtime_generation=4 WHERE id=10")
	e := models.RuntimeShutdownEvent{InstanceID: 10, OwnerUserID: 1, Generation: 3, ClaimID: o.ClaimID, Outcome: o.Outcome}
	delivery := models.RuntimeShutdownDelivery{ID: e.DeliveryID(), Event: e}
	if r.Record(ctx, 10, 1, "pro_agent", o) == nil {
		t.Fatal("inline stale observation accepted")
	}
	if err := r.RecordDelivery(ctx, "pro_agent", delivery); err != nil {
		t.Fatal("historical delivery rejected", err)
	}
	if err := r.RecordDelivery(ctx, "pro_agent", delivery); err != nil {
		t.Fatal("delivery not idempotent", err)
	}
	future := delivery
	future.Event.Generation = 5
	future.ID = future.Event.DeliveryID()
	if r.RecordDelivery(ctx, "pro_agent", future) == nil {
		t.Fatal("future delivery accepted")
	}
	corrupt := delivery
	corrupt.ID = strings.Repeat("b", 64)
	if r.RecordDelivery(ctx, "pro_agent", corrupt) == nil {
		t.Fatal("changed delivery accepted")
	}
	if r.Record(ctx, 10, 2, "pro_agent", o) == nil {
		t.Fatal("wrong owner accepted")
	}
	exec("UPDATE instances SET user_id=2 WHERE id=10")
	if r.RecordDelivery(ctx, "pro_agent", delivery) == nil {
		t.Fatal("former owner historical delivery accepted")
	}
	items, err = r.List(ctx, 10, 1, 0)
	if err != nil || len(items) != 0 {
		t.Fatal("former owner can query")
	}
	items, err = r.List(ctx, 10, 2, 0)
	if err != nil || len(items) != 0 {
		t.Fatal("new owner saw old history")
	}
	exec("UPDATE users SET is_active=FALSE WHERE id=2")
	// Use the current owner/generation so this checks the active-user predicate,
	// not an unrelated stale-generation or former-owner rejection.
	o.Generation = 4
	if r.Record(ctx, 10, 2, "lite_agent", o) == nil {
		t.Fatal("inactive owner accepted")
	}
	e.OwnerUserID = 2
	delivery = models.RuntimeShutdownDelivery{ID: e.DeliveryID(), Event: e}
	if r.RecordDelivery(ctx, "lite_agent", delivery) == nil {
		t.Fatal("inactive owner historical delivery accepted")
	}
}
