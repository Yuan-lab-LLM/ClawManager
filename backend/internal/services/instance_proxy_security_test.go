package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"github.com/gorilla/websocket"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type securityProxyInstances struct {
	repository.InstanceRepository
	instance *models.Instance
}

func protectedNativeProxyFixture(t *testing.T, target, assigned string, protected bool) (*InstanceProxyService, *AccessToken, string) {
	t.Helper()
	a, instances, kube, cm, pod := securityFixture(t)
	instances.instance.Type = RuntimeTypeDeepSeekHarness
	host, port := splitURLHostPortForProxyTest(t, target)
	assignedHost, assignedPort := splitURLHostPortForProxyTest(t, assigned)
	cm.Data["runtime_type"] = instances.instance.Type
	request := RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod-7", AgentType: instances.instance.Type, SecurityProfile: RuntimeSecurityProfile, GatewayPort: assignedPort}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	cm.Data["request"] = string(data)
	cm.Data["endpoint"] = "https://" + assignedHost + ":19090"
	pod.Status.PodIP = assignedHost
	if _, err := kube.CoreV1().ConfigMaps("test").Update(context.Background(), cm, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := kube.CoreV1().Pods("test").Update(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	repo := newV2LifecycleInstanceRepo()
	repo.byID[7] = instances.instance
	bindings := newFakeRuntimeBindingRepo()
	bindings.bindings[7] = &models.InstanceRuntimeBinding{InstanceID: 7, RuntimePodID: 9, GatewayPort: port, State: "running", Generation: 2}
	pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{9: {ID: 9, PodIP: &host, State: "ready"}}}
	access := NewInstanceAccessService()
	t.Cleanup(access.Stop)
	token, err := access.GenerateToken(3, 7, instances.instance.Type, "/api/v1/instances/7/proxy/", "", int32(port), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	s := NewInstanceProxyService(access, WithInstanceProxyRuntimeRepositories(repo, pods, bindings), WithInstanceProxyRuntimeSecurity(a))
	credential, err := a.NativeGatewayCredential(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if !protected {
		instances.instance.RuntimeSecurityProfile = ""
		s.runtimeSecurity = nil
		credential = ""
	}
	return s, token, credential
}

func TestProtectedNativeProxyRejectsSameGenerationStaleTargetBeforeDial(t *testing.T) {
	for _, socket := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "websocket"}[socket], func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				http.Error(w, "sentinel wrong target", 403)
			}))
			defer upstream.Close()
			assigned := httptest.NewServer(http.NotFoundHandler())
			defer assigned.Close()
			s, token, _ := protectedNativeProxyFixture(t, upstream.URL, assigned.URL, true)
			r := httptest.NewRequest("GET", "/api/v1/instances/7/proxy/auth/session", nil)
			w := httptest.NewRecorder()
			var err error
			if socket {
				err = s.ProxyWebSocket(r.Context(), 7, token.Token, w, r)
			} else {
				err = s.ProxyRequest(r.Context(), 7, token.Token, w, r)
			}
			if err == nil || calls.Load() != 0 {
				t.Fatalf("stale target received protected request: error=%v calls=%d", err, calls.Load())
			}
		})
	}
}

func TestNativeProxyIngressHeaderHTTPAndWebSocket(t *testing.T) {
	for _, protected := range []bool{false, true} {
		for _, socket := range []bool{false, true} {
			t.Run(map[bool]string{false: "legacy", true: "protected"}[protected]+"/"+map[bool]string{false: "http", true: "websocket"}[socket], func(t *testing.T) {
				seen := make(chan http.Header, 1)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					seen <- r.Header.Clone()
					if !socket {
						w.Write([]byte("sentinel"))
						return
					}
					c, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
					if err != nil {
						return
					}
					defer c.Close()
					c.WriteMessage(websocket.TextMessage, []byte("sentinel"))
					c.ReadMessage()
				}))
				defer upstream.Close()
				s, token, want := protectedNativeProxyFixture(t, upstream.URL, upstream.URL, protected)
				if !socket {
					r := httptest.NewRequest("GET", "/api/v1/instances/7/proxy/auth/session", nil)
					r.Header.Set("X-ClawManager-Ingress-Token", "spoofed")
					w := httptest.NewRecorder()
					if err := s.ProxyRequest(r.Context(), 7, token.Token, w, r); err != nil {
						t.Fatal(err)
					}
				} else {
					proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if err := s.ProxyWebSocket(r.Context(), 7, token.Token, w, r); err != nil {
							http.Error(w, err.Error(), 502)
						}
					}))
					defer proxy.Close()
					header := http.Header{"X-ClawManager-Ingress-Token": []string{"spoofed"}}
					conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/api/v1/instances/7/proxy/auth/session", header)
					if err != nil {
						t.Fatal(err)
					}
					_, _, err = conn.ReadMessage()
					conn.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
				select {
				case header := <-seen:
					if got := header.Get("X-ClawManager-Ingress-Token"); got != want {
						t.Fatalf("ingress header=%q expected=%q", got, want)
					}
				case <-time.After(time.Second):
					t.Fatal("no upstream request")
				}
			})
		}
	}
}

func (r securityProxyInstances) GetByID(int) (*models.Instance, error) { return r.instance, nil }

func TestManagedProxyProtectedCredentialHasNoLegacyFallback(t *testing.T) {
	a, instances, _, _, _ := securityFixture(t)
	legacy := "legacy-access-secret"
	instances.instance.AccessToken = &legacy
	repo := securityProxyInstances{instance: instances.instance}
	s := NewInstanceProxyService(nil, WithInstanceProxyRuntimeRepositories(repo, nil, nil), WithInstanceProxyRuntimeSecurity(a))
	target := &url.URL{Scheme: "http", Host: "10.2.0.3:24000"}
	credential, _, err := s.managedRuntimeGatewayBearerToken(context.Background(), 7, "opencode", target, true)
	if err != nil || credential == "" || credential == legacy {
		t.Fatalf("protected native credential: %v", err)
	}
	if credential, _, err := s.managedRuntimeGatewayBearerToken(context.Background(), 7, "opencode", target, false); err == nil || credential != "" {
		t.Fatal("standalone route acquired protected native credential")
	}
	instances.instance.RuntimeGeneration++
	if _, _, err = s.managedRuntimeGatewayBearerToken(context.Background(), 7, "opencode", target, true); err == nil {
		t.Fatal("stale assignment accepted")
	}
	s.runtimeSecurity = nil
	if _, _, err = s.managedRuntimeGatewayBearerToken(context.Background(), 7, "opencode", target, true); err == nil {
		t.Fatal("missing authority enabled legacy fallback")
	}
	instances.instance.RuntimeSecurityProfile = ""
	credential, _, err = s.managedRuntimeGatewayBearerToken(context.Background(), 7, "opencode", target, true)
	if err != nil || credential != legacy {
		t.Fatal("unmigrated native credential changed")
	}
}
