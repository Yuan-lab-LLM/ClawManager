package systembackup

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestTeamDataDisabledRetainsHistoryAndCleanup(t *testing.T) {
	s := teamDataJobFixture(t)
	ctx := context.Background()
	j, e := s.Create(ctx, 1, "before-disable-backup", "backup", "")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Cancel(ctx, j.ID); e != nil {
		t.Fatal(e)
	}
	s.cfg.Disabled = true
	if _, e = s.Create(ctx, 1, "after-disable-backup", "backup", ""); e == nil || e.Error() != "feature_disabled" {
		t.Fatal("disabled feature accepted new work", e)
	}
	if _, e = s.Get(ctx, j.ID); e != nil {
		t.Fatal("disabled history unavailable", e)
	}
	if _, e = s.List(ctx); e != nil {
		t.Fatal("disabled list unavailable", e)
	}
	id, _ := RandomID("sdr_")
	_, e = s.db.Exec("INSERT INTO system_team_data_backup_jobs(public_id,operation,source_backup_id,actor_id,idempotency_key,request_hash,config_hash,status) VALUES(?,'restore',?,1,'owned-copy',?,?,'failed')", id, j.ID, s.configHash, s.configHash)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Create(ctx, 1, "disabled-owned-cleanup", "cleanup", id); e != nil {
		t.Fatal("disabled cleanup unavailable", e)
	}
}

func TestTeamDataInterruptedVerificationReferenceCanBeCleaned(t *testing.T) {
	s := teamDataJobFixture(t)
	s.store = testObjectStore(t)
	s.cfg.InstallationID, s.cfg.KeyVersion = "interrupted-reference", "v1"
	_, s.signer, _ = ed25519.GenerateKey(rand.Reader)
	a := teamDataPublishedMetadataFixture(t, s.store, s.cfg.InstallationID, s.signer)
	ctx := context.Background()
	if e := s.catalog().Import(ctx, a, 1, false); e != nil {
		t.Fatal(e)
	}
	j, e := s.Create(ctx, 1, "interrupted-verify", "verify", a.Manifest.Payload.BackupID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.db.Exec("UPDATE system_team_data_backup_jobs SET status='running',owner_id='old-process' WHERE public_id=?", j.ID)
	if e != nil {
		t.Fatal(e)
	}
	run, stop := context.WithTimeout(ctx, 350*time.Millisecond)
	_ = s.runOwned(run)
	stop()
	interrupted, e := s.Get(ctx, j.ID)
	if e != nil || interrupted.Status != "failed" || interrupted.Phase != "interrupted" || interrupted.Error != "process_interrupted" {
		t.Fatal("restart reran verification or lost interrupted status", e, interrupted.Status)
	}
	s.cfg.Disabled = true
	if e = s.cleanup(ctx, j.ID); e != nil {
		t.Fatal("interrupted verification reference has no cleanup path", e)
	}
	state, _, e := s.catalog().Load(ctx)
	if e != nil || len(state.Artifacts[j.SourceID].Leases) != 0 {
		t.Fatal("reference retained", e)
	}
	actual, e := s.Get(ctx, j.ID)
	if e != nil || actual.Status != "cleaned" {
		t.Fatal("cleanup not recorded", e)
	}
}

func TestTeamDataDisabledWorkerRejectsPreviouslyQueuedCapture(t *testing.T) {
	s := teamDataJobFixture(t)
	j, e := s.Create(context.Background(), 1, "queued-before-disable", "backup", "")
	if e != nil {
		t.Fatal(e)
	}
	s.cfg.Disabled = true
	run, stop := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer stop()
	_ = s.runOwned(run)
	actual, e := s.Get(context.Background(), j.ID)
	if e != nil || actual.Status != "failed" || actual.Error != "feature_disabled" {
		t.Fatal("queued capture executed after disable", e, actual.Status, actual.Error)
	}
}

func TestTeamDataOfflineCleanupFencesActiveRecovery(t *testing.T) {
	s, a, _ := teamDataManagementFixture(t)
	jobs := teamDataJobFixture(t)
	s.db, s.configHash, s.owner, s.recoveryOnly = jobs.db, jobs.configHash, "recovery_new-process", true
	ctx := context.Background()
	j, e := s.Create(ctx, 1, "offline-interrupted-verify", "verify", a.Manifest.Payload.BackupID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("UPDATE system_team_data_backup_jobs SET status='running',owner_id='recovery_dead-process' WHERE public_id=?", j.ID); e != nil {
		t.Fatal(e)
	}
	conn, e := s.db.Conn(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	hash, _ := RequestHash(map[string]string{"installation_id": s.cfg.InstallationID})
	name := "team-data-" + hash[:40]
	var locked int
	if e = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", name).Scan(&locked); e != nil || locked != 1 {
		t.Fatal(e)
	}
	if e = s.CleanupRecovery(ctx, j.ID); e == nil {
		t.Fatal("cleanup stole an active recovery")
	}
	active, _ := s.Get(ctx, j.ID)
	if active.Status != "running" {
		t.Fatal("active job mutated")
	}
	if _, e = conn.ExecContext(ctx, "DO RELEASE_LOCK(?)", name); e != nil {
		t.Fatal(e)
	}
	if e = s.CleanupRecovery(ctx, j.ID); e != nil {
		t.Fatal("dead offline recovery has no cleanup path", e)
	}
	actual, _ := s.Get(ctx, j.ID)
	if actual.Status != "cleaned" {
		t.Fatal("interrupted offline work reran", actual.Status)
	}
}
