package services

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
)

var errManagedRedis = errors.New("managed Team Redis unavailable")
var managedRedisAdmission = make(chan struct{}, 64)

const managedRedisUserPrefix = "cm_rt1_"
const managedRedisMaxCommand = 1 << 20

// ManagedTeamInstanceRepository requires real context propagation to SQL.
type ManagedTeamInstanceRepository interface {
	GetByIDContext(context.Context, int) (*models.Instance, error)
}
type managedTeamRepository interface {
	GetTeamByIDContext(context.Context, int) (*models.Team, error)
	GetMemberByTeamInstanceContext(context.Context, int, int) (*models.TeamMember, error)
}

// ManagedTeamRedis keeps all upstream credentials in the backend. It does not
// authenticate HTTP callers: the entry must first verify the services capability.
type ManagedTeamRedis struct {
	instances         ManagedTeamInstanceRepository
	teams             managedTeamRepository
	current           runtimeauth.Lookup
	resolve           func(context.Context, *models.Team) (*redisBus, error)
	networkProtection func(context.Context, netip.Addr) error
	protectedTarget   func(context.Context, *redisBus) (string, error)
}

// NewManagedTeamRedis accepts an optional trusted attestation that the pinned
// Redis IP is unreachable by protected business processes. Without it, a Redis
// server with any enabled nopass user is rejected, including private IPs.
func NewManagedTeamRedis(service TeamService, instances ManagedTeamInstanceRepository, current runtimeauth.Lookup, networkProtection ...func(context.Context, netip.Addr) error) (*ManagedTeamRedis, error) {
	s, ok := service.(*teamService)
	if !ok || s == nil || instances == nil || current == nil || len(networkProtection) > 1 {
		return nil, errManagedRedis
	}
	repo, ok := s.repo.(managedTeamRepository)
	if !ok {
		return nil, errManagedRedis
	}
	m := &ManagedTeamRedis{instances: instances, teams: repo, current: current, resolve: s.redisBusForTeam}
	if len(networkProtection) == 1 {
		m.networkProtection = networkProtection[0]
	}
	return m, nil
}

func (m *ManagedTeamRedis) authorize(ctx context.Context, expected runtimeauth.Identity, teamID int) (*models.Team, error) {
	if ctx.Err() != nil || expected.InstanceID < 1 || expected.UserID < 1 || expected.Generation < 1 || expected.PodUID == "" {
		return nil, errManagedRedis
	}
	current, err := m.current(ctx, expected.InstanceID)
	if err != nil || current != expected {
		return nil, errManagedRedis
	}
	instance, err := m.instances.GetByIDContext(ctx, expected.InstanceID)
	if err != nil || instance == nil || instance.ID != expected.InstanceID || instance.UserID != expected.UserID || instance.EnvironmentOverridesJSON == nil {
		return nil, errManagedRedis
	}
	return m.authorizeTeam(ctx, instance, teamID)
}

func (m *ManagedTeamRedis) authorizeTeam(ctx context.Context, instance *models.Instance, teamID int) (*models.Team, error) {
	var env map[string]string
	if instance == nil || instance.EnvironmentOverridesJSON == nil || len(*instance.EnvironmentOverridesJSON) > 1<<20 || json.Unmarshal([]byte(*instance.EnvironmentOverridesJSON), &env) != nil {
		return nil, errManagedRedis
	}
	id, err := strconv.Atoi(env["CLAWMANAGER_TEAM_ID"])
	if err != nil || id < 1 || (teamID != 0 && id != teamID) {
		return nil, errManagedRedis
	}
	team, err := m.teams.GetTeamByIDContext(ctx, id)
	if err != nil || team == nil || team.ID != id || team.UserID != instance.UserID || team.Status != models.TeamStatusRunning {
		return nil, errManagedRedis
	}
	member, err := m.teams.GetMemberByTeamInstanceContext(ctx, id, instance.ID)
	if err != nil || member == nil || member.TeamID != id || member.UserID != instance.UserID || member.InstanceID == nil || *member.InstanceID != instance.ID {
		return nil, errManagedRedis
	}
	switch member.Status {
	case models.TeamMemberStatusIdle, models.TeamMemberStatusBusy, models.TeamMemberStatusCreating:
	default:
		return nil, errManagedRedis
	}
	return team, nil
}

type ManagedTeamRedisSession struct {
	manager         *ManagedTeamRedis
	identity        runtimeauth.Identity
	teamID          int
	bus             *redisBus
	conn            net.Conn
	reader          *bufio.Reader
	user            string
	pinned          netip.Addr
	needsProtection bool
	once            sync.Once
}

// Open finishes SQL authorization, ACL provisioning and restricted AUTH before
// callers may send HTTP 101. The returned connection never uses master auth.
func (m *ManagedTeamRedis) Open(ctx context.Context, identity runtimeauth.Identity) (session *ManagedTeamRedisSession, err error) {
	select {
	case managedRedisAdmission <- struct{}{}:
	default:
		return nil, errManagedRedis
	}
	defer func() {
		if session == nil {
			<-managedRedisAdmission
		}
		if err != nil {
			err = errManagedRedis
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	team, err := m.authorize(ctx, identity, 0)
	if err != nil {
		return nil, err
	}
	bus, err := m.resolve(ctx, team)
	if err != nil || bus == nil || bus.db < 0 {
		return nil, errManagedRedis
	}
	target := ""
	if m.protectedTarget != nil {
		target, err = m.protectedTarget(ctx, bus)
		if err != nil {
			return nil, errManagedRedis
		}
	}
	admin, err := dialManagedRedis(ctx, bus, target, bus.username, bus.password)
	if err != nil {
		return nil, err
	}
	defer admin.conn.Close()
	remote, err := netip.ParseAddrPort(admin.conn.RemoteAddr().String())
	if err != nil {
		return nil, err
	}
	// Both provisioning and restricted AUTH use the same actual destination.
	pinned := remote.Addr().Unmap()
	address := remote.String()
	// Network protection is an independent, continuously checked guarantee. If
	// absent, every enabled Redis account must require authentication, including
	// named administrators and other users that are not used by this adapter.
	networkProtected := m.networkProtection != nil && m.networkProtection(ctx, pinned) == nil
	if m.protectedTarget != nil && !networkProtected {
		return nil, errManagedRedis
	}
	if !networkProtected {
		if err = managedRedisAuthentication(ctx, admin); err != nil {
			return nil, err
		}
	}
	if err = reapManagedRedisUsers(ctx, admin); err != nil {
		return nil, err
	}
	binding := sha256.Sum256([]byte(fmt.Sprintf("%d/%d/%d/%d/%s", team.ID, identity.UserID, identity.InstanceID, identity.Generation, identity.PodUID)))
	var user string
	var restricted *redisPooledConn
	for attempt := 0; attempt < 2; attempt++ {
		nonce := make([]byte, 32)
		if _, err = rand.Read(nonce); err != nil {
			return nil, err
		}
		user = fmt.Sprintf("%s%d_%x_%s", managedRedisUserPrefix, time.Now().Unix(), binding[:12], hex.EncodeToString(nonce[:8]))
		password := hex.EncodeToString(nonce[8:])
		args := []string{"ACL", "SETUSER", user, "reset", "on", ">" + password, fmt.Sprintf("~claw:team:%d:*", team.ID), "-@all"}
		for _, command := range []string{"ping", "get", "set", "del", "expire", "incr", "hset", "hget", "hgetall", "hdel", "hlen", "sadd", "smembers", "srem", "xadd", "xgroup|create", "xreadgroup", "xread", "xack", "eval", "select", "client|setname"} {
			args = append(args, "+"+command)
		}
		if _, err = managedRedisDo(ctx, admin, args...); err != nil {
			return nil, err
		}
		restricted, err = dialManagedRedis(ctx, bus, address, user, password)
		if err == nil {
			break
		}
		_, _ = managedRedisDo(ctx, admin, "ACL", "DELUSER", user)
	}
	if err != nil {
		return nil, err
	}
	// A reaper disables only old, never-reused identities. A delayed initial AUTH
	// can lose that race; retry above provisions a fresh identity, never a fallback.
	session = &ManagedTeamRedisSession{manager: m, identity: identity, teamID: team.ID, bus: bus, conn: restricted.conn, reader: restricted.reader, user: user, pinned: pinned, needsProtection: networkProtected}
	return session, nil
}

func (s *ManagedTeamRedisSession) check(ctx context.Context) error {
	if _, err := s.manager.authorize(ctx, s.identity, s.teamID); err != nil {
		return err
	}
	if s.needsProtection {
		if s.manager.networkProtection == nil || s.manager.networkProtection(ctx, s.pinned) != nil {
			return errManagedRedis
		}
		return nil
	}
	// Authentication-only admission is not an immutable-ACL assumption. Recheck
	// all accounts on the original pinned server, within the same 2s check budget.
	admin, err := dialManagedRedis(ctx, s.bus, net.JoinHostPort(s.pinned.String(), portManagedRedis(s.conn.RemoteAddr().String())), s.bus.username, s.bus.password)
	if err != nil {
		return errManagedRedis
	}
	defer admin.conn.Close()
	return managedRedisAuthentication(ctx, admin)
}

// Relay preserves Redis's RESP2 replies and pipeline order with bounded command
// buffers. Forbidden context changes/malformed frames close the session; ordinary
// Redis errors are forwarded unchanged. No transactions or PubSub are advertised.
func (s *ManagedTeamRedisSession) Relay(ctx context.Context, downstream net.Conn, buffered io.Reader) error {
	if downstream == nil {
		return errManagedRedis
	}
	if buffered == nil {
		buffered = downstream
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer s.Close()
	closeTraffic := func() { s.conn.Close(); downstream.Close() }
	stop := context.AfterFunc(ctx, closeTraffic)
	defer stop()
	var workers sync.WaitGroup
	results := make(chan error, 2)
	workers.Add(3)
	go func() {
		defer workers.Done()
		r := bufio.NewReader(buffered)
		for {
			args, err := readManagedRedisCommand(r)
			if err != nil {
				if errors.Is(err, io.EOF) {
					if cw, ok := s.conn.(interface{ CloseWrite() error }); ok {
						_ = cw.CloseWrite()
						results <- nil
						return
					}
				}
				results <- err
				return
			}
			if err = guardManagedRedis(args, s.bus.db); err != nil {
				results <- err
				return
			}
			if err = writeRedisCommand(s.conn, args...); err != nil {
				results <- err
				return
			}
		}
	}()
	go func() { defer workers.Done(); _, err := io.Copy(downstream, s.reader); results <- err; cancel() }()
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				checkCtx, done := context.WithTimeout(ctx, 2*time.Second)
				err := s.check(checkCtx)
				done()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	first := <-results
	if first != nil {
		cancel()
	}
	<-results
	cancel()
	closeTraffic()
	workers.Wait()
	if first != nil {
		return errManagedRedis
	}
	return nil
}

// Close is idempotent. Cleanup has an independent bounded context because caller
// cancellation must still retire the restricted identity. Later Opens reap users
// orphaned by backend crashes or unavailable Redis during cleanup.
func (s *ManagedTeamRedisSession) Close() error {
	s.once.Do(func() {
		s.conn.Close()
		defer func() { <-managedRedisAdmission }()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		admin, err := dialManagedRedis(ctx, s.bus, net.JoinHostPort(s.pinned.String(), portManagedRedis(s.conn.RemoteAddr().String())), s.bus.username, s.bus.password)
		if err != nil {
			return
		}
		defer admin.conn.Close()
		_, _ = managedRedisDo(ctx, admin, "ACL", "DELUSER", s.user)
	})
	return nil
}
func portManagedRedis(address string) string { _, port, _ := net.SplitHostPort(address); return port }

func dialManagedRedis(ctx context.Context, bus *redisBus, pinned, username, password string) (*redisPooledConn, error) {
	address := bus.address
	if pinned != "" {
		address = pinned
	}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	var conn net.Conn
	var err error
	if bus.useTLS {
		host, _, e := net.SplitHostPort(bus.address)
		if e != nil {
			return nil, e
		}
		d := tls.Dialer{NetDialer: dialer, Config: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}}
		conn, err = d.DialContext(ctx, "tcp", address)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, errManagedRedis
	}
	success := false
	defer func() {
		if !success {
			conn.Close()
		}
	}()
	pooled := &redisPooledConn{conn: conn, reader: bufio.NewReader(conn)}
	if password != "" || username != "" {
		args := []string{"AUTH", password}
		if username != "" {
			args = []string{"AUTH", username, password}
		}
		if _, err = managedRedisDo(ctx, pooled, args...); err != nil {
			return nil, errManagedRedis
		}
	}
	if bus.db != 0 {
		if _, err = managedRedisDo(ctx, pooled, "SELECT", strconv.Itoa(bus.db)); err != nil {
			return nil, errManagedRedis
		}
	}
	success = true
	return pooled, nil
}
func managedRedisDo(ctx context.Context, c *redisPooledConn, args ...string) (any, error) {
	if ctx.Err() != nil {
		return nil, errManagedRedis
	}
	deadline := time.Now().Add(2 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	c.conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { c.conn.Close() })
	defer stop()
	defer c.conn.SetDeadline(time.Time{})
	if err := writeRedisCommand(c.conn, args...); err != nil {
		return nil, errManagedRedis
	}
	budget := 4 << 20
	return readManagedRedisReply(c.reader, &budget, 0)
}

// ACL LIST stays backend-only. In particular, its password hashes must never be
// logged, included in errors, or relayed. The existing administrative parser
// bounds the whole response; malformed or future unrecognized state fails shut.
func managedRedisAuthentication(ctx context.Context, admin *redisPooledConn) error {
	reply, err := managedRedisDo(ctx, admin, "ACL", "LIST")
	if err != nil {
		return errManagedRedis
	}
	rows, ok := reply.([]any)
	if !ok || len(rows) == 0 {
		return errManagedRedis
	}
	for _, raw := range rows {
		row, ok := raw.(string)
		if !ok {
			return errManagedRedis
		}
		fields := strings.Fields(row)
		if len(fields) < 3 || fields[0] != "user" {
			return errManagedRedis
		}
		on, off, nopass := false, false, false
		for _, flag := range fields[2:] {
			switch flag {
			case "on":
				on = true
			case "off":
				off = true
			case "nopass":
				nopass = true
			}
		}
		if on == off || on && nopass {
			return errManagedRedis
		}
	}
	return nil
}

func reapManagedRedisUsers(ctx context.Context, admin *redisPooledConn) error {
	reply, err := managedRedisDo(ctx, admin, "ACL", "USERS")
	if err != nil {
		return err
	}
	users, ok := reply.([]any)
	if !ok {
		return errManagedRedis
	}
	// Skip already-connected users before applying the orphan cleanup batch.
	// Otherwise many active users across replicas can starve later orphan names.
	clients, err := managedRedisDo(ctx, admin, "CLIENT", "LIST")
	if err != nil {
		return err
	}
	list, ok := clients.(string)
	if !ok {
		return errManagedRedis
	}
	connected := make(map[string]bool)
	for _, line := range strings.Split(list, "\n") {
		for _, field := range strings.Fields(line) {
			if user, ok := strings.CutPrefix(field, "user="); ok {
				connected[user] = true
			}
		}
	}
	processed := 0
	for _, raw := range users {
		user, ok := raw.(string)
		if !ok || !strings.HasPrefix(user, managedRedisUserPrefix) || connected[user] {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(user, managedRedisUserPrefix), "_")
		if len(parts) != 3 || len(parts[1]) != 24 || len(parts[2]) != 16 {
			continue
		}
		stamp, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || stamp > time.Now().Add(-time.Minute).Unix() {
			continue
		}
		if _, err = hex.DecodeString(parts[1] + parts[2]); err != nil {
			continue
		}
		if processed >= 128 {
			break
		}
		processed++
		// Off blocks all subsequent AUTH without killing already-authenticated clients.
		// The following client snapshot is now safe: nobody can become active between
		// the snapshot and DELUSER. Each identity belongs to one connection only.
		if _, err = managedRedisDo(ctx, admin, "ACL", "SETUSER", user, "off"); err != nil {
			return err
		}
		clients, err := managedRedisDo(ctx, admin, "CLIENT", "LIST")
		if err != nil {
			return err
		}
		list, ok := clients.(string)
		if !ok {
			return errManagedRedis
		}
		active := false
		for _, line := range strings.Split(list, "\n") {
			for _, field := range strings.Fields(line) {
				if field == "user="+user {
					active = true
				}
			}
		}
		if !active {
			if _, err = managedRedisDo(ctx, admin, "ACL", "DELUSER", user); err != nil {
				return err
			}
		}
	}
	return nil
}

func readManagedRedisLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadSlice('\n')
	if err != nil || len(line) > 128 || len(line) < 2 || line[len(line)-2] != '\r' {
		return "", errManagedRedis
	}
	return string(line[:len(line)-2]), nil
}
func readManagedRedisCommand(r *bufio.Reader) ([]string, error) {
	b, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	if b != '*' {
		return nil, errManagedRedis
	}
	line, err := readManagedRedisLine(r)
	if err != nil {
		return nil, err
	}
	count, err := strconv.Atoi(line)
	if err != nil || count < 1 || count > 1024 {
		return nil, errManagedRedis
	}
	args := make([]string, 0, count)
	budget := managedRedisMaxCommand
	for i := 0; i < count; i++ {
		b, err := r.ReadByte()
		if err != nil || b != '$' {
			return nil, errManagedRedis
		}
		line, err := readManagedRedisLine(r)
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(line)
		if err != nil || n < 0 || n > budget {
			return nil, errManagedRedis
		}
		budget -= n
		data := make([]byte, n+2)
		if _, err = io.ReadFull(r, data); err != nil {
			// EOF is a clean half-close only before the next array starts.
			return nil, errManagedRedis
		}
		if data[n] != '\r' || data[n+1] != '\n' {
			return nil, errManagedRedis
		}
		args = append(args, string(data[:n]))
	}
	return args, nil
}
func guardManagedRedis(args []string, db int) error {
	if len(args) == 0 {
		return errManagedRedis
	}
	switch strings.ToUpper(args[0]) {
	case "PING", "GET", "SET", "DEL", "EXPIRE", "INCR", "HSET", "HGET", "HGETALL", "HDEL", "HLEN", "SADD", "SMEMBERS", "SREM", "XADD", "XREADGROUP", "XREAD", "XACK":
		return nil
	case "CLIENT":
		if len(args) == 3 && strings.EqualFold(args[1], "SETNAME") {
			return nil
		}
	case "SELECT":
		if len(args) == 2 && args[1] == strconv.Itoa(db) {
			return nil
		}
	case "XGROUP":
		if len(args) >= 2 && strings.EqualFold(args[1], "CREATE") {
			return nil
		}
	case "EVAL":
		if len(args) >= 5 && args[1] == managedCompletionScript && args[2] == "2" {
			return nil
		}
		if len(args) == 13 && args[1] == managedDeferredScript && args[2] == "4" {
			return nil
		}
	}
	return errManagedRedis
}

// Exact strings emitted by AgentsRuntime/plugins/openclaw-redis-team/dist/index.js.
const managedCompletionScript = "local existing = redis.call('GET', KEYS[1])\nif existing then return {0, existing} end\nlocal streamId = redis.call('XADD', KEYS[2], '*', unpack(ARGV))\nredis.call('SET', KEYS[1], streamId)\nreturn {1, streamId}"
const managedDeferredScript = "local current = redis.call('HGET', KEYS[1], ARGV[1])\nif not current or current ~= ARGV[2] then return {0, ''} end\nlocal streamId = redis.call('XADD', KEYS[2], '*', 'payload', ARGV[3])\nredis.call('HSET', KEYS[3], ARGV[4], ARGV[5])\nredis.call('EXPIRE', KEYS[3], 604800)\nredis.call('XADD', KEYS[4], '*', 'payload', ARGV[6])\nredis.call('HDEL', KEYS[1], ARGV[1])\nreturn {1, streamId}"

// Bounded administrative replies only. Runtime replies stream directly and are
// never decoded into memory by the bridge.
func readManagedRedisReply(r *bufio.Reader, budget *int, depth int) (any, error) {
	if depth > 8 || *budget <= 0 {
		return nil, errManagedRedis
	}
	b, err := r.ReadByte()
	if err != nil {
		return nil, errManagedRedis
	}
	line, err := readManagedRedisLine(r)
	if err != nil {
		return nil, err
	}
	*budget -= len(line) + 3
	switch b {
	case '+':
		return line, nil
	case '-':
		return nil, errManagedRedis
	case ':':
		return strconv.ParseInt(line, 10, 64)
	case '$':
		n, e := strconv.Atoi(line)
		if e != nil || n < -1 || n > *budget {
			return nil, errManagedRedis
		}
		if n == -1 {
			return nil, nil
		}
		*budget -= n + 2
		data := make([]byte, n+2)
		if _, err = io.ReadFull(r, data); err != nil || data[n] != '\r' || data[n+1] != '\n' {
			return nil, errManagedRedis
		}
		return string(data[:n]), nil
	case '*':
		n, e := strconv.Atoi(line)
		if e != nil || n < -1 || n > 65536 || n > *budget {
			return nil, errManagedRedis
		}
		if n == -1 {
			return nil, nil
		}
		items := make([]any, 0, n)
		for i := 0; i < n; i++ {
			item, err := readManagedRedisReply(r, budget, depth+1)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	}
	return nil, errManagedRedis
}
