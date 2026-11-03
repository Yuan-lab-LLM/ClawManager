package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/upper/db/v4"
)

var ownerAnalyticsAvailable atomic.Bool

func OwnerAnalyticsAvailable() bool { return ownerAnalyticsAvailable.Load() }

// Validate the actual columns used by the collector; an existing table name
// alone does not prove compatibility. This never repairs/drops existing data.
func verifyOwnerAnalytics(conn *sql.Conn) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rows, err := conn.QueryContext(ctx, `SELECT id,record_type,record_key,scope_id,owner,owner_normalized,instance_id,runtime_type,instance_mode,source_kind,source_key,session_key,question_key,status,occurred_at,created_at,updated_at,version,schema_version,rule_version,payload,prompt_tokens,completion_tokens,total_tokens,invocation_count,question_count,session_count,active_day_count,xp_value,level_value,streak_days,longest_streak_days,rank_position,rank_score,last_activity_at,business_date,timezone,period_type,period_start,period_end,category_key,revision,expires_at,active_work_seconds,login_day_count,login_streak_days,last_login_date,task_completed_count,interval_start,interval_end,task_code,definition_version,progress_value,target_value,completed_at,rewarded_at FROM northbound_owner_records LIMIT 0`)
	if err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	var uniqueColumns int
	err = conn.QueryRowContext(ctx, `SELECT IF(COUNT(*)=2,SUM((SEQ_IN_INDEX=1 AND COLUMN_NAME='record_type') OR (SEQ_IN_INDEX=2 AND COLUMN_NAME='record_key')),0) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='northbound_owner_records' AND INDEX_NAME='uk_owner_record' AND NON_UNIQUE=0`).Scan(&uniqueColumns)
	if err != nil {
		return err
	}
	if uniqueColumns != 2 {
		return fmt.Errorf("owner record uniqueness index unavailable")
	}
	return nil
}

// Only failed optional analytics preparation is retried. Never invoke this in
// response to a portal request; normal operation performs no periodic DDL.
func RetryOwnerAnalytics(ctx context.Context, session db.Session) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if OwnerAnalyticsAvailable() {
				continue
			}
			if err := applyEmbeddedMigrations(session); err != nil {
				log.Printf("Optional owner analytics preparation deferred: %T", err)
			}
		}
	}
}
