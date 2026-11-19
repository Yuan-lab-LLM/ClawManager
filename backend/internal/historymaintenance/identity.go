package historymaintenance

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
)

func preserveIdentities(ctx context.Context, tx *sql.Tx, r Record) error {
	if r.Kind != "team_events" && r.Kind != "team_tasks" {
		return nil
	}
	var row map[string]any
	if err := json.Unmarshal(r.Payload, &row); err != nil {
		return err
	}
	keys := []string{"event_id", "completion_id", "redis_stream_id"}
	if r.Kind == "team_tasks" {
		keys = []string{"message_id", "accepted_completion_id"}
	}
	payload, _ := row["payload_json"].(string)
	digest := sha256.Sum256([]byte(payload))
	payloadHash := hex.EncodeToString(digest[:])
	for _, keyType := range keys {
		value, _ := row[keyType].(string)
		if value == "" {
			continue
		}
		identity := sha256.Sum256([]byte(value))
		_, err := tx.ExecContext(ctx, `INSERT IGNORE INTO history_identity_receipts(kind,team_id,key_type,key_hash,original_id,payload_sha256) VALUES(?,?,?,?,?,?)`, r.Kind, r.TeamID, keyType, identity[:], r.ID, payloadHash)
		if err != nil {
			return err
		}
		var id int64
		var existing string
		if err = tx.QueryRowContext(ctx, `SELECT original_id,payload_sha256 FROM history_identity_receipts WHERE kind=? AND team_id=? AND key_type=? AND key_hash=?`, r.Kind, r.TeamID, keyType, identity[:]).Scan(&id, &existing); err != nil {
			return err
		}
		if id != r.ID || existing != payloadHash {
			return errors.New("archived Team identity conflicts with durable receipt")
		}
	}
	return nil
}
