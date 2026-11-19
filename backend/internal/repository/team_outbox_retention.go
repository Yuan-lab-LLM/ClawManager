package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/upper/db/v4"
)

type TeamOutboxReceiptCandidate struct {
	Closed                                               bool
	OutboxID, TeamID, TaskID                             int
	Destination, MessageID, SourceEventID, PayloadSHA256 string
	DeliveredAt                                          time.Time
}
type TeamOutboxRetentionRepository interface {
	ListEligibleTeamOutboxReceipts(context.Context, int, time.Time, int) ([]TeamOutboxReceiptCandidate, error)
	CloseTeamOutboxReceiptWindow(context.Context, int, int, string) error
	MarkTeamOutboxReceiptReclaimed(context.Context, int) error
}

const outboxTaskRef = `CASE WHEN JSON_VALID(o.payload_json) THEN COALESCE(JSON_UNQUOTE(JSON_EXTRACT(o.payload_json,'$.rootTaskId')),JSON_UNQUOTE(JSON_EXTRACT(o.payload_json,'$.root_task_id')),JSON_UNQUOTE(JSON_EXTRACT(o.payload_json,'$.taskId')),JSON_UNQUOTE(JSON_EXTRACT(o.payload_json,'$.task_id'))) END`
const outboxTerminalTask = `t.status IN ('succeeded','completed','failed','cancelled') AND t.finished_at IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM team_work_items w WHERE w.root_task_id=t.id AND (w.finished_at IS NULL OR w.status NOT IN ('succeeded','completed','failed','cancelled','stale')))
 AND NOT EXISTS(SELECT 1 FROM team_workflow_phases p WHERE p.root_task_id=t.id AND (p.completed_at IS NULL OR p.status NOT IN ('completed','failed','cancelled','skipped')))
 AND NOT EXISTS(SELECT 1 FROM team_event_outbox retry WHERE retry.team_id=o.team_id AND retry.destination LIKE 'internal:%' AND retry.status<>'delivered')`

func outboxRetentionSQL() string {
	return `SELECT o.id,o.team_id,t.id,o.destination,o.message_id,o.source_event_id,o.payload_json,o.delivered_at,closed.outbox_id IS NOT NULL FROM team_event_outbox o LEFT JOIN team_projection_command_sources pcs ON pcs.team_id=o.team_id AND pcs.message_id=o.message_id JOIN team_tasks t ON t.team_id=o.team_id AND ((o.destination='internal:team-projection-command' AND pcs.task_id=t.id) OR (o.destination NOT LIKE 'internal:%' AND (` + outboxTaskRef + ` IN(CAST(t.id AS CHAR),CONCAT('team-',t.team_id,'-task-',t.id)) OR (` + outboxTaskRef + ` IS NULL AND o.source_event_id=CONCAT('dispatch:',o.message_id) AND o.message_id=t.message_id)))) LEFT JOIN team_outbox_receipt_closures closed ON closed.outbox_id=o.id WHERE closed.redis_reclaimed_at IS NULL AND o.status='delivered' AND ` + outboxTerminalTask + ` AND o.delivered_at<? AND o.created_at<? AND t.finished_at<?`
}

func scanOutboxCandidate(row interface{ Scan(...any) error }) (TeamOutboxReceiptCandidate, string, error) {
	var candidate TeamOutboxReceiptCandidate
	var payload string
	err := row.Scan(&candidate.OutboxID, &candidate.TeamID, &candidate.TaskID, &candidate.Destination, &candidate.MessageID, &candidate.SourceEventID, &payload, &candidate.DeliveredAt, &candidate.Closed)
	digest := sha256.Sum256([]byte(payload))
	candidate.PayloadSHA256 = hex.EncodeToString(digest[:])
	return candidate, payload, err
}
func (r *teamRepository) ListEligibleTeamOutboxReceipts(ctx context.Context, teamID int, cutoff time.Time, limit int) ([]TeamOutboxReceiptCandidate, error) {
	if teamID <= 0 || limit < 1 || limit > 100 || cutoff.IsZero() || cutoff.After(time.Now().UTC().AddDate(0, 0, -7)) {
		return nil, errors.New("receipt closure requires explicit Team,>=7day cutoff and1..100rows")
	}
	rows, err := r.sess.SQL().QueryContext(ctx, outboxRetentionSQL()+` AND o.team_id=? ORDER BY o.id LIMIT ?`, cutoff.UTC(), cutoff.UTC(), cutoff.UTC(), teamID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TeamOutboxReceiptCandidate{}
	for rows.Next() {
		candidate, _, err := scanOutboxCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, candidate)
	}
	return out, rows.Err()
}
func (r *teamRepository) CloseTeamOutboxReceiptWindow(ctx context.Context, outboxID, taskID int, proofSHA string) error {
	proof, err := hex.DecodeString(proofSHA)
	if err != nil || len(proof) != 32 || outboxID <= 0 || taskID <= 0 {
		return errors.New("native replay-source window proof and exact identities required")
	}
	var teamID int
	row, err := r.sess.SQL().QueryRowContext(ctx, `SELECT team_id FROM team_event_outbox WHERE id=?`, outboxID)
	if err != nil {
		return err
	}
	if err = row.Scan(&teamID); err != nil {
		return err
	}
	work := func(sess db.Session) error {
		if err := lockTeamProjectionRow(sess, teamID); err != nil {
			return err
		}
		cutoff := time.Now().UTC().AddDate(0, 0, -8)
		row, err := sess.SQL().QueryRowContext(ctx, outboxRetentionSQL()+` AND o.id=? AND t.id=? FOR UPDATE`, cutoff, cutoff, cutoff, outboxID, taskID)
		if err != nil {
			return err
		}
		candidate, _, err := scanOutboxCandidate(row)
		if err != nil {
			return fmt.Errorf("outbox/task recovery state is not safely terminal: %w", err)
		}
		if _, err = sess.SQL().ExecContext(ctx, `INSERT IGNORE INTO history_outbox_receipts(team_id,destination,message_id,source_event_id,payload_sha256,delivered_at,closure_sha256) VALUES(?,?,?,?,?,?,?)`, candidate.TeamID, candidate.Destination, candidate.MessageID, candidate.SourceEventID, candidate.PayloadSHA256, candidate.DeliveredAt, proofSHA); err != nil {
			return err
		}
		var source, digest string
		row, err = sess.SQL().QueryRowContext(ctx, `SELECT source_event_id,payload_sha256 FROM history_outbox_receipts WHERE team_id=? AND destination=? AND message_id=?`, candidate.TeamID, candidate.Destination, candidate.MessageID)
		if err != nil {
			return err
		}
		if err = row.Scan(&source, &digest); err != nil {
			return err
		}
		if source != candidate.SourceEventID || digest != candidate.PayloadSHA256 {
			return errors.New("permanent outbox receipt identity conflict")
		}
		if candidate.Closed {
			return nil
		}
		_, err = sess.SQL().ExecContext(ctx, `INSERT INTO team_outbox_receipt_closures(outbox_id,team_id,task_id,source_window_sha256,closed_before,closed_at) VALUES(?,?,?,?,?,UTC_TIMESTAMP(6))`, candidate.OutboxID, candidate.TeamID, candidate.TaskID, proofSHA, cutoff)
		return err
	}
	if r.inTransaction {
		return work(r.sess)
	}
	return r.sess.TxContext(ctx, work, nil)
}

func (r *teamRepository) MarkTeamOutboxReceiptReclaimed(ctx context.Context, outboxID int) error {
	if outboxID <= 0 {
		return errors.New("outbox identity required")
	}
	var teamID int
	row, err := r.sess.SQL().QueryRowContext(ctx, `SELECT team_id FROM team_outbox_receipt_closures WHERE outbox_id=?`, outboxID)
	if err != nil {
		return err
	}
	if err = row.Scan(&teamID); err != nil {
		return err
	}
	work := func(sess db.Session) error {
		if err := lockTeamProjectionRow(sess, teamID); err != nil {
			return err
		}
		_, err := sess.SQL().ExecContext(ctx, `UPDATE team_outbox_receipt_closures SET redis_reclaimed_at=COALESCE(redis_reclaimed_at,UTC_TIMESTAMP(6)) WHERE outbox_id=? AND team_id=?`, outboxID, teamID)
		return err
	}
	if r.inTransaction {
		return work(r.sess)
	}
	return r.sess.TxContext(ctx, work, nil)
}
