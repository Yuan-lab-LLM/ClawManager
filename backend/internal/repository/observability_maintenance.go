package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"clawreef/internal/backupgate"
	"clawreef/internal/historymaintenance"
	"clawreef/internal/models"
	"clawreef/internal/observability"
	"github.com/upper/db/v4"
)

func observationBodyModel(table string) interface{} {
	switch table {
	case "model_invocations":
		return &models.ModelInvocation{}
	case "audit_events":
		return &models.AuditEvent{}
	case "chat_messages":
		return &models.ChatMessageRecord{}
	}
	return nil
}

// MigrateObservationBodies moves at most 500 historical rows, preserving source
// text unless upload authentication and the conditional SQL update both succeed.
func MigrateObservationBodies(ctx context.Context, table string, after int64, limit int) (int64, int, error) {
	prototype := observationBodyModel(table)
	if prototype == nil {
		return after, 0, errors.New("unregistered body migration table")
	}
	r := installedObservability.Load()
	if r == nil {
		return after, 0, errors.New("durable observability not configured")
	}
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	release, e := backupgate.HoldWorkUnit(ctx, "observability.body_migration")
	if e != nil {
		return after, 0, e
	}
	defer release()
	var ids []struct {
		ID int64 `db:"id"`
	}
	if e = r.sess.WithContext(ctx).Collection(table).Find(db.Cond{"id >": after}).Select("id").OrderBy("id").Limit(limit).All(&ids); e != nil {
		return after, 0, e
	}
	count := 0
	for _, row := range ids {
		value := observationBodyModel(table)
		if e = r.sess.WithContext(ctx).Collection(table).Find(db.Cond{"id": row.ID}).One(value); e != nil {
			return after, count, e
		}
		v := reflect.ValueOf(value).Elem()
		original := reflect.New(v.Type())
		original.Elem().Set(v)
		if v.FieldByName("EventUID").IsNil() {
			hash := sha256.Sum256([]byte(fmt.Sprintf("legacy/%s/%d", table, row.ID)))
			uid := hex.EncodeToString(hash[:])
			v.FieldByName("EventUID").Set(reflect.ValueOf(&uid))
		}
		refs, e := r.externalize(ctx, value)
		if e != nil {
			return after, count, e
		}
		if len(refs) > 0 {
			_, _, uid := observationFields(value)
			e = r.sess.WithContext(ctx).Tx(func(tx db.Session) error {
				created := v.FieldByName("CreatedAt").Interface().(time.Time)
				if e := historymaintenance.LockLogMonth(ctx, monthlySQL{tx.SQL()}, table, created); e != nil {
					return e
				}
				cond := db.Cond{"id": row.ID}
				updates := map[string]interface{}{"event_uid": uid}
				for field, ref := range refs {
					bodyField := v.Type().Field(fieldIndex(v.Type(), field))
					bodyColumn := strings.Split(bodyField.Tag.Get("db"), ",")[0]
					refName := bodyFields(value)[field]
					refStructField := v.Type().Field(fieldIndex(v.Type(), refName))
					refColumn := strings.Split(refStructField.Tag.Get("db"), ",")[0]
					cond[bodyColumn] = original.Elem().FieldByName(field).Interface()
					updates[bodyColumn] = v.FieldByName(field).Interface()
					updates[refColumn] = v.FieldByName(refName).Interface()
					encoded, _ := json.Marshal(ref)
					if _, e := tx.SQL().ExecContext(ctx, `INSERT INTO observability_body_refs(source_table,event_uid,field_name,object_key,body_ref) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE object_key=VALUES(object_key),body_ref=VALUES(body_ref),released_at=NULL`, table, uid, field, ref.ObjectKey, string(encoded)); e != nil {
						return e
					}
				}
				sets := []string{}
				args := []interface{}{}
				for column, value := range updates {
					sets = append(sets, "`"+column+"`=?")
					args = append(args, value)
				}
				wheres := []string{}
				for column, value := range cond {
					wheres = append(wheres, "`"+fmt.Sprint(column)+"` <=> ?")
					args = append(args, value)
				}
				result, e := tx.SQL().ExecContext(ctx, "UPDATE `"+table+"` SET "+strings.Join(sets, ",")+" WHERE "+strings.Join(wheres, " AND "), args...)
				if e != nil {
					return e
				}
				changed, e := result.RowsAffected()
				if e != nil {
					return e
				}
				if changed != 1 {
					return errors.New("historical body changed during migration")
				}
				return historymaintenance.WriteLogBatch(ctx, monthlySQL{tx.SQL()}, table, []any{row.ID})
			})
			if e != nil {
				return after, count, e
			}
			count++
		}
		after = row.ID
	}
	return after, count, nil
}
func fieldIndex(t reflect.Type, name string) int { f, _ := t.FieldByName(name); return f.Index[0] }

// RunObservationBodyMigration resumes persistent cursors after restart. It does
// not reset a completed cursor, so large historical tables are visited once.
func RunObservationBodyMigration(ctx context.Context, onError func(error)) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, table := range []string{"model_invocations", "audit_events", "chat_messages"} {
				r := installedObservability.Load()
				if r == nil {
					return
				}
				work, cancel := context.WithTimeout(ctx, 30*time.Second)
				var prior struct {
					ID int64 `db:"last_id"`
				}
				e := r.sess.WithContext(work).Collection("observability_maintenance_progress").Find(db.Cond{"source_table": table}).One(&prior)
				if e == db.ErrNoMoreRows {
					e = nil
				}
				if e == nil {
					var next int64
					next, _, e = MigrateObservationBodies(work, table, prior.ID, 500)
					if e == nil {
						_, e = r.sess.SQL().ExecContext(work, `INSERT INTO observability_maintenance_progress(source_table,last_id) VALUES(?,?) ON DUPLICATE KEY UPDATE last_id=GREATEST(last_id,VALUES(last_id)),updated_at=UTC_TIMESTAMP(6)`, table, next)
					}
				}
				cancel()
				if e != nil && onError != nil {
					onError(e)
				}
			}
		}
	}
}

// A logical release commits before deleting storage. The independent purge
// retries a lost delete acknowledgement; a newer upload extends retain_until
// under the same row lock and therefore cancels the pending physical delete.
const observationPurgePending = "deleted_at IS NULL AND recovery_closed_at>=retain_until"

func ReclaimObservationObjects(ctx context.Context, limit int) (int, error) {
	r := installedObservability.Load()
	if r == nil {
		return 0, errors.New("durable observability not configured")
	}
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	release, err := backupgate.HoldWorkUnit(ctx, "observability.body_retention")
	if err != nil {
		return 0, err
	}
	defer release()
	var candidates []struct {
		Key string `db:"object_key"`
	}
	eligible := observationRetentionEligible()
	err = r.sess.WithContext(ctx).Collection("observability_objects").Find(db.Raw("(" + observationPurgePending + ") OR (" + eligible + ")")).Select("object_key").OrderBy("retain_until").Limit(limit).All(&candidates)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, candidate := range candidates {
		err = r.sess.WithContext(ctx).Tx(func(tx db.Session) error {
			row, err := tx.SQL().QueryRowContext(ctx, "SELECT object_key FROM observability_objects WHERE object_key=? FOR UPDATE", candidate.Key)
			if err != nil {
				return err
			}
			var key string
			if err = row.Scan(&key); err != nil {
				return err
			}
			row, err = tx.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM observability_objects WHERE object_key=? AND "+eligible, key)
			if err != nil {
				return err
			}
			var n int
			if err = row.Scan(&n); err != nil || n == 0 {
				return err
			}
			if _, err = tx.SQL().ExecContext(ctx, "DELETE FROM observability_archive_dependencies WHERE parent_key=?", key); err != nil {
				return err
			}
			if _, err = tx.SQL().ExecContext(ctx, "UPDATE observability_body_refs SET released_at=UTC_TIMESTAMP(6) WHERE object_key=? AND released_at IS NULL", key); err != nil {
				return err
			}
			_, err = tx.SQL().ExecContext(ctx, "UPDATE observability_objects SET recovery_closed_at=UTC_TIMESTAMP(6) WHERE object_key=?", key)
			return err
		})
		if err != nil {
			return count, err
		}
		purged := false
		err = r.sess.WithContext(ctx).Tx(func(tx db.Session) error {
			row, err := tx.SQL().QueryRowContext(ctx, "SELECT owner,sha256,byte_length,encryption_version FROM observability_objects WHERE object_key=? AND "+observationPurgePending+" FOR UPDATE", candidate.Key)
			if err != nil {
				return err
			}
			ref := observability.BodyRef{ObjectKey: candidate.Key}
			err = row.Scan(&ref.Owner, &ref.SHA256, &ref.ByteLength, &ref.EncryptionVersion)
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			if err = r.objects.Delete(ctx, ref.Owner, ref); err != nil {
				return err
			}
			_, err = tx.SQL().ExecContext(ctx, "UPDATE observability_objects SET deleted_at=UTC_TIMESTAMP(6) WHERE object_key=?", candidate.Key)
			purged = err == nil
			return err
		})
		if err != nil {
			return count, err
		}
		if purged {
			count++
		}
	}
	return count, nil
}
