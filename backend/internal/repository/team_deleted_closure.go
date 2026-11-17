package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"clawreef/internal/models"
)

const teamDeletionReason = "Cancelled by user-requested whole-Team deletion after producer revocation"

func (r *teamRepository) deletionAudit(ctx context.Context, id int, eventID, eventType string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	body := string(raw)
	err = r.CreateEvent(&models.TeamEvent{TeamID: id, EventID: &eventID, EventType: eventType, PayloadJSON: &body})
	if errors.Is(err, ErrDuplicateTeamEvent) {
		return nil
	}
	return err
}

// At most 493 business rows plus three audit insert/updates and one job update.
// Terminal outcomes are retained; unknown states remain explicit blockers.
func (r *teamRepository) settleDeletedTeamWork(ctx context.Context, id int) error {
	remaining := 493
	for _, spec := range []struct{ table, stamp, states string }{
		{"team_tasks", "finished_at", "'pending','dispatched','running','waiting'"},
		{"team_work_items", "finished_at", "'pending','dispatched','running','waiting','blocked','awaiting_review','planned'"},
		{"team_workflow_phases", "completed_at", "'pending','planned','active','awaiting_results','awaiting_leader_decision'"},
	} {
		if remaining == 0 {
			break
		}
		rows, err := r.sess.SQL().QueryContext(ctx, "SELECT id FROM "+spec.table+" WHERE team_id=? AND status IN ("+spec.states+") ORDER BY id LIMIT ? FOR UPDATE", id, remaining)
		if err != nil {
			return err
		}
		ids := []int{}
		for rows.Next() {
			var v int
			if err = rows.Scan(&v); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			continue
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		args := make([]any, len(ids))
		for i, v := range ids {
			args[i] = v
		}
		extra := ""
		if spec.table == "team_tasks" {
			extra = ",workflow_state='failed',error_message='" + teamDeletionReason + "'"
		}
		if _, err = r.sess.SQL().ExecContext(ctx, "UPDATE "+spec.table+" SET status='cancelled',"+spec.stamp+"=COALESCE("+spec.stamp+",UTC_TIMESTAMP(6)),updated_at=UTC_TIMESTAMP(6)"+extra+" WHERE id IN ("+placeholders+")", args...); err != nil {
			return err
		}
		// One audit for each table uses <=3*2 extra mutations; reserve those below.
		if err = r.deletionAudit(ctx, id, fmt.Sprintf("team-delete-settle:%s:%d:%d", spec.table, ids[0], ids[len(ids)-1]), "team_deletion_work_cancelled", map[string]any{"reason": teamDeletionReason, "table": spec.table, "ids": ids}); err != nil {
			return err
		}
		remaining -= len(ids) + 2
		if remaining < 0 {
			remaining = 0
		}
	}
	return nil
}

func (r *teamRepository) finishDeletedTeamReplay(ctx context.Context, id int) error {
	row, err := r.sess.SQL().QueryRowContext(ctx, `SELECT t.user_id,t.status,j.owner_id,j.state,j.scan_cursor,j.producer_ids,j.scan_completed FROM teams t JOIN team_redis_cleanup_jobs j ON j.team_id=t.id WHERE t.id=? FOR UPDATE`, id)
	if err != nil {
		return err
	}
	var owner, jobOwner int
	var status, state, cursor, producers string
	var scanned bool
	if err = row.Scan(&owner, &status, &jobOwner, &state, &cursor, &producers, &scanned); err != nil {
		return err
	}
	if status != "deleted" || owner <= 0 || owner != jobOwner || (state != "archiving" && state != "complete") || cursor != "0" || !scanned {
		return errors.New("native Team cleanup has not completed its source scan")
	}
	var ids []int
	if json.Unmarshal([]byte(producers), &ids) != nil || ids == nil || len(ids) > 500 {
		return errors.New("captured producer inventory unavailable or exceeds bound")
	}
	var blockers int
	row, err = r.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM team_members WHERE team_id=? AND (status<>'deleted' OR instance_id IS NOT NULL)`, id)
	if err != nil {
		return err
	}
	if err = row.Scan(&blockers); err != nil {
		return err
	}
	if blockers != 0 {
		return errors.New("Team producers remain attached")
	}
	for _, producer := range ids {
		if producer <= 0 {
			return errors.New("invalid captured producer identity")
		}
		row, err = r.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instances WHERE id=?`, producer)
		if err != nil {
			return err
		}
		if err = row.Scan(&blockers); err != nil {
			return err
		}
		if blockers != 0 {
			return errors.New("captured producer instance still exists")
		}
	}
	// Every exact source key must have all consecutively numbered, owner-bound
	// archive chunks, followed by native deletion receipts. No age-only proof.
	row, err = r.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM team_redis_cleanup_keys k WHERE k.team_id=? AND (k.key_name NOT LIKE ? OR k.key_sha256<>UNHEX(SHA2(k.key_name,256)) OR NOT k.archive_complete OR k.deleted_at IS NULL OR k.chunk_no<1 OR k.chunk_no<>(SELECT COUNT(*) FROM team_redis_archive_receipts a WHERE a.team_id=k.team_id AND a.key_sha256=k.key_sha256 AND a.key_name=k.key_name AND a.deleted_at IS NOT NULL AND a.chunk_no>=0 AND a.chunk_no<k.chunk_no AND JSON_UNQUOTE(JSON_EXTRACT(a.object_ref,'$.owner'))=? AND JSON_UNQUOTE(JSON_EXTRACT(a.object_ref,'$.encryption_version'))='1' AND JSON_UNQUOTE(JSON_EXTRACT(a.object_ref,'$.sha256'))=LOWER(HEX(a.value_sha256)) AND JSON_UNQUOTE(JSON_EXTRACT(a.object_ref,'$.object_key')) LIKE 'observability/v1/%' AND LENGTH(a.value_sha256)=32))`, id, fmt.Sprintf("claw:team:%d:%%", id), fmt.Sprintf("user:%d", owner))
	if err != nil {
		return err
	}
	if err = row.Scan(&blockers); err != nil {
		return err
	}
	if blockers != 0 {
		return errors.New("Team archive/deletion receipts incomplete")
	}
	row, err = r.sess.SQL().QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM team_tasks WHERE team_id=? AND (finished_at IS NULL OR status NOT IN ('completed','succeeded','failed','cancelled'))) + (SELECT COUNT(*) FROM team_work_items WHERE team_id=? AND (finished_at IS NULL OR status NOT IN ('completed','succeeded','failed','cancelled','skipped','superseded'))) + (SELECT COUNT(*) FROM team_workflow_phases WHERE team_id=? AND (completed_at IS NULL OR status NOT IN ('done','completed','failed','cancelled','skipped','superseded')))`, id, id, id)
	if err != nil {
		return err
	}
	if err = row.Scan(&blockers); err != nil {
		return err
	}
	if blockers != 0 {
		return errors.New("Team task/ledger settlement incomplete")
	}
	var keys, chunks int64
	var receiptDigest string
	// A fixed-width aggregate of all four SHA256 words avoids GROUP_CONCAT
	// truncation while committing every immutable chunk receipt to the proof.
	words := []string{}
	for _, offset := range []int{1, 17, 33, 49} {
		words = append(words, fmt.Sprintf("BIT_XOR(CAST(CONV(SUBSTRING(SHA2(CONCAT(HEX(key_sha256),':',chunk_no,':',HEX(value_sha256),':',object_ref,':',deleted_at),256),%d,16),16,10) AS UNSIGNED))", offset))
	}
	row, err = r.sess.SQL().QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM team_redis_cleanup_keys WHERE team_id=?),COUNT(*),CONCAT_WS(':',`+strings.Join(words, ",")+`) FROM team_redis_archive_receipts WHERE team_id=?`, id, id)
	if err != nil {
		return err
	}
	if err = row.Scan(&keys, &chunks, &receiptDigest); err != nil {
		return err
	}
	proofRaw, _ := json.Marshal([]any{"native-deleted-team-v1", id, owner, producers, keys, chunks, receiptDigest})
	hash := sha256.Sum256(proofRaw)
	proof := hex.EncodeToString(hash[:])
	rows, err := r.sess.SQL().QueryContext(ctx, `SELECT id,source_event_id,destination,message_id,payload_json FROM team_event_outbox WHERE team_id=? AND status NOT IN ('delivered','cancelled') ORDER BY id LIMIT 165 FOR UPDATE`, id)
	if err != nil {
		return err
	}
	type cancellation struct {
		ID                                    int
		Source, Destination, Message, Payload string
	}
	var pending []cancellation
	for rows.Next() {
		var c cancellation
		if err = rows.Scan(&c.ID, &c.Source, &c.Destination, &c.Message, &c.Payload); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, c := range pending {
		h := sha256.Sum256([]byte(c.Payload))
		digest := hex.EncodeToString(h[:])
		if _, err = r.sess.SQL().ExecContext(ctx, `INSERT IGNORE INTO history_outbox_cancellations(team_id,destination,message_id,source_event_id,payload_sha256,cancelled_at,reason,closure_sha256) VALUES(?,?,?,?,?,UTC_TIMESTAMP(6),?,?)`, id, c.Destination, c.Message, c.Source, digest, teamDeletionReason, proof); err != nil {
			return err
		}
		var source, stored string
		row, err = r.sess.SQL().QueryRowContext(ctx, `SELECT source_event_id,payload_sha256 FROM history_outbox_cancellations WHERE team_id=? AND destination=? AND message_id=?`, id, c.Destination, c.Message)
		if err != nil {
			return err
		}
		if err = row.Scan(&source, &stored); err != nil {
			return err
		}
		if source != c.Source || stored != digest {
			return errors.New("permanent cancellation identity conflict")
		}
		if _, err = r.sess.SQL().ExecContext(ctx, `UPDATE team_event_outbox SET status='cancelled',last_error=?,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND team_id=?`, teamDeletionReason, c.ID, id); err != nil {
			return err
		}
	}
	if len(pending) > 0 {
		if err = r.deletionAudit(ctx, id, fmt.Sprintf("team-delete-outbox:%d:%d", pending[0].ID, pending[len(pending)-1].ID), "team_deletion_outbox_cancelled", map[string]any{"reason": teamDeletionReason, "proof_sha256": proof, "first_id": pending[0].ID, "last_id": pending[len(pending)-1].ID, "count": len(pending)}); err != nil {
			return err
		}
	}
	row, err = r.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM team_event_outbox WHERE team_id=? AND status NOT IN ('delivered','cancelled')`, id)
	if err != nil {
		return err
	}
	if err = row.Scan(&blockers); err != nil {
		return err
	}
	if blockers != 0 {
		return nil
	}
	if _, err = r.sess.SQL().ExecContext(ctx, `INSERT IGNORE INTO history_replay_closures(team_id,closed_before,replay_closed,recovery_closed,idempotency_durable,proof_sha256,verified_at) VALUES(?,UTC_TIMESTAMP(6),TRUE,TRUE,TRUE,?,UTC_TIMESTAMP(6))`, id, proof); err != nil {
		return err
	}
	_, err = r.sess.SQL().ExecContext(ctx, `UPDATE team_redis_cleanup_jobs SET state='complete',updated_at=UTC_TIMESTAMP(6) WHERE team_id=?`, id)
	return err
}
