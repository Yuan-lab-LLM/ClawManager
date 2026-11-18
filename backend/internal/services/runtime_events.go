package services

import (
	"context"
	"encoding/json"
	"time"
)

const (
	runtimeEventStreamKey       = "clawmanager:runtime-events"
	runtimeEventStreamMaxLength = 10_000
)

type RuntimeEvent struct {
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type RuntimeEventService interface {
	Publish(ctx context.Context, eventType string, payload any) error
	Read(ctx context.Context, lastID string, block time.Duration) ([]redisStreamMessage, error)
}

func NewRuntimeEventService(redis PlatformRedisClient) RuntimeEventService {
	if redis == nil {
		return noopRuntimeEventService{}
	}
	return &redisRuntimeEventService{redis: redis}
}

type redisRuntimeEventService struct {
	redis PlatformRedisClient
}

func (s *redisRuntimeEventService) Publish(ctx context.Context, eventType string, payload any) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	event := RuntimeEvent{
		Type:      eventType,
		Payload:   payloadJSON,
		CreatedAt: time.Now().UTC(),
	}
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = s.redis.XAdd(ctx, runtimeEventStreamKey, map[string]string{
		"type":       event.Type,
		"payload":    string(event.Payload),
		"created_at": event.CreatedAt.Format(time.RFC3339Nano),
		"event":      string(eventJSON),
	})
	return err
}

func (s *redisRuntimeEventService) Read(ctx context.Context, lastID string, block time.Duration) ([]redisStreamMessage, error) {
	return s.redis.XRead(ctx, runtimeEventStreamKey, lastID, block)
}

type noopRuntimeEventService struct{}

func (noopRuntimeEventService) Publish(ctx context.Context, eventType string, payload any) error {
	return nil
}

func (noopRuntimeEventService) Read(ctx context.Context, lastID string, block time.Duration) ([]redisStreamMessage, error) {
	return nil, nil
}
