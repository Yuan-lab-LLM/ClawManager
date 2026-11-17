package aigateway

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/observability"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	_ "github.com/go-sql-driver/mysql"
	"github.com/upper/db/v4/adapter/mysql"
)

type gatewayObservationObjects map[string][]byte

func (s gatewayObservationObjects) PutObject(_ context.Context, k string, v []byte, _ string) error {
	s[k] = append([]byte(nil), v...)
	return nil
}
func (s gatewayObservationObjects) GetObject(_ context.Context, k string) ([]byte, error) {
	v, ok := s[k]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), v...), nil
}
func TestGatewayExecutedResultSurvivesSQLProjectionOutageAndRestart(t *testing.T) {
	dsn := os.Getenv("CM_OBSERVABILITY_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated marked mysql required")
	}
	raw, e := sql.Open("mysql", dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer raw.Close()
	var marker int
	if e = raw.QueryRow("SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='cm_database_test_marker'").Scan(&marker); e != nil || marker != 1 {
		t.Fatalf("marker: %d %v", marker, e)
	}
	name := fmt.Sprintf("cm_gateway_observe_%d", time.Now().UnixNano())
	if _, e = raw.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); e != nil {
		t.Fatal(e)
	}
	defer raw.Exec("DROP DATABASE `" + name + "`")
	sess, e := mysql.Open(mysql.ConnectionURL{Host: "127.0.0.1:33317", Database: name, User: "root"})
	if e != nil {
		t.Fatal(e)
	}
	defer sess.Close()
	for _, model := range []interface{}{models.ModelInvocation{}, models.AuditEvent{}, models.CostRecord{}, models.ChatMessageRecord{}, models.RiskHit{}} {
		typ := reflect.TypeOf(model)
		columns := []string{}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			column := strings.Split(field.Tag.Get("db"), ",")[0]
			if column == "" || column == "-" {
				continue
			}
			kind := field.Type
			if kind.Kind() == reflect.Ptr {
				kind = kind.Elem()
			}
			sqlType := "TEXT NULL"
			if kind == reflect.TypeOf(time.Time{}) {
				sqlType = "DATETIME(6) NULL"
			} else {
				switch kind.Kind() {
				case reflect.Int, reflect.Int64, reflect.Bool:
					sqlType = "BIGINT NULL"
				case reflect.Float64:
					sqlType = "DOUBLE NULL"
				}
			}
			if column == "id" {
				sqlType = "BIGINT PRIMARY KEY AUTO_INCREMENT"
			}
			if column == "event_uid" {
				sqlType = "CHAR(64) NULL UNIQUE"
			}
			columns = append(columns, "`"+column+"` "+sqlType)
		}
		table := model.(interface{ TableName() string }).TableName()
		if _, e = sess.SQL().Exec("CREATE TABLE `" + table + "`(" + strings.Join(columns, ",") + ")"); e != nil {
			t.Fatal(e)
		}
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
	for _, file := range []string{"114_add_history_maintenance.sql", "118_add_audit_trace_reports.sql", "119_add_session_cost_summaries.sql", "121_guard_observation_reconciliation.sql", "122_add_observation_archive_dependencies.sql", "123_add_monthly_log_catchup.sql"} {
		raw, err := os.ReadFile("../db/migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		for _, line := range strings.Split(string(raw), "\n") {
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
	key := bytes.Repeat([]byte{5}, 32)
	dir := t.TempDir()
	j, e := observability.NewJournal(dir, key, observability.Limits{})
	if e != nil {
		t.Fatal(e)
	}
	objects, _ := observability.NewEncryptedObjects(gatewayObservationObjects{}, key)
	restore := repository.ConfigureObservability(sess, j, objects)
	defer restore()
	ctx := context.Background()
	if e = repository.BeginObservedRequest(ctx, 1, "request-durable"); e != nil {
		t.Fatal(e)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if _, err := sess.SQL().Exec("RENAME TABLE model_invocations TO unavailable_invocations"); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"upstream-executed","choices":[{"message":{"role":"assistant","content":"accepted completed result"}}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`)
	}))
	defer server.Close()
	gateway := &service{httpClient: server.Client(), secretRefService: services.NewSecretRefService(), invocationService: services.NewModelInvocationService(repository.NewModelInvocationRepository(sess)), auditEventService: services.NewAuditEventService(repository.NewAuditEventRepository(sess)), costRecordService: services.NewCostRecordService(repository.NewCostRecordRepository(sess)), chatMessageService: services.NewChatMessageService(repository.NewChatMessageRepository(sess))}
	prepared := &preparedChatRequest{traceID: "trace-durable", requestID: "request-durable", requestIDPtr: stringPtr("request-durable"), sessionID: "session-durable", sessionIDPtr: stringPtr("session-durable"), userID: 1, userIDPtr: intPtr(1), attemptNumber: 1, observabilityAdmitted: true, req: ChatCompletionRequest{Model: "model", Messages: []ChatMessage{{Role: "user", Content: "hello"}}, SessionID: stringPtr("session-durable")}, resolvedModel: &models.LLMModel{ID: 1, DisplayName: "model", ProviderModelName: "model", ProviderType: models.ProviderTypeOpenAICompatible, ProtocolType: models.ProtocolTypeOpenAICompatible, BaseURL: server.URL, Currency: "USD", InputPrice: 1, OutputPrice: 1}}
	response, _, e := gateway.callNonStreamingDeployment(ctx, prepared)
	if e != nil || response == nil || response.StatusCode != 200 || !strings.Contains(string(response.Body), "accepted completed result") {
		t.Fatalf("successful execution mislabeled: %+v %v", response, e)
	}
	if e = repository.FinishObservedRequest(ctx, 1, "request-durable"); e != nil {
		t.Fatal(e)
	}
	states, e := j.Recovery(ctx)
	if e != nil || len(states) != 1 || states[0].Pending < 4 {
		t.Fatalf("real invocation/cost/chat/audit not durable: %+v %v", states, e)
	}
	if _, e = sess.SQL().Exec("RENAME TABLE unavailable_invocations TO model_invocations"); e != nil {
		t.Fatal(e)
	}
	restarted, e := observability.NewJournal(dir, key, observability.Limits{})
	if e != nil {
		t.Fatal(e)
	}
	restoreRestart := repository.ConfigureObservability(sess, restarted, objects)
	defer restoreRestart()
	if _, e = repository.ReplayObservability(ctx, 100); e != nil {
		t.Fatal(e)
	}
	if _, e = repository.ReplayObservability(ctx, 100); e != nil {
		t.Fatal(e)
	}
	for _, table := range []string{"model_invocations", "cost_records", "chat_messages", "audit_events"} {
		row, e := sess.SQL().QueryRow("SELECT COUNT(*) FROM " + table)
		if e != nil {
			t.Fatal(e)
		}
		var n int
		if e = row.Scan(&n); e != nil || n != 1 {
			t.Fatalf("%s replay cardinality %d %v", table, n, e)
		}
	}
	if calls != 1 {
		t.Fatalf("external work repeated %d times", calls)
	}
	if e = repository.BeginObservedRequest(ctx, 1, "request-durable"); e != observability.ErrRecoveryRequired {
		t.Fatalf("admission identity was reclaimed: %v", e)
	}
}
