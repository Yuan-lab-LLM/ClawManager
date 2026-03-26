package services

import (
	"fmt"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// ChatMessageService defines message persistence operations.
type ChatMessageService interface {
	RecordMessages(traceID, sessionID string, requestID *string, userID, instanceID, invocationID *int, messages []PersistedChatMessage) error
	ListMessagesByTraceID(traceID string) ([]models.ChatMessageRecord, error)
}

// PersistedChatMessage is the normalized write shape for chat messages.
type PersistedChatMessage struct {
	Role       string
	Content    string
	SequenceNo int
}

type chatMessageService struct {
	repo repository.ChatMessageRepository
}

// NewChatMessageService creates a new chat message service.
func NewChatMessageService(repo repository.ChatMessageRepository) ChatMessageService {
	return &chatMessageService{repo: repo}
}

func (s *chatMessageService) RecordMessages(traceID, sessionID string, requestID *string, userID, instanceID, invocationID *int, messages []PersistedChatMessage) error {
	traceID = strings.TrimSpace(traceID)
	sessionID = strings.TrimSpace(sessionID)
	if traceID == "" || sessionID == "" || len(messages) == 0 {
		return nil
	}

	normalizedMessages := make([]PersistedChatMessage, 0, len(messages))
	for _, message := range messages {
		role := strings.TrimSpace(message.Role)
		content := strings.TrimSpace(message.Content)
		if role == "" || content == "" {
			continue
		}
		normalizedMessages = append(normalizedMessages, PersistedChatMessage{
			Role:    role,
			Content: content,
		})
	}
	if len(normalizedMessages) == 0 {
		return nil
	}

	existing, err := s.repo.ListByTraceID(traceID)
	if err != nil {
		return fmt.Errorf("failed to load existing chat messages: %w", err)
	}

	commonPrefix := countCommonMessagePrefix(existing, normalizedMessages)
	sequenceNo := len(existing)
	for _, message := range normalizedMessages[commonPrefix:] {
		sequenceNo++
		record := &models.ChatMessageRecord{
			TraceID:      traceID,
			SessionID:    sessionID,
			RequestID:    requestID,
			UserID:       userID,
			InstanceID:   instanceID,
			InvocationID: invocationID,
			Role:         message.Role,
			Content:      message.Content,
			SequenceNo:   sequenceNo,
		}
		if err := s.repo.Create(record); err != nil {
			return fmt.Errorf("failed to record chat message: %w", err)
		}
	}
	return nil
}

func (s *chatMessageService) ListMessagesByTraceID(traceID string) ([]models.ChatMessageRecord, error) {
	items, err := s.repo.ListByTraceID(strings.TrimSpace(traceID))
	if err != nil {
		return nil, fmt.Errorf("failed to list chat messages by trace: %w", err)
	}
	return items, nil
}

func countCommonMessagePrefix(existing []models.ChatMessageRecord, incoming []PersistedChatMessage) int {
	limit := len(existing)
	if len(incoming) < limit {
		limit = len(incoming)
	}

	matched := 0
	for matched < limit {
		if !messagesEquivalent(existing[matched], incoming[matched]) {
			break
		}
		matched++
	}
	return matched
}

func messagesEquivalent(existing models.ChatMessageRecord, incoming PersistedChatMessage) bool {
	return strings.EqualFold(strings.TrimSpace(existing.Role), strings.TrimSpace(incoming.Role)) &&
		strings.TrimSpace(existing.Content) == strings.TrimSpace(incoming.Content)
}
