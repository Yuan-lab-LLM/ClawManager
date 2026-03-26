package repository

import (
	"fmt"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

// ChatSessionRepository defines repository operations for chat sessions.
type ChatSessionRepository interface {
	GetBySessionID(sessionID string) (*models.ChatSession, error)
	Save(session *models.ChatSession) error
}

type chatSessionRepository struct {
	sess db.Session
}

// NewChatSessionRepository creates a new chat session repository and ensures its table exists.
func NewChatSessionRepository(sess db.Session) ChatSessionRepository {
	repo := &chatSessionRepository{sess: sess}
	repo.ensureTable()
	return repo
}

func (r *chatSessionRepository) ensureTable() {
	const query = `
CREATE TABLE IF NOT EXISTS chat_sessions (
  id INT AUTO_INCREMENT PRIMARY KEY,
  session_id VARCHAR(100) NOT NULL UNIQUE,
  user_id INT NULL,
  instance_id INT NULL,
  title VARCHAR(255) NULL,
  last_trace_id VARCHAR(100) NULL,
  started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  last_activity_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_chat_sessions_user_id (user_id),
  INDEX idx_chat_sessions_instance_id (instance_id),
  INDEX idx_chat_sessions_last_activity_at (last_activity_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

	if _, err := r.sess.SQL().Exec(query); err != nil {
		panic(fmt.Errorf("failed to ensure chat_sessions table: %w", err))
	}
}

func (r *chatSessionRepository) GetBySessionID(sessionID string) (*models.ChatSession, error) {
	var item models.ChatSession
	err := r.sess.Collection("chat_sessions").Find(db.Cond{"session_id": sessionID}).One(&item)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get chat session by session id: %w", err)
	}
	return &item, nil
}

func (r *chatSessionRepository) Save(session *models.ChatSession) error {
	now := time.Now()
	existing, err := r.GetBySessionID(session.SessionID)
	if err != nil {
		return err
	}

	if existing == nil {
		if session.StartedAt.IsZero() {
			session.StartedAt = now
		}
		if session.LastActivityAt.IsZero() {
			session.LastActivityAt = now
		}
		res, err := r.sess.Collection("chat_sessions").Insert(session)
		if err != nil {
			return fmt.Errorf("failed to create chat session: %w", err)
		}
		session.ID = int(res.ID().(int64))
		return nil
	}

	existing.UserID = session.UserID
	existing.InstanceID = session.InstanceID
	if session.Title != nil {
		existing.Title = session.Title
	}
	existing.LastTraceID = session.LastTraceID
	existing.LastActivityAt = now
	if err := r.sess.Collection("chat_sessions").Find(db.Cond{"id": existing.ID}).Update(existing); err != nil {
		return fmt.Errorf("failed to update chat session: %w", err)
	}
	session.ID = existing.ID
	session.StartedAt = existing.StartedAt
	session.LastActivityAt = existing.LastActivityAt
	return nil
}
