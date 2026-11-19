package aigateway

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"clawreef/internal/config"
	appdb "clawreef/internal/db"
	"clawreef/internal/models"
	"clawreef/internal/observability"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/upper/db/v4"
)

// These fixtures use the application's complete migration path and only destroy
// the random database they create on the explicitly marked localhost test server.
func observationMatrixMySQL(t *testing.T) db.Session {
	t.Helper()
	dsn := os.Getenv("CM_OBSERVABILITY_TEST_DSN")
	if dsn == "" {
		t.Skip("CM_OBSERVABILITY_TEST_DSN requires isolated marked MySQL")
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil || cfg.Net != "tcp" || cfg.Addr != "127.0.0.1:33317" {
		t.Fatal("observation matrix requires marked localhost MySQL on port 33317")
	}
	root, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	var marker int
	if err = root.QueryRow("SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='cm_database_test_marker'").Scan(&marker); err != nil || marker != 1 {
		t.Fatalf("disposable MySQL marker required: %d %v", marker, err)
	}
	name := fmt.Sprintf("cm_gateway_matrix_%d", time.Now().UnixNano())
	if _, err = root.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := root.Exec("DROP DATABASE `" + name + "`"); err != nil {
			t.Error(err)
		}
	})
	prior := appdb.Session
	sess, err := appdb.Initialize(config.DatabaseConfig{Host: "127.0.0.1", Port: 33317, Database: name, User: cfg.User, Password: cfg.Passwd, MaxOpenConns: 8, MaxIdleConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close(); appdb.Session = prior })
	for _, id := range []int{901, 902} {
		if _, err = sess.SQL().Exec("INSERT INTO users(id,username,email,password_hash,is_active) VALUES(?,?,?,?,TRUE)", id, fmt.Sprintf("matrix-%d", id), fmt.Sprintf("matrix-%d@example.invalid", id), "test-only"); err != nil {
			t.Fatal(err)
		}
	}
	return sess
}

func observationMatrixGateway(t *testing.T, sess db.Session, upstream *httptest.Server) *service {
	t.Helper()
	modelRepo := repository.NewLLMModelRepository(sess)
	model := &models.LLMModel{DisplayName: "matrix-model", ModelGroup: "matrix", ProviderModelName: "matrix-provider", ProviderType: models.ProviderTypeOpenAICompatible, ProtocolType: models.ProtocolTypeOpenAICompatible, BaseURL: upstream.URL, IsActive: true, Currency: "USD", InputPrice: 2, OutputPrice: 5, ContextWindowTokens: 100000}
	existing, err := modelRepo.GetByDisplayName(model.DisplayName)
	if err != nil {
		t.Fatal(err)
	}
	if existing != nil {
		model.ID = existing.ID
		model.CreatedAt = existing.CreatedAt
	}
	if err := modelRepo.Save(model); err != nil {
		t.Fatal(err)
	}
	return &service{
		modelRepo: modelRepo, httpClient: upstream.Client(), streamHTTPClient: upstream.Client(), secretRefService: services.NewSecretRefService(),
		invocationService:  services.NewModelInvocationService(repository.NewModelInvocationRepository(sess)),
		auditEventService:  services.NewAuditEventService(repository.NewAuditEventRepository(sess)),
		costRecordService:  services.NewCostRecordService(repository.NewCostRecordRepository(sess)),
		chatMessageService: services.NewChatMessageService(repository.NewChatMessageRepository(sess)),
		chatSessionService: services.NewChatSessionService(repository.NewChatSessionRepository(sess)),
		riskDetector:       routingRiskDetector{}, riskHitService: services.NewRiskHitService(nil),
	}
}

func observationMatrixRequest(id string) ChatCompletionRequest {
	return ChatCompletionRequest{Model: "matrix-model", RequestID: stringPtr(id), TraceID: stringPtr("trace-" + id), SessionID: stringPtr("session-" + id), Messages: []ChatMessage{{Role: "user", Content: "hello matrix"}}}
}

func observationMatrixCall(t *testing.T, ctx context.Context, gateway *service, userID int, req ChatCompletionRequest) (*ProxyResponse, string, error) {
	t.Helper()
	if !req.Stream {
		return gateway.ChatCompletions(ctx, userID, req)
	}
	recorder := httptest.NewRecorder()
	trace, err := gateway.StreamChatCompletions(ctx, userID, req, recorder)
	if err != nil {
		return nil, trace, err
	}
	result := recorder.Result()
	defer result.Body.Close()
	if !recorder.Flushed || result.Trailer.Get("X-ClawManager-Persistence") != "durably-recorded" {
		t.Errorf("stream did not flush with a durable completion trailer: flushed=%t trailers=%v", recorder.Flushed, result.Trailer)
	}
	headers := result.Header.Clone()
	for name, values := range result.Trailer {
		headers[name] = values
	}
	return &ProxyResponse{StatusCode: result.StatusCode, Headers: headers, Body: append([]byte(nil), recorder.Body.Bytes()...)}, trace, nil
}

type observationMatrixBudget struct {
	routingBudgetStore
	settlements atomic.Int32
}

func (s *observationMatrixBudget) Finalize(ctx context.Context, lease *services.GatewayBudgetLease, amount float64) error {
	s.settlements.Add(1)
	return s.routingBudgetStore.Finalize(ctx, lease, amount)
}

func TestGatewayObservationMatrixExecutedResultSurvivesSQLAndObjectOutage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			observationMatrixExecutedResult(t, stream)
		})
	}
}

func observationMatrixExecutedResult(t *testing.T, stream bool) {
	sess := observationMatrixMySQL(t)
	ctx := t.Context()
	key := bytes.Repeat([]byte{27}, 32)
	dir, objectDir := t.TempDir(), t.TempDir()
	journal, err := observability.NewJournal(dir, key, observability.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	store, err := services.NewObjectStorageService(config.ObjectStorageConfig{LocalFallback: objectDir})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := observability.NewEncryptedObjects(store, key)
	if err != nil {
		t.Fatal(err)
	}
	restore := repository.ConfigureObservability(sess, journal, objects)
	defer restore()
	content := strings.Repeat("provider-accepted-完整结果;", 600)
	responseBody, err := json.Marshal(map[string]interface{}{"id": "matrix-executed-once", "choices": []interface{}{map[string]interface{}{"message": map[string]string{"role": "assistant", "content": content}}}, "usage": map[string]int{"prompt_tokens": 3, "completion_tokens": 4, "total_tokens": 7}})
	if err != nil {
		t.Fatal(err)
	}
	if stream {
		var events strings.Builder
		for index := 0; index < 2; index++ {
			choice := map[string]interface{}{"delta": map[string]string{"content": strings.Repeat("provider-accepted-完整结果;", 300)}}
			if index == 1 {
				choice["finish_reason"] = "stop"
			}
			chunk, err := json.Marshal(map[string]interface{}{"id": "matrix-executed-once", "choices": []interface{}{choice}})
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&events, "data: %s\n\n", chunk)
		}
		events.WriteString("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":4,\"total_tokens\":7}}\n\ndata: [DONE]\n\n")
		responseBody = []byte(events.String())
	}
	var calls atomic.Int32
	providerRequest := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		providerRequest <- body
		// Both failures start only after the actual HTTP provider accepts work.
		_, err = sess.SQL().Exec("CREATE TRIGGER matrix_receipt_outage BEFORE INSERT ON observability_event_receipts FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='matrix SQL projection unavailable'")
		if err == nil {
			err = os.Rename(objectDir, objectDir+"-healthy")
		}
		if err == nil {
			err = os.WriteFile(objectDir, []byte("object storage outage"), 0600)
		}
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		contentType := "application/json"
		if stream {
			contentType = "text/event-stream"
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(responseBody)
		if stream {
			w.(http.Flusher).Flush()
		}
	}))
	defer upstream.Close()
	t.Cleanup(func() { os.RemoveAll(objectDir + "-healthy") })
	gateway := observationMatrixGateway(t, sess, upstream)
	budget := &observationMatrixBudget{}
	var finalized atomic.Int32
	req := observationMatrixRequest("simultaneous-outage")
	req.Stream = stream
	req.UsageFinalizer = func(tokens int) error {
		if tokens != 7 {
			t.Errorf("settled token count %d, want 7", tokens)
		}
		finalized.Add(1)
		return nil
	}
	req.BudgetController = services.NewGatewayBudgetController(budget, 1)
	response, trace, err := observationMatrixCall(t, ctx, gateway, 901, req)
	if err != nil || response == nil || response.StatusCode != http.StatusOK || trace != *req.TraceID {
		t.Fatalf("executed result mislabeled: response=%v trace=%s err=%v", response, trace, err)
	}
	if stream && !bytes.HasSuffix(response.Body, []byte("data: [DONE]\n\n")) {
		t.Fatalf("client streaming terminal event lacks complete SSE delimiter: got tail %q", response.Body[max(0, len(response.Body)-64):])
	}
	if !bytes.Equal(response.Body, responseBody) {
		t.Fatalf("executed result changed: got %d bytes, want %d", len(response.Body), len(responseBody))
	}
	if got := response.Headers.Get("X-ClawManager-Persistence"); got != "durably-recorded" || response.Headers.Get("X-ClawManager-Request-ID") != *req.RequestID {
		t.Fatalf("successful durable WAL response headers: %v", response.Headers)
	}
	if _, err = sess.SQL().Exec("INSERT INTO observability_event_receipts(event_uid,source_table,original_id,payload_sha256) VALUES(?, 'cost_records', 1, ?)", strings.Repeat("a", 64), strings.Repeat("b", 64)); err == nil || !strings.Contains(err.Error(), "matrix SQL projection unavailable") {
		t.Fatalf("real SQL projection fault inactive: %v", err)
	}
	if err = store.PutObject(ctx, "observability/fault-probe", []byte("probe"), "application/octet-stream"); err == nil {
		t.Fatal("real filesystem object outage inactive")
	}
	states, err := journal.Recovery(ctx)
	if err != nil || len(states) != 1 || states[0].Pending != 4 || states[0].State != "terminal" {
		t.Fatalf("four accepted completion facts must remain in WAL: %+v %v", states, err)
	}
	// Replay a separate authenticated snapshot to inspect every pending full fact
	// without acknowledging or mutating the original journal under test.
	snapshot, err := journal.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	copyDir := t.TempDir()
	for name, body := range snapshot {
		path := filepath.Join(copyDir, name)
		if err = os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
		if err = os.Truncate(path, 64<<20); err != nil {
			t.Fatal(err)
		}
	}
	inspection, err := observability.NewJournal(copyDir, key, observability.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	facts := make(map[string]observability.Event)
	if n, err := inspection.Replay(ctx, 100, func(_ context.Context, event observability.Event) error {
		if _, duplicate := facts[event.Kind]; duplicate {
			t.Errorf("duplicate pending fact %s", event.Kind)
		}
		facts[event.Kind] = event
		return nil
	}); err != nil || n != 4 {
		t.Fatalf("inspect durable completion facts: %d %v", n, err)
	}
	var invocation models.ModelInvocation
	var cost models.CostRecord
	var chat models.ChatMessageRecord
	var audit models.AuditEvent
	for kind, target := range map[string]interface{}{"model_invocations": &invocation, "cost_records": &cost, "chat_messages": &chat, "audit_events": &audit} {
		if err = json.Unmarshal(facts[kind].Payload, target); err != nil {
			t.Fatalf("decode durable %s: %v", kind, err)
		}
	}
	wantRequest := <-providerRequest
	if invocation.RequestPayload == nil || *invocation.RequestPayload != string(wantRequest) || invocation.ResponsePayload == nil || *invocation.ResponsePayload != string(responseBody) || invocation.PromptTokens != 3 || invocation.CompletionTokens != 4 || invocation.TotalTokens != 7 || invocation.Status != models.ModelInvocationStatusCompleted || invocation.IsStreaming != stream {
		t.Fatalf("WAL lost full provider request/result/usage: %+v", invocation)
	}
	if chat.Content != content || chat.Role != "assistant" || audit.EventType != "gateway.request.completed" || cost.PromptTokens != 3 || cost.CompletionTokens != 4 || cost.TotalTokens != 7 || cost.Currency != "USD" || math.Abs(cost.EstimatedCost-0.000026) > 1e-12 || cost.InputUnitPrice != 2 || cost.OutputUnitPrice != 5 {
		t.Fatalf("WAL lost completion/accounting values: chat=%+v cost=%+v audit=%+v", chat, cost, audit)
	}
	restarted, err := observability.NewJournal(dir, key, observability.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	restoreRestart := repository.ConfigureObservability(sess, restarted, objects)
	defer restoreRestart()
	if n, err := repository.ReplayObservability(ctx, 100); err == nil || n != 0 {
		t.Fatalf("restart replay must retain work while both stores are down: %d %v", n, err)
	}
	if _, err = sess.SQL().Exec("DROP TRIGGER matrix_receipt_outage"); err != nil {
		t.Fatal(err)
	}
	if n, err := repository.ReplayObservability(ctx, 100); err == nil || n != 0 {
		t.Fatalf("SQL recovery cannot acknowledge unavailable full objects: %d %v", n, err)
	}
	states, err = restarted.Recovery(ctx)
	if err != nil || len(states) != 1 || states[0].Pending != 4 {
		t.Fatalf("object outage lost pending completion facts after restart: %+v %v", states, err)
	}
	if err = os.Remove(objectDir); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(objectDir+"-healthy", objectDir); err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		n, err := repository.ReplayObservability(ctx, 100)
		want := 4
		if pass == 1 {
			want = 0
		}
		if err != nil || n != want {
			t.Fatalf("recovered replay %d: delivered=%d want=%d err=%v", pass, n, want, err)
		}
		for kind, event := range facts {
			var count, originalID int
			var digest string
			row, err := sess.SQL().QueryRow("SELECT COUNT(*),COALESCE(MAX(original_id),0),COALESCE(MAX(payload_sha256),'') FROM observability_event_receipts WHERE event_uid=? AND source_table=?", event.UID, kind)
			if err != nil {
				t.Fatal(err)
			}
			if err = row.Scan(&count, &originalID, &digest); err != nil || count != 1 || originalID <= 0 || digest != event.ReceiptDigest() {
				t.Fatalf("replay %d %s receipt cardinality/identity: %d %d %s %v", pass, kind, count, originalID, digest, err)
			}
			row, err = sess.SQL().QueryRow("SELECT COUNT(*) FROM "+kind+" WHERE event_uid=? AND id=?", event.UID, originalID)
			if err != nil {
				t.Fatal(err)
			}
			if err = row.Scan(&count); err != nil || count != 1 {
				t.Fatalf("replay %d %s business cardinality %d %v", pass, kind, count, err)
			}
		}
		for table, filter := range map[string]string{"model_invocations": "", "cost_records": "", "chat_messages": " AND role='assistant'", "audit_events": " AND event_type='gateway.request.completed'"} {
			var count int
			row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM "+table+" WHERE request_id=?"+filter, *req.RequestID)
			if err != nil {
				t.Fatal(err)
			}
			if err = row.Scan(&count); err != nil || count != 1 {
				t.Fatalf("replay %d %s duplicated completed business work: %d %v", pass, table, count, err)
			}
		}
	}
	rows, err := repository.NewModelInvocationRepository(sess).ListByTraceID(trace)
	if err != nil || len(rows) != 1 || rows[0].ResponsePayload == nil || *rows[0].ResponsePayload != string(responseBody) || rows[0].RequestPayload == nil || *rows[0].RequestPayload != string(wantRequest) || rows[0].TotalTokens != 7 || rows[0].ResponseBodyRef == nil || rows[0].IsStreaming != stream {
		t.Fatalf("recovered serving read lost provider result: %+v %v", rows, err)
	}
	messages, err := repository.NewChatMessageRepository(sess).ListByTraceID(trace)
	if err != nil || len(messages) != 2 || messages[1].Role != "assistant" || messages[1].Content != content || messages[1].ContentBodyRef == nil {
		t.Fatalf("recovered serving chat read lost full object: %+v %v", messages, err)
	}
	var storedCost models.CostRecord
	if err = sess.Collection("cost_records").Find(db.Cond{"event_uid": facts["cost_records"].UID}).One(&storedCost); err != nil || math.Abs(storedCost.EstimatedCost-cost.EstimatedCost) > 1e-12 || storedCost.Currency != "USD" || storedCost.TotalTokens != 7 || storedCost.InvocationID == nil || *storedCost.InvocationID != rows[0].ID {
		t.Fatalf("recovered cost or invocation identity changed: %+v %v", storedCost, err)
	}
	if response, _, err := observationMatrixCall(t, ctx, gateway, 901, req); !errors.Is(err, observability.ErrRecoveryRequired) || response != nil {
		t.Fatalf("recovered request identity allowed a provider retry: %+v %v", response, err)
	}
	if calls.Load() != 1 || finalized.Load() != 1 || budget.settlements.Load() != 1 || math.Abs(budget.finalized-cost.EstimatedCost) > 1e-12 {
		t.Fatalf("provider/usage/charge repeated during replay: provider=%d usage=%d charge=%d amount=%g", calls.Load(), finalized.Load(), budget.settlements.Load(), budget.finalized)
	}
}

func TestGatewayObservationMatrixDiskFullRejectsBeforeProvider(t *testing.T) {
	base := os.Getenv("CM_OBSERVABILITY_TEST_SMALL_DISK")
	if base == "" {
		t.Skip("CM_OBSERVABILITY_TEST_SMALL_DISK requires a disposable 24 MiB filesystem")
	}
	sess := observationMatrixMySQL(t)
	dir, err := os.MkdirTemp(base, "gateway-matrix-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	const reserve = 20 << 20
	key := bytes.Repeat([]byte{28}, 32)
	journal, err := observability.NewJournal(dir, key, observability.Limits{RequestBytes: reserve})
	if err != nil {
		t.Fatal(err)
	}
	store, err := services.NewObjectStorageService(config.ObjectStorageConfig{LocalFallback: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := observability.NewEncryptedObjects(store, key)
	if err != nil {
		t.Fatal(err)
	}
	restore := repository.ConfigureObservability(sess, journal, objects)
	defer restore()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"reserved completion"}}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`)
	}))
	defer upstream.Close()
	defer unblock()
	gateway := observationMatrixGateway(t, sess, upstream)
	accepted := observationMatrixRequest("accepted-on-small-disk")
	type outcome struct {
		response *ProxyResponse
		err      error
	}
	completed := make(chan outcome, 1)
	go func() {
		response, _, err := gateway.ChatCompletions(t.Context(), 901, accepted)
		completed <- outcome{response, err}
	}()
	select {
	case <-entered:
	case result := <-completed:
		t.Fatalf("accepted request did not reach provider: %+v", result)
	case <-time.After(15 * time.Second):
		t.Fatal("accepted request did not reach provider")
	}
	rejectCtx, cancelReject := context.WithTimeout(t.Context(), 5*time.Second)
	response, _, err := gateway.ChatCompletions(rejectCtx, 901, observationMatrixRequest("rejected-on-full-disk"))
	if !errors.Is(err, syscall.ENOSPC) || response != nil || calls.Load() != 1 {
		t.Fatalf("full disk must reject before another HTTP provider call: response=%+v err=%v calls=%d", response, err, calls.Load())
	}
	streamRejected := observationMatrixRequest("stream-rejected-on-full-disk")
	streamRejected.Stream = true
	response, _, err = observationMatrixCall(t, rejectCtx, gateway, 901, streamRejected)
	cancelReject()
	if !errors.Is(err, syscall.ENOSPC) || response != nil || calls.Load() != 1 {
		t.Fatalf("stream full disk must reject before another HTTP provider call: response=%+v err=%v calls=%d", response, err, calls.Load())
	}
	var rejectedAdmissions int
	row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM observability_admissions WHERE request_id IN (?,?)", "rejected-on-full-disk", "stream-rejected-on-full-disk")
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&rejectedAdmissions); err != nil || rejectedAdmissions != 0 {
		t.Fatalf("rejected request acquired durable admission: %d %v", rejectedAdmissions, err)
	}
	unblock()
	select {
	case result := <-completed:
		if result.err != nil || result.response == nil || result.response.StatusCode != http.StatusOK || !strings.Contains(string(result.response.Body), "reserved completion") || result.response.Headers.Get("X-ClawManager-Persistence") != "durably-recorded" {
			t.Fatalf("disk exhaustion consumed accepted completion reserve: %+v", result)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("accepted request could not finish with a full disk")
	}
	for _, table := range []string{"model_invocations", "cost_records"} {
		var count, tokens int
		row, err := sess.SQL().QueryRow("SELECT COUNT(*),COALESCE(SUM(total_tokens),0) FROM "+table+" WHERE request_id=?", *accepted.RequestID)
		if err != nil {
			t.Fatal(err)
		}
		if err = row.Scan(&count, &tokens); err != nil || count != 1 || tokens != 7 {
			t.Fatalf("accepted %s completion lost: count=%d tokens=%d err=%v", table, count, tokens, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("provider called %d times", calls.Load())
	}
	if _, err = repository.ReplayObservability(t.Context(), 100); err != nil {
		t.Fatal(err)
	}
	response, _, err = gateway.ChatCompletions(t.Context(), 901, observationMatrixRequest("rejected-on-full-disk"))
	if err != nil || response == nil || response.StatusCode != http.StatusOK || calls.Load() != 2 {
		t.Fatalf("request rejected before dispatch could not retry after replay released the reservation: response=%+v err=%v calls=%d", response, err, calls.Load())
	}
}

func TestGatewayObservationMatrixLongHistoryPreservesOriginalRequest(t *testing.T) {
	sess := observationMatrixMySQL(t)
	key := bytes.Repeat([]byte{29}, 32)
	journal, err := observability.NewJournal(t.TempDir(), key, observability.Limits{RequestBytes: 20 << 20})
	if err != nil {
		t.Fatal(err)
	}
	store, err := services.NewObjectStorageService(config.ObjectStorageConfig{LocalFallback: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := observability.NewEncryptedObjects(store, key)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.ConfigureObservability(sess, journal, objects)()

	req := observationMatrixRequest("long-history")
	for i := 0; i < 80; i++ {
		req.Messages = append(req.Messages, ChatMessage{Role: "assistant", Content: fmt.Sprintf("reply %d", i)}, ChatMessage{Role: "user", Content: fmt.Sprintf("question %d", i)})
	}
	longInput := strings.Repeat(" alpha beta gamma delta", 250000)
	longOutput := strings.Repeat("model output ", 1600000)
	currentTurnStart := len(req.Messages)
	req.Messages = append(req.Messages, ChatMessage{Role: "user", Content: longInput})
	for i := 0; i < 80; i++ {
		callID := fmt.Sprintf("call-%d", i)
		req.Messages = append(req.Messages,
			ChatMessage{Role: "assistant", ToolCalls: []ToolCall{{ID: callID, Type: "function", Function: &ToolCallFunction{Name: "read_result", Arguments: "{}"}}}},
			ChatMessage{Role: "tool", ToolCallID: callID, Content: fmt.Sprintf("tool result %d", i)},
		)
	}
	var providerMessages atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(body.Messages) != len(req.Messages) || body.Messages[currentTurnStart].Content != longInput {
			t.Error("provider request lost history or original long content")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		providerMessages.Store(int32(len(body.Messages)))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q}}],"usage":{"prompt_tokens":1000000,"completion_tokens":64000,"total_tokens":1064000}}`, longOutput)
	}))
	defer upstream.Close()
	gateway := observationMatrixGateway(t, sess, upstream)
	response, trace, err := gateway.ChatCompletions(t.Context(), 901, req)
	if err != nil || response == nil || response.StatusCode != http.StatusOK {
		t.Fatalf("long history failed: %v", err)
	}
	if providerMessages.Load() != int32(len(req.Messages)) {
		t.Fatal("provider did not receive the entire history")
	}
	if !strings.Contains(string(response.Body), longOutput) {
		t.Fatal("gateway did not return the entire provider response")
	}
	for {
		n, err := repository.ReplayObservability(t.Context(), 500)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	var messages int
	row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM chat_messages WHERE trace_id=?", trace)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&messages); err != nil || messages != len(req.Messages)-currentTurnStart+1 {
		t.Fatalf("current turn and tool history were not persisted once: messages=%d err=%v", messages, err)
	}
	var invocation models.ModelInvocation
	if err = sess.Collection("model_invocations").Find(db.Cond{"trace_id": trace, "request_id": *req.RequestID}).One(&invocation); err != nil {
		t.Fatal(err)
	}
	bodyReader := repository.NewModelInvocationRepository(sess).(interface {
		TraceBody(context.Context, string, string, int, int) (*repository.TraceBodyChunk, error)
	})
	var body strings.Builder
	for offset := 0; ; {
		chunk, err := bodyReader.TraceBody(t.Context(), trace, "model_invocations", invocation.ID, offset)
		if err != nil {
			t.Fatal(err)
		}
		body.WriteString(chunk.Data)
		if chunk.NextOffset == 0 {
			if body.Len() != chunk.TotalBytes {
				t.Fatal("Trace body ended before the reported byte count")
			}
			break
		}
		if chunk.NextOffset <= offset {
			t.Fatal("Trace body offset did not advance")
		}
		offset = chunk.NextOffset
	}
	if err = json.Unmarshal([]byte(body.String()), &invocation); err != nil {
		t.Fatal(err)
	}
	if invocation.RequestBodyRef == nil || invocation.ResponseBodyRef == nil ||
		invocation.RequestPayload == nil || invocation.ResponsePayload == nil ||
		!strings.Contains(*invocation.RequestPayload, longInput) || !strings.Contains(*invocation.ResponsePayload, longOutput) ||
		invocation.PromptTokens != 1000000 || invocation.CompletionTokens != 64000 {
		t.Fatal("Trace lost the original request, response, or token usage")
	}
	var costRows, tokens int
	row, err = sess.SQL().QueryRow("SELECT COUNT(*), COALESCE(SUM(total_tokens), 0) FROM cost_records WHERE request_id=?", *req.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&costRows, &tokens); err != nil || costRows != 1 || tokens != 1064000 {
		t.Fatalf("request token usage was not settled once: rows=%d tokens=%d err=%v", costRows, tokens, err)
	}
}

func TestGatewayObservationMatrixLargeProviderErrorPreservesOriginalResponse(t *testing.T) {
	sess := observationMatrixMySQL(t)
	key := bytes.Repeat([]byte{30}, 32)
	journal, err := observability.NewJournal(t.TempDir(), key, observability.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	store, err := services.NewObjectStorageService(config.ObjectStorageConfig{LocalFallback: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := observability.NewEncryptedObjects(store, key)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.ConfigureObservability(sess, journal, objects)()

	responseBody := strings.Repeat("Detailed provider error. ", 4000)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, responseBody)
	}))
	defer upstream.Close()
	gateway := observationMatrixGateway(t, sess, upstream)
	req := observationMatrixRequest("long-provider-error")
	res, trace, err := gateway.ChatCompletions(t.Context(), 901, req)
	if err != nil || res == nil || res.StatusCode != http.StatusBadRequest || string(res.Body) != responseBody {
		t.Fatalf("provider error did not reach client intact: %v", err)
	}
	if _, err := repository.ReplayObservability(t.Context(), 500); err != nil {
		t.Fatalf("large provider error blocked replay: %v", err)
	}
	rows, err := repository.NewModelInvocationRepository(sess).ListByTraceID(trace)
	if err != nil || len(rows) != 1 || rows[0].ResponsePayload == nil || !strings.Contains(*rows[0].ResponsePayload, strings.TrimSpace(responseBody)) {
		t.Fatalf("failed invocation lost the original provider error: %v", err)
	}
}
