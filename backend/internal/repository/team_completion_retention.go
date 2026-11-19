package repository

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/upper/db/v4"
)

type TeamCompletionReceiptCandidate struct {
	EventID, TaskID                         int
	BusinessEventID, CompletionID, StreamID string
	Closed                                  bool
}
type TeamCompletionRetentionRepository interface {
	ListTeamCompletionReceipts(context.Context, int, time.Time, int) ([]TeamCompletionReceiptCandidate, error)
	CloseTeamCompletionReceipt(context.Context, int, int, int, string) error
	MarkTeamCompletionReceiptReclaimed(context.Context, int, int) error
}

const terminalCompletionSQL = ` FROM team_events e JOIN team_tasks t ON t.id=e.task_id AND t.team_id=e.team_id
 LEFT JOIN team_completion_receipt_closures c ON c.team_id=e.team_id AND c.event_id=e.id
 WHERE c.redis_reclaimed_at IS NULL AND e.redis_stream_id IS NOT NULL AND (e.event_id IS NOT NULL OR e.completion_id IS NOT NULL)
 AND t.status IN ('succeeded','failed','cancelled') AND t.finished_at IS NOT NULL AND t.finished_at<? AND e.created_at<?
 AND NOT EXISTS(SELECT 1 FROM team_work_items w WHERE w.root_task_id=t.id AND (w.finished_at IS NULL OR w.status NOT IN ('succeeded','completed','failed','cancelled','stale')))
 AND NOT EXISTS(SELECT 1 FROM team_workflow_phases p WHERE p.root_task_id=t.id AND (p.completed_at IS NULL OR p.status NOT IN ('completed','failed','cancelled','skipped')))
 AND NOT EXISTS(SELECT 1 FROM team_event_outbox o WHERE o.team_id=e.team_id AND o.destination LIKE 'internal:%' AND o.status<>'delivered')`

func (r *teamRepository) ListTeamCompletionReceipts(ctx context.Context, teamID int, cutoff time.Time, limit int) ([]TeamCompletionReceiptCandidate, error) {
	if teamID < 1 || limit < 1 || limit > 100 || cutoff.After(time.Now().AddDate(0, 0, -7)) {
		return nil, errors.New("bounded terminal receipt window required")
	}
	rows, err := r.sess.SQL().QueryContext(ctx, `SELECT e.id,t.id,COALESCE(e.event_id,''),COALESCE(e.completion_id,''),e.redis_stream_id,c.event_id IS NOT NULL`+terminalCompletionSQL+` AND e.team_id=? ORDER BY e.id LIMIT ?`, cutoff, cutoff, teamID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TeamCompletionReceiptCandidate{}
	for rows.Next() {
		var v TeamCompletionReceiptCandidate
		if err = rows.Scan(&v.EventID, &v.TaskID, &v.BusinessEventID, &v.CompletionID, &v.StreamID, &v.Closed); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *teamRepository) CloseTeamCompletionReceipt(ctx context.Context, teamID, eventID, taskID int, proofSHA string) error {
	proof, err := hex.DecodeString(proofSHA)
	if err != nil || len(proof) != 32 {
		return errors.New("native source window proof required")
	}
	return r.withTeamTransaction(teamID, func(tx *teamRepository) error {
		cutoff := time.Now().UTC().AddDate(0, 0, -8)
		row, err := tx.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*)`+terminalCompletionSQL+` AND e.team_id=? AND e.id=? AND t.id=? FOR UPDATE`, cutoff, cutoff, teamID, eventID, taskID)
		if err != nil {
			return err
		}
		var n int
		if err = row.Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return errors.New("completion recovery dependencies are not closed")
		}
		_, err = tx.sess.SQL().ExecContext(ctx, `INSERT IGNORE INTO team_completion_receipt_closures(team_id,event_id,task_id,source_window_sha256,closed_at) VALUES(?,?,?,?,UTC_TIMESTAMP(6))`, teamID, eventID, taskID, proofSHA)
		return err
	})
}
func (r *teamRepository) MarkTeamCompletionReceiptReclaimed(ctx context.Context, teamID, eventID int) error {
	err := r.sess.WithContext(ctx).Collection("team_completion_receipt_closures").Find(db.Cond{"team_id": teamID, "event_id": eventID}).Update(db.Cond{"redis_reclaimed_at": time.Now().UTC()})
	return err
}
