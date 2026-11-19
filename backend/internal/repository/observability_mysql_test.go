package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/observability"
	_ "github.com/go-sql-driver/mysql"
	"github.com/upper/db/v4"
	"github.com/upper/db/v4/adapter/mysql"
)

type observationObjects map[string][]byte

func (s observationObjects) PutObject(_ context.Context, k string, b []byte, _ string) error {
	s[k] = append([]byte(nil), b...)
	return nil
}
func (s observationObjects) GetObject(_ context.Context, k string) ([]byte, error) {
	v, ok := s[k]
	if !ok {
		return nil, os.ErrNotExist
	}
	return v, nil
}

type failingObservationObjects struct {
	memory observationObjects
	fail   bool
}

func (s *failingObservationObjects) PutObject(ctx context.Context, k string, b []byte, m string) error {
	if s.fail {
		return errors.New("injected object outage")
	}
	return s.memory.PutObject(ctx, k, b, m)
}
func (s *failingObservationObjects) GetObject(ctx context.Context, k string) ([]byte, error) {
	if s.fail {
		return nil, errors.New("injected object outage")
	}
	return s.memory.GetObject(ctx, k)
}
func (s *failingObservationObjects) DeleteObservabilityObject(_ context.Context, k string) error {
	delete(s.memory, k)
	return nil
}

func TestDurableInvocationRealMySQLDedupAndHydration(t *testing.T) {
	dsn := os.Getenv("CM_OBSERVABILITY_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated marked mysql fixture required")
	}
	raw, e := sql.Open("mysql", dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer raw.Close()
	var marker int
	if e = raw.QueryRow("SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='cm_database_test_marker'").Scan(&marker); e != nil || marker != 1 {
		t.Fatalf("fixture marker: %v", e)
	}
	database := fmt.Sprintf("cm_observability_test_%d", time.Now().UnixNano())
	_, e = raw.Exec("CREATE DATABASE `" + database + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")
	defer raw.Exec("DROP DATABASE `" + database + "`")
	if e != nil {
		t.Fatal(e)
	}
	sess, e := mysql.Open(mysql.ConnectionURL{Host: "127.0.0.1:33317", Database: database, User: "root"})
	if e != nil {
		t.Fatal(e)
	}
	defer sess.Close()
	_, e = sess.SQL().Exec("DROP TABLE IF EXISTS model_invocations")
	if e != nil {
		t.Fatal(e)
	}
	_, e = sess.SQL().Exec(`CREATE TABLE model_invocations(id BIGINT PRIMARY KEY AUTO_INCREMENT, event_uid CHAR(64) NULL UNIQUE, trace_id VARCHAR(100), session_id VARCHAR(100), request_id VARCHAR(100), user_id BIGINT, instance_id BIGINT, instance_mode VARCHAR(30),runtime_type VARCHAR(30),gateway_id VARCHAR(100),runtime_pod_id BIGINT,gateway_key_id BIGINT,model_id BIGINT,provider_type VARCHAR(30),requested_model VARCHAR(100),actual_provider_model VARCHAR(100),traffic_class VARCHAR(30),request_payload LONGTEXT,response_payload LONGTEXT,request_body_ref JSON,response_body_ref JSON,prompt_tokens INT,completion_tokens INT,total_tokens INT,cached_tokens INT,reasoning_tokens INT,latency_ms INT,ttft_ms INT,generation_ms INT,tokens_per_second DOUBLE,attempt_no INT,is_streaming BOOLEAN,status VARCHAR(30),error_message TEXT,created_at DATETIME(6),completed_at DATETIME(6))`)
	if e != nil {
		t.Fatal(e)
	}
	summaryMigration, readErr := os.ReadFile("../db/migrations/116_add_query_summaries.sql")
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, statement := range strings.Split(string(summaryMigration), ";") {
		if strings.TrimSpace(statement) != "" {
			if _, err := sess.SQL().Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := sess.SQL().Exec(`CREATE TABLE chat_messages(id BIGINT PRIMARY KEY AUTO_INCREMENT)`); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"028_add_ai_gateway_runtime_attribution.sql", "114_add_history_maintenance.sql", "118_add_audit_trace_reports.sql", "119_add_session_cost_summaries.sql", "121_guard_observation_reconciliation.sql", "122_add_observation_archive_dependencies.sql", "123_add_monthly_log_catchup.sql"} {
		raw, err := os.ReadFile("../db/migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		for _, line := range strings.Split(strings.Split(string(raw), "SET @column_exists")[0], "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "--") {
				lines = append(lines, line)
			}
		}
		for _, statement := range strings.Split(strings.Join(lines, "\n"), ";") {
			if strings.TrimSpace(statement) != "" {
				if _, err = sess.SQL().Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	migration, e := os.ReadFile("../db/migrations/113_durable_observability.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, statement := range strings.Split(string(migration), ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" || strings.Contains(statement, "ALTER TABLE") {
			continue
		}
		if _, e = sess.SQL().Exec(statement); e != nil {
			t.Fatal(e)
		}
	}
	sess.SQL().Exec("DELETE FROM observability_admissions")
	j, e := observability.NewJournal(t.TempDir(), bytes.Repeat([]byte{1}, 32), observability.Limits{})
	if e != nil {
		t.Fatal(e)
	}
	memory := &failingObservationObjects{memory: observationObjects{}}
	objects, _ := observability.NewEncryptedObjects(memory, bytes.Repeat([]byte{1}, 32))
	restore := ConfigureObservability(sess, j, objects)
	defer restore()
	ctx := context.Background()
	if e = BeginObservedRequest(ctx, 1, "req-test"); e != nil {
		t.Fatal(e)
	}
	owner := 1
	body := strings.Repeat("secret", 2000)
	record := &models.ModelInvocation{TraceID: "trace-test", RequestID: "req-test", UserID: &owner, RequestPayload: &body, ResponsePayload: &body, ProviderType: "openai", RequestedModel: "m", ActualProviderModel: "m", Status: "completed", AttemptNo: 1}
	repo := NewModelInvocationRepository(sess)
	if e = repo.Create(record); e != nil {
		t.Fatal(e)
	}
	id := record.ID
	if id == 0 {
		t.Fatal("projection ID missing")
	}
	if e = repo.Create(record); e != nil {
		t.Fatal(e)
	}
	if record.ID != id {
		t.Fatal("duplicate invocation")
	}
	got, e := repo.GetByID(id)
	if e != nil || got.RequestPayload == nil || *got.RequestPayload != body {
		t.Fatalf("hydrate: %v %+v", e, got)
	}
	flowRepo := repo.(*modelInvocationRepository)
	previewRemaining := int64(2*len(body) - 1)
	if _, err := flowRepo.TraceFlowInvocation(ctx, record.TraceID, id, &previewRemaining); !errors.Is(err, ErrTraceFlowBudget) || previewRemaining != int64(2*len(body)-1) {
		t.Fatalf("oversized encrypted bodies read: %d %v", previewRemaining, err)
	}
	previewRemaining++
	preview, e := flowRepo.TraceFlowInvocation(ctx, record.TraceID, id, &previewRemaining)
	if e != nil || previewRemaining != 0 || preview.RequestPayload == nil || *preview.RequestPayload != body || preview.ResponsePayload == nil || *preview.ResponsePayload != body {
		t.Fatalf("flow preview did not hydrate within budget: %d %v", previewRemaining, e)
	}
	row, e := sess.SQL().QueryRow("SELECT COUNT(*),MAX(LENGTH(request_payload)) FROM model_invocations")
	if e != nil {
		t.Fatal(e)
	}
	var count int
	var length sql.NullInt64
	if e = row.Scan(&count, &length); e != nil {
		t.Fatal(e)
	}
	if count != 1 || length.Valid && length.Int64 != 0 {
		t.Fatalf("body copied or duplicate: %d %+v", count, length)
	}
	// A failed object upload must retain historical inline source and cursor.
	legacy := *record
	legacy.ID = 0
	legacy.EventUID = nil
	legacy.RequestID = "legacy-request"
	inserted, err := sess.Collection("model_invocations").Insert(&legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacyID := inserted.ID().(int64)
	memory.fail = true
	after, migrated, err := MigrateObservationBodies(ctx, "model_invocations", int64(id), 500)
	if err == nil || after != int64(id) || migrated != 0 {
		t.Fatalf("object outage migration advanced: %d %d %v", after, migrated, err)
	}
	var inline struct {
		Body *string `db:"request_payload"`
	}
	if err = sess.Collection("model_invocations").Find(db.Cond{"id": legacyID}).One(&inline); err != nil || inline.Body == nil || *inline.Body != body {
		t.Fatalf("source lost during failed upload: %v", err)
	}
	memory.fail = false
	after, migrated, err = MigrateObservationBodies(ctx, "model_invocations", int64(id), 500)
	if err != nil || after != legacyID || migrated != 1 {
		t.Fatalf("migration: %d %d %v", after, migrated, err)
	}
	if e = FinishObservedRequest(ctx, 1, "req-test"); e != nil {
		t.Fatal(e)
	}
	if _, e = ReplayObservability(ctx, 10); e != nil {
		t.Fatal(e)
	}
	for _, table := range []string{"team_redis_archive_receipts", "team_redis_dlq_receipts"} {
		if _, err := sess.SQL().Exec("CREATE TABLE " + table + "(object_ref JSON)"); err != nil {
			t.Fatal(err)
		}
	}
	if _, e = sess.SQL().Exec("UPDATE observability_objects SET retain_until=UTC_TIMESTAMP()-INTERVAL 40 DAY"); e != nil {
		t.Fatal(e)
	}
	if _, e = sess.SQL().Exec("UPDATE observability_body_refs SET created_at=UTC_TIMESTAMP()-INTERVAL 40 DAY"); e != nil {
		t.Fatal(e)
	}
	if n, err := ReclaimObservationObjects(ctx, 500); err != nil || n != 0 {
		t.Fatalf("deleted without restore proof: %d %v", n, err)
	}
	// The native proof writer is exercised with real signed backups and MinIO
	// in TestObservationNativeProofAndExpiredSnapshot. Seed that precondition here.
	if _, err = sess.SQL().Exec("UPDATE observability_objects SET backup_verified_at=UTC_TIMESTAMP(6),restore_verified_at=UTC_TIMESTAMP(6)"); err != nil {
		t.Fatal(err)
	}
	if err = BeginObservedRequest(ctx, 1, "pending-recovery"); err != nil {
		t.Fatal(err)
	}
	if n, err := ReclaimObservationObjects(ctx, 500); err != nil || n != 0 {
		t.Fatalf("deleted with pending recovery: %d %v", n, err)
	}
	if err = FinishObservedRequest(ctx, 1, "pending-recovery"); err != nil {
		t.Fatal(err)
	}
	if _, err = ReplayObservability(ctx, 500); err != nil {
		t.Fatal(err)
	}
	if n, err := ReclaimObservationObjects(ctx, 500); err != nil || n != 1 {
		t.Fatalf("eligible retention: %d %v", n, err)
	}
	expired, err := repo.GetByID(id)
	if err != nil || expired.BodyStatus["RequestPayload"] != "expired" {
		t.Fatalf("expired status missing: %+v %v", expired, err)
	}
	previewRemaining = 1 << 20
	preview, err = flowRepo.TraceFlowInvocation(ctx, record.TraceID, id, &previewRemaining)
	if err != nil || preview.RequestPayload != nil || preview.BodyStatus["RequestPayload"] != "expired" {
		t.Fatalf("flow preview expiration: %+v %v", preview.BodyStatus, err)
	}
	// An old unacknowledged event must not resurrect archived source rows or
	// re-account its fee after the hot row and body have expired.
	if _, err = sess.SQL().Exec("DELETE FROM model_invocations WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(record)
	if err = installedObservability.Load().project(ctx, observability.Event{UID: *record.EventUID, Owner: "user:1", RequestID: record.RequestID, Kind: "model_invocations", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	remaining, err := sess.Collection("model_invocations").Find(db.Cond{"id": id}).Count()
	if err != nil || remaining != 0 {
		t.Fatalf("archived event resurrected: %d %v", remaining, err)
	}
}

func TestBodyMaintenanceTableWhitelist(t *testing.T) {
	if _, _, err := MigrateObservationBodies(context.Background(), "users; DROP TABLE users", 0, 500); err == nil {
		t.Fatal("unregistered table accepted")
	}
}
