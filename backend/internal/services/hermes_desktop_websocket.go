package services

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ProxyWebSocket terminates both sockets so forbidden RPCs can never reach
// Hermes. Each browser frame contains one or more newline-delimited JSON-RPC
// objects, not arbitrary raw frames or a JSON batch array.
func (s *HermesDesktopService) ProxyWebSocket(ctx context.Context, c *HermesDesktopClaims, ticket string, w http.ResponseWriter, r *http.Request) error {
	return s.proxyHermesWebSocket(ctx, c, ticket, "/api/ws", nil, w, r)
}

func (s *HermesDesktopService) proxyHermesWebSocket(ctx context.Context, c *HermesDesktopClaims, ticket, path string, query url.Values, w http.ResponseWriter, r *http.Request) error {
	if err := s.redeemTicket(ctx, ticket, c); err != nil {
		return err
	}
	target, err := s.authorizeClaims(ctx, c)
	if err != nil {
		return err
	}
	dial, err := s.gatewayAuth.DialWebSocket(ctx, target.authKey(), target.url, *target.instance.AccessToken, path, query)
	if err != nil {
		return ErrHermesDesktopUpstream
	}
	upstream, cookies := dial.Conn, dial.Cookies
	defer upstream.Close()
	upgrader := websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: func(*http.Request) bool { return true }}
	// The handler performs strict Origin validation before authenticating or
	// consuming a ticket. Do not call this service from an unguarded route.
	client, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	defer client.Close()
	client.SetReadLimit(256 << 10)
	upstream.SetReadLimit(hermesDesktopMaxBody)
	var writeMu sync.Mutex
	write := func(kind int, payload []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = client.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return client.WriteMessage(kind, payload)
	}
	done := make(chan struct{}, 2)
	rpcScope := newHermesDesktopRPCScope()
	go func() {
		defer func() { done <- struct{}{} }()
		window := time.Now()
		count := 0
		for {
			kind, frame, err := client.ReadMessage()
			if err != nil {
				return
			}
			if kind != websocket.TextMessage && !(path == "/api/pty" && kind == websocket.BinaryMessage) {
				return
			}
			epoch, err := s.sessionEpoch(ctx, c.UserID)
			if err != nil || epoch != c.Epoch {
				return
			}
			if time.Since(window) >= time.Second {
				window = time.Now()
				count = 0
			}
			if path == "/api/events" {
				return
			} // Passive subscription, not an RPC channel.
			if path == "/api/pty" {
				count++
				if count > 100 {
					return
				}
				_ = upstream.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if upstream.WriteMessage(kind, frame) != nil {
					return
				}
				continue
			}
			lines := bytes.Split(bytes.TrimSpace(frame), []byte("\n"))
			count += len(lines)
			if count > 100 {
				return
			}
			for _, line := range lines {
				filtered, err := hermesDesktopFilterRPC(line)
				if err == nil {
					if !s.desktopRPCModelAllowed(ctx, target, filtered) {
						err = ErrHermesDesktopForbidden
					} else {
						err = rpcScope.admit(filtered)
					}
				}
				if err != nil {
					var request map[string]json.RawMessage
					_ = json.Unmarshal(line, &request)
					id := request["id"]
					if len(id) == 0 || len(id) > 256 {
						id = json.RawMessage("null")
					}
					code, message := -32601, "Unsupported Desktop Web operation"
					if err == errHermesDesktopUnboundSession {
						// The stock App cold-resumes only a session-not-found;
						// -32601 instead selects a legacy degraded-cache branch.
						code, message = 4001, errHermesDesktopUnboundSession.Error()
					}
					failure, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
					if write(websocket.TextMessage, failure) != nil {
						return
					}
					continue
				}
				// Catalogue validation may await an upstream HTTP read. A logout
				// during that await must win before any model/create write leaves
				// CM; the frame's earlier epoch check is no longer sufficient.
				epoch, err := s.sessionEpoch(ctx, c.UserID)
				if err != nil || epoch != c.Epoch {
					return
				}
				_ = upstream.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if upstream.WriteMessage(websocket.TextMessage, filtered) != nil {
					return
				}
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		redactor := newHermesDesktopStreamRedactor(*target.instance.AccessToken, cookies, dial.Ticket)
		for {
			kind, frame, err := upstream.ReadMessage()
			if err != nil || (kind != websocket.TextMessage && !(path == "/api/pty" && kind == websocket.BinaryMessage)) {
				return
			}
			if path == "/api/pty" {
				cleaned := redactor.Filter(frame)
				if len(cleaned) > 0 && write(kind, cleaned) != nil {
					return
				}
				continue
			}
			var cleaned [][]byte
			for _, line := range bytes.Split(bytes.TrimSpace(frame), []byte("\n")) {
				line, err = rpcScope.observe(line)
				if err != nil {
					return
				}
				safe, err := hermesDesktopSanitize(line, *target.instance.AccessToken, cookies, dial.Ticket)
				if err != nil {
					return
				}
				cleaned = append(cleaned, safe)
			}
			if write(websocket.TextMessage, bytes.Join(cleaned, []byte("\n"))) != nil {
				return
			}
		}
	}()
	// Bound lifetime to the short CM cookie and re-check ownership, active
	// account, feature gate and exact runtime generation while a socket lives.
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	expires := time.NewTimer(time.Until(c.ExpiresAt.Time))
	defer expires.Stop()
	for {
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return nil
		case <-expires.C:
			_ = upstream.Close()
			_ = client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4401, "ClawManager session ended; reconnect"), time.Now().Add(time.Second))
			return nil
		case <-ticker.C:
			if _, err := s.authorizeClaims(ctx, c); err != nil {
				_ = upstream.Close()
				_ = client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4401, "ClawManager session ended; reconnect"), time.Now().Add(time.Second))
				return nil
			}
			if client.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil || upstream.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
				return nil
			}
		}
	}
}

// PTY is a byte stream, unlike the Desktop JSON-RPC channel. Hold only a suffix
// that might be the start of a known secret, so split-frame credentials cannot
// escape while ordinary terminal output remains immediate. Pending prefixes are
// discarded on close, not flushed to the browser.
type hermesDesktopStreamRedactor struct {
	secrets [][]byte
	pending []byte
}

func newHermesDesktopStreamRedactor(password string, cookies []*http.Cookie, ticket string) *hermesDesktopStreamRedactor {
	redactor := &hermesDesktopStreamRedactor{}
	secrets := []string{password, ticket}
	for _, cookie := range cookies {
		secrets = append(secrets, cookie.Value)
	}
	for _, secret := range secrets {
		if secret != "" {
			redactor.secrets = append(redactor.secrets, []byte(secret))
		}
	}
	return redactor
}

func (r *hermesDesktopStreamRedactor) Filter(frame []byte) []byte {
	body := append(r.pending, frame...)
	for _, secret := range r.secrets {
		body = bytes.ReplaceAll(body, secret, []byte("[redacted]"))
	}
	hold := 0
	for _, secret := range r.secrets {
		for n := min(len(secret)-1, len(body)); n > hold; n-- {
			if bytes.Equal(body[len(body)-n:], secret[:n]) {
				hold = n
				break
			}
		}
	}
	end := len(body) - hold
	r.pending = bytes.Clone(body[end:])
	return body[:end]
}
