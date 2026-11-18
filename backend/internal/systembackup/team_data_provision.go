package systembackup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/minio/minio-go/v7"
)

// Receipts live in the recovery control DB, not in the database whose schema
// may fail halfway through creation. Intent alone never authorizes deletion.
type teamDataProvisionReceipt struct {
	Target string `json:"target"`
	State  string `json:"state"`
	Server string `json:"server,omitempty"`
}

func (s *TeamDataService) recordProvision(ctx context.Context, j TeamDataJob, kind string, receipt teamDataProvisionReceipt) error {
	if kind != "database" && kind != "bucket" {
		return errors.New("validation_failed")
	}
	raw, e := json.Marshal(receipt)
	if e != nil {
		return e
	}
	r, e := s.db.ExecContext(ctx, "UPDATE system_team_data_backup_jobs SET provisioning_json=JSON_SET(COALESCE(provisioning_json,JSON_OBJECT()),?,CAST(? AS JSON)),updated_at=UTC_TIMESTAMP(6) WHERE public_id=? AND operation='restore' AND config_hash=? AND status='running' AND owner_id=? AND cancel_requested=0", "$."+kind, string(raw), j.ID, s.configHash, s.owner)
	return requireRow(r, e)
}

func (s *TeamDataService) provisionOwned(ctx context.Context, j TeamDataJob, kind, target, server string) (bool, error) {
	var raw []byte
	e := s.db.QueryRowContext(ctx, "SELECT provisioning_json FROM system_team_data_backup_jobs WHERE public_id=? AND operation='restore' AND config_hash=?", j.ID, s.configHash).Scan(&raw)
	if e != nil {
		return false, e
	}
	var receipts map[string]teamDataProvisionReceipt
	if len(raw) > 0 {
		if e = json.Unmarshal(raw, &receipts); e != nil {
			return false, e
		}
	}
	r := receipts[kind]
	return r.State == "created" && r.Target == target && r.Server == server, nil
}

func (s *TeamDataService) prepareRestoreDatabase(ctx context.Context, j TeamDataJob, prepare func(context.Context, *sql.DB) error) (*sql.DB, error) {
	database := teamDataDatabase(j.ID)
	if !restoreDatabasePattern.MatchString(database) {
		return nil, errors.New("validation_failed")
	}
	var server string
	if e := s.admin.QueryRowContext(ctx, "SELECT @@server_uuid").Scan(&server); e != nil {
		return nil, e
	}
	receipt := teamDataProvisionReceipt{Target: database, State: "intent", Server: server}
	if e := s.recordProvision(ctx, j, "database", receipt); e != nil {
		return nil, e
	}
	// No IF NOT EXISTS: a pre-existing database must never receive our receipt.
	if _, e := s.admin.ExecContext(ctx, "CREATE DATABASE `"+database+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); e != nil {
		return nil, e
	}
	receipt.State = "created"
	if e := s.recordProvision(ctx, j, "database", receipt); e != nil {
		return nil, e
	}
	admin, e := s.restoreDatabase(ctx, j.ID)
	if e != nil {
		return nil, e
	}
	if e = prepare(ctx, admin); e != nil {
		admin.Close()
		return nil, e
	}
	return admin, nil
}

func (s *TeamDataService) createRestoreBucket(ctx context.Context, j TeamDataJob) error {
	bucket := teamDataBucket(j.ID)
	receipt := teamDataProvisionReceipt{Target: bucket, State: "intent"}
	if e := s.recordProvision(ctx, j, "bucket", receipt); e != nil {
		return e
	}
	// S3 may return success for an existing bucket owned by the same account.
	// Refuse it first; new run IDs and the single recovery worker fence creation.
	if exists, e := s.objects.BucketExists(ctx, bucket); e != nil {
		return e
	} else if exists {
		return errors.New("restore_ownership_unproven")
	}
	if e := s.objects.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); e != nil {
		return e
	}
	receipt.State = "created"
	return s.recordProvision(ctx, j, "bucket", receipt)
}
