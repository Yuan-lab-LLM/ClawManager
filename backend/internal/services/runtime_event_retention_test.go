package services

import (
	"context"
	"os"
	"sync"
	"testing"
)

// Requires an isolated disposable Redis on the test node, never the platform DB.
func TestRuntimeEventRetentionRedisCapsExistingBacklogAndPreservesTeamStream(t *testing.T) {
	port := os.Getenv("TEST_RUNTIME_EVENT_REDIS_PORT")
	if port == "" {
		t.Skip("requires disposable test-node Redis")
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			t.Fatal("localhost port required")
		}
	}
	bus, err := newRedisBus("redis://127.0.0.1:" + port + "/0")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	marker, owned, err := bus.Get(ctx, "cm:test:runtime-event-retention:isolated")
	if err != nil || !owned || marker != "cm-redis-retention-test-20261104" {
		t.Fatal("refusing to modify Redis without the disposable fixture marker")
	}
	teamKey := "team:retention-test:inbox"
	for _, key := range []string{runtimeEventStreamKey, teamKey} {
		if err = bus.Del(ctx, key); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = bus.Del(context.Background(), key) })
		_, err = bus.do(ctx, "EVAL", `for i=1,10003 do redis.call('XADD',KEYS[1],'*','number',i) end return 1`, "1", key)
		if err != nil {
			t.Fatal(err)
		}
	}
	service := NewRuntimeEventService(bus)
	if err = service.Publish(ctx, "runtime.test", map[string]int{"instance_id": 25}); err != nil {
		t.Fatal(err)
	}
	length, err := bus.do(ctx, "XLEN", runtimeEventStreamKey)
	if err != nil || length != int64(10000) {
		t.Fatalf("existing backlog not capped at 10000: %v %v", length, err)
	}
	// Four oldest entries were removed while retaining the newest event.
	oldest, err := bus.do(ctx, "XRANGE", runtimeEventStreamKey, "-", "+", "COUNT", "1")
	if err != nil {
		t.Fatal(err)
	}
	messages := parseRedisStreamEntries(oldest.([]interface{}))
	if len(messages) != 1 || messages[0].Fields["number"] != "5" {
		t.Fatalf("wrong retained boundary: %v", messages)
	}
	var workers sync.WaitGroup
	errs := make(chan error, 16)
	for n := 0; n < 16; n++ {
		workers.Add(1)
		go func(n int) { defer workers.Done(); errs <- service.Publish(ctx, "runtime.concurrent", n) }(n)
	}
	workers.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	length, err = bus.do(ctx, "XLEN", runtimeEventStreamKey)
	if err != nil || length != int64(10000) {
		t.Fatalf("concurrent writes exceeded cap: %v %v", length, err)
	}
	if _, err = bus.XAdd(ctx, teamKey, map[string]string{"task": "retained"}); err != nil {
		t.Fatal(err)
	}
	length, err = bus.do(ctx, "XLEN", teamKey)
	if err != nil || length != int64(10004) {
		t.Fatalf("unrelated Team stream was trimmed: %v %v", length, err)
	}
}
