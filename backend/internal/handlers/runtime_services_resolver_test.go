package handlers

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type runtimeRedisFake struct{}

func (runtimeRedisFake) Close() error { return nil }
func (runtimeRedisFake) Relay(ctx context.Context, conn net.Conn, reader io.Reader) error {
	_, err := io.Copy(conn, reader)
	return err
}

func TestRuntimeRedisUpgradeRequiresExactProtocol(t *testing.T) {
	for _, protocol := range []string{"", "websocket", "clawmanager-redis"} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/services/team/redis", nil)
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", protocol)
		if got := validRuntimeRedisUpgrade(r); got != (protocol == "clawmanager-redis") {
			t.Fatal("Redis upgrade protocol boundary")
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/services/team/redis?host=attacker", strings.NewReader("untrusted"))
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "clawmanager-redis")
	if validRuntimeRedisUpgrade(r) {
		t.Fatal("Redis upgrade accepted arbitrary request data")
	}
}

func TestRuntimeRedisBufferedUpgrade(t *testing.T) {
	op := runtimeRedisOperation(runtimeRedisFake{})
	server := httptest.NewServer(http.HandlerFunc(op.Serve))
	defer server.Close()
	c, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = io.WriteString(c, "POST /api/v1/runtime/services/team/redis HTTP/1.1\r\nHost: local\r\nConnection: Upgrade\r\nUpgrade: clawmanager-redis\r\nContent-Length: 0\r\n\r\nbuffered-resp")
	reader := bufio.NewReader(c)
	response, err := http.ReadResponse(reader, nil)
	if err != nil || response.StatusCode != 101 {
		t.Fatal("Redis upgrade failed")
	}
	_ = c.(*net.TCPConn).CloseWrite()
	payload, err := io.ReadAll(reader)
	if err != nil || string(payload) != "buffered-resp" {
		t.Fatal("buffered Redis bytes or half-close lost")
	}
}
