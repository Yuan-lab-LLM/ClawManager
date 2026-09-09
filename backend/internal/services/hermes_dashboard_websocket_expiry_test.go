package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

func TestHermesDashboardWebSocketLeaseExpiryReconnectsWithNewLease(t *testing.T) {
	var upstreamConnections atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/password-login":
			dashboardTestLogin(t, w, r)
		case "/api/auth/ws-ticket":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ticket":"private-upstream-ticket"}`))
		case "/api/pty":
			conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }, Subprotocols: []string{hermesGatewayProtocol}}).Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			upstreamConnections.Add(1)
			for {
				kind, body, err := conn.ReadMessage()
				if err != nil || conn.WriteMessage(kind, body) != nil {
					return
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	s := dashboardServiceFixture(t, upstream.URL)
	first, _ := dashboardActivateClaims(t, s)
	// Match the on-wire JWT precision, with at least two seconds for the
	// authenticated handshake even when the full package is running under load.
	first.ExpiresAt = jwt.NewNumericDate(time.Now().Add(3 * time.Second))
	firstRaw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, first).SignedString(s.key)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(HermesDashboardCookieName(123))
		if err != nil {
			http.Error(w, "denied", http.StatusUnauthorized)
			return
		}
		claims, err := s.AuthenticateDashboard(r.Context(), cookie.Value, 123)
		if err != nil {
			http.Error(w, "denied", http.StatusUnauthorized)
			return
		}
		if err := s.ProxyDashboardWebSocket(r.Context(), claims, r.URL.Query().Get("ticket"), "/api/pty", r.URL.Query(), w, r); err != nil {
			http.Error(w, "denied", http.StatusUnauthorized)
		}
	}))
	defer proxy.Close()
	dial := func(claims *HermesDesktopClaims, raw string) (*websocket.Conn, error) {
		ticketURL, _, err := s.MintTicket(claims)
		if err != nil {
			return nil, err
		}
		ticketParsed, _ := url.Parse(ticketURL)
		headers := http.Header{"Cookie": {HermesDashboardCookieName(123) + "=" + raw}}
		conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/?channel=chat-123&ticket="+url.QueryEscape(ticketParsed.Query().Get("ticket")), headers)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return conn, err
	}
	oldSocket, err := dial(first, firstRaw)
	if err != nil {
		t.Fatal(err)
	}
	defer oldSocket.Close()
	// Parent renewal happens before the existing connection's old expiry.
	renewed, renewedRaw := dashboardActivateClaims(t, s)
	_ = oldSocket.SetReadDeadline(time.Now().Add(6 * time.Second))
	_, _, err = oldSocket.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseServiceRestart) {
		t.Fatalf("lease-only expiry must permit classic automatic reconnect, got %v", err)
	}
	if time.Now().Before(first.ExpiresAt.Time.Add(-time.Second)) {
		t.Fatal("socket closed before its lease expiry")
	}
	newSocket, err := dial(renewed, renewedRaw)
	if err != nil {
		t.Fatalf("new CM lease could not mint a new ticket and reconnect: %v", err)
	}
	defer newSocket.Close()
	_ = newSocket.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := newSocket.WriteMessage(websocket.BinaryMessage, []byte("input!")); err != nil {
		t.Fatal(err)
	}
	_, echo, err := newSocket.ReadMessage()
	if err != nil || string(echo) != "input!" || upstreamConnections.Load() != 2 {
		t.Fatalf("renewed PTY did not work: %v connections=%d", err, upstreamConnections.Load())
	}
	if err := s.RevokeUserSessions(context.Background(), renewed.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateDashboard(context.Background(), renewedRaw, 123); !errors.Is(err, ErrHermesDesktopUnauthorized) {
		t.Fatal("revoked lease still authenticates")
	}
	_ = newSocket.WriteMessage(websocket.BinaryMessage, []byte("must-not-forward"))
	if _, _, err := newSocket.ReadMessage(); err == nil {
		t.Fatal("revoked socket forwarded input")
	}
	if conn, err := dial(renewed, renewedRaw); err == nil {
		_ = conn.Close()
		t.Fatal("revoked lease reconnected")
	}
	if upstreamConnections.Load() != 2 {
		t.Fatal("revoked reconnection reached upstream")
	}
}

func TestHermesDashboardWebSocketExpiryCloseCodeRechecksRevocation(t *testing.T) {
	for _, mode := range []string{"expired", "not-expired", "timer-before-wall-clock-expiry", "desktop", "revoked", "timer-revoked-before-wall-clock-expiry", "owner-changed", "generation-changed", "inactive"} {
		t.Run(mode, func(t *testing.T) {
			s := dashboardServiceFixture(t, "http://127.0.0.1:9000")
			claims := &HermesDesktopClaims{Surface: "dashboard", UserID: 45, InstanceID: 123, Generation: 3, PodID: 9, Port: 9000, Epoch: "initial", RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Second))}}
			leaseExpired := true
			switch mode {
			case "not-expired":
				leaseExpired = false
				claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Minute))
			case "timer-before-wall-clock-expiry", "timer-revoked-before-wall-clock-expiry":
				// Deterministically model the timer firing followed by a backward
				// wall-clock adjustment; do not change the machine clock or sleep.
				claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Minute))
				if mode == "timer-revoked-before-wall-clock-expiry" {
					if err := s.RevokeUserSessions(context.Background(), 45); err != nil {
						t.Fatal(err)
					}
				}
			case "desktop":
				claims.Surface = ""
			case "revoked":
				if err := s.RevokeUserSessions(context.Background(), 45); err != nil {
					t.Fatal(err)
				}
			case "owner-changed":
				s.config.Instances.(*v2LifecycleInstanceRepo).byID[123].UserID = 46
			case "generation-changed":
				s.config.Instances.(*v2LifecycleInstanceRepo).byID[123].RuntimeGeneration++
			case "inactive":
				s.config.Users.(*desktopUserRepo).users[45].IsActive = false
			}
			want := 4401
			if mode == "expired" || mode == "timer-before-wall-clock-expiry" {
				want = websocket.CloseServiceRestart
			}
			if got := s.hermesWebSocketSessionCloseCode(context.Background(), claims, leaseExpired); got != want {
				t.Fatalf("close code=%d want=%d", got, want)
			}
		})
	}
}
