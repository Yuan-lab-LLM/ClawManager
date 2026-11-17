package systembackup

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"slices"
	"strings"

	"clawreef/internal/backupgate"
	"clawreef/internal/observability"
)

// RecordObservationRestoreProof runs only on the source side after in-process
// native verification. Recovery connections and historical keys never enter
// the writer. A replay can resume after any committed batch.
func (r RestoredDataVerification) RecordObservationRestoreProof(ctx context.Context, source, control *sql.DB, scope string) (int64, error) {
	if r.verifiedObjectOrigin == "" || !r.IntegrityVerified || scope == "" || source == control {
		return 0, errors.New("verified full restore and independent writer gate required")
	}
	if err := backupgate.RequireSameDatabase(ctx, source, control); err != nil {
		return 0, err
	}
	var database, origin string
	if err := source.QueryRowContext(ctx, "SELECT DATABASE(),origin_installation_id FROM system_backup_installation_state WHERE singleton_id=1").Scan(&database, &origin); err != nil {
		return 0, err
	}
	if restoreDatabasePattern.MatchString(database) || origin != r.verifiedObjectOrigin {
		return 0, errors.New("source installation differs from verified origin")
	}
	var recorded int64
	// Finish legacy archive inventories before releasing proof for any children.
	// A source root without a complete inventory keeps the owner's GC closed.
	for _, parent := range slices.Sorted(maps.Keys(r.verifiedArchiveEdges)) {
		deps := make([]observability.BodyDependency, 0, len(r.verifiedArchiveEdges[parent]))
		for _, key := range r.verifiedArchiveEdges[parent] {
			deps = append(deps, observability.BodyDependency{Ref: r.verifiedObjects[key]})
		}
		release, err := backupgate.HoldRequest(ctx, control, scope)
		if err != nil {
			return 0, err
		}
		err = observability.IndexArchive(ctx, source, r.verifiedObjects[parent], "", deps)
		release()
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
	}
	for batch := range slices.Chunk(slices.Sorted(maps.Keys(r.verifiedObjects)), 250) {
		release, err := backupgate.HoldRequest(ctx, control, scope)
		if err != nil {
			return recorded, err
		}
		// Keep the indexed object key lookup while comparing every identity
		// component byte-for-byte even on a case-insensitive source database.
		var args []any
		var clauses []string
		for _, key := range batch {
			ref := r.verifiedObjects[key]
			clauses = append(clauses, "(object_key=? AND BINARY object_key=? AND BINARY owner=? AND BINARY sha256=? AND byte_length=? AND encryption_version=?)")
			args = append(args, key, key, ref.Owner, ref.SHA256, ref.ByteLength, ref.EncryptionVersion)
		}
		result, err := source.ExecContext(ctx, "UPDATE observability_objects SET backup_verified_at=UTC_TIMESTAMP(6),restore_verified_at=UTC_TIMESTAMP(6) WHERE deleted_at IS NULL AND (backup_verified_at IS NULL OR restore_verified_at IS NULL) AND ("+strings.Join(clauses, " OR ")+")", args...)
		release()
		if err != nil {
			return recorded, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return recorded, err
		}
		recorded += n
	}
	return recorded, nil
}
