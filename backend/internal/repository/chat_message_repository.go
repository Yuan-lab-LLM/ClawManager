package repository

import (
	"fmt"
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
	res, err := r.sess.Collection("chat_messages").Insert(message)
	if err != nil {
		return fmt.Errorf("failed to create chat message: %w", err)
	}
	message.ID = int(res.ID().(int64))
	return nil
}

func (r *chatMessageRepository) ListByTraceID(traceID string) ([]models.ChatMessageRecord, error) {
	var items []models.ChatMessageRecord
	if err := r.sess.Collection("chat_messages").Find(db.Cond{"trace_id": traceID}).OrderBy("sequence_no", "id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list chat messages by trace: %w", err)
	}
	return items, nil
}
