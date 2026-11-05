package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/workbench"
	"github.com/gorilla/websocket"
)

// This optional gate composes the real CM proxy with the runtime's test-only
// pool/worker/private-helper harness. It requires no production account or Pod.
func TestWorkbenchPreviewExternalRuntimeRoundTrip(t *testing.T) {
	file := os.Getenv("CM_WORKBENCH_PREVIEW_HARNESS_FILE")
	if file == "" {
		t.Skip("requires the disposable Runtime preview harness")
	}
	var metadata struct {
		Endpoint        string `json:"endpoint"`
		Token           string `json:"token"`
		InstanceID      int    `json:"instance_id"`
		UserID          int    `json:"user_id"`
		Generation      int    `json:"generation"`
		GatewayID       string `json:"gateway_id"`
		GatewayPort     int    `json:"gateway_port"`
		ApplicationPort int    `json:"application_port"`
	}
	input, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	err = json.NewDecoder(io.LimitReader(input, 4096)).Decode(&metadata)
	_ = input.Close()
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := url.Parse(metadata.Endpoint)
	if err != nil || endpoint.Scheme != "http" || endpoint.Hostname() != "127.0.0.1" || metadata.Token == "" || metadata.ApplicationPort < 1 || metadata.ApplicationPort > 65535 {
		t.Fatal("invalid disposable loopback harness metadata")
	}
	instance := &models.Instance{ID: metadata.InstanceID, UserID: metadata.UserID, RuntimeGeneration: metadata.Generation, Status: "running"}
	binding := &models.InstanceRuntimeBinding{InstanceID: metadata.InstanceID, RuntimePodID: 1, Generation: metadata.Generation, GatewayID: metadata.GatewayID, GatewayPort: metadata.GatewayPort, WorkspacePath: "/test-workspace"}
	previews, err := workbench.NewPreviews("http://p-{token}.localhost", &previewTestStore{values: map[string]workbench.PreviewLease{}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var instanceMu sync.RWMutex
	service := NewInstanceWorkbenchService(workbenchTestInstances{instance: instance, mu: &instanceMu}, workbenchTestBindings{binding}, workbenchTestPods{&models.RuntimePod{ID: 1, AgentEndpoint: &metadata.Endpoint}}, NewRuntimeAgentClient(metadata.Token).(WorkbenchAgent), previews)
	service.previewRecheckInterval = 10 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	caps, err := service.Capabilities(ctx, metadata.UserID, false, metadata.InstanceID)
	if err != nil || caps["preview"] != true {
		t.Fatalf("real runtime capability RPC failed: %v %+v", err, caps)
	}
	created, err := service.CreatePreview(ctx, metadata.UserID, false, metadata.InstanceID, metadata.Generation, "http://localhost:"+strconv.Itoa(metadata.ApplicationPort))
	if err != nil {
		t.Fatal(err)
	}
	capability, err := url.Parse(created["url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(http.HandlerFunc(service.ServePreview))
	defer proxy.Close()
	payload := []byte{0, 255, 1, 2, 'e', 'c', 'h', 'o'}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, proxy.URL+"/e%63ho?x=%2f&x=2", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Host = capability.Host
	request.Header.Set("Authorization", "Bearer application-only")
	request.Header.Set("Cookie", "fixture=1")
	request.Header.Set("X-ClawManager-Untrusted", "must-not-reach-app")
	response, err := proxy.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != 200 || !bytes.Equal(output, payload) {
		t.Fatalf("real runtime body roundtrip failed: status=%d body=%q error=%v", response.StatusCode, output, err)
	}
	if response.Header.Get("X-Test-Request-URI") != "/e%63ho?x=%2f&x=2" || response.Header.Get("X-Test-Authorization") != "Bearer application-only" || response.Header.Get("X-Test-Cookie") != "fixture=1" || response.Header.Get("X-Test-Control-Leaked") != "false" {
		t.Fatalf("real runtime request boundary changed: %+v", response.Header)
	}
	headers := http.Header{"Host": []string{capability.Host}, "Authorization": []string{"Bearer application-only"}, "Origin": []string{"http://" + capability.Host}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/ws", headers)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err = conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
		t.Fatal(err)
	}
	kind, echo, err := conn.ReadMessage()
	if err != nil || kind != websocket.BinaryMessage || !bytes.Equal(echo, payload) {
		t.Fatalf("real runtime WS roundtrip failed: %q %v", echo, err)
	}
	instanceMu.Lock()
	instance.RuntimeGeneration++
	instanceMu.Unlock()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err = conn.ReadMessage()
	var timeout net.Error
	if err == nil || errors.As(err, &timeout) && timeout.Timeout() {
		t.Fatalf("stale real-runtime WebSocket was not closed: %v", err)
	}
	denied := httptest.NewRecorder()
	request = httptest.NewRequest("GET", "http://"+capability.Host+"/echo", nil)
	service.ServePreview(denied, request)
	if denied.Code != 404 {
		t.Fatalf("stale capability still routed: %d", denied.Code)
	}
}
