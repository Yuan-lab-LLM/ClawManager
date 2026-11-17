package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawreef/internal/config"
	"clawreef/internal/observability"
	"clawreef/internal/repository"
)

type dlqReplayRaceRepository struct {
	repository.TeamRepository
	repository.TeamRedisMaintenanceRepository
	afterReceipt func() error
}

func (r *dlqReplayRaceRepository) RecordTeamDLQArchive(ctx context.Context, team int, id string, ref json.RawMessage, raw []byte) error {
	if err := r.TeamRedisMaintenanceRepository.RecordTeamDLQArchive(ctx, team, id, ref, raw); err != nil {
		return err
	}
	if r.afterReceipt != nil {
		return r.afterReceipt()
	}
	return nil
}

func TestLiveTeamDLQVerifiedArchiveFailureRaceAndBoundedRecovery(t *testing.T) {
	sess, repo, team, _, _ := projectionMySQL(t)
	bus, _ := capacityRedisFixture(t)
	cleanupMaintenanceTeam(t, bus, team.ID)
	ctx := context.Background()
	key := teamDLQKey(team.ID)
	old := time.Now().Add(-9 * 24 * time.Hour).UnixMilli()
	payload := string([]byte{0, 255, 128}) + strings.Repeat("exact retained dead letter ", 12)
	for sequence := 0; sequence < 101; sequence++ {
		if _, err := bus.doRaw(ctx, "XADD", key, fmt.Sprintf("%d-%d", old, sequence), "payload", payload); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := bus.doRaw(ctx, "XADD", key, "*", "payload", "recent retained error"); err != nil {
		t.Fatal(err)
	}
	assertLength := func(expected int64) {
		t.Helper()
		if length, err := bus.doRaw(ctx, "XLEN", key); err != nil || length != expected {
			t.Fatalf("DLQ length=%v expected=%d err=%v", length, expected, err)
		}
	}
	objectsDir := filepath.Join(t.TempDir(), "objects")
	if err := os.Mkdir(objectsDir, 0700); err != nil {
		t.Fatal(err)
	}
	objects, err := NewObjectStorageService(config.ObjectStorageConfig{LocalFallback: objectsDir})
	if err != nil {
		t.Fatal(err)
	}
	master := bytes.Repeat([]byte{23}, 32)
	encrypted, err := observability.NewEncryptedObjects(objects, master)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := observability.NewJournal(t.TempDir(), master, observability.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	restore := repository.ConfigureObservability(sess, journal, encrypted)
	defer restore()
	service := &teamService{repo: repo}
	if err = os.Rename(objectsDir, objectsDir+"-offline"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(objectsDir, []byte("filesystem object outage"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = service.archiveLiveTeamDLQ(ctx, team, bus); err == nil {
		t.Fatal("filesystem outage did not retain DLQ")
	}
	assertLength(102)
	if err = os.Remove(objectsDir); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(objectsDir+"-offline", objectsDir); err != nil {
		t.Fatal(err)
	}
	if _, err = sess.SQL().Exec(`CREATE TRIGGER test_dlq_receipt_failure BEFORE INSERT ON team_redis_dlq_receipts FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='owned DLQ receipt outage'`); err != nil {
		t.Fatal(err)
	}
	if err = service.archiveLiveTeamDLQ(ctx, team, bus); err == nil {
		t.Fatal("receipt failure did not retain archived source")
	}
	assertLength(102)
	if _, err = sess.SQL().Exec("DROP TRIGGER test_dlq_receipt_failure"); err != nil {
		t.Fatal(err)
	}
	race := &dlqReplayRaceRepository{TeamRepository: repo, TeamRedisMaintenanceRepository: repo.(repository.TeamRedisMaintenanceRepository)}
	race.afterReceipt = func() error { _, err := bus.doRaw(ctx, "XGROUP", "CREATE", key, "new-replay", "0"); return err }
	service.repo = race
	if err = service.archiveLiveTeamDLQ(ctx, team, bus); err == nil || !strings.Contains(err.Error(), "CM_DLQ_GROUP_RACE") {
		t.Fatal("new replay group was not fenced atomically", err)
	}
	assertLength(102)
	race.afterReceipt = nil
	if err = service.archiveLiveTeamDLQ(ctx, team, bus); err == nil {
		t.Fatal("existing replay group did not fence archive deletion")
	}
	assertLength(102)
	if _, err = bus.doRaw(ctx, "XGROUP", "DESTROY", key, "new-replay"); err != nil {
		t.Fatal(err)
	}
	if err = service.archiveLiveTeamDLQ(ctx, team, bus); err != nil {
		t.Fatal(err)
	}
	assertLength(2) // A single pass archives at most100 old records.
	if err = service.archiveLiveTeamDLQ(ctx, team, bus); err != nil {
		t.Fatal(err)
	}
	assertLength(1)
	if err = service.archiveLiveTeamDLQ(ctx, team, bus); err != nil {
		t.Fatal(err)
	}
	assertLength(1) // The recent entry is still inside its replay window.
	var count int
	row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM team_redis_dlq_receipts WHERE team_id=?", team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&count); err != nil || count != 101 {
		t.Fatalf("durable receipts=%d err=%v", count, err)
	}
	var reference string
	row, err = sess.SQL().QueryRow("SELECT object_ref FROM team_redis_dlq_receipts WHERE team_id=? AND stream_id=?", team.ID, fmt.Sprintf("%d-0", old))
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&reference); err != nil {
		t.Fatal(err)
	}
	raw, err := repository.DurableArchiveRead(ctx, "user:"+fmt.Sprint(team.UserID), json.RawMessage(reference))
	if err != nil {
		t.Fatal(err)
	}
	var archive struct {
		Key  []byte            `json:"key"`
		Type string            `json:"type"`
		Data []json.RawMessage `json:"data"`
	}
	if err = json.Unmarshal(raw, &archive); err != nil || string(archive.Key) != key || archive.Type != "stream" {
		t.Fatal("archive identity damaged", err)
	}
	var fields []string
	if len(archive.Data) != 2 || json.Unmarshal(archive.Data[1], &fields) != nil || len(fields) != 2 {
		t.Fatal("archive fields damaged")
	}
	decoded, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil || string(decoded) != payload {
		t.Fatal("binary DLQ content was not retained exactly", err)
	}
	if _, err = repository.DurableArchiveRead(ctx, "user:999999", json.RawMessage(reference)); err == nil {
		t.Fatal("cross-owner DLQ archive read accepted")
	}
}
