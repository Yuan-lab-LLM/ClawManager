package services

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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
	if len(payloadJSON)+len(eventJSON)+len(eventType)+128 > 16<<10 {
		// UI events are refresh notifications; the authoritative status remains
		// queryable in SQL. Never retain arbitrary provider bodies in this stream.
		var original map[string]json.RawMessage
		_ = json.Unmarshal(payloadJSON, &original)
		compact := map[string]any{"refresh_required": true, "payload_sha256": fmt.Sprintf("%x", sha256.Sum256(payloadJSON))}
		for _, name := range []string{"instance_id", "user_id", "generation", "pod_uid", "team_id"} {
			if value := original[name]; len(value) > 0 && len(value) <= 256 {
				compact[name] = value
			}
		}
		payloadJSON, err = json.Marshal(compact)
		if err != nil {
			return err
		}
		event.Payload = payloadJSON
		eventJSON, err = json.Marshal(event)
		if err != nil {
			return err
		}
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
