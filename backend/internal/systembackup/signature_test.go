package systembackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestLegacyIdempotencyHashesStillReplayAfterCanonicalization(t *testing.T) {
	ctx := context.Background()
	s := Store{controlDB(t), "legacy-hash"}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	c := testConfig()
	if _, _, err := s.SaveConfigIdempotent(ctx, 0, 1, "legacy-config-0001", c, permitConfig); err != nil {
		t.Fatal(err)
	}
	request := struct {
		Version uint64       `json:"expected_version"`
		Config  BackupConfig `json:"config"`
	}{0, c}
	raw, _ := json.Marshal(request)
	h := sha256.Sum256(raw)
	legacy := hex.EncodeToString(h[:])
	if canonical, _ := RequestHash(request); legacy == canonical {
		t.Fatal("fixture does not distinguish hash formats")
	}
	if _, err := s.DB.Exec("UPDATE system_backup_operations SET request_hash=?", legacy); err != nil {
		t.Fatal(err)
	}
	if _, replay, err := s.SaveConfigIdempotent(ctx, 0, 1, "legacy-config-0001", c, permitConfig); err != nil || !replay {
		t.Fatal("legacy response loss retry rejected", replay, err)
	}
	r := CreateTaskRequest{Type: Backup, Purpose: "functional_test"}
	x, _, err := s.Create(ctx, 1, "legacy-task-0001", r, permitTask)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(r)
	h = sha256.Sum256(raw)
	legacy = hex.EncodeToString(h[:])
	if _, err = s.DB.Exec("UPDATE system_backup_tasks SET request_hash=? WHERE public_id=?", legacy, x.ID); err != nil {
		t.Fatal(err)
	}
	if _, replay, err := s.Create(ctx, 1, "legacy-task-0001", r, permitTask); err != nil || !replay {
		t.Fatal("legacy task retry rejected", replay, err)
	}
}
