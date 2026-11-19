package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
)

var ErrTeamOutboxCapacity = errors.New("Team durable outbox capacity exhausted; retry after pending deliveries drain")

type TeamDispatchOutboxRepository interface {
	EnqueueTeamDispatch(*models.TeamTask, *models.TeamEventOutbox) error
	MarkTeamDispatchSent(taskID int, streamID string, at time.Time) error
}

func TeamOutboxReceiptIdentity(teamID int, destination, messageID string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s", teamID, destination, messageID)))
	return hex.EncodeToString(h[:])
}

// The task exists before workspace preparation; acceptance is the atomic durable
// envelope commit. Redis availability is independent of this acceptance point.
func (r *teamRepository) EnqueueTeamDispatch(task *models.TeamTask, outbox *models.TeamEventOutbox) error {
	if task == nil || outbox == nil || task.ID < 1 || task.TeamID != outbox.TeamID {
		return fmt.Errorf("task/outbox identity required")
	}
	return r.withTeamTransaction(task.TeamID, func(tx *teamRepository) error {
		if err := RequireTeamRunning(context.Background(), tx.sess, task.TeamID); err != nil {
			return err
		}
		current, err := tx.GetTaskByID(task.ID)
		if err != nil {
			return err
		}
		if current == nil || current.TeamID != task.TeamID {
			return fmt.Errorf("dispatch task identity mismatch")
		}
		if current.Status != models.TeamTaskStatusPending || current.RedisStreamID != nil {
			*task = *current
			return nil
		}
		if err := tx.UpdateTask(task); err != nil {
			return err
		}
		return tx.CreateEventOutbox(outbox)
	})
}

func (r *teamRepository) MarkTeamDispatchSent(taskID int, streamID string, at time.Time) error {
	_, err := r.sess.SQL().Exec("UPDATE team_tasks SET status='dispatched',redis_stream_id=?,dispatched_at=?,updated_at=? WHERE id=? AND status='pending' AND redis_stream_id IS NULL", streamID, at, at, taskID)
	return err
}

func (r *teamRepository) admitTeamOutbox(outbox *models.TeamEventOutbox) error {
	// Serializes only bounded admission queries, never a Redis/network operation.
	if _, err := r.sess.SQL().Exec("UPDATE team_outbox_admission SET generation=generation+1 WHERE id=1"); err != nil {
		return err
	}
	prior, err := r.sess.SQL().QueryRow("SELECT id,payload_json FROM team_event_outbox WHERE team_id=? AND destination=? AND message_id=?", outbox.TeamID, outbox.Destination, outbox.MessageID)
	if err != nil {
		return err
	}
	var duplicate int
	var payload string
	if err = prior.Scan(&duplicate, &payload); err == nil {
		if payload != outbox.PayloadJSON {
			return fmt.Errorf("Team outbox identity payload conflict")
		}
		return nil
	} else if err != sql.ErrNoRows {
		return err
	}
	maximum := 65536
	if strings.HasPrefix(outbox.Destination, "internal:") {
		maximum = 256 << 10
	}
	if len(outbox.PayloadJSON) > maximum {
		return ErrTeamOutboxCapacity
	}
	var count, bytes, teamCount, teamBytes int64
	// A terminal cancellation releases outstanding-work capacity only when its
	// permanent receipt authenticates the exact source and payload. A status-only
	// update must not silently remove unproven work from admission accounting.
	// Keep indexed equality joins, with byte-exact guards for Unicode CI columns.
	row, err := r.sess.SQL().QueryRow(`SELECT COUNT(*),COALESCE(SUM(OCTET_LENGTH(o.payload_json)),0),COALESCE(SUM(o.team_id=?),0),COALESCE(SUM(CASE WHEN o.team_id=? THEN OCTET_LENGTH(o.payload_json) ELSE 0 END),0) FROM team_event_outbox o WHERE o.status<>'delivered' AND NOT (o.status='cancelled' AND EXISTS(SELECT 1 FROM history_outbox_cancellations c WHERE c.team_id=o.team_id AND c.destination=o.destination AND c.message_id=o.message_id AND BINARY c.destination=BINARY o.destination AND BINARY c.message_id=BINARY o.message_id AND c.source_event_id=o.source_event_id AND BINARY c.source_event_id=BINARY o.source_event_id AND c.payload_sha256=SHA2(o.payload_json,256) AND BINARY c.payload_sha256=BINARY SHA2(o.payload_json,256) AND c.cancelled_at IS NOT NULL AND c.reason<>'' AND c.closure_sha256 REGEXP '^[0-9a-f]{64}$' AND BINARY c.closure_sha256=BINARY LOWER(c.closure_sha256)))`, outbox.TeamID, outbox.TeamID)
	if err != nil {
		return err
	}
	if err = row.Scan(&count, &bytes, &teamCount, &teamBytes); err != nil {
		return err
	}
	maxCount, maxBytes, maxTeam, maxTeamBytes := int64(100000), int64(512<<20), int64(10000), int64(128<<20)
	if strings.HasPrefix(outbox.SourceEventID, "dispatch:") {
		maxCount = maxCount * 3 / 4
		maxBytes = maxBytes * 3 / 4
		maxTeam = maxTeam * 3 / 4
		maxTeamBytes = maxTeamBytes * 3 / 4
	}
	if count >= maxCount || bytes+int64(len(outbox.PayloadJSON)) > maxBytes || teamCount >= maxTeam || teamBytes+int64(len(outbox.PayloadJSON)) > maxTeamBytes {
		return ErrTeamOutboxCapacity
	}
	return nil
}

func (r *teamRepository) admitTeamTask(task *models.TeamTask) error {
	if len(task.PayloadJSON) > 65536 {
		return ErrTeamOutboxCapacity
	}
	if _, err := r.sess.SQL().Exec("UPDATE team_outbox_admission SET generation=generation+1 WHERE id=1"); err != nil {
		return err
	}
	row, err := r.sess.SQL().QueryRow(`SELECT COUNT(*),COALESCE(SUM(OCTET_LENGTH(payload_json)),0),COALESCE(SUM(team_id=?),0),COALESCE(SUM(CASE WHEN team_id=? THEN OCTET_LENGTH(payload_json) ELSE 0 END),0) FROM team_tasks WHERE status NOT IN ('succeeded','failed','cancelled')`, task.TeamID, task.TeamID)
	if err != nil {
		return err
	}
	var count, bytes, teamCount, teamBytes int64
	if err = row.Scan(&count, &bytes, &teamCount, &teamBytes); err != nil {
		return err
	}
	if count >= 75000 || bytes+int64(len(task.PayloadJSON)) > 384<<20 || teamCount >= 7500 || teamBytes+int64(len(task.PayloadJSON)) > 96<<20 {
		return ErrTeamOutboxCapacity
	}
	return nil
}
