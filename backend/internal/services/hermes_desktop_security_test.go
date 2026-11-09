package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHermesDesktopProtectedRejectsMissingAuthority(t *testing.T) {
	s := desktopFixture(t, "http://127.0.0.1:24000")
	s.config.Instances.(*v2LifecycleInstanceRepo).byID[123].RuntimeSecurityProfile = RuntimeSecurityProfile
	d, err := s.Describe(context.Background(), 45, 123)
	if err == nil && d.Available {
		t.Fatal("protected Hermes advertised legacy authentication without runtime authority")
	}
}

func protectedDesktopFixture(t *testing.T, upstream string) (*HermesDesktopService, *fake.Clientset, *corev1.ConfigMap) {
	t.Helper()
	s := desktopFixture(t, upstream)
	instance := s.config.Instances.(*v2LifecycleInstanceRepo).byID[123]
	instance.RuntimeSecurityProfile = RuntimeSecurityProfile
	pod := s.config.Pods.(*fakeRuntimePodRepo).pods[9]
	uid := "hermes-protected-pod"
	pod.PodUID = &uid
	port := s.config.Bindings.(*fakeRuntimeBindingRepo).bindings[123].GatewayPort
	request, err := json.Marshal(RuntimeAgentCreateGatewayRequest{InstanceID: 123, UserID: 45, Generation: 3, PodUID: uid, AgentType: "hermes", SecurityProfile: RuntimeSecurityProfile, GatewayPort: port})
	if err != nil {
		t.Fatal(err)
	}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "lite-instance-123", Namespace: "test"}, Data: map[string]string{
		"instance_id": "123", "user_id": "45", "generation": "3", "pod_uid": uid, "runtime_type": "hermes", "desired_state": "running", "endpoint": "https://" + *pod.PodIP + ":19090", "request": string(request),
	}}
	live := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "test", UID: types.UID(uid), Labels: map[string]string{"clawmanager.io/runtime-protocol": "container-slots-v1", "clawmanager.io/runtime-security": RuntimeSecurityProfile}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: *pod.PodIP, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
	kube := fake.NewSimpleClientset(cm, live)
	a, err := NewRuntimeSecurityAuthority(kube, "test", &securityInstances{instance: instance}, []byte(strings.Repeat("h", 32)))
	if err != nil {
		t.Fatal(err)
	}
	s.config.RuntimeSecurity = a
	return s, kube, cm
}

func TestHermesDesktopProtectedNativeCredentialLifecycle(t *testing.T) {
	var credential string
	var expectedCredential atomic.Value
	var logins, tickets, sockets, requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		credential, _ := expectedCredential.Load().(string)
		if !strings.HasPrefix(credential, "ui1_") || r.Header.Get(NativeIngressTokenHeader) != credential || r.URL.RawQuery != "" {
			t.Error("protected ingress missing target-bound credential or credential placed in URL")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Origin") != "http://clawmanager.internal:9001" {
			t.Error("trusted origin lost")
		}
		switch r.URL.Path {
		case "/auth/password-login":
			logins.Add(1)
			var body map[string]string
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["password"] != credential {
				t.Error("protected login used legacy password")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "protected-private-cookie", Path: "/", HttpOnly: true})
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/config/schema":
			_ = json.NewEncoder(w).Encode(map[string]any{"fields": map[string]any{"example": map[string]string{"description": credential + " managed-Hermes-password"}}})
		case "/api/auth/ws-ticket":
			tickets.Add(1)
			if c, err := r.Cookie("hermes_session_at"); err != nil || c.Value != "protected-private-cookie" {
				t.Error("ticket missing upstream cookie")
			}
			_, _ = w.Write([]byte(`{"ticket":"protected-private-ticket"}`))
		case "/api/ws":
			sockets.Add(1)
			if strings.Join(websocket.Subprotocols(r), ",") != "hermes-gateway-v1,hermes-gateway-ticket.protected-private-ticket" {
				t.Error("websocket missing private ticket")
			}
			c, err := (&websocket.Upgrader{Subprotocols: []string{"hermes-gateway-v1"}, CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer c.Close()
			_ = c.WriteJSON(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]string{"type": "gateway.ready", "message": credential + " managed-Hermes-password protected-private-ticket"}})
			for {
				if _, _, err := c.ReadMessage(); err != nil {
					return
				}
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	s, kube, cm := protectedDesktopFixture(t, upstream.URL)
	targetURL, _ := url.Parse(upstream.URL)
	var err error
	credential, err = s.config.RuntimeSecurity.NativeGatewayCredentialForTarget(context.Background(), 123, targetURL)
	if err != nil {
		t.Fatal(err)
	}
	expectedCredential.Store(credential)
	d, token, err := s.Activate(context.Background(), 45, 123)
	if err != nil || !d.Available {
		t.Fatalf("protected activate unavailable: %v", err)
	}
	claims, err := s.Authenticate(context.Background(), token, 123)
	if err != nil {
		t.Fatal(err)
	}
	body, err := s.ProxyAPI(context.Background(), claims, http.MethodGet, "/config/schema", nil)
	if err != nil || strings.Contains(string(body), credential) || strings.Contains(string(body), "managed-Hermes-password") || !strings.Contains(string(body), "[redacted]") {
		t.Fatal("protected HTTP failed or exposed native/platform credential")
	}
	wsURL, _, err := s.MintTicket(claims)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(wsURL)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.ProxyWebSocket(r.Context(), claims, r.URL.Query().Get("ticket"), w, r); err != nil {
			http.Error(w, "denied", http.StatusUnauthorized)
		}
	}))
	defer proxy.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/?"+u.RawQuery, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, ready, err := conn.ReadMessage()
	if err != nil || strings.Contains(string(ready), credential) || strings.Contains(string(ready), "managed-Hermes-password") || strings.Contains(string(ready), "protected-private-ticket") {
		t.Fatal("protected WS failed or exposed credential")
	}
	cm.Data["desired_state"] = "stopped"
	if _, err := kube.CoreV1().ConfigMaps("test").Update(context.Background(), cm, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("revoked protected socket remained open")
	} else if closeErr, ok := err.(*websocket.CloseError); !ok || closeErr.Code != 4401 {
		t.Fatalf("expected explicit revocation close, got %T", err)
	}
	before := requests.Load()
	if _, err := s.ProxyAPI(context.Background(), claims, http.MethodGet, "/config/schema", nil); err == nil || requests.Load() != before {
		t.Fatal("revoked assignment reused cached native cookie")
	}
	if logins.Load() != 1 || tickets.Load() != 1 || sockets.Load() != 1 {
		t.Fatal("native login/ticket/websocket not exercised exactly once")
	}
}

func TestHermesDesktopProtectedTargetFailures(t *testing.T) {
	for _, scenario := range []string{"unknown-profile", "missing-authority", "different-pod", "different-port", "wrong-owner", "stale-generation", "missing-assignment", "no-platform-token"} {
		t.Run(scenario, func(t *testing.T) {
			s, kube, cm := protectedDesktopFixture(t, "http://127.0.0.1:24000")
			instance := s.config.Instances.(*v2LifecycleInstanceRepo).byID[123]
			switch scenario {
			case "unknown-profile":
				instance.RuntimeSecurityProfile = "unknown"
			case "missing-authority":
				s.config.RuntimeSecurity = nil
			case "different-pod":
				other := "wrong-pod"
				s.config.Pods.(*fakeRuntimePodRepo).pods[9].PodUID = &other
			case "different-port":
				s.config.Bindings.(*fakeRuntimeBindingRepo).bindings[123].GatewayPort = 25000
			case "wrong-owner":
				cm.Data["user_id"] = "99"
			case "stale-generation":
				cm.Data["generation"] = strconv.Itoa(instance.RuntimeGeneration + 1)
			case "missing-assignment":
				if err := kube.CoreV1().ConfigMaps("test").Delete(context.Background(), cm.Name, metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
			case "no-platform-token":
				instance.AccessToken = nil
			}
			if scenario == "wrong-owner" || scenario == "stale-generation" {
				if _, err := kube.CoreV1().ConfigMaps("test").Update(context.Background(), cm, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			d, err := s.Describe(context.Background(), 45, 123)
			available := err == nil && d != nil && d.Available
			if available != (scenario == "no-platform-token") {
				t.Fatal("protected target gate mismatch")
			}
		})
	}
}

func TestHermesNativeIngressHeaderCannotEscapeTarget(t *testing.T) {
	a, err := NewHermesGatewayAuth("http://clawmanager.internal:9001")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), hermesNativeIngressContextKey{}, hermesNativeIngress{host: "127.0.0.1:24000", credential: "ui1_fake"})
	for _, target := range []string{"http://127.0.0.1:24000/api", "ws://127.0.0.1:24000/api/ws", "http://127.0.0.2:24000/api", "http://127.0.0.1:24001/api", "https://127.0.0.1:24000/api"} {
		r, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		r.Header.Set(NativeIngressTokenHeader, "untrusted-browser-header")
		a.SetHeaders(r)
		want := r.URL.Host == "127.0.0.1:24000" && r.URL.Scheme != "https"
		if (r.Header.Get(NativeIngressTokenHeader) == "ui1_fake") != want || r.Header.Get(NativeIngressTokenHeader) == "untrusted-browser-header" {
			t.Fatal("ingress header escaped exact authorized target")
		}
	}
}
