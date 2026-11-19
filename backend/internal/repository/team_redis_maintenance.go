package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

type TeamRedisCleanupJob struct {
	TeamID      int       `db:"team_id"`
	OwnerID     int       `db:"owner_id"`
	State       string    `db:"state"`
	ScanCursor  string    `db:"scan_cursor"`
	ProducerIDs string    `db:"producer_ids"`
	CreatedAt   time.Time `db:"created_at"`
}
type TeamRedisCleanupKey struct {
	TeamID   int    `db:"team_id"`
	Key      string `db:"key_name"`
	Kind     string `db:"kind"`
	Cursor   string `db:"next_cursor"`
	Chunk    int64  `db:"chunk_no"`
	Complete bool   `db:"archive_complete"`
}
type TeamRedisMaintenanceRepository interface {
	PrepareTeamRedisCleanup(*models.Team, []int) error
	ListTeamRedisMaintenance(afterID, limit int) ([]models.Team, error)
	TeamRedisCleanupJob(int) (*TeamRedisCleanupJob, error)
	ClaimTeamRedisCleanup(context.Context, int, string) (bool, error)
	CloseDeletedTeamWork(context.Context, int) error
	DiscoverTeamRedisCleanupKeys(context.Context, int, string, map[string]string) error
	PendingTeamRedisCleanupKey(context.Context, int) (*TeamRedisCleanupKey, error)
	SaveTeamRedisArchive(context.Context, *TeamRedisCleanupKey, string, bool, json.RawMessage, []byte) error
	MarkTeamRedisKeyDeleted(context.Context, *TeamRedisCleanupKey) error
	FinishTeamRedisCleanup(context.Context, int) error
	RecordTeamDLQArchive(context.Context, int, string, json.RawMessage, []byte) error
}

type TeamProjectionCommandSourceRepository interface {
	RegisterProjectionCommandSource(context.Context, int, string, string, int) error
}

func (r *teamRepository) RegisterProjectionCommandSource(ctx context.Context, teamID int, messageID, stream string, taskID int) error {
	if !r.inTransaction {
		return fmt.Errorf("projection source registration requires source transaction")
	}
	_, err := r.sess.SQL().ExecContext(ctx, "INSERT IGNORE INTO team_projection_command_sources(team_id,message_id,source_stream_id,task_id) VALUES(?,?,?,?)", teamID, messageID, stream, taskID)
	if err != nil {
		return err
	}
	row, err := r.sess.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM team_projection_command_sources WHERE team_id=? AND message_id=? AND source_stream_id=? AND task_id=?", teamID, messageID, stream, taskID)
	if err != nil {
		return err
	}
	var n int
	if err = row.Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("projection command source identity conflict")
	}
	return nil
}

func (r *teamRepository) RecordTeamDLQArchive(ctx context.Context, teamID int, id string, ref json.RawMessage, raw []byte) error {
	h := sha256.Sum256(raw)
	_, err := r.sess.SQL().ExecContext(ctx, "INSERT INTO team_redis_dlq_receipts(team_id,stream_id,value_sha256,object_ref,archived_at) VALUES(?,?,UNHEX(?),?,UTC_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE object_ref=IF(value_sha256=VALUES(value_sha256),object_ref,NULL)", teamID, id, hex.EncodeToString(h[:]), string(ref))
	return err
}

func (r *teamRepository) PrepareTeamRedisCleanup(team *models.Team, ids []int) error {
	if team == nil || ids == nil {
		return fmt.Errorf("captured producer inventory required")
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	_, err = r.sess.SQL().Exec("INSERT IGNORE INTO team_redis_cleanup_jobs(team_id,owner_id,producer_ids,created_at,updated_at) VALUES(?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", team.ID, team.UserID, string(raw))
	return err
}
func (r *teamRepository) ListTeamRedisMaintenance(after, limit int) ([]models.Team, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	var rows []models.Team
	result, err := r.sess.SQL().Query("SELECT id FROM teams WHERE id>? AND (status<>'deleted' OR id IN (SELECT team_id FROM team_redis_cleanup_jobs WHERE state IN ('pending','archiving'))) ORDER BY id LIMIT ?", after, limit)
	if err != nil {
		return nil, err
	}
	var ids []int
	for result.Next() {
		var id int
		if err = result.Scan(&id); err != nil {
			result.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = result.Err()
	result.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		row, err := r.GetTeamByID(id)
		if err != nil {
			return nil, err
		}
		if row != nil {
			rows = append(rows, *row)
		}
	}
	return rows, nil
}
func (r *teamRepository) TeamRedisCleanupJob(id int) (*TeamRedisCleanupJob, error) {
	var row TeamRedisCleanupJob
	err := r.sess.Collection("team_redis_cleanup_jobs").Find(db.Cond{"team_id": id}).One(&row)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	return &row, err
}
func (r *teamRepository) ClaimTeamRedisCleanup(ctx context.Context, id int, holder string) (bool, error) {
	res, err := r.sess.SQL().ExecContext(ctx, "UPDATE team_redis_cleanup_jobs SET lease_owner=?,lease_until=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 45 SECOND),updated_at=UTC_TIMESTAMP(6) WHERE team_id=? AND state IN ('pending','archiving') AND (lease_until IS NULL OR lease_until<UTC_TIMESTAMP(6))", holder, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
func (r *teamRepository) CloseDeletedTeamWork(ctx context.Context, id int) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return r.withTeamTransaction(id, func(tx *teamRepository) error {
		team, err := tx.GetTeamByID(id)
		if err != nil {
			return err
		}
		if team == nil || team.Status != models.TeamStatusDeleted {
			return fmt.Errorf("Team deletion is incomplete")
		}
		members, err := tx.ListMembersByTeamID(id)
		if err != nil {
			return err
		}
		for _, m := range members {
			if m.Status != models.TeamMemberStatusDeleted || m.InstanceID != nil {
				return fmt.Errorf("Team producer revocation incomplete")
			}
		}
		if err = tx.settleDeletedTeamWork(ctx, id); err != nil {
			return err
		}
		_, err = tx.sess.SQL().ExecContext(ctx, "UPDATE team_redis_cleanup_jobs SET state='archiving',updated_at=UTC_TIMESTAMP(6) WHERE team_id=?", id)
		return err
	})
}
func redisKeyHash(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
func (r *teamRepository) DiscoverTeamRedisCleanupKeys(ctx context.Context, id int, cursor string, keys map[string]string) error {
	return r.withTeamTransaction(id, func(tx *teamRepository) error {
		for key, kind := range keys {
			if _, err := tx.sess.SQL().ExecContext(ctx, "INSERT IGNORE INTO team_redis_cleanup_keys(team_id,key_sha256,key_name,kind) VALUES(?,UNHEX(?),?,?)", id, redisKeyHash(key), key, kind); err != nil {
				return err
			}
		}
		_, err := tx.sess.SQL().ExecContext(ctx, "UPDATE team_redis_cleanup_jobs SET scan_cursor=?,scan_completed=?,updated_at=UTC_TIMESTAMP(6) WHERE team_id=?", cursor, cursor == "0", id)
		return err
	})
}
func (r *teamRepository) PendingTeamRedisCleanupKey(ctx context.Context, id int) (*TeamRedisCleanupKey, error) {
	var row TeamRedisCleanupKey
	err := r.sess.WithContext(ctx).Collection("team_redis_cleanup_keys").Find(db.Cond{"team_id": id, "deleted_at": nil}).OrderBy("key_name").One(&row)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	return &row, err
}
func (r *teamRepository) SaveTeamRedisArchive(ctx context.Context, k *TeamRedisCleanupKey, next string, complete bool, ref json.RawMessage, raw []byte) error {
	h := sha256.Sum256(raw)
	return r.withTeamTransaction(k.TeamID, func(tx *teamRepository) error {
		_, err := tx.sess.SQL().ExecContext(ctx, "INSERT INTO team_redis_archive_receipts(team_id,key_sha256,key_name,chunk_no,value_sha256,object_ref,archived_at) VALUES(?,UNHEX(?),?,?,UNHEX(?),?,UTC_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE object_ref=IF(value_sha256=VALUES(value_sha256),object_ref,NULL)", k.TeamID, redisKeyHash(k.Key), k.Key, k.Chunk, hex.EncodeToString(h[:]), string(ref))
		if err != nil {
			return err
		}
		_, err = tx.sess.SQL().ExecContext(ctx, "UPDATE team_redis_cleanup_keys SET next_cursor=?,chunk_no=chunk_no+1,archive_complete=? WHERE team_id=? AND key_sha256=UNHEX(?) AND chunk_no=?", next, complete, k.TeamID, redisKeyHash(k.Key), k.Chunk)
		return err
	})
}
func (r *teamRepository) MarkTeamRedisKeyDeleted(ctx context.Context, k *TeamRedisCleanupKey) error {
	return r.withTeamTransaction(k.TeamID, func(tx *teamRepository) error {
		_, err := tx.sess.SQL().ExecContext(ctx, "UPDATE team_redis_cleanup_keys SET deleted_at=UTC_TIMESTAMP(6) WHERE team_id=? AND key_sha256=UNHEX(?) AND archive_complete=TRUE", k.TeamID, redisKeyHash(k.Key))
		if err != nil {
			return err
		}
		_, err = tx.sess.SQL().ExecContext(ctx, "UPDATE team_redis_archive_receipts SET deleted_at=UTC_TIMESTAMP(6) WHERE team_id=? AND key_sha256=UNHEX(?)", k.TeamID, redisKeyHash(k.Key))
		return err
	})
}
func (r *teamRepository) FinishTeamRedisCleanup(ctx context.Context, id int) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return r.withTeamTransaction(id, func(tx *teamRepository) error { return tx.finishDeletedTeamReplay(ctx, id) })
}
