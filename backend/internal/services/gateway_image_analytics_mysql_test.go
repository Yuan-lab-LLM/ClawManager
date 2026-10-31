package services

import (
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"clawreef/internal/repository"
	"github.com/upper/db/v4"
)

func applyImageAnalyticsTestMigration(t *testing.T, sess db.Session) {
	t.Helper()
	raw, err := os.ReadFile("../db/migrations/074_add_image_analytics_outbox.sql")
	if err != nil {
		t.Fatal(err)
	}
	// PREPARE variables are connection-local. No DDL in the publication TX.
	conn, err := sess.Driver().(*sql.DB).Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, statement := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := conn.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
}

func TestImageAnalyticsMySQLAtomicRecovery(t *testing.T) {
	sess := imageMySQLTestSession(t)
	exec := func(query string, args ...interface{}) {
		t.Helper()
		if _, err := sess.SQL().Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := os.ReadFile("../db/migrations/073_add_gateway_image_jobs.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(schema))
	t.Cleanup(func() {
		for _, table := range []string{"cost_records", "model_invocations", "gateway_image_jobs"} {
			_, _ = sess.SQL().Exec("DROP TABLE IF EXISTS " + table)
		}
	})
	// Old jobs must not be emitted again when the migration is applied/retried.
	exec(`INSERT INTO gateway_image_jobs (request_id,scope_hash,idempotency_hash,request_hash,user_id,model_id,state,estimate,deadline_at,created_at,updated_at) VALUES ('legacy','legacy','legacy','legacy',1,2,'completed',0,NOW(),NOW(),NOW())`)
	applyImageAnalyticsTestMigration(t, sess)
	applyImageAnalyticsTestMigration(t, sess)
	repository.NewModelInvocationRepository(sess)
	repository.NewCostRecordRepository(sess)
	store := NewGatewayImageJobService(sess, nil)
	if err := store.PublishRecords(t.Context(), "legacy"); err != nil {
		t.Fatal(err)
	}
	snapshot := imageAnalyticsTestContext(t)
	claim := func(id string) {
		t.Helper()
		_, won, err := store.Claim(t.Context(), &ImageJob{RequestID: id, ScopeHash: ImageJobScope(ImageArtifactOwner{UserID: 1}), IdempotencyHash: ImageIdempotencyHash(id), RequestHash: imageJobHash([]byte(id)), UserID: 1, ModelID: 2, Estimate: .2, AnalyticsContext: &snapshot})
		if err != nil || !won {
			t.Fatal(won, err)
		}
	}
	count := func(table, id string) int {
		t.Helper()
		row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM "+table+" WHERE request_id=?", id)
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err := row.Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	claim("image_completed")
	if err := store.Arm(t.Context(), "image_completed", nil); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	outcome := &ImageJobOutcome{Dispatched: true, ProviderStatus: 200, UsageReported: true, PromptTokens: 10, CompletionTokens: 20, CompletedAt: &now}
	if err := store.Finish(t.Context(), "image_completed", "completed", .2, nil, "", outcome); err != nil {
		t.Fatal(err)
	}
	// Force the SECOND insert to fail: the invocation insert must roll back too.
	exec("ALTER TABLE cost_records ADD CONSTRAINT image_test_reject_cost CHECK (estimated_cost < 0)")
	if err := store.PublishRecords(t.Context(), "image_completed"); err == nil {
		t.Fatal("sink failure hidden")
	}
	if count("model_invocations", "image_completed") != 0 || count("cost_records", "image_completed") != 0 {
		t.Fatal("partial transaction committed")
	}
	got, err := store.Load(t.Context(), ImageArtifactOwner{UserID: 1}, "image_completed")
	if err != nil || got.AnalyticsRecorded || got.AnalyticsOutcome == nil {
		t.Fatal("outbox lost", got, err)
	}
	exec("ALTER TABLE cost_records DROP CHECK image_test_reject_cost")
	// Fresh worker, multiple competing consumers and replay after success.
	restarted := NewGatewayImageJobService(sess, nil)
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := restarted.PublishRecords(t.Context(), "image_completed"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := restarted.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if count("model_invocations", "image_completed") != 1 || count("cost_records", "image_completed") != 1 || count("model_invocations", "legacy") != 0 {
		t.Fatal("duplicate/missing analytics")
	}
	row, err := sess.SQL().QueryRow(`SELECT i.status,i.total_tokens,c.total_tokens,c.estimated_cost,c.actual_cost,c.invocation_id=i.id FROM model_invocations i JOIN cost_records c ON c.request_id=i.request_id WHERE i.request_id='image_completed'`)
	if err != nil {
		t.Fatal(err)
	}
	var status string
	var inputTokens, costTokens int
	var estimate float64
	var actual *float64
	var linked bool
	if err := row.Scan(&status, &inputTokens, &costTokens, &estimate, &actual, &linked); err != nil || status != "completed" || inputTokens != 30 || costTokens != 30 || estimate != .2 || actual != nil || !linked {
		t.Fatal(status, inputTokens, costTokens, estimate, actual, linked, err)
	}

	// Crash before finalization: running is unknown, preparing is free/failed.
	claim("image_crashed_running")
	claim("image_crashed_preparing")
	if err := store.Arm(t.Context(), "image_crashed_running", nil); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE gateway_image_jobs SET deadline_at='2000-01-01' WHERE request_id IN ('image_crashed_running','image_crashed_preparing')")
	if err := restarted.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if count("model_invocations", "image_crashed_running") != 1 || count("cost_records", "image_crashed_running") != 1 || count("model_invocations", "image_crashed_preparing") != 1 || count("cost_records", "image_crashed_preparing") != 0 {
		t.Fatal("incorrect crash recovery records")
	}
	row, err = sess.SQL().QueryRow("SELECT response_payload,latency_ms,completed_at FROM model_invocations WHERE request_id='image_crashed_running'")
	if err != nil {
		t.Fatal(err)
	}
	var response string
	var latency *int
	var finished *time.Time
	if err := row.Scan(&response, &latency, &finished); err != nil {
		t.Fatal(err)
	}
	var safe map[string]interface{}
	if err := json.Unmarshal([]byte(response), &safe); err != nil || safe["outcome_uncertain"] != true || safe["recovered_without_final_outcome"] != true || latency != nil || finished != nil {
		t.Fatal(safe, latency, finished, err)
	}
	// One corrupted/unsupported snapshot must not suppress other pending rows.
	claim("image_bad_snapshot")
	claim("image_good_after_bad")
	for _, id := range []string{"image_bad_snapshot", "image_good_after_bad"} {
		if err := store.Finish(t.Context(), id, "failed", 0, nil, "image_policy_denied"); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE gateway_image_jobs SET analytics_context='{"version":999}' WHERE request_id='image_bad_snapshot'`)
	if err := restarted.Recover(t.Context()); err == nil {
		t.Fatal("unrecoverable snapshot hidden")
	}
	if count("model_invocations", "image_bad_snapshot") != 0 || count("model_invocations", "image_good_after_bad") != 1 {
		t.Fatal("bad snapshot blocked good work")
	}
}
