package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/backupgate"
	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// This is an operator-reviewed migration protocol, never an automatic recovery
// shortcut. A digest proves freshness, not correctness of historical effects.
type TeamProjectionAudit struct {
	Version          int                        `json:"version"`
	TeamID           int                        `json:"team_id"`
	Cursor           string                     `json:"cursor"`
	StateSHA256      string                     `json:"state_sha256"`
	State            json.RawMessage            `json:"state"`
	Reviewer         string                     `json:"reviewer"`
	BaselineEvidence string                     `json:"baseline_evidence"`
	Resolutions      []TeamProjectionResolution `json:"resolutions"`
}

type TeamProjectionResolution struct {
	OutboxID      int    `json:"outbox_id"`
	PayloadSHA256 string `json:"payload_sha256"`
	Disposition   string `json:"disposition"`
	Evidence      string `json:"evidence"`
}

func projectionSHA(value []byte) string { h := sha256.Sum256(value); return hex.EncodeToString(h[:]) }

func (r *teamRepository) projectionAuditSnapshot(ctx context.Context, teamID int) (json.RawMessage, error) {
	snapshot := map[string][]map[string]interface{}{}
	total := 0
	for _, table := range []string{"teams", "team_members", "team_tasks", "team_work_items", "team_workflow_phases", "team_events", "team_event_outbox", "team_projection_receipts", "team_projection_watermarks", "history_outbox_receipts", "history_identity_receipts"} {
		key, order := "team_id", "id"
		if table == "teams" {
			key = "id"
		}
		if table == "team_projection_receipts" {
			order = "identity_hash"
		}
		if table == "team_projection_watermarks" {
			order = "team_id"
		}
		if table == "history_outbox_receipts" {
			order = "destination,message_id"
		}
		if table == "history_identity_receipts" {
			order = "kind,key_type,key_hash"
		}
		rows, err := r.sess.SQL().QueryContext(ctx, "SELECT * FROM "+table+" WHERE "+key+"=? ORDER BY "+order+" LIMIT 10001 FOR UPDATE", teamID)
		if err != nil {
			return nil, err
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			return nil, err
		}
		data := []map[string]interface{}{}
		for rows.Next() {
			if len(data) >= 10000 {
				rows.Close()
				return nil, fmt.Errorf("Team projection audit exceeds 10000 rows in %s; use a separately reviewed partitioned audit", table)
			}
			values := make([]interface{}, len(columns))
			ptrs := make([]interface{}, len(columns))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err = rows.Scan(ptrs...); err != nil {
				rows.Close()
				return nil, err
			}
			item := map[string]interface{}{}
			for i, column := range columns {
				item[column] = values[i]
			}
			encoded, err := json.Marshal(item)
			if err != nil {
				rows.Close()
				return nil, err
			}
			total += len(encoded)
			if total > 32<<20 {
				rows.Close()
				return nil, fmt.Errorf("Team projection audit exceeds 32 MiB; use a separately reviewed partitioned audit")
			}
			data = append(data, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		snapshot[table] = data
	}
	return json.Marshal(snapshot)
}

func (r *teamRepository) legacyProjectionRows(teamID int) ([]models.TeamEventOutbox, error) {
	var rows []models.TeamEventOutbox
	err := r.sess.Collection("team_event_outbox").Find(db.Cond{"team_id": teamID, "destination": "internal:team-event-projection", "status !=": "delivered", "source_event_id NOT LIKE": "projection-v2:%"}).OrderBy("id").All(&rows)
	return rows, err
}

func (r *teamRepository) ExportTeamProjectionAudit(ctx context.Context, teamID int) (json.RawMessage, error) {
	var manifest TeamProjectionAudit
	_, err := r.withProjectionOwner(ctx, teamID, func(tx *teamRepository, team *models.Team) error {
		state, err := tx.projectionAuditSnapshot(ctx, teamID)
		if err != nil {
			return err
		}
		manifest = TeamProjectionAudit{Version: 1, TeamID: teamID, Cursor: team.RedisEventsLastID, StateSHA256: projectionSHA(state), State: state, Resolutions: []TeamProjectionResolution{}}
		rows, err := tx.legacyProjectionRows(teamID)
		if err != nil {
			return err
		}
		for _, row := range rows {
			manifest.Resolutions = append(manifest.Resolutions, TeamProjectionResolution{OutboxID: row.ID, PayloadSHA256: projectionSHA([]byte(row.PayloadJSON))})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(manifest)
}

func (r *teamRepository) ApplyTeamProjectionAudit(ctx context.Context, raw json.RawMessage) error {
	release, err := backupgate.HoldWorkUnit(ctx, "team.projection_audit")
	if err != nil {
		return err
	}
	defer release()
	if len(raw) > 40<<20 {
		return fmt.Errorf("projection audit exceeds 40 MiB")
	}
	var manifest TeamProjectionAudit
	if err = json.Unmarshal(raw, &manifest); err != nil {
		return err
	}
	if manifest.Version != 1 || manifest.TeamID <= 0 || strings.TrimSpace(manifest.Reviewer) == "" || len(manifest.Reviewer) > 255 || strings.TrimSpace(manifest.BaselineEvidence) == "" {
		return fmt.Errorf("reviewer and full historical business/source-window evidence are required")
	}
	canonical, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	auditID := projectionSHA(canonical)
	_, err = r.withProjectionOwner(ctx, manifest.TeamID, func(tx *teamRepository, team *models.Team) error {
		found, err := tx.sess.Collection("team_projection_audits").Find(db.Cond{"audit_sha256": auditID}).Count()
		if err != nil {
			return err
		}
		if found > 0 {
			return nil
		}
		state, err := tx.projectionAuditSnapshot(ctx, team.ID)
		if err != nil {
			return err
		}
		var declared interface{}
		decoder := json.NewDecoder(bytes.NewReader(manifest.State))
		decoder.UseNumber()
		if err = decoder.Decode(&declared); err != nil {
			return err
		}
		declaredJSON, err := json.Marshal(declared)
		if err != nil {
			return err
		}
		if manifest.Cursor != team.RedisEventsLastID || manifest.StateSHA256 != projectionSHA(state) || manifest.StateSHA256 != projectionSHA(declaredJSON) {
			return fmt.Errorf("reviewed Team projection snapshot changed; export and review again")
		}
		legacy, err := tx.legacyProjectionRows(team.ID)
		if err != nil {
			return err
		}
		if len(legacy) != len(manifest.Resolutions) {
			return fmt.Errorf("every legacy projection requires an explicit reviewed disposition")
		}
		resolutions := map[int]TeamProjectionResolution{}
		for _, resolution := range manifest.Resolutions {
			if _, duplicate := resolutions[resolution.OutboxID]; duplicate {
				return fmt.Errorf("duplicate projection resolution")
			}
			resolutions[resolution.OutboxID] = resolution
		}
		for _, row := range legacy {
			resolution, ok := resolutions[row.ID]
			if !ok || resolution.PayloadSHA256 != projectionSHA([]byte(row.PayloadJSON)) || strings.TrimSpace(resolution.Evidence) == "" {
				return fmt.Errorf("legacy projection %d has missing or changed evidence", row.ID)
			}
			var message struct {
				ID     string
				Fields map[string]string
			}
			if err = json.Unmarshal([]byte(row.PayloadJSON), &message); err != nil || message.ID == "" {
				return fmt.Errorf("legacy projection %d has invalid source identity", row.ID)
			}
			business := message.Fields["eventId"]
			if business == "" {
				business = message.Fields["event_id"]
			}
			var payload map[string]interface{}
			if json.Unmarshal([]byte(message.Fields["payload"]), &payload) == nil {
				for _, key := range []string{"eventId", "event_id"} {
					if value, ok := payload[key].(string); ok && value != "" {
						business = value
						break
					}
				}
			}
			switch resolution.Disposition {
			case "effects_verified_complete":
				identities := [][]byte{projectionIdentity("stream", message.ID)}
				if business != "" {
					identities = append(identities, projectionIdentity("event", business))
				}
				for _, identity := range identities {
					if _, err = tx.sess.SQL().ExecContext(ctx, "INSERT IGNORE INTO team_projection_receipts(team_id,identity_hash,stream_id) VALUES(?,UNHEX(?),?)", team.ID, hex.EncodeToString(identity), message.ID); err != nil {
						return err
					}
				}
				if err = tx.MarkEventOutboxDelivered(row.ID, time.Now().UTC()); err != nil {
					return err
				}
			case "effects_verified_rolled_back":
				streamExists, err := tx.EventExistsByStreamID(team.ID, message.ID)
				if err != nil {
					return err
				}
				businessExists, err := tx.EventExistsByEventID(team.ID, business)
				if err != nil {
					return err
				}
				committed, err := tx.projectionCommitted(team.ID, message.ID, business)
				if err != nil {
					return err
				}
				if streamExists || businessExists || committed {
					return fmt.Errorf("legacy projection %d still has source/completion markers; rollback proof conflicts", row.ID)
				}
				// Keep the original transport envelope and message ID. The old source
				// recovery marker and its reviewed replacement both remain in this audit.
				if _, err = tx.sess.SQL().ExecContext(ctx, "UPDATE team_event_outbox SET source_event_id=?,available_at=?,updated_at=? WHERE id=?", "projection-v2:audited:"+projectionSHA([]byte(row.SourceEventID)), time.Now().UTC(), time.Now().UTC(), row.ID); err != nil {
					return err
				}
			default:
				return fmt.Errorf("legacy projection %d has unknown disposition", row.ID)
			}
		}
		if _, err = tx.sess.SQL().ExecContext(ctx, `INSERT INTO team_projection_watermarks(team_id,committed_cursor,baseline_verified) VALUES(?,?,1) ON DUPLICATE KEY UPDATE committed_cursor=VALUES(committed_cursor),baseline_verified=1,updated_at=CURRENT_TIMESTAMP(6)`, team.ID, team.RedisEventsLastID); err != nil {
			return err
		}
		_, err = tx.sess.SQL().ExecContext(ctx, "INSERT INTO team_projection_audits(audit_sha256,team_id,state_sha256,reviewer,manifest_json) VALUES(?,?,?,?,?)", auditID, team.ID, manifest.StateSHA256, manifest.Reviewer, string(canonical))
		return err
	})
	return err
}
