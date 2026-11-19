package services

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRedisReplyRejectsInvalidOrOverBudgetResponses(t *testing.T) {
	const maxBytes = 8 << 20
	bulk := func(n int) string { return fmt.Sprintf("$%d\r\n%s\r\n", n, strings.Repeat("x", n)) }
	tests := map[string]string{
		"oversized bulk":          bulk(maxBytes + 1),
		"aggregate bulk bytes":    "*2\r\n" + bulk(4<<20) + bulk(4<<20),
		"aggregate header bytes":  "*2\r\n" + bulk(4<<20) + bulk((4<<20)-20),
		"array elements":          "*100001\r\n" + strings.Repeat("$-1\r\n", 100001),
		"aggregate elements":      "*2\r\n*50000\r\n" + strings.Repeat("$-1\r\n", 50000) + "*50000\r\n" + strings.Repeat("$-1\r\n", 50000),
		"nesting depth":           strings.Repeat("*1\r\n", 17) + "+OK\r\n",
		"negative bulk length":    "$-2\r\n",
		"negative array length":   "*-2\r\n",
		"overflow bulk length":    "$999999999999999999999999999999\r\n",
		"overflow array length":   "*999999999999999999999999999999\r\n",
		"line without CRLF":       "+OK\n",
		"header without CRLF":     "$2\nOK\r\n",
		"invalid bulk terminator": "$2\r\nOKxx",
		"oversized line":          "+" + strings.Repeat("x", 8192) + "\r\n",
		"truncated bulk":          "$3\r\na",
		"truncated error line":    "-ERR incomplete",
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := readRedisReply(bufio.NewReader(strings.NewReader(raw))); err == nil {
				t.Fatal("expected invalid or over-budget reply to fail")
			}
		})
	}
}

func TestRedisReplyPreservesValidResponses(t *testing.T) {
	var nested interface{} = "OK"
	for range 16 {
		nested = []interface{}{nested}
	}
	tests := []struct {
		raw  string
		want interface{}
	}{
		{"+OK\r\n", "OK"},
		{":-42\r\n", int64(-42)},
		{"$-1\r\n", nil},
		{"*-1\r\n", nil},
		{"$0\r\n\r\n", ""},
		{"*0\r\n", []interface{}{}},
		{"*2\r\n$3\r\na\x00b\r\n*2\r\n$-1\r\n:1\r\n", []interface{}{"a\x00b", []interface{}{nil, int64(1)}}},
		{strings.Repeat("*1\r\n", 16) + "+OK\r\n", nested},
	}
	for _, tt := range tests {
		got, err := readRedisReply(bufio.NewReader(strings.NewReader(tt.raw)))
		if err != nil {
			t.Fatalf("valid response rejected: %v", err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("got %#v, want %#v", got, tt.want)
		}
	}
	// The byte budget includes the complete wire response and resets per reply.
	const size = (8 << 20) - 12
	raw := fmt.Sprintf("$%d\r\n%s\r\n+OK\r\n", size, strings.Repeat("x", size))
	reader := bufio.NewReader(strings.NewReader(raw))
	got, err := readRedisReply(reader)
	if err != nil {
		t.Fatalf("exactly 8 MiB reply failed: %v", err)
	}
	if value, ok := got.(string); !ok || len(value) != size {
		t.Fatal("exactly 8 MiB reply has unexpected type or size")
	}
	if got, err := readRedisReply(reader); err != nil || got != "OK" {
		t.Fatalf("second reply = %v, %v", got, err)
	}
}

func TestRedisReplyBoundsLineRead(t *testing.T) {
	raw := "+" + strings.Repeat("x", 1<<20) + "\r\n"
	source := strings.NewReader(raw)
	reader := bufio.NewReader(source)
	if _, err := readRedisReply(reader); err == nil {
		t.Fatal("expected line budget error")
	}
	if consumed := len(raw) - source.Len() - reader.Buffered(); consumed > 4097 {
		t.Fatalf("consumed %d bytes from oversized line, want at most 4097", consumed)
	}
}

func TestRedisBusXReadIncludesCount(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	command := make(chan interface{}, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			command <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		args, err := readRedisReply(bufio.NewReader(conn))
		if err != nil {
			command <- err
			return
		}
		command <- args
		_, _ = io.WriteString(conn, "*-1\r\n")
	}()
	bus, err := newRedisBus("redis://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if messages, err := bus.XRead(context.Background(), "reliable-events", "7-1", 250*time.Millisecond); err != nil || messages != nil {
		t.Fatalf("XRead = %v, %v", messages, err)
	}
	want := []interface{}{"XREAD", "COUNT", "100", "BLOCK", "250", "STREAMS", "reliable-events", "7-1"}
	if got := <-command; !reflect.DeepEqual(got, want) {
		t.Fatalf("XRead command = %#v, want %#v", got, want)
	}
}

func TestRedisBusDiscardsRejectedConnection(t *testing.T) {
	for name, response := range map[string]string{
		"budget":    "$8388609\r\n+STALE\r\n",
		"malformed": "$2\r\nOKxx+STALE\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			server := make(chan error, 1)
			go func() {
				for i := 0; i < 2; i++ {
					conn, err := listener.Accept()
					if err != nil {
						server <- err
						return
					}
					_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
					if err := readRedisPoolTestCommand(bufio.NewReader(conn)); err != nil {
						conn.Close()
						server <- err
						return
					}
					if i == 0 {
						_, _ = io.WriteString(conn, response)
						var b [1]byte
						_, err = conn.Read(b[:])
						conn.Close()
						if err != io.EOF {
							server <- fmt.Errorf("rejected connection was not closed: %v", err)
							return
						}
					} else {
						_, err = io.WriteString(conn, "$5\r\nfresh\r\n")
						conn.Close()
					}
				}
				server <- nil
			}()
			bus, err := newRedisBus("redis://" + listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := bus.Get(context.Background(), "reliable-events"); err == nil {
				t.Fatal("expected rejected reply error")
			} else if name == "budget" && !strings.Contains(err.Error(), "budget") {
				t.Fatalf("expected immediate budget error, got %v", err)
			}
			if len(bus.pool.open) != 0 || len(bus.pool.idle) != 0 {
				t.Fatal("rejected connection remained pooled")
			}
			if value, found, err := bus.Get(context.Background(), "reliable-events"); err != nil || !found || value != "fresh" {
				t.Fatalf("fresh connection Get = %q, %v, %v", value, found, err)
			}
			if err := <-server; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRedisBusXReadReducesCountOnlyForResponseBudget(t *testing.T) {
	for _, test := range []struct {
		name       string
		rejected   string
		validAtOne bool
		wantCounts []string
	}{
		{"aggregate budget recovers", "$8388609\r\n+STALE\r\n", true, []string{"100", "50", "25", "12", "6", "3", "1"}},
		{"single entry too large", "$8388609\r\n+STALE\r\n", false, []string{"100", "50", "25", "12", "6", "3", "1"}},
		{"malformed", "$2\r\nOKxx", false, []string{"100"}},
		{"server error", "-ERR rejected\r\n", false, []string{"100"}},
		{"truncated wire", "$3\r\na", false, []string{"100"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			type result struct {
				counts []string
				err    error
			}
			server := make(chan result, 1)
			go func() {
				var counts []string
				finish := func(err error) { server <- result{counts, err} }
				for range 8 {
					conn, err := listener.Accept()
					if err != nil {
						finish(err)
						return
					}
					_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
					raw, err := readRedisReply(bufio.NewReader(conn))
					if err != nil {
						conn.Close()
						finish(err)
						return
					}
					args, ok := raw.([]interface{})
					if !ok || len(args) != 8 || args[0] != "XREAD" || args[1] != "COUNT" || args[6] != "reliable-events" || args[7] != "7-1" {
						conn.Close()
						finish(fmt.Errorf("unexpected XREAD command: %#v", raw))
						return
					}
					count, ok := args[2].(string)
					if !ok {
						conn.Close()
						finish(fmt.Errorf("invalid COUNT: %#v", args[2]))
						return
					}
					counts = append(counts, count)
					if test.validAtOne && count == "1" {
						_, err = io.WriteString(conn, "*1\r\n*2\r\n$15\r\nreliable-events\r\n*1\r\n*2\r\n$3\r\n8-0\r\n*2\r\n$4\r\nkind\r\n$2\r\nok\r\n")
						conn.Close()
						finish(err)
						return
					}
					_, err = io.WriteString(conn, test.rejected)
					if err != nil {
						conn.Close()
						finish(err)
						return
					}
					if test.name == "truncated wire" {
						conn.Close()
						finish(nil)
						return
					}
					var next [1]byte
					_, err = conn.Read(next[:])
					conn.Close()
					if err != io.EOF {
						finish(fmt.Errorf("rejected connection reused or not closed: %v", err))
						return
					}
					if len(test.wantCounts) == 1 || count == "1" {
						finish(nil)
						return
					}
				}
				finish(fmt.Errorf("XREAD retry limit exceeded"))
			}()
			bus, err := newRedisBus("redis://" + listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			messages, err := bus.XRead(context.Background(), "reliable-events", "7-1", time.Millisecond)
			if test.validAtOne {
				want := []redisStreamMessage{{ID: "8-0", Fields: map[string]string{"kind": "ok"}}}
				if err != nil || !reflect.DeepEqual(messages, want) {
					t.Fatalf("XRead = %#v, %v; want %#v", messages, err, want)
				}
			} else {
				if err == nil || messages != nil {
					t.Fatalf("XRead = %#v, %v; want no messages and explicit error", messages, err)
				}
				if test.name == "single entry too large" && (!strings.Contains(err.Error(), "COUNT 1") || !errors.Is(err, errRedisReplyBudget)) {
					t.Fatalf("single-entry error must identify terminal COUNT 1 and preserve budget classification: %v", err)
				}
			}
			got := <-server
			if got.err != nil {
				t.Fatal(got.err)
			}
			if !reflect.DeepEqual(got.counts, test.wantCounts) {
				t.Fatalf("COUNT attempts = %v, want %v", got.counts, test.wantCounts)
			}
			if !test.validAtOne && (len(bus.pool.open) != 0 || len(bus.pool.idle) != 0) {
				t.Fatal("rejected connection remained pooled")
			}
		})
	}
}

func TestRedisReplyBudgetErrorsAreClassified(t *testing.T) {
	for _, raw := range []string{
		"$8388609\r\n", "*100001\r\n",
		strings.Repeat("*1\r\n", 17) + "+OK\r\n",
		"+" + strings.Repeat("x", 4096),
	} {
		_, err := readRedisReply(bufio.NewReader(strings.NewReader(raw)))
		if !errors.Is(err, errRedisReplyBudget) {
			t.Fatalf("expected classified budget error, got %v", err)
		}
		if !errors.Is(fmt.Errorf("wrapped: %w", err), errRedisReplyBudget) {
			t.Fatalf("wrapped budget error lost classification: %v", err)
		}
	}
	for _, raw := range []string{"$-2\r\n", "$2\r\nOKxx", "-ERR private-response\r\n", "$3\r\na"} {
		_, err := readRedisReply(bufio.NewReader(strings.NewReader(raw)))
		if err == nil || errors.Is(err, errRedisReplyBudget) {
			t.Fatalf("non-budget reply incorrectly classified: %v", err)
		}
	}
}

func TestRedisReadFailureReasonExcludesResponseDetails(t *testing.T) {
	for _, test := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("auth wrapper: %w", &redisReplyBudgetError{limit: "byte"}), "Redis response byte budget exceeded at COUNT 1; operator recovery required"},
		{fmt.Errorf("%w: private-response", errRedisReplyMalformed), "Redis response framing is invalid"},
		{fmt.Errorf("%w: private-response", errRedisServerReply), "Redis server rejected the command"},
		{io.ErrUnexpectedEOF, "Redis connection closed before a complete response"},
		{&net.DNSError{Err: "private-response", IsTimeout: true}, "Redis connection timed out"},
		{&net.DNSError{Err: "private-response"}, "Redis connection failed"},
		{errors.New("redis://user:private-response@example.invalid payload"), "Redis read failed"},
	} {
		if got := redisReadFailureReason(test.err); got != test.want {
			t.Fatalf("reason = %q, want %q", got, test.want)
		}
	}
}
