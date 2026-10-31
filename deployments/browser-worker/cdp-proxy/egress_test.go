package main

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkerEgressKeepsKeyInTrustedProxy(t *testing.T) {
	scope := `{"instance_id":7,"user_id":3,"generation":2,"pod_uid":"runtime-pod"}`
	key := strings.Repeat("a", 64)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "Bearer bw1."+base64.RawURLEncoding.EncodeToString([]byte(scope))+".worker-pod-uid."+key || r.URL.String() != "http://public.example/page" {
			t.Error("worker credential/target boundary")
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()
	broker, err := newWorkerEgress(context.Background(), upstream.URL, scope, "worker-pod-uid", key)
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close()
	r := httptest.NewRequest("GET", "http://public.example/page", nil)
	r.RemoteAddr = "127.0.0.1:3210"
	r.Header.Set("Proxy-Authorization", "Bearer forged")
	w := httptest.NewRecorder()
	broker.Egress().ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "ok" || strings.Contains(w.Body.String(), key) {
		t.Fatal("worker egress failed or exposed key")
	}
}
