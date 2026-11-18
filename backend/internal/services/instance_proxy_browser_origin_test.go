package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawreef/internal/browserorigin"
	"clawreef/internal/models"
	"github.com/gorilla/websocket"
)

type nativeOriginTestStore struct {
	binding browserorigin.Binding
	err     error
}

func (s *nativeOriginTestStore) Bind(ctx context.Context, id int, kind, target string, first, last int) (browserorigin.Binding, error) {
	if s.err != nil {
		return browserorigin.Binding{}, s.err
	}
	if s.binding.Port == 0 {
		s.binding = browserorigin.Binding{Port: first, InstanceID: id, Kind: kind, Target: target}
	}
	return s.binding, nil
}
func (s *nativeOriginTestStore) ByPort(context.Context, int) (browserorigin.Binding, error) {
	return s.binding, s.err
}

func TestBrowserNativeProxyURL(t *testing.T) {
	reg, err := browserorigin.New("https://10.130.81.54:30443", "31400-31527", &nativeOriginTestStore{})
	if err != nil {
		t.Fatal(err)
	}
	s := NewInstanceProxyService(nil)
	s.SetBrowserOrigins(reg)
	for _, kind := range []string{RuntimeTypeOpenCode, RuntimeTypeDeepSeekHarness} {
		store := &nativeOriginTestStore{}
		reg, err = browserorigin.New("https://10.130.81.54:30443", "31400-31527", store)
		if err != nil {
			t.Fatal(err)
		}
		s.SetBrowserOrigins(reg)
		instance := &models.Instance{ID: 118, Type: kind, RuntimeType: "gateway"}
		if got := s.GetProxyURLForInstance(instance, "a+b"); got != "https://10.130.81.54:31400/?token=a%2Bb" {
			t.Fatalf("%s URL=%s", kind, got)
		}
		if store.binding.InstanceID != 118 || store.binding.Target != kind || store.binding.Kind != browserorigin.Native {
			t.Fatalf("wrong native binding: %+v", store.binding)
		}
	}
	if got := s.GetProxyURLForInstance(&models.Instance{ID: 118, Type: "ubuntu"}, ""); got != "/api/v1/instances/118/proxy/" {
		t.Fatal(got)
	}
	if s.BrowserOrigins() != reg {
		t.Fatal("registry accessor")
	}
	full, _ := browserorigin.New("https://10.130.81.54:30443", "31400-31527", &nativeOriginTestStore{err: browserorigin.ErrCapacity})
	s.SetBrowserOrigins(full)
	if got := s.GetProxyURLForInstance(&models.Instance{ID: 118, Type: RuntimeTypeDeepSeekHarness, RuntimeType: "gateway"}, ""); got != "" {
		t.Fatal(got)
	}
	s.SetBrowserOrigins(nil)
	t.Setenv(deepSeekHarnessPublicURLTemplateEnvVar, "")
	if got := s.GetProxyURLForInstance(&models.Instance{ID: 118, Type: RuntimeTypeDeepSeekHarness, RuntimeType: "gateway"}, ""); got != "/api/v1/instances/118/proxy/" {
		t.Fatal(got)
	}
}

func TestBrowserNativeProxyCookieBoundary(t *testing.T) {
	for _, socket := range []bool{false, true} {
		for _, native := range []bool{false, true} {
			name := "http"
			if socket {
				name = "ws"
			}
			if native {
				name += "/native"
			} else {
				name += "/legacy"
			}
			t.Run(name, func(t *testing.T) {
				cookies := make(chan string, 1)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					cookies <- r.Header.Get("Cookie")
					headers := http.Header{"Set-Cookie": []string{"app=one; Path=/", "instance_access_136=evil; Path=/"}}
					if socket {
						c, e := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, headers)
						if e == nil {
							defer c.Close()
							_, _, _ = c.ReadMessage()
						}
						return
					}
					w.Header().Set("Access-Control-Allow-Origin", "*")
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					for _, v := range headers.Values("Set-Cookie") {
						w.Header().Add("Set-Cookie", v)
					}
					_, _ = w.Write([]byte("ok"))
				}))
				defer upstream.Close()
				s, token := newDeepSeekHarnessV2ProxyTestService(t, upstream.URL, 136, "internal")
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if native {
						r = r.WithContext(browserorigin.WithNativeContext(r.Context()))
					}
					r.Header.Set(DedicatedRuntimeOriginHeader, RuntimeTypeDeepSeekHarness)
					http.SetCookie(w, &http.Cookie{Name: "cm_handler", Value: "keep"})
					var e error
					if socket {
						e = s.ProxyWebSocket(r.Context(), 136, token.Token, w, r)
					} else {
						e = s.ProxyRequest(r.Context(), 136, token.Token, w, r)
					}
					if e != nil {
						http.Error(w, e.Error(), 502)
					}
				}))
				defer proxy.Close()
				var response *http.Response
				if socket {
					dialer := *websocket.DefaultDialer
					dialer.HandshakeTimeout = 3 * time.Second
					c, r, e := dialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/api/v1/instances/136/proxy/api/events.mux", http.Header{"Cookie": []string{"instance_access_136=secret; app=shared"}})
					if e != nil {
						t.Fatal(e)
					}
					defer c.Close()
					response = r
				} else {
					r, _ := http.NewRequest("GET", proxy.URL+"/api/v1/instances/136/proxy/api/health", nil)
					r.Header.Set("Cookie", "instance_access_136=secret; app=shared")
					var e error
					response, e = http.DefaultClient.Do(r)
					if e != nil {
						t.Fatal(e)
					}
					defer response.Body.Close()
					if response.StatusCode != 200 {
						t.Fatal(response.Status)
					}
				}
				got := <-cookies
				if native && got != "" {
					t.Fatalf("upstream cookies leaked: %s", got)
				}
				if !native && got == "" {
					t.Fatal("legacy cookies lost")
				}
				if native && response.Header.Get("Access-Control-Allow-Origin") != "" {
					t.Fatal("native wildcard CORS allowed")
				}
				found := false
				for _, cookie := range response.Cookies() {
					if cookie.Name == "cm_handler" {
						found = true
					}
					if native && (cookie.Name == "app" || cookie.Value == "evil") {
						t.Fatalf("upstream Set-Cookie leaked: %v", cookie)
					}
				}
				if !found {
					t.Fatal("handler cookie lost")
				}
			})
		}
	}
}
