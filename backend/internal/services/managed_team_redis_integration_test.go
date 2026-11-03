package services

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
)

// TEST_MANAGED_REDIS_DISPOSABLE=1 explicitly attests this is an isolated Redis 7
// server. The only address accepted is 127.0.0.1 and the port comes from the test
// environment, never application config. The authentication regression temporarily
// disables the disposable default user and restores its enabled state.
func TestManagedRedisReal7(t *testing.T) {
	if os.Getenv("TEST_MANAGED_REDIS_DISPOSABLE") != "1" {
		t.Skip("requires explicit disposable Redis 7 opt-in")
	}
	port, err := strconv.Atoi(os.Getenv("TEST_MANAGED_REDIS_PORT"))
	if err != nil || port < 1 || port > 65535 {
		t.Fatal("invalid isolated localhost Redis port")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	base, _ := newRedisBus(fmt.Sprintf("redis://127.0.0.1:%d/3", port))
	bootstrap, err := dialManagedRedis(ctx, base, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.conn.Close()
	do := func(c *redisPooledConn, args ...string) any {
		t.Helper()
		v, e := managedRedisDo(ctx, c, args...)
		if e != nil {
			t.Fatalf("admin %s: %v", args[0], e)
		}
		return v
	}
	info, ok := do(bootstrap, "INFO", "server").(string)
	if !ok || !strings.Contains(info, "redis_version:7.") {
		t.Fatal("requires Redis 7")
	}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	adminUser := "cm_test_admin_" + suffix
	do(bootstrap, "ACL", "SETUSER", adminUser, "reset", "on", ">test-only-sentinel", "+@all", "~*", "&*")
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 2*time.Second)
		defer done()
		_, _ = managedRedisDo(cleanup, bootstrap, "ACL", "DELUSER", adminUser)
	}()
	bus, _ := newRedisBus(fmt.Sprintf("redis://%s:test-only-sentinel@127.0.0.1:%d/3", adminUser, port))
	admin, err := dialManagedRedis(ctx, bus, "", bus.username, bus.password)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.conn.Close()
	teamID := int(time.Now().UnixNano()%1000000000) + 10000
	env := fmt.Sprintf(`{"CLAWMANAGER_TEAM_ID":"%d"}`, teamID)
	instanceID := 7
	id := runtimeauth.Identity{UserID: 3, InstanceID: instanceID, Generation: 4, PodUID: "test-pod"}
	var revoked atomic.Bool
	manager := &ManagedTeamRedis{instances: managedInstanceFixture{&models.Instance{ID: instanceID, UserID: 3, EnvironmentOverridesJSON: &env}}, teams: &managedTeamFixture{&models.Team{ID: teamID, UserID: 3, Status: models.TeamStatusRunning}, &models.TeamMember{TeamID: teamID, InstanceID: &instanceID, UserID: 3, Status: models.TeamMemberStatusIdle}}, current: func(context.Context, int) (runtimeauth.Identity, error) {
		if revoked.Load() {
			return runtimeauth.Identity{}, errManagedRedis
		}
		return id, nil
	}, resolve: func(context.Context, *models.Team) (*redisBus, error) { return bus, nil }}
	prefix := fmt.Sprintf("claw:team:%d:", teamID)
	defer func() {
		keys, err := managedRedisDo(context.Background(), admin, "KEYS", prefix+"*")
		if err == nil {
			args := []string{"DEL"}
			for _, key := range keys.([]any) {
				args = append(args, key.(string))
			}
			if len(args) > 1 {
				_, _ = managedRedisDo(context.Background(), admin, args...)
			}
		}
	}()
	t.Run("nopass requires attestation", func(t *testing.T) {
		if s, e := manager.Open(ctx, id); e == nil {
			s.Close()
			t.Fatal("unattested nopass accepted")
		}
	})
	manager.networkProtection = func(_ context.Context, ip netip.Addr) error {
		if ip != netip.MustParseAddr("127.0.0.1") {
			return errManagedRedis
		}
		return nil
	}
	open := func(t *testing.T) (*ManagedTeamRedisSession, net.Conn, *bufio.Reader, <-chan error) {
		t.Helper()
		s, e := manager.Open(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		client, server := net.Pipe()
		done := make(chan error, 1)
		go func() { done <- s.Relay(ctx, server, nil) }()
		t.Cleanup(func() {
			client.Close()
			s.Close()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("relay did not join")
			}
		})
		client.SetDeadline(time.Now().Add(12 * time.Second))
		return s, client, bufio.NewReader(client), done
	}
	command := func(t *testing.T, c net.Conn, r *bufio.Reader, args ...string) (any, error) {
		t.Helper()
		if e := writeRedisCommand(c, args...); e != nil {
			t.Fatal(e)
		}
		return readRedisReply(r)
	}
	t.Run("all user authentication and live ACL changes", func(t *testing.T) {
		protection := manager.networkProtection
		manager.networkProtection = nil
		outsider := "cm_test_nopass_" + suffix
		defer func() {
			manager.networkProtection = protection
			cleanup, done := context.WithTimeout(context.Background(), 2*time.Second)
			defer done()
			for _, args := range [][]string{
				{"ACL", "SETUSER", adminUser, "resetpass", ">test-only-sentinel"},
				{"ACL", "SETUSER", "default", "on"},
				{"ACL", "DELUSER", outsider},
			} {
				if _, err := managedRedisDo(cleanup, admin, args...); err != nil {
					t.Error("disposable ACL restoration failed")
				}
			}
		}()
		do(admin, "ACL", "SETUSER", "default", "off")
		do(admin, "ACL", "SETUSER", adminUser, "nopass")
		t.Run("named nopass admin with configured password rejected", func(t *testing.T) {
			if session, err := manager.Open(ctx, id); err == nil {
				session.Close()
				t.Fatal("named nopass admin accepted")
			}
		})
		t.Run("named nopass pinned protection revokes", func(t *testing.T) {
			manager.networkProtection = protection
			defer func() { manager.networkProtection = nil }()
			session, err := manager.Open(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			manager.networkProtection = func(context.Context, netip.Addr) error { return errManagedRedis }
			if err = session.check(ctx); err == nil {
				t.Fatal("network protection revocation ignored")
			}
		})
		do(admin, "ACL", "SETUSER", adminUser, "resetpass", ">test-only-sentinel")
		do(admin, "ACL", "SETUSER", outsider, "reset", "on", "nopass", "+@all", "~*")
		t.Run("other enabled nopass account rejected", func(t *testing.T) {
			if session, err := manager.Open(ctx, id); err == nil {
				session.Close()
				t.Fatal("other nopass account accepted")
			}
		})
		do(admin, "ACL", "SETUSER", outsider, "off")
		t.Run("disabled nopass and protected admin accepted", func(t *testing.T) {
			session, err := manager.Open(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			session.Close()
		})
		t.Run("safe to nopass closes blocked traffic", func(t *testing.T) {
			_, c, r, _ := open(t)
			if err := writeRedisCommand(c, "XREAD", "BLOCK", "0", "STREAMS", prefix+"acl-revoke", "$"); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			do(admin, "ACL", "SETUSER", outsider, "on")
			if _, err := r.ReadByte(); err == nil {
				t.Fatal("live ACL loss did not close blocked traffic")
			}
			if time.Since(started) >= 10*time.Second {
				t.Fatal("live ACL revocation exceeded ten seconds")
			}
		})
	})
	t.Run("pipeline errors same team and DB", func(t *testing.T) {
		s, c, r, _ := open(t)
		if s.bus.db != 3 {
			t.Fatal("wrong DB")
		}
		commands := [][]string{{"SELECT", "3"}, {"SET", prefix + "shared", "value"}, {"GET", "claw:team:other:secret"}, {"PING"}}
		sent := make(chan error, 1)
		go func() {
			for _, a := range commands {
				if e := writeRedisCommand(c, a...); e != nil {
					sent <- e
					return
				}
			}
			sent <- nil
		}()
		if v, e := readRedisReply(r); e != nil || v != "OK" {
			t.Fatal(v, e)
		}
		if v, e := readRedisReply(r); e != nil || v != "OK" {
			t.Fatal(v, e)
		}
		if _, e := readRedisReply(r); e == nil || !strings.Contains(e.Error(), "NOPERM") {
			t.Fatal("cross-Team key allowed", e)
		}
		if v, e := readRedisReply(r); e != nil || v != "PONG" {
			t.Fatal("pipeline lost", v, e)
		}
		if e := <-sent; e != nil {
			t.Fatal(e)
		}
		_, c2, r2, _ := open(t)
		if v, e := command(t, c2, r2, "GET", prefix+"shared"); e != nil || v != "value" {
			t.Fatal("same-Team connection failed", v, e)
		}
	})
	t.Run("known Lua atomicity", func(t *testing.T) {
		_, c, r, _ := open(t)
		args := []string{"EVAL", managedCompletionScript, "2", prefix + "completion", prefix + "events", "payload", "{}"}
		v, e := command(t, c, r, args...)
		if e != nil || v.([]interface{})[0] != int64(1) {
			t.Fatal(v, e)
		}
		v, e = command(t, c, r, args...)
		if e != nil || v.([]interface{})[0] != int64(0) {
			t.Fatal("duplicate completion", v, e)
		}
		if _, e = command(t, c, r, "HSET", prefix+"deferred", "field", "expected"); e != nil {
			t.Fatal(e)
		}
		args = []string{"EVAL", managedDeferredScript, "4", prefix + "deferred", prefix + "inbox", prefix + "dispatch", prefix + "events", "field", "expected", "payload", "assignment", "state", "event"}
		v, e = command(t, c, r, args...)
		if e != nil || v.([]interface{})[0] != int64(1) {
			t.Fatal(v, e)
		}
		v, e = command(t, c, r, args...)
		if e != nil || v.([]interface{})[0] != int64(0) {
			t.Fatal("duplicate deferred dispatch", v, e)
		}
		if v, e = command(t, c, r, "HGET", prefix+"dispatch", "assignment"); e != nil || v != "state" {
			t.Fatal(v, e)
		}
		if v, e = command(t, c, r, "HLEN", prefix+"deferred"); e != nil || v != int64(0) {
			t.Fatal(v, e)
		}
	})
	t.Run("Redis itself denies dynamic Lua cross-Team key", func(t *testing.T) {
		s, e := manager.Open(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close()
		if e = writeRedisCommand(s.conn, "EVAL", "return redis.call('GET', ARGV[1])", "0", "claw:team:other:secret"); e != nil {
			t.Fatal(e)
		}
		if _, e = readRedisReply(s.reader); e == nil {
			t.Fatal("Redis dynamic key ACL failed", e)
		}
	})
	t.Run("BLOCK and wakeup", func(t *testing.T) {
		_, c, r, _ := open(t)
		if _, e := command(t, c, r, "XGROUP", "CREATE", prefix+"blocking", "g", "0", "MKSTREAM"); e != nil {
			t.Fatal(e)
		}
		if e := writeRedisCommand(c, "XREADGROUP", "GROUP", "g", "c", "BLOCK", "0", "STREAMS", prefix+"blocking", ">"); e != nil {
			t.Fatal(e)
		}
		_, producer, pr, _ := open(t)
		if _, e := command(t, producer, pr, "XADD", prefix+"blocking", "*", "payload", "wake"); e != nil {
			t.Fatal(e)
		}
		if v, e := readRedisReply(r); e != nil || v == nil {
			t.Fatal("blocked read not woken", v, e)
		}
	})
	for _, args := range [][]string{{"AUTH", "default", ""}, {"RESET"}, {"HELLO", "2", "AUTH", "default", ""}, {"SELECT", "0"}, {"CLIENT", "REPLY", "OFF"}, {"EVAL", "return 1", "0"}, {"SCRIPT", "LOAD", "return 1"}, {"MULTI"}, {"SUBSCRIBE", "x"}} {
		t.Run("guard "+args[0], func(t *testing.T) {
			_, c, r, _ := open(t)
			if e := writeRedisCommand(c, args...); e != nil {
				t.Fatal(e)
			}
			if _, e := r.ReadByte(); e == nil {
				t.Fatal("forbidden command accepted")
			}
		})
	}
	t.Run("cleanup and crash reaper preserve active users", func(t *testing.T) {
		s, c, r, _ := open(t)
		name := s.user
		// Model an old active generation without sleeping: rename is unsupported, so
		// provision an old test identity and authenticate a real connection under it.
		old := fmt.Sprintf("%s%d_%024x_%016x", managedRedisUserPrefix, time.Now().Add(-2*time.Minute).Unix(), 1, 2)
		do(admin, "ACL", "SETUSER", old, "reset", "on", ">old-test-password", "+ping")
		active, e := dialManagedRedis(ctx, &redisBus{address: bus.address}, "", old, "old-test-password")
		if e != nil {
			t.Fatal(e)
		}
		defer active.conn.Close()
		defer func() { _, _ = managedRedisDo(context.Background(), admin, "ACL", "DELUSER", old) }()
		if e = reapManagedRedisUsers(ctx, admin); e != nil {
			t.Fatal(e)
		}
		if v, e := managedRedisDo(ctx, active, "PING"); e != nil || v != "PONG" {
			t.Fatal("active older generation killed", v, e)
		}
		active.conn.Close()
		if e = reapManagedRedisUsers(ctx, admin); e != nil {
			t.Fatal(e)
		}
		if v := do(admin, "ACL", "GETUSER", old); v != nil {
			t.Fatal("orphan user retained")
		}
		if v, e := command(t, c, r, "PING"); e != nil || v != "PONG" {
			t.Fatal("unrelated current connection killed", v, e)
		}
		s.Close()
		if v := do(admin, "ACL", "GETUSER", name); v != nil {
			t.Fatal("cleanup retained ACL user")
		}
	})
	t.Run("assignment revoke closes blocked traffic within ten seconds", func(t *testing.T) {
		_, c, r, _ := open(t)
		if e := writeRedisCommand(c, "XREAD", "BLOCK", "0", "STREAMS", prefix+"revoke", "$"); e != nil {
			t.Fatal(e)
		}
		started := time.Now()
		revoked.Store(true)
		defer revoked.Store(false)
		if _, e := r.ReadByte(); e == nil {
			t.Fatal("revoked block stayed alive")
		}
		if time.Since(started) >= 10*time.Second {
			t.Fatal("revoke exceeded ten seconds")
		}
	})
	t.Run("reconnect after cleanup", func(t *testing.T) {
		_, c, r, _ := open(t)
		if v, e := command(t, c, r, "PING"); e != nil || v != "PONG" {
			t.Fatal(v, e)
		}
	})
}
