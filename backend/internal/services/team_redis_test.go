package services

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRedisBusReusesPooledConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	var accepted atomic.Int32
	var active sync.Map
	var workers sync.WaitGroup
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			active.Store(conn, struct{}{})
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer active.Delete(conn)
				defer conn.Close()
				serveRedisPoolTestConnection(conn)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		active.Range(func(key, _ any) bool {
			_ = key.(net.Conn).Close()
			return true
		})
		<-acceptDone
		workers.Wait()
	})

	bus, err := newRedisBus(fmt.Sprintf("redis://%s/0", listener.Addr()))
	if err != nil {
		t.Fatalf("newRedisBus: %v", err)
	}
	if err := bus.Set(context.Background(), "first", "value", 0); err != nil {
		t.Fatalf("first Set: %v", err)
	}
	if err := bus.Set(context.Background(), "second", "value", 0); err != nil {
		t.Fatalf("second Set: %v", err)
	}
	if got := accepted.Load(); got != 1 {
		t.Fatalf("accepted connections = %d, want 1", got)
	}
}

func serveRedisPoolTestConnection(conn net.Conn) {
	reader := bufio.NewReader(conn)
	for {
		if err := readRedisPoolTestCommand(reader); err != nil {
			return
		}
		if _, err := io.WriteString(conn, "+OK\r\n"); err != nil {
			return
		}
	}
}

func readRedisPoolTestCommand(reader *bufio.Reader) error {
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasPrefix(line, "*") {
		return fmt.Errorf("invalid RESP array: %q", line)
	}
	count, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "*")))
	if err != nil {
		return err
	}
	for range count {
		lengthLine, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		if !strings.HasPrefix(lengthLine, "$") {
			return fmt.Errorf("invalid RESP bulk length: %q", lengthLine)
		}
		length, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(lengthLine, "$")))
		if err != nil {
			return err
		}
		if _, err := io.CopyN(io.Discard, reader, int64(length+2)); err != nil {
			return err
		}
	}
	return nil
}
