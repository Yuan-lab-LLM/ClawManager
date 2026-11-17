package repository

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// settledOutboxReceipt preserves delivered and cancelled business facts after retention.
// It must execute in the same serialized Team transaction as an enqueue. Old
// schema fixtures fail closed too; production migrations run before workers.
func settledOutboxReceipt(sess db.Session, outbox *models.TeamEventOutbox) (bool, error) {
	row, err := sess.SQL().QueryRow(`SELECT source_event_id,payload_sha256,reason FROM history_outbox_cancellations WHERE team_id=? AND destination=? AND message_id=?`, outbox.TeamID, outbox.Destination, outbox.MessageID)
	if err != nil {
		return false, err
	}
	var cancelledSource, cancelledDigest, reason string
	err = row.Scan(&cancelledSource, &cancelledDigest, &reason)
	if err == nil {
		hash := sha256.Sum256([]byte(outbox.PayloadJSON))
		if cancelledSource != outbox.SourceEventID || cancelledDigest != hex.EncodeToString(hash[:]) {
			return false, fmt.Errorf("cancelled outbox identity reused with different source or payload")
		}
		outbox.Status = "cancelled"
		outbox.DeliveredAt = nil
		outbox.LastError = &reason
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	row, err = sess.SQL().QueryRow(`SELECT source_event_id,payload_sha256,delivered_at FROM history_outbox_receipts WHERE team_id=? AND destination=? AND message_id=?`, outbox.TeamID, outbox.Destination, outbox.MessageID)
	if err != nil {
		return false, err
	}
	var source, digest string
	var delivered time.Time
	err = row.Scan(&source, &digest, &delivered)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	hash := sha256.Sum256([]byte(outbox.PayloadJSON))
	if source != outbox.SourceEventID || digest != hex.EncodeToString(hash[:]) {
		return false, fmt.Errorf("delivered outbox identity reused with different source or payload")
	}
	outbox.Status = "delivered"
	outbox.DeliveredAt = &delivered
	return true, nil
}
