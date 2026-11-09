package services

import (
	"bufio"
	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagedRedisAdminNamedUser(t *testing.T)           { testManagedRedisAdminNamedUser(t, "sentinel") }
func TestManagedRedisAdminNamedUserNoPassword(t *testing.T) { testManagedRedisAdminNamedUser(t, "") }
func testManagedRedisAdminNamedUser(t *testing.T, password string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan any, 1)
	go func() {
		c, e := listener.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(time.Second))
		a, e := readRedisReply(bufio.NewReader(c))
		if e != nil {
			received <- e
			return
		}
		received <- a
		fmt.Fprint(c, "+OK\r\n")
	}()
	a, _ := newRedisBus("redis://admin:" + password + "@" + listener.Addr().String())
	b, _ := newRedisBus("redis://other:" + password + "@" + listener.Addr().String())
	if a.pool == b.pool {
		t.Error("different ACL users share an admin pool")
	}
	c, err := a.pool.dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c.conn.Close()
	if got := <-received; !reflect.DeepEqual(got, []interface{}{"AUTH", "admin", password}) {
		t.Fatalf("AUTH = %#v", got)
	}
}

func TestManagedRedisRESPBoundsAndContextGuard(t *testing.T) {
	good := [][]string{{"PING"}, {"CLIENT", "SETNAME", "native"}, {"SELECT", "3"}, {"XREADGROUP", "GROUP", "g", "c", "BLOCK", "0", "STREAMS", "claw:team:1:a", ">"}, {"EVAL", managedCompletionScript, "2", "claw:team:1:c", "claw:team:1:e", "payload", "{}"}, {"EVAL", managedDeferredScript, "4", "a", "b", "c", "d", "f", "v", "p", "a", "s", "e"}}
	for _, a := range good {
		if err := guardManagedRedis(a, 3); err != nil {
			t.Errorf("allowed %s: %v", a[0], err)
		}
	}
	bad := [][]string{{"AUTH", "default", ""}, {"RESET"}, {"HELLO", "2", "AUTH", "default", ""}, {"CLIENT", "REPLY", "OFF"}, {"SELECT", "0"}, {"SELECT", "03"}, {"SCRIPT", "LOAD", "return 1"}, {"EVALSHA", "abc", "0"}, {"EVAL", "while true do end", "0"}, {"EVAL", managedCompletionScript, "0"}, {"MULTI"}, {"SUBSCRIBE", "x"}, {"CONFIG", "GET", "*"}, {"KEYS", "*"}, {"XGROUP", "DESTROY", "a", "g"}}
	for _, a := range bad {
		if err := guardManagedRedis(a, 3); err == nil {
			t.Errorf("allowed forbidden %v", a)
		}
	}
	for _, raw := range []string{"*99999999999999999999999\r\n", "*2\r\n$4\r\nPING\r\n$1048577\r\n", "*1\r\n$-1\r\n", "*1\r\n$4\r\nPINGxx", "PING\r\n", "*1\r\n$" + strings.Repeat("1", 1024) + "\r\n"} {
		if _, err := readManagedRedisCommand(bufio.NewReader(strings.NewReader(raw))); err == nil {
			t.Errorf("accepted malformed/oversize frame")
		}
	}
	raw := "*2\r\n$4\r\nPING\r\n$3\r\nx\x00y\r\n*1\r\n$4\r\nPING\r\n"
	r := bufio.NewReader(strings.NewReader(raw))
	a, err := readManagedRedisCommand(r)
	if err != nil || !reflect.DeepEqual(a, []string{"PING", "x\x00y"}) {
		t.Fatalf("binary payload: %v %v", a, err)
	}
	a, err = readManagedRedisCommand(r)
	if err != nil || len(a) != 1 {
		t.Fatalf("pipeline lost: %v %v", a, err)
	}
}

type managedInstanceFixture struct{ instance *models.Instance }

func (f managedInstanceFixture) GetByIDContext(ctx context.Context, id int) (*models.Instance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.instance, nil
}

type managedTeamFixture struct {
	team   *models.Team
	member *models.TeamMember
}

func (f *managedTeamFixture) GetTeamByIDContext(ctx context.Context, id int) (*models.Team, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.team, nil
}
func (f *managedTeamFixture) GetMemberByTeamInstanceContext(ctx context.Context, team, id int) (*models.TeamMember, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.member, nil
}
func TestManagedRedisAuthoritativeTeam(t *testing.T) {
	env := `{"CLAWMANAGER_TEAM_ID":"12","CLAWMANAGER_TEAM_REDIS_URL":"redis://attacker"}`
	id := 7
	identity := runtimeauth.Identity{UserID: 3, InstanceID: 7, Generation: 4, PodUID: "pod"}
	inst := &models.Instance{ID: 7, UserID: 3, EnvironmentOverridesJSON: &env}
	teams := &managedTeamFixture{&models.Team{ID: 12, UserID: 3, Status: models.TeamStatusRunning}, &models.TeamMember{TeamID: 12, InstanceID: &id, UserID: 3, Status: models.TeamMemberStatusIdle}}
	manager := &ManagedTeamRedis{instances: managedInstanceFixture{inst}, teams: teams, current: func(context.Context, int) (runtimeauth.Identity, error) { return identity, nil }}
	if _, err := manager.authorize(context.Background(), identity, 0); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(){"wrong user": func() { teams.team.UserID = 9 }, "wrong member user": func() { teams.member.UserID = 8 }, "member removed": func() { teams.member.Status = models.TeamMemberStatusDeleting }, "team deleted": func() { teams.team.Status = models.TeamStatusDeleted }, "wrong instance": func() { x := 9; teams.member.InstanceID = &x }, "caller stale gen": func() { identity.Generation++ }, "no SQL team": func() { env = `{}` }}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			oldTeam, oldMember, oldEnv, oldID := *teams.team, *teams.member, env, identity
			defer func() { *teams.team, *teams.member, env, identity = oldTeam, oldMember, oldEnv, oldID }()
			expected := identity
			change()
			if _, err := manager.authorize(context.Background(), expected, 12); err == nil {
				t.Fatal("unauthorized team accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.authorize(ctx, identity, 12); err == nil {
		t.Fatal("canceled lookup accepted")
	}
}

func TestManagedRedisNativeScriptsMatch(t *testing.T) {
	source, err := os.ReadFile("../../../../AgentsRuntime/plugins/openclaw-redis-team/dist/index.js")
	if err != nil {
		t.Fatal(err)
	}
	if !managedNativeScriptsMatch(t, string(source)) {
		t.Fatal("native emitted scripts changed")
	}
	for name, mutated := range map[string]string{
		"inserted Lua":      strings.Replace(string(source), `"local existing = redis.call('GET', KEYS[1])",`, `"local existing = redis.call('GET', KEYS[1])",`+"\n"+`    "local extra = 1",`, 1),
		"unused old script": strings.Replace(string(source), "    script,\n    2,", "    \"return 0\",\n    2,", 1),
		"separator":         strings.Replace(string(source), `"return {1, streamId}",`+"\n"+`  ].join("\n");`, `"return {1, streamId}",`+"\n"+`  ].join(" ");`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if managedNativeScriptsMatch(t, mutated) {
				t.Fatal("changed actual EVAL script escaped contract check")
			}
		})
	}
}
func managedNativeScriptsMatch(t *testing.T, source string) bool {
	t.Helper()
	// Execute the actual native functions, with only their surrounding helpers and
	// Redis transport stubbed. This captures the EVAL arguments actually emitted;
	// unused old arrays, inserted statements and altered join separators cannot pass.
	const harness = `
const fs = require("node:fs"), vm = require("node:vm");
const source = fs.readFileSync(0, "utf8");
function extract(name) {
 const marker = "async function " + name + "(";
 const start = source.indexOf(marker);
 const end = source.indexOf("\n}\n", start);
 if (start < 0 || end < 0 || source.indexOf(marker, start + 1) >= 0) throw Error("native function boundary changed");
 return source.slice(start, end + 2);
}
const emitted = [];
const sandbox = {
 redis: {command: async (...args) => { if (args[0] !== "EVAL") throw Error("unexpected command"); emitted.push(args[1]); return [1, "id"]; }},
 completionKey: () => "completion", eventsKey: () => "events", eventStreamFields: () => ["payload", "{}"],
 eventFor: () => ({}), intFrom: (value, fallback) => fallback, nowIso: () => "now",
 inboxKey: () => "inbox", assignmentDispatchStateKey: () => "dispatch",
};
(async () => {
 const body = extract("xaddTerminalOnce") + "\n" + extract("dispatchDeferredAssignment") +
 "\n(async () => { await xaddTerminalOnce(redis, {}, 'id', {}); await dispatchDeferredAssignment(redis, {}, {}, 'deferred', 'field', 'expected'); })()";
 await vm.runInNewContext(body, sandbox, {timeout: 1000});
 process.stdout.write(JSON.stringify(emitted));
})().catch(() => {process.stderr.write("native script capture failed"); process.exitCode = 1;});
`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", "-e", harness)
	command.Stdin = strings.NewReader(source)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("capture native emitted Lua (Node required): %v", err)
	}
	var scripts []string
	if err = json.Unmarshal(output, &scripts); err != nil {
		t.Fatal("invalid native Lua capture")
	}
	return reflect.DeepEqual(scripts, []string{managedCompletionScript, managedDeferredScript})
}

func TestManagedRedisAdmissionAndAdministrativeBounds(t *testing.T) {
	for i := 0; i < cap(managedRedisAdmission); i++ {
		managedRedisAdmission <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(managedRedisAdmission); i++ {
			<-managedRedisAdmission
		}
	}()
	if _, err := (&ManagedTeamRedis{}).Open(context.Background(), runtimeauth.Identity{}); err == nil {
		t.Fatal("global admission ignored")
	}
	for _, raw := range []string{"$4194305\r\n", "*999999999999999999\r\n", "*65537\r\n", strings.Repeat("*1\r\n", 10) + "+OK\r\n"} {
		budget := 4 << 20
		if _, err := readManagedRedisReply(bufio.NewReader(strings.NewReader(raw)), &budget, 0); err == nil {
			t.Fatal("accepted unbounded admin reply")
		}
	}
}

func TestManagedRedisTruncatedPayloadIsNotHalfClose(t *testing.T) {
	_, err := readManagedRedisCommand(bufio.NewReader(strings.NewReader("*1\r\n$4\r\n")))
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatal("truncated command mistaken for clean half-close", err)
	}
}

func TestManagedRedisReaperDoesNotStarveOrphansBehindActiveUsers(t *testing.T) {
	admin, server := net.Pipe()
	defer admin.Close()
	old := time.Now().Add(-2 * time.Minute).Unix()
	users := make([]string, 129)
	var clients strings.Builder
	for i := range users {
		users[i] = fmt.Sprintf("%s%d_%024x_%016x", managedRedisUserPrefix, old, i+1, i+1)
		if i < 128 {
			fmt.Fprintf(&clients, "id=%d user=%s\n", i, users[i])
		}
	}
	removed := make(chan string, 129)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		reader := bufio.NewReader(server)
		for {
			args, err := readManagedRedisCommand(reader)
			if err != nil {
				return
			}
			switch {
			case len(args) == 2 && args[0] == "ACL" && args[1] == "USERS":
				fmt.Fprintf(server, "*%d\r\n", len(users))
				for _, user := range users {
					fmt.Fprintf(server, "$%d\r\n%s\r\n", len(user), user)
				}
			case len(args) == 2 && args[0] == "CLIENT":
				fmt.Fprintf(server, "$%d\r\n%s\r\n", clients.Len(), clients.String())
			case len(args) == 3 && args[0] == "ACL" && args[1] == "DELUSER":
				removed <- args[2]
				fmt.Fprint(server, ":1\r\n")
			default:
				fmt.Fprint(server, "+OK\r\n")
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := reapManagedRedisUsers(ctx, &redisPooledConn{conn: admin, reader: bufio.NewReader(admin)}); err != nil {
		t.Fatal(err)
	}
	admin.Close()
	<-done
	select {
	case name := <-removed:
		if name != users[128] {
			t.Fatal("removed active user", name)
		}
	default:
		t.Fatal("orphan after 128 active users was never reaped")
	}
}

// This wire fixture models ACL metadata changes. Real Redis enforcement remains
// covered by the separately gated Redis 7 suite.
func managedACLFixture(t *testing.T, lines []string) (*ManagedTeamRedis, runtimeauth.Identity, func([]string)) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var acl atomic.Value
	acl.Store(lines)
	var active sync.Map
	var workers sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			active.Store(c, true)
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer active.Delete(c)
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					a, e := readManagedRedisCommand(r)
					if e != nil {
						return
					}
					switch {
					case len(a) >= 2 && a[0] == "ACL" && a[1] == "LIST":
						rows := acl.Load().([]string)
						fmt.Fprintf(c, "*%d\r\n", len(rows))
						for _, row := range rows {
							fmt.Fprintf(c, "$%d\r\n%s\r\n", len(row), row)
						}
					case len(a) >= 2 && a[0] == "ACL" && a[1] == "GETUSER":
						fmt.Fprint(c, "*2\r\n$5\r\nflags\r\n*2\r\n$3\r\noff\r\n$6\r\nnopass\r\n")
					case len(a) >= 2 && a[0] == "ACL" && a[1] == "USERS":
						fmt.Fprint(c, "*0\r\n")
					case len(a) >= 2 && a[0] == "CLIENT" && a[1] == "LIST":
						fmt.Fprint(c, "$0\r\n\r\n")
					case len(a) >= 2 && a[0] == "ACL" && a[1] == "DELUSER":
						fmt.Fprint(c, ":1\r\n")
					default:
						fmt.Fprint(c, "+OK\r\n")
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		<-done
		active.Range(func(key, value any) bool { key.(net.Conn).Close(); return true })
		workers.Wait()
	})
	bus, _ := newRedisBus("redis://admin:configured-password@" + listener.Addr().String() + "/3")
	identity := runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 4, PodUID: "pod"}
	instanceID := 7
	env := `{"CLAWMANAGER_TEAM_ID":"12"}`
	manager := &ManagedTeamRedis{instances: managedInstanceFixture{&models.Instance{ID: 7, UserID: 3, EnvironmentOverridesJSON: &env}}, teams: &managedTeamFixture{&models.Team{ID: 12, UserID: 3, Status: models.TeamStatusRunning}, &models.TeamMember{TeamID: 12, InstanceID: &instanceID, UserID: 3, Status: models.TeamMemberStatusIdle}}, current: func(context.Context, int) (runtimeauth.Identity, error) { return identity, nil }, resolve: func(context.Context, *models.Team) (*redisBus, error) { return bus, nil }}
	return manager, identity, func(lines []string) { acl.Store(lines) }
}
func TestManagedRedisAllUserAuthenticationProtection(t *testing.T) {
	safe := []string{"user default off nopass ~* &* +@all", "user admin on #sentinel-hash ~* &* +@all"}
	for name, rows := range map[string][]string{
		"named admin nopass":    {safe[0], "user admin on nopass ~* &* +@all"},
		"other enabled nopass":  append(append([]string{}, safe...), "user outsider on nopass ~* &* +@all"),
		"malformed ACL listing": {"unrecognized ACL state"},
	} {
		t.Run(name, func(t *testing.T) {
			m, id, _ := managedACLFixture(t, rows)
			if session, err := m.Open(context.Background(), id); err == nil {
				session.Close()
				t.Fatal("unprotected ACL state accepted")
			}
		})
	}
	t.Run("passwords and disabled nopass accepted", func(t *testing.T) {
		m, id, _ := managedACLFixture(t, append(safe, "user disabled off nopass +@all"))
		session, err := m.Open(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		session.Close()
	})
	t.Run("authentication only rechecks live ACL changes", func(t *testing.T) {
		m, id, change := managedACLFixture(t, safe)
		session, err := m.Open(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		change(append(append([]string{}, safe...), "user outsider on nopass ~* +@all"))
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err = session.check(ctx); err == nil {
			t.Fatal("live safe-to-nopass change was ignored")
		}
	})
	t.Run("network exception pins and rechecks", func(t *testing.T) {
		m, id, _ := managedACLFixture(t, []string{safe[0], "user admin on nopass ~* +@all"})
		calls := 0
		revoke := false
		m.networkProtection = func(_ context.Context, addr netip.Addr) error {
			calls++
			if addr != netip.MustParseAddr("127.0.0.1") {
				t.Error("unpinned network attestation")
			}
			if revoke {
				return errManagedRedis
			}
			return nil
		}
		session, err := m.Open(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		if calls == 0 {
			t.Fatal("network exception never attested")
		}
		revoke = true
		if err = session.check(context.Background()); err == nil {
			t.Fatal("revoked network protection ignored")
		}
	})
}
