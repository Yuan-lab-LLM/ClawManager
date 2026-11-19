package services

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func capacityRedisFixture(t *testing.T) (*redisBus, int) {
	t.Helper()
	raw := os.Getenv("TEST_TEAM_CONTEXT_REDIS_URL")
	if raw == "" {
		t.Skip("isolated Redis required")
	}
	b, err := newRedisBus(raw)
	if err != nil || !strings.HasPrefix(b.address, "127.0.0.1:") {
		t.Fatal("disposable loopback Redis required")
	}
	marker, ok, err := b.Get(context.Background(), "cm:test:runtime-event-retention:isolated")
	if err != nil || !ok || marker != "cm-redis-retention-test-20261104" {
		t.Fatal("disposable Redis marker required")
	}
	id := int(time.Now().UnixNano()%1000000000) + 10000
	prefix := fmt.Sprintf("claw:team:%d:", id)
	t.Cleanup(func() {
		cursor := "0"
		for {
			reply, err := b.do(context.Background(), "SCAN", cursor, "MATCH", prefix+"*", "COUNT", "200")
			if err != nil {
				return
			}
			parts := reply.([]interface{})
			cursor = parts[0].(string)
			for _, key := range parts[1].([]interface{}) {
				_, _ = b.do(context.Background(), "UNLINK", key.(string))
			}
			if cursor == "0" {
				return
			}
		}
	})
	return b, id
}

func TestReliableTeamInboxHasAdmissionBoundWithoutDiscardingHistory(t *testing.T) {
	b, id := capacityRedisFixture(t)
	key := teamInboxKey(id, "worker")
	accepted := 0
	for accepted <= 2000 {
		if _, err := b.XAdd(context.Background(), key, map[string]string{"payload": `{"event":"new_task","text":"task"}`}); err != nil {
			break
		}
		accepted++
	}
	if accepted == 0 || accepted > 2000 {
		t.Fatalf("reliable inbox must reject new work at its capacity, accepted=%d", accepted)
	}
	length, err := b.do(context.Background(), "XLEN", key)
	if err != nil || length != int64(accepted) {
		t.Fatalf("capacity enforcement discarded accepted history: %v %v", length, err)
	}
}

func TestReliableTeamRejectsOversizeMessageWithoutCreatingStream(t *testing.T) {
	b, id := capacityRedisFixture(t)
	key := teamInboxKey(id, "worker")
	if _, err := b.XAdd(context.Background(), key, map[string]string{"payload": strings.Repeat("x", 65537)}); err == nil {
		t.Fatal("oversize reliable message accepted")
	}
	length, err := b.do(context.Background(), "XLEN", key)
	if err != nil || length != int64(0) {
		t.Fatalf("rejected message changed stream: %v %v", length, err)
	}
}

func TestTeamStateRejectsUnboundedHashAndPreservesPriorValue(t *testing.T) {
	b, id := capacityRedisFixture(t)
	key := fmt.Sprintf("claw:team:%d:state", id)
	for i := 0; i < 1024; i++ {
		if _, err := b.do(context.Background(), "HSET", key, fmt.Sprint(i), "ok"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.do(context.Background(), "HSET", key, "overflow", "lost"); err == nil {
		t.Fatal("unbounded hash accepted")
	}
	if _, err := b.do(context.Background(), "HSET", key, "0", "updated"); err != nil {
		t.Fatal("replacement must remain possible", err)
	}
	if _, err := b.do(context.Background(), "SET", fmt.Sprintf("claw:team:%d:big", id), strings.Repeat("x", 65537)); err == nil {
		t.Fatal("unbounded state value accepted")
	}
}

func TestReliableReceiptSurvivesReconciliationAndCapacity(t *testing.T) {
	b, id := capacityRedisFixture(t)
	key, receipt := teamInboxKey(id, "worker"), teamCapacityPrefix(id)+"receipt:proof"
	first, err := b.xAddReliableOnce(context.Background(), key, map[string]string{"payload": "{}"}, receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.initializeTeamCapacity(context.Background(), id, true); err != nil {
		t.Fatal(err)
	}
	tracked, err := b.doRaw(context.Background(), "HGET", teamCapacityPrefix(id)+"usage", "k:"+receipt)
	if err != nil || tracked != "1" {
		t.Fatal("receipt escaped global key accounting", tracked, err)
	}
	if _, err := b.doRaw(context.Background(), "HSET", teamCapacityPrefix(id)+"usage", "bytes", "134217728"); err != nil {
		t.Fatal(err)
	}
	got, err := b.xAddReliableOnce(context.Background(), key, map[string]string{"payload": "{}"}, receipt)
	if err != nil || got != first {
		t.Fatal("retry must return original identity despite full quota", got, err)
	}
}

func TestManagedDeferredCapacityPreflightDoesNotPartiallyDispatch(t *testing.T) {
	b, id := capacityRedisFixture(t)
	ctx := context.Background()
	p := fmt.Sprintf("claw:team:%d:", id)
	if err := b.initializeTeamCapacity(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.do(ctx, "HSET", p+"deferred", "assignment", "expected"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.doRaw(ctx, "HSET", teamCapacityPrefix(id)+"usage", "b:"+p+"events", "67108864"); err != nil {
		t.Fatal(err)
	}
	s := &ManagedTeamRedisSession{bus: b, teamID: id}
	args, err := s.capacityCommand(ctx, []string{"EVAL", managedDeferredScript, "4", p + "deferred", p + "inbox:worker", p + "dispatch", p + "events", "assignment", "expected", "{}", "assignment", "state", "{}"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.doRaw(ctx, args...); err == nil {
		t.Fatal("second stream capacity was bypassed")
	}
	if n, _ := b.doRaw(ctx, "XLEN", p+"inbox:worker"); n != int64(0) {
		t.Fatal("first write survived rejected second write", n)
	}
	if v, _ := b.doRaw(ctx, "HGET", p+"deferred", "assignment"); v != "expected" {
		t.Fatal("rejected script consumed deferred work", v)
	}
}

func TestSafeTeamTrimPreservesPendingAndRejectsLateHistoricalGroup(t *testing.T) {
	b, id := capacityRedisFixture(t)
	ctx := context.Background()
	key := teamInboxKey(id, "worker")
	ids := []string{}
	for i := 0; i < 5; i++ {
		x, e := b.XAdd(ctx, key, map[string]string{"payload": "{}"})
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, x)
	}
	if _, e := b.do(ctx, "XGROUP", "CREATE", key, "required", "0", "MKSTREAM"); e != nil {
		t.Fatal(e)
	}
	if _, e := b.do(ctx, "XREADGROUP", "GROUP", "required", "consumer", "COUNT", "3", "STREAMS", key, ">"); e != nil {
		t.Fatal(e)
	}
	if _, e := b.do(ctx, "XACK", key, "required", ids[0]); e != nil {
		t.Fatal(e)
	}
	n, e := b.trimReliableTeamStream(ctx, key, "")
	if e != nil || n != 1 {
		t.Fatal("must trim only strictly before oldest pending", n, e)
	}
	rows, e := b.doRaw(ctx, "XRANGE", key, "-", "+", "COUNT", "10")
	if e != nil || len(rows.([]interface{})) != 4 {
		t.Fatal("pending or unread rows lost", rows, e)
	}
	if _, e = b.do(ctx, "XGROUP", "CREATE", key, "late", "0", "MKSTREAM"); e == nil {
		t.Fatal("late historical group admitted after trim")
	}
	if _, e = b.doRaw(ctx, "XGROUP", "CREATE", key, "unknown", "0", "MKSTREAM"); e != nil {
		t.Fatal(e)
	}
	if _, e = b.trimReliableTeamStream(ctx, key, ""); e == nil {
		t.Fatal("unregistered group failed to fence maintenance")
	}
}

func TestManagedTeamCannotMutateCapacityNamespace(t *testing.T) {
	b, id := capacityRedisFixture(t)
	s := &ManagedTeamRedisSession{bus: b, teamID: id}
	for _, args := range [][]string{{"SET", teamCapacityPrefix(id) + "usage", "forged"}, {"DEL", teamInboxKey(id, "worker")}, {"XREAD", "STREAMS", teamCapacityPrefix(id) + "receipt:secret", "0"}, {"GET", fmt.Sprintf("claw:team:%d:state", id+1)}} {
		if _, err := s.capacityCommand(context.Background(), args); err == nil {
			t.Fatalf("unsafe command accepted: %s", args[0])
		}
	}
}

func TestReliableTeamCompletionReserveRemainsAvailable(t *testing.T) {
	b, id := capacityRedisFixture(t)
	ctx := context.Background()
	key := teamInboxKey(id, "worker")
	count := 0
	for ; count < 2001; count++ {
		if _, err := b.XAdd(ctx, key, map[string]string{"payload": `{"event":"new_task"}`}); err != nil {
			break
		}
	}
	if count != 1500 {
		t.Fatal("unexpected new-work reserve", count)
	}
	if _, err := b.XAdd(ctx, key, map[string]string{"payload": `{"event":"task_failed","reason":"terminal"}`}); err != nil {
		t.Fatal("new work consumed completion reserve", err)
	}
}

func TestRuntimeUIStreamExpiresOldNotifications(t *testing.T) {
	b, _ := capacityRedisFixture(t)
	ctx := context.Background()
	if _, err := b.doRaw(ctx, "DEL", runtimeEventStreamKey); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.doRaw(ctx, "DEL", runtimeEventStreamKey) })
	if _, err := b.doRaw(ctx, "XADD", runtimeEventStreamKey, "1-0", "payload", "old"); err != nil {
		t.Fatal(err)
	}
	if err := NewRuntimeEventService(b).Publish(ctx, "runtime.instance.running", map[string]int{"instance_id": 17}); err != nil {
		t.Fatal(err)
	}
	if rows, err := b.doRaw(ctx, "XRANGE", runtimeEventStreamKey, "1-0", "1-0"); err != nil || len(rows.([]interface{})) != 0 {
		t.Fatal("notification outside24h retained", rows, err)
	}
}

func TestQuiescentTeamTrimRemovesAcknowledgedTailButPendingGhostFencesClosure(t *testing.T) {
	b, id := capacityRedisFixture(t)
	ctx := context.Background()
	key := teamInboxKey(id, "worker")
	first, err := b.XAdd(ctx, key, map[string]string{"payload": "{}"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.do(ctx, "XGROUP", "CREATE", key, "required", "0", "MKSTREAM"); err != nil {
		t.Fatal(err)
	}
	if _, err = b.do(ctx, "XREADGROUP", "GROUP", "required", "worker", "COUNT", "10", "STREAMS", key, ">"); err != nil {
		t.Fatal(err)
	}
	if _, err = b.do(ctx, "XACK", key, "required", first); err != nil {
		t.Fatal(err)
	}
	if n, err := b.trimReliableTeamStream(ctx, key, ""); err != nil || n != 1 {
		t.Fatal("fully ACKed idle tail was not safely reclaimed", n, err)
	}
	if proof, err := verifyTeamReplaySourceWindow(ctx, b, id, time.Now().Add(time.Hour)); err != nil || len(proof) != 64 {
		t.Fatal("empty delivered source failed proof", proof, err)
	}
	second, err := b.XAdd(ctx, key, map[string]string{"payload": "{}"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.do(ctx, "XREADGROUP", "GROUP", "required", "worker", "COUNT", "10", "STREAMS", key, ">"); err != nil {
		t.Fatal(err)
	}
	if _, err = b.doRaw(ctx, "XDEL", key, second); err != nil {
		t.Fatal(err)
	}
	if _, err = verifyTeamReplaySourceWindow(ctx, b, id, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("pending ghost incorrectly granted source closure")
	}
}
