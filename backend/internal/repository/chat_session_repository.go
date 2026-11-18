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
	ListByInstanceID(instanceID int) ([]models.ChatSession, error)
	Save(session *models.ChatSession) error
}

type chatSessionRepository struct {
	sess db.Session
}

// NewChatSessionRepository creates a new chat session repository (schema is owned by embedded migrations).
func NewChatSessionRepository(sess db.Session) ChatSessionRepository {
	repo := &chatSessionRepository{sess: sess}
	return repo
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

func (r *chatSessionRepository) ListByInstanceID(instanceID int) ([]models.ChatSession, error) {
	var items []models.ChatSession
	if err := r.sess.Collection("chat_sessions").Find(db.Cond{"instance_id": instanceID}).OrderBy("-last_activity_at").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list chat sessions by instance id: %w", err)
	}
	return items, nil
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
