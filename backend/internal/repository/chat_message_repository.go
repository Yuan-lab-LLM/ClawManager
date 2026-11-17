package repository

import (
	"context"
	"fmt"
	"sort"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

// ChatMessageRepository defines repository operations for persisted chat messages.
type ChatMessageRepository interface {
	Create(message *models.ChatMessageRecord) error
	ListByTraceID(traceID string) ([]models.ChatMessageRecord, error)
}

type chatMessageRepository struct {
	sess db.Session
}

// NewChatMessageRepository creates a new chat message repository (schema is owned by embedded migrations).
func NewChatMessageRepository(sess db.Session) ChatMessageRepository {
	repo := &chatMessageRepository{sess: sess}
	return repo
}

func (r *chatMessageRepository) Create(message *models.ChatMessageRecord) error {
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now()
	}
	if handled, err := persistObservation(context.Background(), r.sess, "chat_messages", message); handled {
		return err
	}
	return r.sess.Tx(func(tx db.Session) error {
		res, err := tx.Collection("chat_messages").Insert(message)
		if err != nil {
			return fmt.Errorf("failed to create chat message: %w", err)
		}
		message.ID = int(res.ID().(int64))
		return maintainObservationSummary(context.Background(), tx, "chat_messages", message)
	})
}

func (r *chatMessageRepository) ListByTraceID(traceID string) ([]models.ChatMessageRecord, error) {
	items, err := routedLogDetails[models.ChatMessageRecord](r.sess, "chat_messages", traceID, "", 0, 200, false)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].SequenceNo != items[j].SequenceNo {
			return items[i].SequenceNo < items[j].SequenceNo
		}
		return items[i].ID < items[j].ID
	})
	return items, err
}
