package systembackup

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTeamDataRedisConnectionWaitsForNewService(t *testing.T) {
	reserved, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	address := reserved.Addr().String()
	reserved.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
		listener, e := net.Listen("tcp", address)
		if e != nil {
			done <- e
			return
		}
		defer listener.Close()
		go func() { <-ctx.Done(); listener.Close() }()
		conn, e := listener.Accept()
		if e != nil {
			done <- e
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		reader := bufio.NewReader(conn)
		for i := 0; i < 2; i++ {
			budget := int64(4096)
			if _, e = readRedisReply(reader, &budget, 0); e != nil {
				done <- e
				return
			}
			if _, e = fmt.Fprint(conn, "+OK\r\n"); e != nil {
				done <- e
				return
			}
		}
		done <- nil
	}()
	client, e := connectTeamRedis(ctx, teamDataRedis{address: address, writer: "fixture-password"}, "writer")
	if e != nil {
		t.Fatal("new isolated service was not given time to become reachable", e)
	}
	defer client.Close()
	if e = <-done; e != nil {
		t.Fatal(e)
	}
}

func TestRESPRejectsUnboundedMalformedAndNestedReplies(t *testing.T) {
	for _, raw := range []string{"$99999999999\r\n", "*9999999999\r\n", "$-2\r\n", "$3\r\nabcXX", "*1\r\n*1\r\n" + strings.Repeat("*1\r\n", 20), "-NOAUTH password-secret\r\n"} {
		budget := int64(1024)
		_, e := readRedisReply(bufio.NewReader(strings.NewReader(raw)), &budget, 0)
		if e == nil || strings.Contains(e.Error(), "password-secret") {
			t.Fatal("unbounded or leaking reply", e)
		}
	}
}

func TestRedisHistoryVerifiesUnorderedBinaryHashAndSetContents(t *testing.T) {
	source := redisFixture(t, "TEST_SYSTEM_BACKUP_REDIS_PORT")
	target := redisFixture(t, "TEST_SYSTEM_BACKUP_REDIS_TARGET_PORT")
	ctx := context.Background()
	id, _ := RandomID("test_")
	run, _ := RandomID("sdr_")
	hashKey, setKey := []byte(id+":hash"), []byte(id+":set")
	t.Cleanup(func() {
		for _, key := range [][]byte{hashKey, setKey} {
			_, _ = source.command(context.Background(), []byte("DEL"), key)
			_, _ = target.command(context.Background(), []byte("DEL"), []byte("cm:restore-history:"+run+":"+hex.EncodeToString(key)))
		}
	})
	value := bytes.Repeat([]byte{0, 255, 128, 'x'}, 128)
	for i := 0; i < 100; i++ {
		field := []byte(fmt.Sprintf("field-%04d", i))
		member := append([]byte(fmt.Sprintf("member-%04d-", i)), 0, 255)
		if _, e := source.command(ctx, []byte("HSET"), hashKey, field, value); e != nil {
			t.Fatal(e)
		}
		if _, e := source.command(ctx, []byte("SADD"), setKey, member); e != nil {
			t.Fatal(e)
		}
	}
	var archive bytes.Buffer
	b := RedisBudget{100, 4 << 20, 1 << 20}
	if e := CaptureRedisLogical(ctx, source, &archive, b); e != nil {
		t.Fatal(e)
	}
	if e := RestoreRedisHistory(ctx, target, bytes.NewReader(archive.Bytes()), b, run); e != nil {
		t.Fatal(e)
	}
	if _, e := VerifyRedisHistory(ctx, target, bytes.NewReader(archive.Bytes()), b, run); e != nil {
		t.Fatal("unordered serialization reported false data loss", e)
	}
	actualHash := []byte("cm:restore-history:" + run + ":" + hex.EncodeToString(hashKey))
	actualSet := []byte("cm:restore-history:" + run + ":" + hex.EncodeToString(setKey))
	_, _ = target.command(ctx, []byte("HSET"), actualHash, []byte("field-0000"), []byte("corrupt"))
	if _, e := VerifyRedisHistory(ctx, target, bytes.NewReader(archive.Bytes()), b, run); e == nil {
		t.Fatal("hash value corruption accepted")
	}
	_, _ = target.command(ctx, []byte("HSET"), actualHash, []byte("field-0000"), value)
	_, _ = target.command(ctx, []byte("SADD"), actualSet, []byte("unexpected member"))
	if _, e := VerifyRedisHistory(ctx, target, bytes.NewReader(archive.Bytes()), b, run); e == nil {
		t.Fatal("extra set member accepted")
	}
}

func TestRedisPermissionErrorRetainsOnlySafeCode(t *testing.T) {
	b := int64(1024)
	_, e := readRedisReply(bufio.NewReader(strings.NewReader("-NOPERM private-key-and-command-detail\r\n")), &b, 0)
	if e == nil || !strings.Contains(e.Error(), "NOPERM") || strings.Contains(e.Error(), "private-key") {
		t.Fatal("permission result unavailable or detail leaked", e)
	}
}

func TestTeamDataRedisACLAllowsConnectionAndReadOnlyVerification(t *testing.T) {
	c := redisFixture(t, "TEST_SYSTEM_BACKUP_REDIS_PORT")
	id, _ := RandomID("sdr_")
	password, _ := RandomID("")
	r := teamDataRedis{admin: password, writer: password, reader: password}
	ctx := context.Background()
	for _, line := range strings.Split(teamDataRedisConfig(r, id), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "user" || (fields[1] != "writer" && fields[1] != "reader") {
			continue
		}
		role := fields[1]
		user := "test_" + id[4:16] + role
		args := [][]byte{[]byte("ACL"), []byte("SETUSER"), []byte(user)}
		for _, f := range fields[2:] {
			args = append(args, []byte(f))
		}
		if _, e := c.command(ctx, args...); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _, _ = c.command(context.Background(), []byte("ACL"), []byte("DELUSER"), []byte(user)) })
		client, e := ConnectRedis(ctx, RedisOptions{Address: "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_REDIS_PORT"), Username: user, Password: password, MaxReplyBytes: 1 << 20})
		if e != nil {
			t.Errorf("%s cannot connect with generated ACL: %v", role, e)
			continue
		}
		if role == "reader" {
			_, e = client.command(ctx, []byte("SET"), []byte("cm:restore-history:"+id+":probe"), []byte("must not write"))
			if e == nil || !strings.Contains(e.Error(), "NOPERM") {
				t.Error("read-only proof failed", e)
			}
		}
		client.Close()
	}
}
func redisFixtureDatabase(t *testing.T) int {
	t.Helper()
	raw := os.Getenv("TEST_SYSTEM_BACKUP_REDIS_DATABASE")
	if raw == "" {
		return 0
	}
	value, e := strconv.Atoi(raw)
	if e != nil || value < 0 || value > 15 {
		t.Fatal("invalid fixture Redis database")
	}
	return value
}

func redisFixture(t *testing.T, variable string) *RedisClient {
	t.Helper()
	port := os.Getenv(variable)
	if port == "" {
		t.Skip("requires disposable Redis fixtures")
	}
	c, e := ConnectRedis(context.Background(), RedisOptions{Database: redisFixtureDatabase(t), Address: "127.0.0.1:" + port, Password: os.Getenv("TEST_SYSTEM_BACKUP_REDIS_PASSWORD"), MaxReplyBytes: 8 << 20})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func TestRedisHistoryPreservesBinaryStreamsAndDoesNotReplay(t *testing.T) {
	source := redisFixture(t, "TEST_SYSTEM_BACKUP_REDIS_PORT")
	target := redisFixture(t, "TEST_SYSTEM_BACKUP_REDIS_TARGET_PORT")
	ctx := context.Background()
	for _, c := range []*RedisClient{source, target} {
		n, e := c.command(ctx, []byte("DBSIZE"))
		if e != nil || n != int64(0) {
			t.Fatal("fixture must be empty", n, e)
		}
	}
	prefix, _ := RandomID("test_")
	sourceKeys := [][]byte{[]byte(prefix + ":binary"), []byte(prefix + ":stream"), []byte(prefix + ":expiring")}
	run, _ := RandomID("sdr_")
	t.Cleanup(func() {
		for _, key := range sourceKeys {
			_, _ = source.command(ctx, []byte("DEL"), key)
			_, _ = target.command(ctx, []byte("DEL"), []byte("cm:restore-history:"+run+":"+hex.EncodeToString(key)))
		}
	})
	commands := [][][]byte{{[]byte("SET"), sourceKeys[0], {0, 255, 128, 10}}, {[]byte("XADD"), sourceKeys[1], []byte("*"), []byte("task"), []byte(`{"status":"running","partial":"retained"}`)}, {[]byte("XGROUP"), []byte("CREATE"), sourceKeys[1], []byte("workers"), []byte("0")}, {[]byte("XREADGROUP"), []byte("GROUP"), []byte("workers"), []byte("old-consumer"), []byte("STREAMS"), sourceKeys[1], []byte(">")}, {[]byte("SET"), sourceKeys[2], []byte("original-session-history"), []byte("PX"), []byte("60000")}}
	for _, cmd := range commands {
		if _, e := source.command(ctx, cmd...); e != nil {
			t.Fatal(e)
		}
	}
	budget := RedisBudget{MaxItems: 100, MaxBytes: 8 << 20, MaxInventoryBytes: 1 << 20}
	var archive bytes.Buffer
	if e := CaptureRedisLogical(ctx, source, &archive, budget); e != nil {
		t.Fatal(e)
	}
	var records []RedisRecord
	if e := ReadRedisSnapshot(bytes.NewReader(archive.Bytes()), budget, func(r RedisRecord) error { records = append(records, r); return nil }); e != nil || len(records) != 3 {
		t.Fatal(len(records), e)
	}
	if e := RestoreRedisHistory(ctx, source, bytes.NewReader(archive.Bytes()), budget, run); e == nil {
		t.Fatal("restored into source")
	}
	if e := RestoreRedisHistory(ctx, target, bytes.NewReader(archive.Bytes()), budget, run); e != nil {
		t.Fatal(e)
	}
	for _, r := range records {
		key := []byte("cm:restore-history:" + run + ":" + hex.EncodeToString(r.Key))
		actual, e := target.command(ctx, []byte("DUMP"), key)
		if e != nil || !bytes.Equal(actual.([]byte), r.Dump) {
			t.Fatal("Redis history loss", e)
		}
		exists, e := target.command(ctx, []byte("EXISTS"), r.Key)
		if e != nil || exists != int64(0) {
			t.Fatal("reactivated source key", e)
		}
	}
	pending, e := target.command(ctx, []byte("XPENDING"), []byte("cm:restore-history:"+run+":"+hex.EncodeToString(sourceKeys[1])), []byte("workers"))
	if e != nil || pending.([]any)[0] != int64(1) {
		t.Fatal("lost pending delivery history", e)
	}
	if count, e := VerifyRedisHistory(ctx, target, bytes.NewReader(archive.Bytes()), budget, run); e != nil || count != 3 {
		t.Fatal("Redis history readback failed", count, e)
	}
	if _, e := target.command(ctx, []byte("SET"), []byte("cm:restore-history:"+run+":"+hex.EncodeToString(sourceKeys[0])), []byte("tampered")); e != nil {
		t.Fatal(e)
	}
	if _, e := VerifyRedisHistory(ctx, target, bytes.NewReader(archive.Bytes()), budget, run); e == nil {
		t.Fatal("Redis history corruption accepted")
	}
	if e = ReadRedisSnapshot(bytes.NewReader(archive.Bytes()[:archive.Len()-10]), budget, nil); e == nil {
		t.Fatal("truncation accepted")
	}
}
