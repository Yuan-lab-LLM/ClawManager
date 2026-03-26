package services

import (
	"fmt"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// ChatSessionService defines session persistence operations.
type ChatSessionService interface {
	GetSession(sessionID string) (*models.ChatSession, error)
	EnsureSession(sessionID string, userID, instanceID *int, traceID *string, title *string) (*models.ChatSession, error)
}

type chatSessionService struct {
	repo repository.ChatSessionRepository
}

// NewChatSessionService creates a new chat session service.
func NewChatSessionService(repo repository.ChatSessionRepository) ChatSessionService {
	return &chatSessionService{repo: repo}
}

func (s *chatSessionService) GetSession(sessionID string) (*models.ChatSession, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}

	session, err := s.repo.GetBySessionID(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get chat session: %w", err)
	}
	return session, nil
}

func (s *chatSessionService) EnsureSession(sessionID string, userID, instanceID *int, traceID *string, title *string) (*models.ChatSession, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}

	session := &models.ChatSession{
		SessionID:   sessionID,
		UserID:      userID,
		InstanceID:  instanceID,
		LastTraceID: traceID,
	}
	if title != nil {
		trimmed := strings.TrimSpace(*title)
		if trimmed != "" {
			session.Title = &trimmed
		}
	}

	if err := s.repo.Save(session); err != nil {
		return nil, fmt.Errorf("failed to ensure chat session: %w", err)
	}
	return session, nil
}
