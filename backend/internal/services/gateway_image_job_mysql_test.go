package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/repository"
	"github.com/upper/db/v4"
)

// Uses a real SQL uniqueness constraint, transactions and the production budget
// repository. The database guard is shared with the encrypted-artifact test.
func TestImageJobsMySQLRecoveryIdempotencyAndBudgetHold(t *testing.T) {
	sess := imageMySQLTestSession(t)
	exec := func(query string, args ...interface{}) {
		t.Helper()
		if _, err := sess.SQL().Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	// Deliberately minimal surrounding schema; image job schema is the exact
	// migration. No application database or existing tables may be used here.
	for _, ddl := range []string{
		`CREATE TABLE gateway_keys (id BIGINT PRIMARY KEY, budget_limit DECIMAL(18,8) NOT NULL DEFAULT 10, budget_spent DECIMAL(18,8) NOT NULL DEFAULT 0, budget_period VARCHAR(16) NOT NULL DEFAULT 'daily', budget_reset_at TIMESTAMP NULL, budget_currency_review_required BOOLEAN NOT NULL DEFAULT FALSE, budget_currency_reviewed_at TIMESTAMP NULL, updated_at TIMESTAMP NULL) ENGINE=InnoDB`,
		`CREATE TABLE gateway_key_budget_reservations (reservation_id VARCHAR(64) PRIMARY KEY, gateway_key_id BIGINT NOT NULL, reserved_cost DECIMAL(18,8) NOT NULL, actual_cost DECIMAL(18,8) NULL, settlement_status VARCHAR(16) NOT NULL DEFAULT 'reserved', expires_at TIMESTAMP NOT NULL, created_at TIMESTAMP NOT NULL) ENGINE=InnoDB`,
		`CREATE TABLE gateway_key_budget_ledger (reservation_id VARCHAR(64) PRIMARY KEY, gateway_key_id BIGINT NOT NULL, actual_cost DECIMAL(18,8) NOT NULL, recorded_at TIMESTAMP NOT NULL) ENGINE=InnoDB`,
	} {
		exec(ddl)
	}
	t.Cleanup(func() {
		for _, name := range []string{"gateway_image_jobs", "gateway_key_budget_ledger", "gateway_key_budget_reservations", "gateway_keys"} {
			_, _ = sess.SQL().Exec("DROP TABLE IF EXISTS " + name)
		}
	})
	schema, err := os.ReadFile("../db/migrations/073_add_gateway_image_jobs.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(schema))
	exec(string(schema)) // repeat-safe migration
	applyImageAnalyticsTestMigration(t, sess)
	exec("INSERT INTO gateway_keys(id) VALUES(7)")
	repo := repository.NewGatewayKeyRepository(sess)
	budget := NewGatewayBudgetService(repo)
	store := NewGatewayImageJobService(sess, budget)
	keyID, instanceID := 7, 8
	owner := ImageArtifactOwner{UserID: 1, InstanceID: &instanceID, GatewayKeyID: &keyID}
	job := func(id, key string) *ImageJob {
		return &ImageJob{RequestID: id, ScopeHash: ImageJobScope(owner), IdempotencyHash: ImageIdempotencyHash(key), RequestHash: imageJobHash([]byte("same payload")), UserID: 1, InstanceID: &instanceID, GatewayKeyID: &keyID, ModelID: 100, Estimate: .2}
	}
	// Concurrent submissions from different processes must return the same job.
	var claimed atomic.Int32
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, won, err := store.Claim(t.Context(), job(fmt.Sprintf("image_concurrent_%d", i), "same-key"))
			if err != nil {
				t.Error(err)
				return
			}
			if won {
				claimed.Add(1)
			}
			ids <- got.RequestID
		}(i)
	}
	wg.Wait()
	close(ids)
	var id string
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("duplicate jobs", id, got)
		}
		id = got
	}
	if claimed.Load() != 1 {
		t.Fatal("claim is not atomic", claimed.Load())
	}
	// Concurrent Auto selection may change, but the winner's deployment is pinned.
	competitor := job("image_auto_competitor", "same-key")
	competitor.ModelID = 101
	if got, won, err := store.Claim(t.Context(), competitor); err != nil || won || got.RequestID != id || got.ModelID != 100 {
		t.Fatal("Auto pinning failed", got, won, err)
	}
	if got, err := store.FindIdempotent(t.Context(), owner, "same-key"); err != nil || got == nil || got.RequestID != id {
		t.Fatal("pinned lookup failed", got, err)
	}
	if got, err := store.FindIdempotent(t.Context(), ImageArtifactOwner{UserID: 9}, "same-key"); err != nil || got != nil {
		t.Fatal("cross-owner idempotency lookup", got, err)
	}
	conflict := job("image_conflict", "same-key")
	conflict.RequestHash = imageJobHash([]byte("different"))
	if _, _, err := store.Claim(t.Context(), conflict); !errors.Is(err, ErrImageIdempotencyConflict) {
		t.Fatal(err)
	}
	// Different credential scope may reuse the same client idempotency key.
	other := job("image_other_scope", "same-key")
	other.UserID = 2
	other.ScopeHash = ImageJobScope(ImageArtifactOwner{UserID: 2})
	if _, won, err := store.Claim(t.Context(), other); err != nil || !won {
		t.Fatal(won, err)
	}
	if _, err := store.Load(t.Context(), ImageArtifactOwner{UserID: 2}, id); !errors.Is(err, ErrImageJobUnavailable) {
		t.Fatal("cross-owner read")
	}
	lease, err := budget.Reserve(t.Context(), 7, .2)
	if err != nil {
		t.Fatal(err)
	}
	// A mismatched lease rolls back without arming the request.
	wrong := *lease
	wrong.GatewayKeyID = 9
	if err := store.Arm(t.Context(), id, &wrong); err == nil {
		t.Fatal("wrong lease accepted")
	}
	if err := store.Arm(t.Context(), id, lease); err != nil {
		t.Fatal(err)
	}
	if err := budget.Release(t.Context(), lease); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE gateway_key_budget_reservations SET expires_at='2000-01-01' WHERE reservation_id=?", lease.ReservationID)
	exec("UPDATE gateway_keys SET budget_reset_at='2000-01-01' WHERE id=7")
	// Automatic period rollover must retain the in-flight image hold.
	if err := repo.ReserveBudget(t.Context(), 7, "rollover", 0, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertImageBudgetHold(t, sess, lease.ReservationID)
	// Manual budget resets must also retain it.
	if err := repo.UpdateBudgetPolicy(t.Context(), 7, .25, "daily", true); err != nil {
		t.Fatal(err)
	}
	assertImageBudgetHold(t, sess, lease.ReservationID)
	if _, err := budget.Reserve(t.Context(), 7, .1); !errors.Is(err, ErrGatewayKeyBudgetExceeded) {
		t.Fatal("pending image bypassed budget", err)
	}
	// Simulate a process crash: a fresh worker sees expired running/preparing.
	exec("UPDATE gateway_image_jobs SET deadline_at='2000-01-01'")
	restarted := NewGatewayImageJobService(sess, budget)
	if err := restarted.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	recovered, err := restarted.Load(t.Context(), owner, id)
	if err != nil || recovered.State != "unknown" || !recovered.Settled || recovered.Charge != .2 {
		t.Fatal(recovered, err)
	}
	if err := restarted.Finish(t.Context(), id, "completed", 0, nil, ""); err == nil {
		t.Fatal("late executor overwrote frozen unknown outcome")
	}
	// Repeated/concurrent recovery and settlement cannot charge the ledger twice.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := restarted.Settle(t.Context(), id); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := restarted.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	var ledgerCount int
	var spent float64
	row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM gateway_key_budget_ledger WHERE reservation_id=?", lease.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	row, err = sess.SQL().QueryRow("SELECT budget_spent FROM gateway_keys WHERE id=7")
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(&spent); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 1 || spent != .2 {
		t.Fatal("duplicate/lost charge", ledgerCount, spent)
	}
	if got, won, err := restarted.Claim(t.Context(), job("image_after_restart", "same-key")); err != nil || won || got.State != "unknown" {
		t.Fatal("restart erased deduplication", got, won, err)
	}
	prepared, err := restarted.Load(t.Context(), ImageArtifactOwner{UserID: 2}, other.RequestID)
	if err != nil || prepared.State != "failed" || prepared.Charge != 0 || !prepared.Settled {
		t.Fatal("undispatched request charged", prepared, err)
	}

	// Completed metadata survives a settlement failure and a worker restart.
	completed := job("image_completed", "completed-key")
	if _, _, err := store.Claim(t.Context(), completed); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE gateway_keys SET budget_limit=10 WHERE id=7")
	lease, err = budget.Reserve(t.Context(), 7, .2)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Arm(t.Context(), completed.RequestID, lease); err != nil {
		t.Fatal(err)
	}
	metadata := `{"created":123,"artifact_ids":["img_saved"]}`
	if err := store.Finish(t.Context(), completed.RequestID, "completed", .2, &metadata, ""); err != nil {
		t.Fatal(err)
	}
	broken := NewGatewayImageJobService(sess, &imageFailingBudget{GatewayBudgetService: budget})
	if err := broken.Settle(t.Context(), completed.RequestID); err == nil {
		t.Fatal("failure hidden")
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := restarted.Settle(t.Context(), completed.RequestID); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := restarted.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	got, err := restarted.Load(t.Context(), owner, completed.RequestID)
	if err != nil || !got.Settled || got.State != "completed" || got.ResultMetadata == nil || *got.ResultMetadata != metadata {
		t.Fatal("saved result lost", got, err)
	}
	row, err = sess.SQL().QueryRow("SELECT COUNT(*) FROM gateway_key_budget_ledger WHERE reservation_id=?", lease.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(&ledgerCount); err != nil || ledgerCount != 1 {
		t.Fatal("concurrent settlement duplicated ledger", ledgerCount, err)
	}
	row, err = sess.SQL().QueryRow("SELECT budget_spent FROM gateway_keys WHERE id=7")
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(&spent); err != nil || spent != .4 {
		t.Fatal("concurrent settlement duplicated charge", spent, err)
	}
}

type imageFailingBudget struct{ GatewayBudgetService }

func (*imageFailingBudget) Finalize(context.Context, *GatewayBudgetLease, float64) error {
	return errors.New("temporary settlement failure")
}

func assertImageBudgetHold(t *testing.T, sess db.Session, id string) {
	t.Helper()
	row, err := sess.SQL().QueryRow("SELECT settlement_status FROM gateway_key_budget_reservations WHERE reservation_id=?", id)
	if err != nil {
		t.Fatal(err)
	}
	var status string
	if err := row.Scan(&status); err != nil || status != "image_pending" {
		t.Fatal("image hold lost", status, err)
	}
}
