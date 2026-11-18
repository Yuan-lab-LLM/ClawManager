package services

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"github.com/gorilla/websocket"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type hermesAtomicPodRepo struct {
	repository.RuntimePodRepository
	pod atomic.Pointer[models.RuntimePod]
}

func (p *hermesAtomicPodRepo) GetByID(context.Context, int64) (*models.RuntimePod, error) {
	return p.pod.Load(), nil
}

func TestHermesProtectedWSRebindDoesNotAdoptNewTarget(t *testing.T) {
	var pods *hermesAtomicPodRepo
	s, c, done, _ := hermesRevocationWS(t, func(s *HermesDesktopService) {
		pods = &hermesAtomicPodRepo{RuntimePodRepository: s.config.Pods}
		pod, err := s.config.Pods.GetByID(context.Background(), 9)
		if err != nil {
			t.Fatal(err)
		}
		pods.pod.Store(pod)
		s.config.Pods = pods
	})
	old, reason, err := s.resolve(context.Background(), 45, 123)
	if err != nil || reason != "" {
		t.Fatal("original target unavailable")
	}
	ctx := context.Background()
	kube := s.config.RuntimeSecurity.kube
	cm, err := kube.CoreV1().ConfigMaps("test").Get(ctx, "lite-instance-123", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	live, err := kube.CoreV1().Pods("test").Get(ctx, "pool", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	newUID, newIP := "replacement-hermes-pod", "127.0.0.2"
	var request RuntimeAgentCreateGatewayRequest
	if json.Unmarshal([]byte(cm.Data["request"]), &request) != nil {
		t.Fatal("fixture request invalid")
	}
	request.PodUID = newUID
	encoded, _ := json.Marshal(request)
	cm.Data["request"], cm.Data["pod_uid"], cm.Data["endpoint"] = string(encoded), newUID, "https://"+newIP+":19090"
	live.UID, live.Status.PodIP = types.UID(newUID), newIP
	if _, err := kube.CoreV1().ConfigMaps("test").Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := kube.CoreV1().Pods("test").Update(ctx, live, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	rebound := *pods.pod.Load()
	rebound.PodUID, rebound.PodIP = &newUID, &newIP
	pods.pod.Store(&rebound)
	current, reason, err := s.resolve(ctx, 45, 123)
	if err != nil || reason != "" || current.binding.Generation != old.binding.Generation || current.pod.ID != old.pod.ID || current.binding.GatewayPort != old.binding.GatewayPort {
		t.Fatal("new target must remain authorized with unchanged SQL allocation identifiers")
	}
	if _, err := s.hermesUpstreamContext(ctx, current); err != nil {
		t.Fatal("new target is not valid")
	}
	if _, err := s.hermesUpstreamContext(ctx, old); err == nil {
		t.Fatal("old target unexpectedly remained valid")
	}
	_ = c.SetReadDeadline(time.Now().Add(7 * time.Second))
	_, _, err = c.ReadMessage()
	if closed, ok := err.(*websocket.CloseError); !ok || closed.Code != 4401 {
		t.Fatalf("old socket adopted new target instead of revoking: %T", err)
	}
	hermesWait(t, done, 2*time.Second, "old socket handler did not finish")
}

type hermesBlockingUser struct {
	repository.UserRepository
	armed            atomic.Bool
	entered, release chan struct{}
	once             sync.Once
}

func (b *hermesBlockingUser) GetByID(id int) (*models.User, error) {
	if b.armed.Load() {
		b.once.Do(func() { close(b.entered) })
		<-b.release
	}
	return b.UserRepository.GetByID(id)
}

type hermesBlockingSource struct {
	runtimeSecurityInstanceSource
	armed            atomic.Bool
	entered, release chan struct{}
	once             sync.Once
}

func (b *hermesBlockingSource) GetByIDContext(_ context.Context, id int) (*models.Instance, error) {
	if b.armed.Load() {
		b.once.Do(func() { close(b.entered) })
		<-b.release
	}
	return b.runtimeSecurityInstanceSource.GetByIDContext(context.Background(), id)
}

type hermesBlockingRedis struct {
	*desktopRedis
	armed            atomic.Bool
	entered, release chan struct{}
	once             sync.Once
}

func (b *hermesBlockingRedis) Get(ctx context.Context, key string) (string, bool, error) {
	if b.armed.Load() {
		b.once.Do(func() { close(b.entered) })
		<-b.release
	}
	return b.desktopRedis.Get(ctx, key)
}

func hermesWait(t *testing.T, done <-chan struct{}, timeout time.Duration, reason string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal(reason)
	}
}
func hermesAssertJoined(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
		t.Fatal("handler detached blocked check/worker")
	case <-time.After(100 * time.Millisecond):
	}
}

func hermesRevocationWS(t *testing.T, configure func(*HermesDesktopService)) (*HermesDesktopService, *websocket.Conn, <-chan struct{}, context.CancelFunc) {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/password-login":
			http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "fixture-private-cookie", Path: "/"})
			_, _ = w.Write([]byte(`{}`))
		case "/api/auth/ws-ticket":
			_, _ = w.Write([]byte(`{"ticket":"fixture-ticket"}`))
		case "/api/ws":
			c, err := (&websocket.Upgrader{Subprotocols: []string{"hermes-gateway-v1"}, CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer c.Close()
			_ = c.WriteMessage(websocket.TextMessage, []byte(`{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready"}}`))
			for {
				if _, _, err := c.ReadMessage(); err != nil {
					return
				}
			}
		default:
			w.WriteHeader(404)
		}
	}))
	s, _, _ := protectedDesktopFixture(t, upstream.URL)
	configure(s)
	d, token, err := s.Activate(context.Background(), 45, 123)
	if err != nil || !d.Available {
		upstream.Close()
		t.Fatal("activation failed")
	}
	claims, err := s.Authenticate(context.Background(), token, 123)
	if err != nil {
		t.Fatal(err)
	}
	wsURL, _, err := s.MintTicket(claims)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(wsURL)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		if err := s.ProxyWebSocket(ctx, claims, r.URL.Query().Get("ticket"), w, r); err != nil {
			http.Error(w, "denied", 401)
		}
	}))
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/?"+u.RawQuery, nil)
	if err != nil {
		cancel()
		proxy.Close()
		upstream.Close()
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := c.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); c.Close(); proxy.Close(); upstream.Close() })
	return s, c, done, cancel
}

func TestHermesProtectedWSBlockedLookupClosesBeforeJoin(t *testing.T) {
	b := &hermesBlockingUser{entered: make(chan struct{}), release: make(chan struct{})}
	_, c, done, _ := hermesRevocationWS(t, func(s *HermesDesktopService) { b.UserRepository = s.config.Users; s.config.Users = b })
	var release sync.Once
	unblock := func() { release.Do(func() { close(b.release) }) }
	t.Cleanup(unblock)
	b.armed.Store(true)
	hermesWait(t, b.entered, 7*time.Second, "periodic lookup never started")
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err := c.ReadMessage()
	var timeout net.Error
	if err == nil || (errors.As(err, &timeout) && timeout.Timeout()) {
		t.Fatal("blocked context-ignoring lookup retained WS past independent deadline")
	}
	hermesAssertJoined(t, done)
	unblock()
	hermesWait(t, done, 2*time.Second, "handler did not join released lookup")
}

func TestHermesProtectedWSCancellationJoinsBlockedRelay(t *testing.T) {
	b := &hermesBlockingRedis{entered: make(chan struct{}), release: make(chan struct{})}
	_, c, done, cancel := hermesRevocationWS(t, func(s *HermesDesktopService) { b.desktopRedis = s.config.Redis.(*desktopRedis); s.config.Redis = b })
	var release sync.Once
	unblock := func() { release.Do(func() { close(b.release) }) }
	t.Cleanup(unblock)
	b.armed.Store(true)
	if err := c.WriteMessage(websocket.TextMessage, []byte(`{"jsonrpc":"2.0","id":1,"method":"unknown","params":{}}`)); err != nil {
		t.Fatal(err)
	}
	hermesWait(t, b.entered, 2*time.Second, "relay lookup never started")
	cancel()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := c.ReadMessage()
	var timeout net.Error
	if err == nil || (errors.As(err, &timeout) && timeout.Timeout()) {
		t.Fatal("canceled request retained socket")
	}
	hermesAssertJoined(t, done)
	unblock()
	hermesWait(t, done, 2*time.Second, "handler did not join released relay")
}

func TestHermesProtectedHTTPSlowBodyRevocation(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "revoked", true: "blocked-check"}[blocked], func(t *testing.T) {
			bodyStarted, disconnected := make(chan struct{}), make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/auth/password-login" {
					http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "private", Path: "/"})
					_, _ = w.Write([]byte(`{}`))
					return
				}
				_, _ = w.Write([]byte(`{"fields":`))
				w.(http.Flusher).Flush()
				close(bodyStarted)
				<-r.Context().Done()
				close(disconnected)
			}))
			defer upstream.Close()
			s, kube, cm := protectedDesktopFixture(t, upstream.URL)
			d, token, err := s.Activate(context.Background(), 45, 123)
			if err != nil || !d.Available {
				t.Fatal("activation failed")
			}
			claims, err := s.Authenticate(context.Background(), token, 123)
			if err != nil {
				t.Fatal(err)
			}
			b := &hermesBlockingSource{runtimeSecurityInstanceSource: s.config.RuntimeSecurity.instances, entered: make(chan struct{}), release: make(chan struct{})}
			s.config.RuntimeSecurity.instances = b
			var release sync.Once
			unblock := func() { release.Do(func() { close(b.release) }) }
			defer unblock()
			done := make(chan struct{})
			var callErr error
			go func() {
				defer close(done)
				_, callErr = s.ProxyAPI(context.Background(), claims, http.MethodGet, "/config/schema", nil)
			}()
			hermesWait(t, bodyStarted, 3*time.Second, "slow response not started")
			if blocked {
				b.armed.Store(true)
				hermesWait(t, b.entered, 7*time.Second, "HTTP authority guard never started")
			} else {
				cm.Data["desired_state"] = "stopped"
				if _, err := kube.CoreV1().ConfigMaps("test").Update(context.Background(), cm, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			bound := 7 * time.Second
			if blocked {
				bound = 3 * time.Second
			}
			hermesWait(t, disconnected, bound, "revoked/blocked guard retained native HTTP body")
			if blocked {
				hermesAssertJoined(t, done)
				unblock()
			}
			hermesWait(t, done, 2*time.Second, "HTTP guard did not join")
			if callErr == nil {
				t.Fatal("revoked HTTP body returned successfully")
			}
		})
	}
}

func TestHermesProtectedHTTPRechecksBeforeReturn(t *testing.T) {
	var revoke func()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/password-login" {
			http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "private", Path: "/"})
			_, _ = w.Write([]byte(`{}`))
			return
		}
		revoke()
		_, _ = w.Write([]byte(`{"fields":{}}`))
	}))
	defer upstream.Close()
	s, kube, cm := protectedDesktopFixture(t, upstream.URL)
	revoke = func() {
		cm.Data["desired_state"] = "stopped"
		if _, err := kube.CoreV1().ConfigMaps("test").Update(context.Background(), cm, metav1.UpdateOptions{}); err != nil {
			t.Error(err)
		}
	}
	d, token, err := s.Activate(context.Background(), 45, 123)
	if err != nil || !d.Available {
		t.Fatal("activation failed")
	}
	claims, err := s.Authenticate(context.Background(), token, 123)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProxyAPI(context.Background(), claims, http.MethodGet, "/config/schema", nil); err == nil {
		t.Fatal("fast native response returned after assignment revocation")
	}
}
