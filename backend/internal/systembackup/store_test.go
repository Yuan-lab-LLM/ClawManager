package systembackup

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"
)

func testConfig() BackupConfig {
	c := DefaultConfig()
	c.Enabled = true
	c.ArtifactTarget = &SecretRef{"kubernetes", "archive", "v1", "artifact"}
	c.EncryptionKey = &SecretRef{"vault", "backup", "v1", "encryption"}
	c.SigningKey = &SecretRef{"vault", "sign", "v1", "signing"}
	c.Catalog = &SecretRef{"kubernetes", "catalog", "v1", "catalog"}
	return c
}
func permitConfig(BackupConfig, BackupConfig) error                               { return nil }
func permitTask(context.Context, *sql.Tx, ConfigVersion, CreateTaskRequest) error { return nil }

func TestStoreConfigCASAndIdentity(t *testing.T) {
	ctx := context.Background()
	s := Store{controlDB(t), "installation-test"}
	if e := s.Bootstrap(ctx); e != nil {
		t.Fatal(e)
	}
	other := s
	other.InstallationID = "other"
	if e := other.Bootstrap(ctx); e == nil {
		t.Fatal("identity changed")
	}
	c, e := s.Config(ctx)
	if e != nil || c.Config.Enabled || c.Version != 0 {
		t.Fatal(c, e)
	}
	if _, e = s.SaveConfig(ctx, 0, 1, testConfig(), nil); e == nil {
		t.Fatal("missing authorization")
	}
	c, e = s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig)
	if e != nil || c.Version != 1 {
		t.Fatal(c, e)
	}
	if _, e = s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig); PublicError(e) != "version_conflict" {
		t.Fatal(e)
	}
	var versions int
	if e = s.DB.QueryRow("SELECT COUNT(*) FROM system_backup_config_versions").Scan(&versions); e != nil || versions != 1 {
		t.Fatal(versions, e)
	}
}

func TestStoreConcurrentIdempotencyClaimAndCancel(t *testing.T) {
	ctx := context.Background()
	s := Store{controlDB(t), "installation-test"}
	if e := s.Bootstrap(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig); e != nil {
		t.Fatal(e)
	}
	type result struct {
		task   Task
		replay bool
		err    error
	}
	ch := make(chan result, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, r, e := s.Create(ctx, 1, "request-0001", CreateTaskRequest{Type: Backup, Purpose: "acceptance"}, permitTask)
			ch <- result{v, r, e}
		}()
	}
	wg.Wait()
	close(ch)
	var task Task
	created := 0
	for r := range ch {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if task.ID != "" && task.ID != r.task.ID {
			t.Fatal("duplicate task")
		}
		task = r.task
		if !r.replay {
			created++
		}
	}
	if created != 1 {
		t.Fatal(created)
	}
	if _, _, e := s.Create(ctx, 1, "request-0001", CreateTaskRequest{Type: Backup, Purpose: "functional_test"}, permitTask); PublicError(e) != "idempotency_conflict" {
		t.Fatal(e)
	}
	if queued, _, e := s.Create(ctx, 1, "request-0002", CreateTaskRequest{Type: Backup, Purpose: "acceptance"}, permitTask); e != nil || queued.Status != "pending" || queued.PendingReason != "global_capture_queue" {
		t.Fatal("concurrent backups must queue", queued, e)
	}
	claimed, e := s.Claim(ctx, task.ID, "controller-a", task.Version, 30*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Claim(ctx, task.ID, "controller-b", claimed.Version, 30*time.Second); !errors.Is(e, ErrFence) {
		t.Fatal(e)
	}
	advanced, e := s.Advance(ctx, claimed, "controller-a", "preparing", "none")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Advance(ctx, claimed, "controller-a", "preparing", "none"); !errors.Is(e, ErrFence) {
		t.Fatal("stale CAS accepted", e)
	}
	canceled, e := s.Cancel(ctx, task.ID, advanced.Version, 1)
	if e != nil || canceled.Status != "canceling" {
		t.Fatal(canceled, e)
	}
	if _, e = s.Advance(ctx, advanced, "controller-a", "ready_for_capture", "none"); !errors.Is(e, ErrFence) {
		t.Fatal("lost cancellation", e)
	}
	canceled, e = s.Advance(ctx, canceled, "controller-a", "canceled", "canceled_by_actor")
	if e != nil || canceled.FinishedAt == nil {
		t.Fatal(canceled, e)
	}
	events, e := s.Events(ctx, task.ID, 0)
	if e != nil || len(events) != 4 {
		t.Fatal(events, e)
	}
}

func TestConfigRejectsSecretMaterialAndUnboundedBudgets(t *testing.T) {
	c := testConfig()
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	c.EncryptionKey.Version = "latest"
	if e := c.Validate(); e == nil {
		t.Fatal("mutable key alias")
	}
	c = testConfig()
	c.ArtifactTarget.Name = "https://user:secret@host"
	if e := c.Validate(); e == nil {
		t.Fatal("inline credential")
	}
	c = testConfig()
	c.StagingCapacityBytes = 1 << 30
	if e := c.Validate(); e == nil {
		t.Fatal("insufficient staging")
	}
}
