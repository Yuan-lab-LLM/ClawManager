package observability

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// IndexArchive records children and a completeness marker atomically. Child row
// locks serialize a new archive with retention deleting its last live copy.
// The marker is inserted last and means even an empty archive was inspected.
func IndexArchive(ctx context.Context, db *sql.DB, parent BodyRef, kind string, deps []BodyDependency) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	lock := func(ref BodyRef) (bool, error) {
		var owner, digest string
		var length int64
		var version int
		var deleted bool
		err := tx.QueryRowContext(ctx, `SELECT owner,sha256,byte_length,encryption_version,(deleted_at IS NOT NULL OR COALESCE(recovery_closed_at>=retain_until,FALSE)) FROM observability_objects WHERE object_key=? AND BINARY object_key=? FOR UPDATE`, ref.ObjectKey, ref.ObjectKey).Scan(&owner, &digest, &length, &version, &deleted)
		if err == nil && (owner != ref.Owner || digest != ref.SHA256 || length != ref.ByteLength || version != ref.EncryptionVersion) {
			err = errors.New("archive dependency source identity changed")
		}
		return deleted, err
	}
	deleted, err := lock(parent)
	if err != nil {
		return err
	}
	if deleted {
		return errors.New("archive parent expired")
	}
	for _, dep := range deps {
		if dep.Ref.Owner != parent.Owner {
			return errors.New("archive child owner differs")
		}
		deleted, err := lock(dep.Ref)
		if err != nil {
			return err
		}
		if deleted {
			if kind == "" || kind != dep.Field.Table || dep.Field.UID == "" {
				return errors.New("required archive child expired")
			}
			var found int
			encoded, err := json.Marshal(dep.Ref)
			if err != nil {
				return err
			}
			err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM observability_body_refs b JOIN observability_objects o ON o.object_key=b.object_key WHERE b.source_table=? AND b.event_uid=? AND b.field_name=? AND BINARY b.object_key=? AND b.body_ref=CAST(? AS JSON) AND b.released_at IS NOT NULL AND o.recovery_closed_at IS NOT NULL AND o.backup_verified_at IS NOT NULL AND o.restore_verified_at IS NOT NULL`, kind, dep.Field.UID, dep.Field.Field, dep.Ref.ObjectKey, string(encoded)).Scan(&found)
			if err != nil {
				return err
			}
			if found != 1 {
				return errors.New("archive field expiry evidence missing")
			}
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT IGNORE INTO observability_archive_dependencies(parent_key,child_key) VALUES(?,?)`, parent.ObjectKey, dep.Ref.ObjectKey); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT IGNORE INTO observability_archive_dependencies(parent_key,child_key) VALUES(?,'')`, parent.ObjectKey); err != nil {
		return err
	}
	return tx.Commit()
}
