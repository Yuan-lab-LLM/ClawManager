package services

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	errRedisReplyBudget    = errors.New("redis reply budget exceeded")
	errRedisReplyMalformed = errors.New("malformed redis reply")
	errRedisServerReply    = errors.New("redis server rejected command")
)

type redisReplyBudgetError struct{ limit string }

func (e *redisReplyBudgetError) Error() string        { return "redis reply " + e.limit + " budget exceeded" }
func (e *redisReplyBudgetError) Is(target error) bool { return target == errRedisReplyBudget }

// These fixed reasons deliberately exclude server reply text, URLs and payloads.
func redisReadFailureReason(err error) string {
	var budgetErr *redisReplyBudgetError
	var networkErr net.Error
	switch {
	case errors.As(err, &budgetErr):
		return "Redis response " + budgetErr.limit + " budget exceeded at COUNT 1; operator recovery required"
	case errors.Is(err, errRedisReplyMalformed):
		return "Redis response framing is invalid"
	case errors.Is(err, errRedisServerReply):
		return "Redis server rejected the command"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "Redis connection closed before a complete response"
	case errors.As(err, &networkErr):
		if networkErr.Timeout() {
			return "Redis connection timed out"
		}
		return "Redis connection failed"
	default:
		return "Redis read failed"
	}
}

type redisStreamMessage struct {
	ID     string
	Fields map[string]string
}

type redisBus struct {
	commandHook   func(context.Context, []string) (any, bool, error)
	teamLiveGuard func(context.Context, int) error
	address       string
	username      string
	password      string
	db            int
	useTLS        bool
	pool          *redisConnPool
}

const (
	redisPoolMaxOpen = 256
	redisPoolMaxIdle = 128
	redisDialTimeout = 5 * time.Second

	redisReplyMaxBytes    = 8 << 20
	redisReplyMaxElements = 100000
	redisReplyMaxDepth    = 16
	redisReplyMaxLine     = 4096
)

// redisConnPool is intentionally small and shared by all redisBus values with
// identical connection settings. Team services create redisBus values while
// processing events, so opening a new TCP connection per command creates a
// connection storm under normal heartbeat and gateway-report traffic.
type redisConnPool struct {
	address  string
	username string
	password string
	db       int
	useTLS   bool

	idle chan *redisPooledConn
	open chan struct{}
}

type redisPooledConn struct {
	conn   net.Conn
	reader *bufio.Reader
}

var redisConnPools = struct {
	sync.Mutex
	byKey map[string]*redisConnPool
}{
	byKey: make(map[string]*redisConnPool),
}

func newRedisBus(rawURL string) (*redisBus, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("redis url is invalid: %w", err)
	}
	if parsed.Scheme != "redis" && parsed.Scheme != "rediss" {
		return nil, fmt.Errorf("redis url scheme must be redis or rediss")
	}
	address := parsed.Host
	if !strings.Contains(address, ":") {
		address += ":6379"
	}
	password, _ := parsed.User.Password()
	username := parsed.User.Username()
	dbIndex := 0
	if path := strings.Trim(parsed.Path, "/"); path != "" {
		parsedDB, err := strconv.Atoi(path)
		if err != nil {
			return nil, fmt.Errorf("redis db index is invalid: %w", err)
		}
		dbIndex = parsedDB
	}
	bus := &redisBus{
		address:  address,
		username: username,
		password: password,
		db:       dbIndex,
		useTLS:   parsed.Scheme == "rediss",
	}
	bus.pool = sharedRedisConnPool(bus.address, bus.username, bus.password, bus.db, bus.useTLS)
	return bus, nil
}

func (b *redisBus) XAdd(ctx context.Context, key string, fields map[string]string) (string, error) {
	args := []string{"XADD", key}
	// Runtime UI notifications retain only the newest events. Reliable Team
	// streams keep their existing delivery and retention semantics.
	if key == runtimeEventStreamKey {
		args = append(args, "MAXLEN", "=", strconv.Itoa(runtimeEventStreamMaxLength))
	}
	args = append(args, "*")
	for field, value := range fields {
		args = append(args, field, value)
	}
	reply, err := b.do(ctx, args...)
	if err != nil {
		return "", err
	}
	id, ok := reply.(string)
	if ok && id == "" && b.commandHook != nil {
		// A projection transaction queued this command in its durable outbox.
		// No Redis stream ID exists until the post-commit sender executes it.
		return "", nil
	}
	if !ok || strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("unexpected redis XADD response")
	}
	return id, nil
}

func (b *redisBus) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		ttl = time.Second
	}
	reply, err := b.do(ctx, "SET", key, value, "NX", "PX", fmt.Sprintf("%d", ttl.Milliseconds()))
	if err != nil {
		return false, err
	}
	if reply == nil {
		return false, nil
	}
	return reply == "OK", nil
}

func (b *redisBus) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	args := []string{"SET", key, value}
	if ttl > 0 {
		args = append(args, "PX", fmt.Sprintf("%d", ttl.Milliseconds()))
	}
	_, err := b.do(ctx, args...)
	return err
}

func (b *redisBus) Get(ctx context.Context, key string) (string, bool, error) {
	reply, err := b.do(ctx, "GET", key)
	if err != nil {
		return "", false, err
	}
	if reply == nil {
		return "", false, nil
	}
	value, ok := reply.(string)
	if !ok {
		return "", false, fmt.Errorf("unexpected redis GET response")
	}
	return value, true, nil
}

func (b *redisBus) Del(ctx context.Context, key string) error {
	_, err := b.do(ctx, "DEL", key)
	return err
}

func (b *redisBus) XRead(ctx context.Context, key, lastID string, block time.Duration) ([]redisStreamMessage, error) {
	blockMillis := int(block / time.Millisecond)
	if blockMillis <= 0 {
		blockMillis = 5000
	}
	// Retry the same cursor with progressively smaller batches only when the
	// complete response exceeds a safety budget. At most seven attempts reach
	// COUNT 1; a single oversized entry remains retained and explicitly blocked.
	var reply interface{}
	for count := 100; ; count = max(1, count/2) {
		var err error
		reply, err = b.do(ctx, "XREAD", "COUNT", strconv.Itoa(count), "BLOCK", strconv.Itoa(blockMillis), "STREAMS", key, lastID)
		if err == nil {
			break
		}
		if !errors.Is(err, errRedisReplyBudget) {
			return nil, err
		}
		if count == 1 {
			return nil, fmt.Errorf("redis XREAD COUNT 1 exceeds response budget: %w", err)
		}
	}
	if reply == nil {
		return nil, nil
	}
	root, ok := reply.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%w: unexpected XREAD response", errRedisReplyMalformed)
	}
	var messages []redisStreamMessage
	for _, streamRaw := range root {
		stream, ok := streamRaw.([]interface{})
		if !ok || len(stream) != 2 {
			continue
		}
		messageList, ok := stream[1].([]interface{})
		if !ok {
			continue
		}
		for _, messageRaw := range messageList {
			message, ok := messageRaw.([]interface{})
			if !ok || len(message) != 2 {
				continue
			}
			id, ok := message[0].(string)
			if !ok {
				continue
			}
			fieldList, ok := message[1].([]interface{})
			if !ok {
				continue
			}
			fields := map[string]string{}
			for i := 0; i+1 < len(fieldList); i += 2 {
				field, okField := fieldList[i].(string)
				value, okValue := fieldList[i+1].(string)
				if okField && okValue {
					fields[field] = value
				}
			}
			messages = append(messages, redisStreamMessage{ID: id, Fields: fields})
		}
	}
	return messages, nil
}

func (b *redisBus) XRevRange(ctx context.Context, key string, count int) ([]redisStreamMessage, error) {
	if count <= 0 {
		count = 100
	}
	reply, err := b.do(ctx, "XREVRANGE", key, "+", "-", "COUNT", strconv.Itoa(count))
	if err != nil {
		return nil, err
	}
	root, ok := reply.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected redis XREVRANGE response")
	}
	return parseRedisStreamEntries(root), nil
}

func parseRedisStreamEntries(entries []interface{}) []redisStreamMessage {
	messages := make([]redisStreamMessage, 0, len(entries))
	for _, messageRaw := range entries {
		message, ok := messageRaw.([]interface{})
		if !ok || len(message) != 2 {
			continue
		}
		id, ok := message[0].(string)
		if !ok {
			continue
		}
		fieldList, ok := message[1].([]interface{})
		if !ok {
			continue
		}
		fields := map[string]string{}
		for i := 0; i+1 < len(fieldList); i += 2 {
			field, okField := fieldList[i].(string)
			value, okValue := fieldList[i+1].(string)
			if okField && okValue {
				fields[field] = value
			}
		}
		messages = append(messages, redisStreamMessage{ID: id, Fields: fields})
	}
	return messages
}

func (b *redisBus) do(ctx context.Context, args ...string) (interface{}, error) {
	if b != nil && b.commandHook != nil {
		if reply, handled, err := b.commandHook(ctx, args); handled {
			return reply, err
		}
	}
	if len(args) == 5 && strings.EqualFold(args[0], "EVAL") && args[1] == publishTeamWorkflowContextScript {
		if id, ok := teamRedisID(args[3]); ok {
			if err := b.initializeTeamCapacity(ctx, id, false); err != nil {
				return nil, err
			}
			p := teamCapacityPrefix(id)
			return b.doRaw(ctx, "EVAL", capacityGuardedScript(args[1], 1, true), "3", args[3], p+"usage", p+"lock", args[4])
		}
	}
	if len(args) > 6 && strings.EqualFold(args[0], "XADD") && args[1] == runtimeEventStreamKey {
		fieldStart := 3
		if args[2] == "MAXLEN" {
			fieldStart = 6
		}
		if len(args) < fieldStart+2 {
			return nil, fmt.Errorf("invalid runtime UI event")
		}
		bytes := 0
		for _, v := range args[fieldStart:] {
			bytes += len(v)
		}
		if bytes > 16<<10 {
			return nil, fmt.Errorf("runtime UI event exceeds 16 KiB")
		}
		out := []string{"EVAL", `local now=redis.call('TIME'); local minimum=tostring(tonumber(now[1])*1000-86400000)..'-0'; redis.call('XTRIM',KEYS[1],'MINID','=',minimum); return redis.call('XADD',KEYS[1],'MAXLEN','=',10000,'*',unpack(ARGV))`, "1", runtimeEventStreamKey}
		return b.doRaw(ctx, append(out, args[fieldStart:]...)...)
	}
	if len(args) >= 5 && strings.EqualFold(args[0], "XADD") && reliableTeamStream(args[1]) {
		if args[2] != "*" || (len(args)-3)%2 != 0 {
			return nil, fmt.Errorf("invalid reliable Team append")
		}
		fields := make(map[string]string, (len(args)-3)/2)
		for i := 3; i < len(args); i += 2 {
			fields[args[i]] = args[i+1]
		}
		return b.xAddReliableOnce(ctx, args[1], fields, "")
	}
	if teamStateCommand(args) {
		id, _ := teamRedisID(args[1])
		if err := b.initializeTeamCapacity(ctx, id, false); err != nil {
			return nil, err
		}
		return b.doRaw(ctx, teamStateArgs(args)...)
	}
	if transformed, ok := teamGroupArgs(args); ok {
		id, _ := teamRedisID(args[2])
		if err := b.initializeTeamCapacity(ctx, id, false); err != nil {
			return nil, err
		}
		return b.doRaw(ctx, transformed...)
	}
	return b.doRaw(ctx, args...)
}

func (b *redisBus) doRaw(ctx context.Context, args ...string) (interface{}, error) {
	if b == nil || b.pool == nil {
		return nil, fmt.Errorf("redis bus is not initialized")
	}
	pooled, err := b.pool.acquire(ctx)
	if err != nil {
		return nil, err
	}
	reusable := false
	defer func() {
		b.pool.release(pooled, reusable)
	}()

	if err := writeRedisCommand(pooled.conn, args...); err != nil {
		return nil, err
	}
	reply, err := readRedisReply(pooled.reader)
	if err != nil {
		return nil, err
	}
	reusable = true
	return reply, nil
}

func sharedRedisConnPool(address, username, password string, db int, useTLS bool) *redisConnPool {
	key := fmt.Sprintf("%t|%q|%d|%q|%q", useTLS, address, db, username, password)
	redisConnPools.Lock()
	defer redisConnPools.Unlock()
	if pool := redisConnPools.byKey[key]; pool != nil {
		return pool
	}
	pool := &redisConnPool{
		address:  address,
		username: username,
		password: password,
		db:       db,
		useTLS:   useTLS,
		idle:     make(chan *redisPooledConn, redisPoolMaxIdle),
		open:     make(chan struct{}, redisPoolMaxOpen),
	}
	redisConnPools.byKey[key] = pool
	return pool
}

func (p *redisConnPool) acquire(ctx context.Context) (*redisPooledConn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case pooled := <-p.idle:
		return pooled, nil
	default:
	}

	select {
	case pooled := <-p.idle:
		return pooled, nil
	case p.open <- struct{}{}:
		pooled, err := p.dial(ctx)
		if err != nil {
			<-p.open
			return nil, err
		}
		return pooled, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *redisConnPool) release(pooled *redisPooledConn, reusable bool) {
	if pooled == nil {
		return
	}
	if !reusable {
		_ = pooled.conn.Close()
		<-p.open
		return
	}
	select {
	case p.idle <- pooled:
	default:
		_ = pooled.conn.Close()
		<-p.open
	}
}

func (p *redisConnPool) dial(ctx context.Context) (*redisPooledConn, error) {
	dialer := &net.Dialer{Timeout: redisDialTimeout}
	var conn net.Conn
	var err error
	if p.useTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", p.address, &tls.Config{MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", p.address)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to connect redis: %w", err)
	}
	reader := bufio.NewReader(conn)
	if p.password != "" || p.username != "" {
		auth := []string{"AUTH", p.password}
		if p.username != "" {
			auth = []string{"AUTH", p.username, p.password}
		}
		if err := writeRedisCommand(conn, auth...); err != nil {
			_ = conn.Close()
			return nil, err
		}
		if _, err := readRedisReply(reader); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("redis auth failed: %w", err)
		}
	}
	if p.db > 0 {
		if err := writeRedisCommand(conn, "SELECT", strconv.Itoa(p.db)); err != nil {
			_ = conn.Close()
			return nil, err
		}
		if _, err := readRedisReply(reader); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("redis select db failed: %w", err)
		}
	}
	return &redisPooledConn{conn: conn, reader: reader}, nil
}

func writeRedisCommand(conn net.Conn, args ...string) error {
	var builder strings.Builder
	builder.WriteString("*")
	builder.WriteString(strconv.Itoa(len(args)))
	builder.WriteString("\r\n")
	for _, arg := range args {
		builder.WriteString("$")
		builder.WriteString(strconv.Itoa(len(arg)))
		builder.WriteString("\r\n")
		builder.WriteString(arg)
		builder.WriteString("\r\n")
	}
	if _, err := conn.Write([]byte(builder.String())); err != nil {
		return fmt.Errorf("failed to write redis command: %w", err)
	}
	return nil
}

// One budget covers the entire response, including nested headers and payloads.
// Callers must discard the connection after any parsing error: unread bytes may
// still belong to the rejected response.
type redisReplyBudget struct {
	bytes    int
	elements int
}

func readRedisReply(reader *bufio.Reader) (interface{}, error) {
	budget := redisReplyBudget{bytes: redisReplyMaxBytes, elements: redisReplyMaxElements}
	return readRedisReplyBudget(reader, &budget, 0)
}

func readRedisReplyBudget(reader *bufio.Reader, budget *redisReplyBudget, depth int) (interface{}, error) {
	if depth > redisReplyMaxDepth {
		return nil, &redisReplyBudgetError{limit: "depth"}
	}
	if budget.bytes <= 0 {
		return nil, &redisReplyBudgetError{limit: "byte"}
	}
	prefix, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	budget.bytes--
	line, err := readRedisLine(reader, budget)
	if err != nil {
		return nil, err
	}
	switch prefix {
	case '+':
		return line, nil
	case '-':
		return nil, fmt.Errorf("%w: %s", errRedisServerReply, line)
	case ':':
		value, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid integer", errRedisReplyMalformed)
		}
		return value, nil
	case '$', '*':
		size, err := strconv.ParseInt(line, 10, 64)
		if err != nil || size < -1 {
			return nil, fmt.Errorf("%w: invalid length", errRedisReplyMalformed)
		}
		if size == -1 {
			return nil, nil
		}
		if prefix == '$' {
			if size > int64(budget.bytes)-2 {
				return nil, &redisReplyBudgetError{limit: "byte"}
			}
			buf := make([]byte, int(size)+2)
			if _, err := io.ReadFull(reader, buf); err != nil {
				return nil, err
			}
			budget.bytes -= len(buf)
			if buf[len(buf)-2] != '\r' || buf[len(buf)-1] != '\n' {
				return nil, fmt.Errorf("%w: invalid bulk CRLF", errRedisReplyMalformed)
			}
			return string(buf[:len(buf)-2]), nil
		}
		// Even an empty scalar needs at least three wire bytes. Check both
		// budgets before allocating an array from a server-provided length.
		if size > int64(budget.elements) || size > int64(budget.bytes/3) {
			return nil, &redisReplyBudgetError{limit: "element"}
		}
		budget.elements -= int(size)
		items := make([]interface{}, int(size))
		for i := range items {
			items[i], err = readRedisReplyBudget(reader, budget, depth+1)
			if err != nil {
				return nil, err
			}
		}
		return items, nil
	default:
		return nil, fmt.Errorf("%w: unexpected prefix", errRedisReplyMalformed)
	}
}

func readRedisLine(reader *bufio.Reader, budget *redisReplyBudget) (string, error) {
	line := make([]byte, 0, 32)
	for len(line) < redisReplyMaxLine {
		if budget.bytes <= 0 {
			return "", &redisReplyBudgetError{limit: "byte"}
		}
		b, err := reader.ReadByte()
		if err != nil {
			return "", err
		}
		budget.bytes--
		line = append(line, b)
		if b == '\n' {
			if len(line) < 2 || line[len(line)-2] != '\r' {
				return "", fmt.Errorf("%w: invalid line CRLF", errRedisReplyMalformed)
			}
			return string(line[:len(line)-2]), nil
		}
	}
	return "", &redisReplyBudgetError{limit: "line"}
}
