package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"clawreef/internal/observability"
)

// DurableHistoryArchivePut receives the trusted registered SQL source kind
// from history maintenance. Team Redis archives never inherit SQL field expiry.
func DurableHistoryArchivePut(ctx context.Context, owner, key string, raw []byte) (json.RawMessage, error) {
	parts := strings.Split(key, "/")
	if len(parts) != 4 || parts[0] != "history" {
		return nil, errors.New("invalid history archive identity")
	}
	return durableArchivePut(ctx, owner, parts[1], raw)
}
func indexObservationArchive(ctx context.Context, r *observationRuntime, parent observability.BodyRef, kind string, raw []byte) error {
	deps, err := observability.ArchiveDependencies(raw)
	if err != nil {
		return err
	}
	pool, ok := r.sess.Driver().(*sql.DB)
	if !ok {
		return errors.New("archive dependency SQL unavailable")
	}
	return observability.IndexArchive(ctx, pool, parent, kind, deps)
}

// Each reachable archive keeps its own ciphertext. Its authenticated edges
// protect children. Older roots have no completeness marker until verified.
func observationRetentionEligible() string {
	const obj = "observability_objects"
	conditions := []string{
		"retain_until<UTC_TIMESTAMP(6) AND deleted_at IS NULL AND (recovery_closed_at IS NULL OR recovery_closed_at<retain_until) AND backup_verified_at IS NOT NULL AND restore_verified_at IS NOT NULL",
		"NOT EXISTS (SELECT 1 FROM observability_body_refs b WHERE b.object_key=" + obj + ".object_key AND b.released_at IS NULL AND b.created_at>UTC_TIMESTAMP(6)-INTERVAL 30 DAY)",
		"NOT EXISTS (SELECT 1 FROM observability_admissions a WHERE a.owner=" + obj + ".owner AND a.state<>'terminal')",
		"NOT EXISTS (SELECT 1 FROM observability_archive_dependencies d WHERE d.child_key=" + obj + ".object_key)",
	}
	for _, table := range []string{"history_archive_records", "team_redis_archive_receipts", "team_redis_dlq_receipts"} {
		key := "JSON_UNQUOTE(JSON_EXTRACT(a.object_ref,'$.object_key'))"
		owner := "JSON_UNQUOTE(JSON_EXTRACT(a.object_ref,'$.owner'))=" + obj + ".owner"
		if table == "history_archive_records" {
			owner = "a.owner_id=CAST(SUBSTRING(" + obj + ".owner,6) AS UNSIGNED) AND " + obj + ".owner LIKE 'user:%'"
		}
		conditions = append(conditions, "NOT EXISTS (SELECT 1 FROM "+table+" a WHERE "+owner+" AND ("+key+"="+obj+".object_key OR NOT EXISTS (SELECT 1 FROM observability_archive_dependencies d WHERE d.parent_key="+key+" AND d.child_key='')))")
	}
	return strings.Join(conditions, " AND ")
}
