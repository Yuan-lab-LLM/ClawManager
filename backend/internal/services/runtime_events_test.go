package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRuntimeEventServiceNoopWhenRedisNil(t *testing.T) {
	service := NewRuntimeEventService(nil)

	if err := service.Publish(context.Background(), "runtime.test", map[string]string{"ok": "true"}); err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}
	messages, err := service.Read(context.Background(), "0", time.Millisecond)
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if messages != nil {
		t.Fatalf("messages = %#v, want nil", messages)
	}
}

func TestRuntimeEventServicePublishesJSONFields(t *testing.T) {
	redis := &fakePlatformRedisClient{}
	service := NewRuntimeEventService(redis)

	if err := service.Publish(context.Background(), "runtime.instance.running", map[string]int{"instance_id": 17}); err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}
	if redis.xaddKey != runtimeEventStreamKey {
		t.Fatalf("XAdd key = %q", redis.xaddKey)
	}
	if redis.xaddFields["type"] != "runtime.instance.running" {
		t.Fatalf("type field = %q", redis.xaddFields["type"])
	}
	var payload map[string]int
	if err := json.Unmarshal([]byte(redis.xaddFields["payload"]), &payload); err != nil {
		t.Fatalf("payload is not json: %v", err)
	}
	if payload["instance_id"] != 17 {
		t.Fatalf("payload instance_id = %d", payload["instance_id"])
	}
	var event RuntimeEvent
	if err := json.Unmarshal([]byte(redis.xaddFields["event"]), &event); err != nil {
		t.Fatalf("event is not json: %v", err)
	}
	if event.Type != "runtime.instance.running" {
		t.Fatalf("event type = %q", event.Type)
	}
}

func TestRuntimeEventLargeNotificationKeepsRefreshIdentityWithinBudget(t *testing.T) {
	r := &fakePlatformRedisClient{}
	if err := NewRuntimeEventService(r).Publish(context.Background(), "runtime.instance.running", map[string]any{"instance_id": 17, "generation": 5, "huge": strings.Repeat("x", 100000)}); err != nil {
		t.Fatal(err)
	}
	bytes := 0
	for k, v := range r.xaddFields {
		bytes += len(k) + len(v)
	}
	if bytes > 16<<10 {
		t.Fatal("UI notification exceeded capacity", bytes)
	}
	var compact map[string]any
	if err := json.Unmarshal([]byte(r.xaddFields["payload"]), &compact); err != nil {
		t.Fatal(err)
	}
	if compact["refresh_required"] != true || compact["instance_id"] != float64(17) || compact["generation"] != float64(5) || compact["payload_sha256"] == "" {
		t.Fatal("large notification lost refresh identity", compact)
	}
}

type fakePlatformRedisClient struct {
	xaddKey    string
	xaddFields map[string]string
}

func (c *fakePlatformRedisClient) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	return true, nil
}

func (c *fakePlatformRedisClient) Del(ctx context.Context, key string) error {
	return nil
}

func (c *fakePlatformRedisClient) XAdd(ctx context.Context, key string, fields map[string]string) (string, error) {
	c.xaddKey = key
	c.xaddFields = fields
	return "1-0", nil
}

func (c *fakePlatformRedisClient) XRead(ctx context.Context, key, lastID string, block time.Duration) ([]redisStreamMessage, error) {
	return []redisStreamMessage{{ID: "1-0", Fields: map[string]string{"type": "runtime.test"}}}, nil
}
