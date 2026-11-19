package repository

import (
	"clawreef/internal/historymaintenance"
	"clawreef/internal/models"
	"context"
	"database/sql"
	"fmt"
	"github.com/upper/db/v4"
	"strings"
	"time"
)

func maintainCostSummary(ctx context.Context, tx db.Session, v *models.CostRecord) error {
	if e := lockCostSummaryDays(ctx, tx, []interface{}{v.ID}); e != nil {
		return e
	}
	currency := strings.ToUpper(strings.TrimSpace(v.Currency))
	if currency == "" {
		currency = "USD"
	}
	inserted, e := tx.SQL().ExecContext(ctx, `INSERT IGNORE INTO observation_cost_facts(id,invocation_id,instance_id,session_id,trace_id,currency,estimated_cost)VALUES(?,?,?,?,?,?,?)`, v.ID, v.InvocationID, v.InstanceID, v.SessionID, v.TraceID, currency, v.EstimatedCost)
	if e != nil {
		return e
	}
	n, e := inserted.RowsAffected()
	if e != nil {
		return e
	}
	if n > 0 {
		if e = materializeCostOverview(ctx, tx, "cr.id=?", v.ID); e != nil {
			return e
		}
	}
	if v.InvocationID == nil {
		if n == 0 {
			return nil
		}
		if v.InstanceID != nil && v.SessionID != nil && *v.SessionID != "" {
			if _, e = tx.SQL().ExecContext(ctx, `INSERT INTO observation_legacy_trace_costs(instance_id,session_id,trace_id,currency,estimated_cost)VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE estimated_cost=estimated_cost+VALUES(estimated_cost)`, v.InstanceID, v.SessionID, v.TraceID, currency, v.EstimatedCost); e != nil {
				return e
			}
		}
		_, e = tx.SQL().ExecContext(ctx, `UPDATE observation_cost_facts SET resolved=TRUE WHERE id=?`, v.ID)
		return e
	}
	// Locking the receipt makes late association/replay idempotent.
	row, e := tx.SQL().QueryRowContext(ctx, `SELECT resolved FROM observation_cost_facts WHERE id=? FOR UPDATE`, v.ID)
	if e != nil {
		return e
	}
	var resolved bool
	if e = row.Scan(&resolved); e != nil {
		return e
	}
	if resolved {
		return nil
	}
	row, e = tx.SQL().QueryRowContext(ctx, `SELECT status,instance_id,session_id,created_at FROM observation_invocation_summaries WHERE id=?`, v.InvocationID)
	if e != nil {
		return e
	}
	var status string
	var instance sql.NullInt64
	var session sql.NullString
	var created time.Time
	e = row.Scan(&status, &instance, &session, &created)
	if e == sql.ErrNoRows {
		_, e = tx.SQL().ExecContext(ctx, `UPDATE observation_summary_state SET ready=FALSE WHERE id=2`)
		return e
	}
	if e != nil {
		return e
	}
	eligible := status != models.ModelInvocationStatusBlocked && instance.Valid && session.Valid && v.InstanceID != nil && v.SessionID != nil && int(instance.Int64) == *v.InstanceID && session.String == *v.SessionID && session.String != ""
	if eligible {
		_, e = tx.SQL().ExecContext(ctx, `INSERT INTO observation_session_cost_daily(instance_id,session_id,day_utc,currency,estimated_cost)VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE estimated_cost=estimated_cost+VALUES(estimated_cost)`, instance.Int64, session.String, created.UTC().Format("2006-01-02"), currency, v.EstimatedCost)
		if e != nil {
			return e
		}
		_, e = tx.SQL().ExecContext(ctx, `UPDATE observation_cost_facts SET resolved=TRUE,eligible_at=? WHERE id=?`, created, v.ID)
	} else {
		_, e = tx.SQL().ExecContext(ctx, `UPDATE observation_cost_facts SET resolved=TRUE WHERE id=?`, v.ID)
	}
	return e
}
func reconcileInvocationCosts(ctx context.Context, tx db.Session, id int) error {
	var rows []models.CostRecord
	e := tx.Collection("observation_cost_facts").Find(db.Cond{"invocation_id": id, "resolved": false}).Select("id", "invocation_id", "instance_id", "session_id", "trace_id", "currency", "estimated_cost").OrderBy("id").Limit(500).All(&rows)
	if e != nil {
		return e
	}
	if len(rows) == 0 {
		return nil
	}
	for i := range rows {
		if e = maintainCostSummary(ctx, tx, &rows[i]); e != nil {
			return e
		}
	}
	_, e = tx.SQL().ExecContext(ctx, `UPDATE observation_summary_state SET ready=TRUE WHERE id=2 AND target_id<=COALESCE((SELECT after_id FROM history_maintenance_cursors WHERE kind='query_cost_summary'),0) AND NOT EXISTS(SELECT 1 FROM observation_cost_facts WHERE resolved=FALSE)`)
	return e
}

// Bounded initial and pending-association sweeps. Unknown invocation identities
// remain pending and rotate, so one missing dependency cannot starve later rows.
func ReconcileCostSummaries(ctx context.Context, sess db.Session, limit int) (int, error) {
	if limit < 1 || limit > 500 {
		limit = 500
	}
	done := 0
	e := sess.WithContext(ctx).Tx(func(tx db.Session) error {
		for _, kind := range []string{"query_cost_summary", "query_cost_pending"} {
			if _, e := tx.SQL().ExecContext(ctx, `INSERT IGNORE INTO history_maintenance_cursors(kind,after_id,updated_at)VALUES(?,0,UTC_TIMESTAMP(6))`, kind); e != nil {
				return e
			}
		}
		row, e := tx.SQL().QueryRowContext(ctx, `SELECT after_id FROM history_maintenance_cursors WHERE kind='query_cost_summary' FOR UPDATE`)
		if e != nil {
			return e
		}
		var after int
		if e = row.Scan(&after); e != nil {
			return e
		}
		var costs []models.CostRecord
		ids, e := observationCatchupIDs(ctx, tx, "cost_records", after, limit)
		if e != nil {
			return e
		}
		ids, e = historymaintenance.WriteLogPrefix(ctx, monthlySQL{tx.SQL()}, "cost_records", ids)
		if e != nil {
			return e
		}
		if len(ids) > 0 {
			e = tx.Collection("cost_records").Find(db.Cond{"id IN": ids}).Select("id", "invocation_id", "instance_id", "session_id", "trace_id", "currency", "estimated_cost").OrderBy("id").Limit(limit).All(&costs)
		}
		if e != nil {
			return e
		}
		if e = materializeCostBatch(ctx, tx, costs); e != nil {
			return e
		}
		if len(costs) > 0 {
			after = max(after, costs[len(costs)-1].ID)
			done += len(costs)
		}
		if e = clearObservationPending(ctx, tx, "cost_records", ids...); e != nil {
			return e
		}
		if _, e = tx.SQL().ExecContext(ctx, `UPDATE history_maintenance_cursors SET after_id=?,updated_at=UTC_TIMESTAMP(6) WHERE kind='query_cost_summary'`, after); e != nil {
			return e
		}
		_, e = tx.SQL().ExecContext(ctx, `UPDATE observation_summary_state SET ready=(target_id<=? AND NOT EXISTS(SELECT 1 FROM observation_cost_facts WHERE resolved=FALSE)) WHERE id=2`, after)
		return e
	})
	if e != nil {
		return done, e
	}
	e = sess.WithContext(ctx).Tx(func(tx db.Session) error {
		row, e := tx.SQL().QueryRowContext(ctx, `SELECT after_id FROM history_maintenance_cursors WHERE kind='query_cost_pending' FOR UPDATE`)
		if e != nil {
			return e
		}
		var after int
		if e = row.Scan(&after); e != nil {
			return e
		}
		rows, e := tx.SQL().QueryContext(ctx, `SELECT id,invocation_id,instance_id,session_id,trace_id,currency,estimated_cost FROM observation_cost_facts WHERE resolved=FALSE AND id>? ORDER BY id LIMIT ?`, after, limit)
		if e != nil {
			return e
		}
		var pending []models.CostRecord
		for rows.Next() {
			var v models.CostRecord
			if e = rows.Scan(&v.ID, &v.InvocationID, &v.InstanceID, &v.SessionID, &v.TraceID, &v.Currency, &v.EstimatedCost); e != nil {
				rows.Close()
				return e
			}
			pending = append(pending, v)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(pending) == 0 {
			after = 0
		}
		for i := range pending {
			if e = maintainCostSummary(ctx, tx, &pending[i]); e != nil {
				return e
			}
			after = pending[i].ID
		}
		_, e = tx.SQL().ExecContext(ctx, `UPDATE history_maintenance_cursors SET after_id=?,updated_at=UTC_TIMESTAMP(6) WHERE kind='query_cost_pending'`, after)
		return e
	})
	return done, e
}

func sessionCostSummarySQL(filter SessionUsageFilter) (string, []interface{}) {
	q := `SELECT d.instance_id,d.session_id,d.currency,SUM(d.estimated_cost) estimated_cost FROM observation_session_cost_daily d JOIN tokens t ON t.instance_id=d.instance_id AND t.session_id=d.session_id WHERE 1=1`
	args := []interface{}{}
	var low, high *time.Time
	if filter.Since != nil {
		v := filter.Since.UTC().Truncate(24 * time.Hour)
		if v.Before(*filter.Since) {
			v = v.Add(24 * time.Hour)
		}
		if !v.Equal(*filter.Since) {
			low = &v
		}
		q += " AND d.day_utc>=?"
		args = append(args, v)
	}
	if filter.Until != nil {
		v := filter.Until.UTC().Truncate(24 * time.Hour)
		if !v.Equal(*filter.Until) {
			high = &v
		}
		q += " AND d.day_utc<?"
		args = append(args, v)
	}
	q += " GROUP BY d.instance_id,d.session_id,d.currency"
	if low != nil || high != nil {
		q += ` UNION ALL SELECT f.instance_id,f.session_id,f.currency,SUM(f.estimated_cost) FROM observation_cost_facts f JOIN tokens t ON t.instance_id=f.instance_id AND t.session_id=f.session_id WHERE f.eligible_at IS NOT NULL`
		q, args = appendTimeFilter(q, args, filter, "f.eligible_at")
		q += " AND ("
		if low != nil {
			q += "f.eligible_at<?"
			args = append(args, *low)
		}
		if high != nil {
			if low != nil {
				q += " OR "
			}
			q += "f.eligible_at>=?"
			args = append(args, *high)
		}
		q += ") GROUP BY f.instance_id,f.session_id,f.currency"
	}
	q += ` UNION ALL SELECT l.instance_id,l.session_id,l.currency,l.estimated_cost FROM observation_legacy_trace_costs l JOIN tokens t ON t.instance_id=l.instance_id AND t.session_id=l.session_id WHERE EXISTS(SELECT 1 FROM invocation_rows mi WHERE mi.instance_id=l.instance_id AND mi.session_id=l.session_id AND mi.trace_id=l.trace_id)`
	return q, args
}

func materializeCostBatch(ctx context.Context, tx db.Session, costs []models.CostRecord) error {
	if len(costs) == 0 {
		return nil
	}
	ids := make([]interface{}, len(costs))
	for i, c := range costs {
		ids[i] = c.ID
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	if e := lockCostSummaryDays(ctx, tx, ids); e != nil {
		return e
	}
	rows, e := tx.SQL().QueryContext(ctx, `SELECT id FROM observation_cost_facts WHERE id IN (`+marks+`)`, ids...)
	if e != nil {
		return e
	}
	seen := map[int]bool{}
	for rows.Next() {
		var id int
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		seen[id] = true
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	ids = nil
	for _, c := range costs {
		if !seen[c.ID] {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	marks = strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	result, e := tx.SQL().ExecContext(ctx, `INSERT IGNORE INTO observation_cost_facts(id,invocation_id,instance_id,session_id,trace_id,currency,estimated_cost,eligible_at,resolved)
 SELECT cr.id,cr.invocation_id,cr.instance_id,cr.session_id,cr.trace_id,COALESCE(NULLIF(UPPER(TRIM(cr.currency)),''),'USD'),cr.estimated_cost,
 CASE WHEN mi.status<>'blocked' AND mi.instance_id=cr.instance_id AND mi.session_id=cr.session_id AND cr.session_id<>'' THEN mi.created_at ELSE NULL END,
 (cr.invocation_id IS NULL OR mi.id IS NOT NULL)
 FROM cost_records cr LEFT JOIN observation_invocation_summaries mi ON mi.id=cr.invocation_id WHERE cr.id IN (`+marks+`)`, ids...)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != int64(len(ids)) {
		return fmt.Errorf("cost summary batch raced with delivery; retry")
	}
	if e = materializeCostOverview(ctx, tx, "cr.id IN ("+marks+")", ids...); e != nil {
		return e
	}
	if _, e = tx.SQL().ExecContext(ctx, `INSERT INTO observation_session_cost_daily(instance_id,session_id,day_utc,currency,estimated_cost) SELECT instance_id,session_id,DATE(eligible_at),currency,SUM(estimated_cost) FROM observation_cost_facts WHERE id IN (`+marks+`) AND eligible_at IS NOT NULL GROUP BY instance_id,session_id,DATE(eligible_at),currency ON DUPLICATE KEY UPDATE estimated_cost=estimated_cost+VALUES(estimated_cost)`, ids...); e != nil {
		return e
	}
	_, e = tx.SQL().ExecContext(ctx, `INSERT INTO observation_legacy_trace_costs(instance_id,session_id,trace_id,currency,estimated_cost) SELECT instance_id,session_id,trace_id,currency,SUM(estimated_cost) FROM observation_cost_facts WHERE id IN (`+marks+`) AND invocation_id IS NULL AND instance_id IS NOT NULL AND session_id IS NOT NULL AND session_id<>'' GROUP BY instance_id,session_id,trace_id,currency ON DUPLICATE KEY UPDATE estimated_cost=estimated_cost+VALUES(estimated_cost)`, ids...)
	return e
}

func rebuildCostDay(ctx context.Context, tx db.Session, start, end time.Time) error {
	if _, e := tx.SQL().ExecContext(ctx, `DELETE FROM observation_cost_overview_daily WHERE day_utc=?`, start); e != nil {
		return e
	}
	if e := materializeCostOverview(ctx, tx, "cr.recorded_at>=? AND cr.recorded_at<? AND EXISTS(SELECT 1 FROM observation_cost_facts f WHERE f.id=cr.id)", start, end); e != nil {
		return e
	}
	if _, e := tx.SQL().ExecContext(ctx, `DELETE FROM observation_session_cost_daily WHERE day_utc=?`, start); e != nil {
		return e
	}
	if _, e := tx.SQL().ExecContext(ctx, `INSERT INTO observation_session_cost_daily SELECT instance_id,session_id,DATE(eligible_at),currency,SUM(estimated_cost) FROM observation_cost_facts WHERE eligible_at>=? AND eligible_at<? GROUP BY instance_id,session_id,DATE(eligible_at),currency`, start, end); e != nil {
		return e
	}
	_, e := tx.SQL().ExecContext(ctx, `INSERT INTO observation_legacy_trace_costs SELECT f.instance_id,f.session_id,f.trace_id,f.currency,SUM(f.estimated_cost) FROM observation_cost_facts f JOIN (SELECT DISTINCT instance_id,session_id,trace_id FROM observation_invocation_summaries WHERE created_at>=? AND created_at<?) affected ON affected.instance_id=f.instance_id AND affected.session_id=f.session_id AND affected.trace_id=f.trace_id WHERE f.invocation_id IS NULL GROUP BY f.instance_id,f.session_id,f.trace_id,f.currency ON DUPLICATE KEY UPDATE estimated_cost=VALUES(estimated_cost)`, start, end)
	return e
}
