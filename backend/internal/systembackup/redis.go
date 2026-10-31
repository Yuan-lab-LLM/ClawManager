package systembackup

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RedisOptions struct {
	Address, Username, Password string
	Database                    int
	TLS                         *tls.Config
	MaxReplyBytes               int64
}
type RedisClient struct {
	conn   net.Conn
	reader *bufio.Reader
	max    int64
	mu     sync.Mutex
}

func ConnectRedis(ctx context.Context, o RedisOptions) (*RedisClient, error) {
	if _, _, e := net.SplitHostPort(o.Address); e != nil || o.Database < 0 || o.Database > 15 || o.MaxReplyBytes < 1024 || o.MaxReplyBytes > 64<<20 {
		return nil, errors.New("invalid Redis configuration")
	}
	dial := net.Dialer{Timeout: 5 * time.Second}
	var conn net.Conn
	var e error
	if o.TLS != nil {
		if o.TLS.InsecureSkipVerify {
			return nil, errors.New("Redis TLS verification is required")
		}
		d := tls.Dialer{NetDialer: &dial, Config: o.TLS}
		conn, e = d.DialContext(ctx, "tcp", o.Address)
	} else {
		conn, e = dial.DialContext(ctx, "tcp", o.Address)
	}
	if e != nil {
		return nil, errors.New("Redis unavailable")
	}
	c := &RedisClient{conn: conn, reader: bufio.NewReader(conn), max: o.MaxReplyBytes}
	failed := true
	defer func() {
		if failed {
			conn.Close()
		}
	}()
	if o.Password != "" {
		args := [][]byte{[]byte("AUTH")}
		if o.Username != "" {
			args = append(args, []byte(o.Username))
		}
		args = append(args, []byte(o.Password))
		if _, e = c.command(ctx, args...); e != nil {
			return nil, errors.New("Redis authentication failed")
		}
	}
	if _, e = c.command(ctx, []byte("SELECT"), []byte(strconv.Itoa(o.Database))); e != nil {
		return nil, errors.New("Redis database unavailable")
	}
	failed = false
	return c, nil
}
func (c *RedisClient) Close() error { return c.conn.Close() }
func (c *RedisClient) command(ctx context.Context, args ...[]byte) (any, error) {
	if len(args) == 0 {
		return nil, errors.New("empty Redis command")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if e := c.conn.SetDeadline(deadline); e != nil {
		return nil, e
	}
	cancelDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = c.conn.SetDeadline(time.Now()); close(cancelDone) })
	defer func() {
		if !stop() {
			<-cancelDone
		}
	}()
	var wire bytes.Buffer
	fmt.Fprintf(&wire, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&wire, "$%d\r\n", len(arg))
		wire.Write(arg)
		wire.WriteString("\r\n")
	}
	if e := writeFull(c.conn, wire.Bytes()); e != nil {
		return nil, errors.New("Redis write failed")
	}
	remaining := c.max
	value, e := readRedisReply(c.reader, &remaining, 0)
	if e != nil {
		return nil, e
	}
	return value, nil
}
func readRedisReply(r *bufio.Reader, budget *int64, depth int) (any, error) {
	if depth > 16 || *budget <= 0 {
		return nil, errors.New("Redis reply budget exceeded")
	}
	line, e := r.ReadSlice('\n')
	if e != nil {
		return nil, fmt.Errorf("Redis reply read failed: %w", e)
	}
	if len(line) < 3 || len(line) > 4096 || line[len(line)-2] != '\r' {
		return nil, errors.New("invalid Redis reply")
	}
	*budget -= int64(len(line))
	if *budget < 0 {
		return nil, errors.New("Redis reply budget exceeded")
	}
	kind := line[0]
	value := string(line[1 : len(line)-2])
	switch kind {
	case '+':
		return []byte(value), nil
	case '-':
		code := strings.SplitN(value, " ", 2)[0]
		switch code {
		case "NOPERM", "NOAUTH", "WRONGPASS", "BUSYKEY":
			return nil, fmt.Errorf("Redis command rejected: %s", code)
		}
		return nil, errors.New("Redis command rejected")
	case ':':
		n, e := strconv.ParseInt(value, 10, 64)
		if e != nil {
			return nil, errors.New("invalid Redis integer")
		}
		return n, nil
	case '$', '*':
		n, e := strconv.ParseInt(value, 10, 64)
		if e != nil || n < -1 {
			return nil, errors.New("invalid Redis size")
		}
		if n == -1 {
			return nil, nil
		}
		if kind == '$' {
			if n > *budget-2 {
				return nil, errors.New("Redis reply budget exceeded")
			}
			out := make([]byte, n+2)
			if _, e = io.ReadFull(r, out); e != nil {
				return nil, e
			}
			*budget -= n + 2
			if out[n] != '\r' || out[n+1] != '\n' {
				return nil, errors.New("invalid Redis bulk reply")
			}
			return out[:n], nil
		}
		if n > 100000 || n > *budget/3 {
			return nil, errors.New("Redis array budget exceeded")
		}
		out := make([]any, n)
		for i := range out {
			out[i], e = readRedisReply(r, budget, depth+1)
			if e != nil {
				return nil, e
			}
		}
		return out, nil
	default:
		return nil, errors.New("unsupported Redis reply")
	}
}

type RedisRecord struct {
	Kind            string `json:"kind"`
	ServerID        string `json:"server_id,omitempty"`
	Key             []byte `json:"key,omitempty"`
	Type            string `json:"type,omitempty"`
	Dump            []byte `json:"dump,omitempty"`
	ExpiresAtMillis int64  `json:"expires_at_millis,omitempty"`
	Count           int64  `json:"count,omitempty"`
	SHA256          string `json:"sha256,omitempty"`
}
type RedisBudget struct {
	MaxItems          int
	MaxBytes          int64
	MaxInventoryBytes int64
}

func (b RedisBudget) validate() error {
	if b.MaxItems < 1 || b.MaxBytes < 1 || b.MaxInventoryBytes < 1024 {
		return errors.New("Redis budget required")
	}
	return nil
}

// CaptureRedisLogical preserves binary values, stream groups/PEL and original
// expirations using Redis DUMP. It is not itself a cross-source checkpoint: the
// caller must hold the proven writer barrier and account for expiring keys.
func CaptureRedisLogical(ctx context.Context, c *RedisClient, w io.Writer, b RedisBudget) error {
	return captureRedisLogical(ctx, c, w, b, false)
}

// Team data excludes the global runtime-event log and unrelated caches. A key
// that expires before its atomic read is absent at that observation time.
func CaptureTeamDataRedisLogical(ctx context.Context, c *RedisClient, w io.Writer, b RedisBudget) error {
	return captureRedisLogical(ctx, c, w, b, true)
}
func captureRedisLogical(ctx context.Context, c *RedisClient, w io.Writer, b RedisBudget, teamData bool) error {
	if e := b.validate(); e != nil {
		return e
	}
	serverID, e := redisServerID(ctx, c)
	if e != nil {
		return e
	}
	seen := map[string]bool{}
	var keys [][]byte
	var inventory int64
	cursor := "0"
	for {
		args := [][]byte{[]byte("SCAN"), []byte(cursor), []byte("COUNT"), []byte("500")}
		if teamData {
			args = append(args, []byte("MATCH"), []byte("claw:team:*"))
		}
		reply, e := c.command(ctx, args...)
		if e != nil {
			return fmt.Errorf("Redis SCAN failed: %w", e)
		}
		parts, ok := reply.([]any)
		if !ok || len(parts) != 2 {
			return errors.New("invalid Redis scan")
		}
		next, ok := parts[0].([]byte)
		if !ok {
			return errors.New("invalid Redis cursor")
		}
		cursor = string(next)
		if _, e = strconv.ParseUint(cursor, 10, 64); e != nil {
			return errors.New("invalid Redis cursor")
		}
		items, ok := parts[1].([]any)
		if !ok {
			return errors.New("invalid Redis keys")
		}
		for _, item := range items {
			key, ok := item.([]byte)
			if !ok {
				return errors.New("invalid Redis key")
			}
			s := string(key)
			if seen[s] {
				continue
			}
			inventory += int64(len(key))*2 + 128
			if len(keys) >= b.MaxItems || inventory > b.MaxInventoryBytes {
				return errors.New("capacity_exceeded")
			}
			seen[s] = true
			keys = append(keys, key)
		}
		if cursor == "0" {
			break
		}
		if e = ctx.Err(); e != nil {
			return e
		}
	}
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare(keys[i], keys[j]) < 0 })
	encoder := json.NewEncoder(w)
	if e := encoder.Encode(RedisRecord{Kind: "redis_dump_v1", ServerID: serverID}); e != nil {
		return e
	}
	hash := sha256.New()
	var total, count int64
	// EVAL_RO makes each key's DUMP/type/expiry observation atomic. It cannot
	// prove a whole-database snapshot; that remains the runner's gate contract.
	script := []byte(`local v=redis.call('DUMP',KEYS[1]); if not v then return {} end; local t=redis.call('TYPE',KEYS[1]); local ttl=redis.call('PTTL',KEYS[1]); local clock=redis.call('TIME'); return {v,t.ok,ttl,clock[1],clock[2]}`)
	for _, key := range keys {
		reply, e := c.command(ctx, []byte("EVAL_RO"), script, []byte("1"), key)
		if e != nil {
			return fmt.Errorf("Redis key capture failed: %w", e)
		}
		values, ok := reply.([]any)
		if teamData && ok && len(values) == 0 {
			continue
		}
		if !ok || len(values) != 5 {
			return errors.New("Redis inventory changed during capture")
		}
		dump, ok := values[0].([]byte)
		if !ok {
			return errors.New("invalid Redis dump")
		}
		kind, ok := values[1].([]byte)
		if !ok {
			return errors.New("invalid Redis type")
		}
		ttl, ok := values[2].(int64)
		if !ok || ttl < -1 {
			return errors.New("invalid Redis expiry")
		}
		sec, ok := values[3].([]byte)
		if !ok {
			return errors.New("invalid Redis time")
		}
		micros, ok := values[4].([]byte)
		if !ok {
			return errors.New("invalid Redis time")
		}
		seconds, e := strconv.ParseInt(string(sec), 10, 64)
		if e != nil {
			return e
		}
		us, e := strconv.ParseInt(string(micros), 10, 64)
		if e != nil {
			return e
		}
		expires := int64(0)
		if ttl >= 0 {
			expires = seconds*1000 + us/1000 + ttl
		}
		record := RedisRecord{Kind: "key", Key: key, Type: string(kind), Dump: dump, ExpiresAtMillis: expires}
		raw, e := json.Marshal(record)
		if e != nil {
			return e
		}
		total += int64(len(raw)) + 1
		if total > b.MaxBytes {
			return errors.New("capacity_exceeded")
		}
		raw = append(raw, '\n')
		hash.Write(raw)
		if e = writeFull(w, raw); e != nil {
			return e
		}
		count++
	}
	return encoder.Encode(RedisRecord{Kind: "end", Count: count, SHA256: hex.EncodeToString(hash.Sum(nil))})
}

func ReadRedisSnapshot(r io.Reader, b RedisBudget, onKey func(RedisRecord) error) error {
	return readRedisSnapshot(r, b, nil, onKey)
}
func readRedisSnapshot(r io.Reader, b RedisBudget, onHeader func(RedisRecord) error, onKey func(RedisRecord) error) error {
	if e := b.validate(); e != nil {
		return e
	}
	scan := bufio.NewScanner(io.LimitReader(r, b.MaxBytes+1))
	scan.Buffer(make([]byte, 64<<10), maxRowBytes)
	started, ended := false, false
	hash := sha256.New()
	var count, total int64
	var previous []byte
	for scan.Scan() {
		line := scan.Bytes()
		total += int64(len(line)) + 1
		if total > b.MaxBytes || ended {
			return errors.New("Redis archive exceeds bounds")
		}
		var record RedisRecord
		if e := strictDecode(line, &record); e != nil {
			return e
		}
		if !started {
			serverID, decodeErr := hex.DecodeString(record.ServerID)
			if record.Kind != "redis_dump_v1" || decodeErr != nil || len(serverID) != 20 {
				return errors.New("invalid Redis archive")
			}
			if onHeader != nil {
				if e := onHeader(record); e != nil {
					return e
				}
			}
			started = true
			continue
		}
		switch record.Kind {
		case "key":
			if len(record.Dump) == 0 || record.ExpiresAtMillis < 0 || !strings.Contains("|string|list|set|zset|hash|stream|", "|"+record.Type+"|") || (count > 0 && bytes.Compare(previous, record.Key) >= 0) {
				return errors.New("invalid or duplicate Redis key")
			}
			previous = append(previous[:0], record.Key...)
			count++
			if count > int64(b.MaxItems) {
				return errors.New("capacity_exceeded")
			}
			raw, _ := json.Marshal(record)
			hash.Write(append(raw, '\n'))
			if onKey != nil {
				if e := onKey(record); e != nil {
					return e
				}
			}
		case "end":
			if record.Count != count || record.SHA256 != hex.EncodeToString(hash.Sum(nil)) {
				return errors.New("Redis checksum mismatch")
			}
			ended = true
		default:
			return errors.New("unknown Redis record")
		}
	}
	if e := scan.Err(); e != nil {
		return e
	}
	if !ended {
		return errors.New("incomplete Redis archive")
	}
	return nil
}

// RestoreRedisHistory uses a separate history namespace in an isolated Redis
// target. No source inbox/group key is reactivated, and no message is delivered.
// Original TTLs remain in the immutable archive; historical keys do not expire.
func RestoreRedisHistory(ctx context.Context, c *RedisClient, r io.Reader, b RedisBudget, runID string) error {
	if !publicIDPattern.MatchString(runID) || !strings.HasPrefix(runID, "sdr_") {
		return errors.New("invalid restore run")
	}
	prefix := "cm:restore-history:" + runID + ":"
	targetID, e := redisServerID(ctx, c)
	if e != nil {
		return e
	}
	size, e := c.command(ctx, []byte("DBSIZE"))
	if e != nil {
		return e
	}
	if n, ok := size.(int64); !ok || n != 0 {
		return errors.New("Redis restore target must be empty")
	}
	return readRedisSnapshot(r, b, func(header RedisRecord) error {
		if header.ServerID == targetID {
			return errors.New("Redis restore target is the source server")
		}
		return nil
	}, func(record RedisRecord) error {
		key := prefix + hex.EncodeToString(record.Key)
		_, e := c.command(ctx, []byte("RESTORE"), []byte(key), []byte("0"), record.Dump)
		if e != nil {
			return errors.New("Redis history restore refused existing or invalid key")
		}
		return nil
	})
}

func redisServerID(ctx context.Context, c *RedisClient) (string, error) {
	reply, e := c.command(ctx, []byte("INFO"), []byte("server"))
	if e != nil {
		return "", fmt.Errorf("Redis identity failed: %w", e)
	}
	raw, ok := reply.([]byte)
	if !ok {
		return "", errors.New("invalid Redis server identity")
	}
	for _, line := range strings.Split(string(raw), "\r\n") {
		if strings.HasPrefix(line, "run_id:") {
			id := strings.TrimPrefix(line, "run_id:")
			value, e := hex.DecodeString(id)
			if e == nil && len(value) == 20 {
				return id, nil
			}
		}
	}
	return "", errors.New("Redis server identity unavailable")
}
