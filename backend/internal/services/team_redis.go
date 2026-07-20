package services

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type redisStreamMessage struct {
	ID     string
	Fields map[string]string
}

type redisBus struct {
	address  string
	password string
	db       int
	useTLS   bool
	pool     *redisConnPool
}

const (
	redisPoolMaxOpen = 256
	redisPoolMaxIdle = 128
	redisDialTimeout = 5 * time.Second
)

// redisConnPool is intentionally small and shared by all redisBus values with
// identical connection settings. Team services create redisBus values while
// processing events, so opening a new TCP connection per command creates a
// connection storm under normal heartbeat and gateway-report traffic.
type redisConnPool struct {
	address  string
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
		password: password,
		db:       dbIndex,
		useTLS:   parsed.Scheme == "rediss",
	}
	bus.pool = sharedRedisConnPool(bus.address, bus.password, bus.db, bus.useTLS)
	return bus, nil
}

func (b *redisBus) XAdd(ctx context.Context, key string, fields map[string]string) (string, error) {
	args := []string{"XADD", key, "*"}
	for field, value := range fields {
		args = append(args, field, value)
	}
	reply, err := b.do(ctx, args...)
	if err != nil {
		return "", err
	}
	id, ok := reply.(string)
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

func (b *redisBus) Del(ctx context.Context, key string) error {
	_, err := b.do(ctx, "DEL", key)
	return err
}

func (b *redisBus) XRead(ctx context.Context, key, lastID string, block time.Duration) ([]redisStreamMessage, error) {
	blockMillis := int(block / time.Millisecond)
	if blockMillis <= 0 {
		blockMillis = 5000
	}
	reply, err := b.do(ctx, "XREAD", "BLOCK", strconv.Itoa(blockMillis), "STREAMS", key, lastID)
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, nil
	}
	root, ok := reply.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected redis XREAD response")
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

func sharedRedisConnPool(address, password string, db int, useTLS bool) *redisConnPool {
	key := fmt.Sprintf("%t|%s|%d|%s", useTLS, address, db, password)
	redisConnPools.Lock()
	defer redisConnPools.Unlock()
	if pool := redisConnPools.byKey[key]; pool != nil {
		return pool
	}
	pool := &redisConnPool{
		address:  address,
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
	if p.password != "" {
		if err := writeRedisCommand(conn, "AUTH", p.password); err != nil {
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

func readRedisReply(reader *bufio.Reader) (interface{}, error) {
	prefix, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	switch prefix {
	case '+':
		line, err := readRedisLine(reader)
		return line, err
	case '-':
		line, _ := readRedisLine(reader)
		return nil, fmt.Errorf("redis error: %s", line)
	case ':':
		line, err := readRedisLine(reader)
		if err != nil {
			return nil, err
		}
		return strconv.ParseInt(line, 10, 64)
	case '$':
		line, err := readRedisLine(reader)
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if size < 0 {
			return nil, nil
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(reader, buf); err != nil {
			return nil, err
		}
		return string(buf[:size]), nil
	case '*':
		line, err := readRedisLine(reader)
		if err != nil {
			return nil, err
		}
		count, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if count < 0 {
			return nil, nil
		}
		items := make([]interface{}, 0, count)
		for i := 0; i < count; i++ {
			item, err := readRedisReply(reader)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unexpected redis reply prefix %q", prefix)
	}
}

func readRedisLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}
