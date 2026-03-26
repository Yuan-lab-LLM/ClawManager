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

// NewChatMessageRepository creates a new chat message repository and ensures its table exists.
func NewChatMessageRepository(sess db.Session) ChatMessageRepository {
	repo := &chatMessageRepository{sess: sess}
	repo.ensureTable()
	return repo
}

func (r *chatMessageRepository) ensureTable() {
	const query = `
CREATE TABLE IF NOT EXISTS chat_messages (
  id INT AUTO_INCREMENT PRIMARY KEY,
  trace_id VARCHAR(100) NOT NULL,
  session_id VARCHAR(100) NOT NULL,
  request_id VARCHAR(100) NULL,
  user_id INT NULL,
  instance_id INT NULL,
  invocation_id INT NULL,
  role VARCHAR(50) NOT NULL,
  content LONGTEXT NOT NULL,
  sequence_no INT NOT NULL DEFAULT 0,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_chat_messages_trace_id (trace_id),
  INDEX idx_chat_messages_session_id (session_id),
  INDEX idx_chat_messages_request_id (request_id),
  INDEX idx_chat_messages_user_id (user_id),
  INDEX idx_chat_messages_sequence_no (sequence_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

	if _, err := r.sess.SQL().Exec(query); err != nil {
		panic(fmt.Errorf("failed to ensure chat_messages table: %w", err))
	}
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
