package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4/adapter/mysql"
)

// Optional real-MySQL test. Dedicated local disposable database only; never
// point this test at a deployment database. It simulates 500 lifecycles without
// provisioning 500 runtimes or touching any user workspaces.
func TestLifecycleBatchMySQL500(t *testing.T) {
	host := os.Getenv("CLAWMANAGER_BATCH_TEST_MYSQL")
	if host == "" {
		t.Skip("set CLAWMANAGER_BATCH_TEST_MYSQL for disposable local MySQL")
	}
	if !strings.HasPrefix(host, "127.0.0.1:") {
		t.Fatal("test must use a local disposable database")
	}
	database := os.Getenv("CLAWMANAGER_BATCH_TEST_DB")
	if database == "" {
		database = "clawmanager_batch_test"
	}
	if database != "clawmanager_batch_test" && !strings.HasPrefix(database, "clawmanager_batch_test_") {
		t.Fatal("disposable test database required")
	}
	sess, err := mysql.Open(mysql.ConnectionURL{Host: host, User: "root", Password: os.Getenv("CLAWMANAGER_BATCH_TEST_PASSWORD"), Database: database, Options: map[string]string{"parseTime": "true"}})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	for _, ddl := range []string{
		`CREATE TABLE instances (id INT PRIMARY KEY,user_id INT NOT NULL,name VARCHAR(255),instance_mode VARCHAR(20),status VARCHAR(20))`,
		`CREATE TABLE team_members (id INT PRIMARY KEY,instance_id INT)`,
		`CREATE TABLE instance_runtime_bindings (instance_id INT PRIMARY KEY,runtime_pod_id BIGINT)`,
		`CREATE TABLE northbound_operations (id BIGINT PRIMARY KEY AUTO_INCREMENT,operation_id VARCHAR(64) UNIQUE,user_id INT,session_id VARCHAR(64),api_key_id VARCHAR(64) NULL,auth_scope VARCHAR(64) NULL,operation_type VARCHAR(64),idempotency_key_hash CHAR(64),request_hash CHAR(64),request_payload JSON,status VARCHAR(32),available_at DATETIME(6),instance_id INT NULL,attempt_count INT DEFAULT 0,lease_owner VARCHAR(128),lease_expires_at DATETIME(6),error_code VARCHAR(64),error_message VARCHAR(512),created_at DATETIME(6),started_at DATETIME(6),finished_at DATETIME(6),updated_at DATETIME(6))`,
	} {
		if _, err := sess.SQL().Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile("../db/migrations/064_add_instance_lifecycle_batches.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range strings.Split(string(data), ";") {
		if strings.TrimSpace(sql) != "" {
			if _, err := sess.SQL().Exec(sql); err != nil {
				t.Fatal(err)
			}
		}
	}
	r := NewNorthboundRepository(sess)
	if _, err := sess.SQL().Exec(`ALTER TABLE northbound_operations ADD UNIQUE KEY uk_operation_request (user_id,operation_type,idempotency_key_hash)`); err != nil {
		t.Fatal(err)
	}
	policySQL, err := os.ReadFile("../db/migrations/065_add_lifecycle_batch_retry_policy.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range strings.Split(string(policySQL), ";") {
		if strings.TrimSpace(stmt) != "" {
			if _, err := sess.SQL().Exec(stmt); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Existing deployments may use different DB defaults from migration 045.
	// Exercise mixed collations rather than the fresh-DB happy path alone.
	if _, err := sess.SQL().Exec("ALTER TABLE northbound_operations CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.SQL().Exec("ALTER TABLE instance_lifecycle_batch_items CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.HasBatchReservation(1); err != nil {
		t.Fatalf("mixed collation access check: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	for group := 0; group < 2; group++ {
		action := "restart"
		if group == 1 {
			action = "reset"
		}
		b := &models.InstanceLifecycleBatch{BatchID: fmt.Sprintf("batch-%d", group), UserID: 1, Action: action, Status: "running", RequestHash: strings.Repeat("a", 64), CreatedAt: now, UpdatedAt: now}
		items := []models.InstanceLifecycleBatchItem{}
		ops := []*models.NorthboundOperation{}
		for n := 1; n <= 250; n++ {
			id := group*250 + n
			name := fmt.Sprintf("accept-%d", id)
			if _, err := sess.SQL().Exec(`INSERT INTO instances VALUES (?,1,?,'lite','error')`, id, name); err != nil {
				t.Fatal(err)
			}
			if _, err := sess.SQL().Exec(`INSERT INTO instance_runtime_bindings VALUES (?,?)`, id, id%20+1); err != nil {
				t.Fatal(err)
			}
			opID := fmt.Sprintf("op-%d", id)
			items = append(items, models.InstanceLifecycleBatchItem{BatchID: b.BatchID, SourceID: id, SourceName: name, RuntimeType: fmt.Sprintf("type-%d-%d", group, id%10), OperationID: opID, State: "pending"})
			ops = append(ops, &models.NorthboundOperation{OperationID: opID, UserID: 1, SessionID: b.BatchID, OperationType: "lite_instance_" + action, IdempotencyKeyHash: strings.Repeat("a", 64), RequestHash: strings.Repeat("b", 64), RequestPayload: fmt.Sprintf(`{"instance_id":%d}`, id), Status: "batch_pending", AvailableAt: now, InstanceID: &id, CreatedAt: now, UpdatedAt: now})
		}
		for _, op := range ops {
			op.IdempotencyKeyHash = fmtLifecycleRetryHash(op.OperationID)
		}
		if err := r.CreateLifecycleBatch(ctx, b, items, ops, false); err != nil {
			t.Fatal(err)
		}
		if err := r.CreateLifecycleBatch(ctx, b, items, ops, false); err != nil {
			t.Fatalf("idempotent replay: %v", err)
		}
	}
	if err := r.ControlLifecycleBatch(ctx, "batch-1", 1, "pause"); err != nil {
		t.Fatal(err)
	}
	if err := r.AdvanceLifecycleBatches(ctx); err != nil {
		t.Fatal(err)
	}
	paused, _ := r.LifecycleBatchItems("batch-1")
	for _, i := range paused {
		if i.State != "pending" {
			t.Fatal("paused batch dispatched")
		}
	}
	if err := r.ControlLifecycleBatch(ctx, "batch-1", 1, "resume"); err != nil {
		t.Fatal(err)
	}
	completed := 0
	peak := 0
	for round := 0; round < 510 && completed < 500; round++ {
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for n := 0; n < 8; n++ {
			wg.Add(1)
			go func() { defer wg.Done(); errs <- NewNorthboundRepository(sess).AdvanceLifecycleBatches(ctx) }()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		active := 0
		resets := 0
		pods := map[int64]bool{}
		for group := 0; group < 2; group++ {
			items, err := r.LifecycleBatchItems(fmt.Sprintf("batch-%d", group))
			if err != nil {
				t.Fatal(err)
			}
			for _, i := range items {
				if i.State != "active" {
					continue
				}
				active++
				if group == 1 {
					resets++
				}
				if pods[i.RuntimePodID] {
					t.Fatal("same pod admitted twice")
				}
				pods[i.RuntimePodID] = true
				if err := r.MarkOperationSucceeded(ctx, i.OperationID, i.SourceID, time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
				completed++
			}
		}
		if active > peak {
			peak = active
		}
		if active > 10 || resets > 5 || active-resets > 5 {
			t.Fatalf("limit exceeded active=%d reset=%d", active, resets)
		}
		if active == 0 {
			t.Fatal("queue stalled")
		}
	}
	if completed != 500 {
		t.Fatalf("completed %d/500", completed)
	}
	if peak != 10 {
		t.Fatalf("expected mixed work to exercise all 10 slots, peak=%d", peak)
	}
	// A direct mutation and a batch cannot acquire the same instance together.
	release, err := r.ReserveInstanceMutations([]int{1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReserveInstanceMutations([]int{1}); err == nil {
		t.Fatal("duplicate direct mutation admitted")
	}
	release()
	if err := r.AdvanceLifecycleBatches(ctx); err != nil {
		t.Fatal(err)
	}
	for group := 0; group < 2; group++ {
		b, err := r.GetLifecycleBatch(fmt.Sprintf("batch-%d", group), 1)
		if err != nil || b.Status != "completed" {
			t.Fatalf("completion: %v %v", b, err)
		}
	}
	count, err := sess.Collection("instances").Find().Count()
	if err != nil || count != 500 {
		t.Fatalf("scheduler touched instance data: %d %v", count, err)
	}
	// Failure pauses remaining work. Cancellation never cancels an active item.
	b := &models.InstanceLifecycleBatch{BatchID: "failure-batch", UserID: 1, Action: "reset", Status: "running", RequestHash: strings.Repeat("c", 64), CreatedAt: now, UpdatedAt: now}
	items := []models.InstanceLifecycleBatchItem{}
	ops := []*models.NorthboundOperation{}
	for _, id := range []int{1, 9, 17} {
		opID := fmt.Sprintf("failure-%d", id)
		items = append(items, models.InstanceLifecycleBatchItem{BatchID: b.BatchID, SourceID: id, SourceName: fmt.Sprintf("accept-%d", id), RuntimeType: "type-1", OperationID: opID, State: "pending"})
		ops = append(ops, &models.NorthboundOperation{OperationID: opID, UserID: 1, SessionID: b.BatchID, OperationType: "lite_instance_reset", IdempotencyKeyHash: strings.Repeat("a", 64), RequestHash: strings.Repeat("b", 64), RequestPayload: fmt.Sprintf(`{"instance_id":%d}`, id), Status: "batch_pending", AvailableAt: now, InstanceID: &id, CreatedAt: now, UpdatedAt: now})
	}
	for _, op := range ops {
		op.IdempotencyKeyHash = fmtLifecycleRetryHash(op.OperationID)
	}
	if err := r.CreateLifecycleBatch(ctx, b, items, ops, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReserveInstanceMutations([]int{1}); err == nil {
		t.Fatal("direct mutation overlapped batch")
	}
	if err := r.AdvanceLifecycleBatches(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkOperationFailed(ctx, "failure-1", "SIMULATED", "replacement failed; source retained", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := r.AdvanceLifecycleBatches(ctx); err != nil {
		t.Fatal(err)
	}
	pausedBatch, _ := r.GetLifecycleBatch(b.BatchID, 1)
	if pausedBatch.Status != "paused" {
		t.Fatal("failure did not pause batch")
	}
	if err := r.ControlLifecycleBatch(ctx, b.BatchID, 2, "resume"); err == nil {
		t.Fatal("another user controlled batch")
	}
	if err := r.ControlLifecycleBatch(ctx, b.BatchID, 1, "cancel"); err != nil {
		t.Fatal(err)
	}
	if err := r.AdvanceLifecycleBatches(ctx); err != nil {
		t.Fatal(err)
	}
	final, _ := r.LifecycleBatchItems(b.BatchID)
	if final[0].State != "failed" || final[1].State != "cancelled" || final[2].State != "cancelled" {
		t.Fatalf("cancel states: %+v", final)
	}
	// New policy: five on a single pod, failure does not block the first pass,
	// one bounded second pass, and cancellation lets in-flight work finish.
	newBatch := func(key string, n int) {
		batch := &models.InstanceLifecycleBatch{BatchID: key, UserID: 1, Action: "restart", Status: "running", PolicyVersion: 2, RequestHash: strings.Repeat("f", 64), CreatedAt: now, UpdatedAt: now}
		var entries []models.InstanceLifecycleBatchItem
		var operations []*models.NorthboundOperation
		for id := 31; id < 31+n; id++ {
			if _, err := sess.SQL().Exec(`UPDATE instance_runtime_bindings SET runtime_pod_id=77 WHERE instance_id=?`, id); err != nil {
				t.Fatal(err)
			}
			opID := fmt.Sprintf("%s-%d", key, id)
			source := id
			entries = append(entries, models.InstanceLifecycleBatchItem{BatchID: key, SourceID: id, SourceName: fmt.Sprintf("accept-%d", id), RuntimeType: "hermes", OperationID: opID, State: "pending"})
			operations = append(operations, &models.NorthboundOperation{OperationID: opID, UserID: 1, SessionID: key, OperationType: "lite_instance_restart", IdempotencyKeyHash: fmtLifecycleRetryHash(opID), RequestHash: strings.Repeat("a", 64), RequestPayload: fmt.Sprintf(`{"instance_id":%d}`, id), Status: "batch_pending", AvailableAt: now, InstanceID: &source, CreatedAt: now, UpdatedAt: now})
		}
		if err := r.CreateLifecycleBatch(ctx, batch, entries, operations, false); err != nil {
			t.Fatal(err)
		}
	}
	newBatch("v2-retry", 12)
	if err := r.AdvanceLifecycleBatches(ctx); err != nil {
		t.Fatal(err)
	}
	v2, _ := r.LifecycleBatchItems("v2-retry")
	activeCount := 0
	for _, item := range v2 {
		if item.State == "active" {
			activeCount++
		}
	}
	if activeCount != 5 {
		t.Fatalf("single pod admitted %d, want 5", activeCount)
	}
	if op, err := r.ClaimNextOperation(ctx, "ordinary-worker", now, now.Add(time.Minute)); err != nil || op != nil {
		t.Fatalf("ordinary worker claimed v2 task: %v %v", op, err)
	}
	claimed := map[string]bool{}
	// One dispatch wave must lease all five, not eventually lease them over
	// repeated scheduler ticks while already-running operations complete.
	for round := 0; round < 1; round++ {
		var workers sync.WaitGroup
		results := make(chan *models.NorthboundOperation, 10)
		errors := make(chan error, 10)
		for n := 0; n < 10; n++ {
			workers.Add(1)
			go func(n int) {
				defer workers.Done()
				claimTime := time.Now().UTC()
				op, err := r.ClaimNextBatchOperation(ctx, fmt.Sprintf("v2-worker-%d-%d", round, n), claimTime, claimTime.Add(time.Minute))
				results <- op
				errors <- err
			}(n)
		}
		workers.Wait()
		close(results)
		close(errors)
		for err := range errors {
			if err != nil {
				t.Fatal(err)
			}
		}
		for op := range results {
			if op != nil {
				if claimed[op.OperationID] {
					t.Fatal("operation leased twice")
				}
				claimed[op.OperationID] = true
			}
		}
	}
	if len(claimed) != 5 {
		t.Fatalf("workers leased %d/5 tasks", len(claimed))
	}
	if err := r.MarkOperationFailed(ctx, "v2-retry-31", "LIFECYCLE_FAILED", "simulated transient", now); err != nil {
		t.Fatal(err)
	}
	if err := r.AdvanceLifecycleBatches(ctx); err != nil {
		t.Fatal(err)
	}
	bb, _ := r.GetLifecycleBatch("v2-retry", 1)
	if bb.Status != "running" || bb.RetryRound != 0 {
		t.Fatalf("failure paused or prematurely retried: %+v", bb)
	}
	if release, err := r.ReserveInstanceMutations([]int{31}); err == nil {
		release()
		t.Fatal("failed first attempt lost reservation")
	}
	for step := 0; step < 10; step++ {
		v2, _ = r.LifecycleBatchItems("v2-retry")
		for _, item := range v2 {
			if item.State == "active" && !strings.HasSuffix(item.OperationID, "_retry") {
				if err := r.MarkOperationSucceeded(ctx, item.OperationID, item.SourceID, now); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := r.AdvanceLifecycleBatches(ctx); err != nil {
			t.Fatal(err)
		}
		bb, _ = r.GetLifecycleBatch("v2-retry", 1)
		if bb.RetryRound == 1 {
			break
		}
	}
	if bb.RetryRound != 1 {
		t.Fatal("second round not scheduled")
	}
	v2, _ = r.LifecycleBatchItems("v2-retry")
	if v2[0].OperationID != "v2-retry-31_retry" || v2[0].State != "active" {
		t.Fatalf("retry: %+v", v2[0])
	}
	history, err := r.LifecycleAttemptHistory(v2[0].ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("attempt history %v %v", history, err)
	}
	if err := r.MarkOperationFailed(ctx, v2[0].OperationID, "LIFECYCLE_FAILED", "second failure", now); err != nil {
		t.Fatal(err)
	}
	if err := r.AdvanceLifecycleBatches(ctx); err != nil {
		t.Fatal(err)
	}
	bb, _ = r.GetLifecycleBatch("v2-retry", 1)
	if bb.Status != "completed_with_errors" {
		t.Fatalf("unbounded retry: %+v", bb)
	}
	newBatch("v2-stop", 7)
	if err := r.AdvanceLifecycleBatches(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.ControlLifecycleBatch(ctx, "v2-stop", 1, "cancel"); err != nil {
		t.Fatal(err)
	}
	v2, _ = r.LifecycleBatchItems("v2-stop")
	activeCount = 0
	cancelled := 0
	for _, item := range v2 {
		switch item.State {
		case "active":
			activeCount++
			if err := r.MarkOperationSucceeded(ctx, item.OperationID, item.SourceID, now); err != nil {
				t.Fatal(err)
			}
		case "cancelled":
			cancelled++
		}
	}
	if activeCount != 5 || cancelled != 2 {
		t.Fatalf("stop interrupted in-flight work: active=%d cancelled=%d", activeCount, cancelled)
	}
	if err := r.AdvanceLifecycleBatches(ctx); err != nil {
		t.Fatal(err)
	}
	bb, _ = r.GetLifecycleBatch("v2-stop", 1)
	if bb.Status != "cancelled" {
		t.Fatalf("stop not drained: %+v", bb)
	}
}
