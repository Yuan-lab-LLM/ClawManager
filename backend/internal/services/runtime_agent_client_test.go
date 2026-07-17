package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRuntimeAgentClientCreateGateway(t *testing.T) {
	var gotToken string
	var gotReq RuntimeAgentCreateGatewayRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/gateways" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		gotToken = r.Header.Get("X-ClawManager-Control-Token")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(RuntimeAgentCreateGatewayResponse{
			GatewayID: "gw-7-3",
			Port:      20017,
			PID:       runtimeAgentIntPtr(8842),
			Status:    "running",
		})
	}))
	defer server.Close()

	client := NewRuntimeAgentClient("secret")
	resp, err := client.CreateGateway(context.Background(), server.URL, RuntimeAgentCreateGatewayRequest{
		InstanceID:    7,
		UserID:        8,
		AgentType:     "openclaw",
		WorkspacePath: "/workspaces/openclaw/user-8/instance-7",
		PortRange:     RuntimeAgentPortRange{Start: RuntimeGatewayPortStart, End: RuntimeGatewayPortEnd},
		UID:           200007,
		GID:           200007,
		CPUCores:      2,
		MemoryMB:      4096,
		DiskQuotaMB:   20480,
		Generation:    3,
	})
	if err != nil {
		t.Fatalf("CreateGateway returned error: %v", err)
	}
	if gotToken != "secret" {
		t.Fatalf("unexpected token %q", gotToken)
	}
	if gotReq.InstanceID != 7 || gotReq.PortRange.Start != 20000 {
		t.Fatalf("unexpected request %#v", gotReq)
	}
	if resp.GatewayID != "gw-7-3" || resp.Port != 20017 {
		t.Fatalf("unexpected response %#v", resp)
	}
}

func TestRuntimeAgentClientCreateGatewayTrimsEndpointSlash(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/gateways" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(RuntimeAgentCreateGatewayResponse{
			GatewayID: "gw-7-3",
			Port:      20017,
			Status:    "running",
		})
	}))
	defer server.Close()

	client := NewRuntimeAgentClient("secret")
	if _, err := client.CreateGateway(context.Background(), server.URL+"/", RuntimeAgentCreateGatewayRequest{}); err != nil {
		t.Fatalf("CreateGateway returned error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one gateway create call, got %d", calls)
	}
}

func TestRuntimeAgentClientDefaultTimeoutAllowsGatewayStartup(t *testing.T) {
	client := NewRuntimeAgentClient("secret")
	agentClient, ok := client.(*runtimeAgentHTTPClient)
	if !ok {
		t.Fatalf("unexpected client type %T", client)
	}
	if agentClient.httpClient.Timeout != 30*time.Second {
		t.Fatalf("default timeout = %v, want 30s", agentClient.httpClient.Timeout)
	}
}

func TestRuntimeAgentClientDeleteGatewayEscapesGatewayID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("unexpected method %s", r.Method)
		}
		if r.URL.EscapedPath() != "/v1/gateways/gw-7%2F3" {
			t.Fatalf("unexpected escaped path %s", r.URL.EscapedPath())
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewRuntimeAgentClient("secret")
	if err := client.DeleteGateway(context.Background(), server.URL, "gw-7/3"); err != nil {
		t.Fatalf("DeleteGateway returned error: %v", err)
	}
}

func TestRuntimeAgentClientHealthHandlesSuccessAndRedirect(t *testing.T) {
	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" {
			t.Fatalf("unexpected success path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer successServer.Close()
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			w.Header().Set("Location", "/ok")
			w.WriteHeader(http.StatusFound)
			_, _ = w.Write([]byte("redirect not allowed"))
		case "/ok":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected redirect path %s", r.URL.Path)
		}
	}))
	defer redirectServer.Close()

	client := NewRuntimeAgentClient("secret")
	if err := client.Health(context.Background(), successServer.URL); err != nil {
		t.Fatalf("Health returned error for 204: %v", err)
	}
	err := client.Health(context.Background(), redirectServer.URL)
	if err == nil || !strings.Contains(err.Error(), "runtime agent status 302") || !strings.Contains(err.Error(), "redirect not allowed") {
		t.Fatalf("unexpected redirect error %v", err)
	}
}

func TestRuntimeAgentClientWithHTTPClientDoesNotMutateRedirectPolicy(t *testing.T) {
	supplied := &http.Client{Timeout: 10 * time.Second}
	client := NewRuntimeAgentClientWithHTTPClient("secret", supplied)

	if supplied.CheckRedirect != nil {
		t.Fatalf("constructor mutated supplied CheckRedirect")
	}

	agentClient, ok := client.(*runtimeAgentHTTPClient)
	if !ok {
		t.Fatalf("unexpected client type %T", client)
	}
	if agentClient.httpClient == supplied {
		t.Fatalf("constructor reused supplied client pointer")
	}
	if agentClient.httpClient.Timeout != supplied.Timeout {
		t.Fatalf("timeout was not preserved: got %v want %v", agentClient.httpClient.Timeout, supplied.Timeout)
	}
	if agentClient.httpClient.CheckRedirect == nil {
		t.Fatalf("copied client has no redirect policy")
	}
}

func TestRuntimeAgentClientDrainSendsJSONBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/drain" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected content type %q", r.Header.Get("Content-Type"))
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if strings.TrimSpace(string(body)) != `{"draining":true}` {
			t.Fatalf("unexpected body %s", body)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewRuntimeAgentClient("secret")
	if err := client.Drain(context.Background(), server.URL); err != nil {
		t.Fatalf("Drain returned error: %v", err)
	}
}

func TestRuntimeAgentClientResyncInstanceSkills(t *testing.T) {
	var gotMethod, gotPath, gotToken string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-ClawManager-Control-Token")
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewRuntimeAgentClient("secret")
	if err := client.ResyncInstanceSkills(context.Background(), server.URL, 12, "full"); err != nil {
		t.Fatalf("ResyncInstanceSkills returned error: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/skills/resync" || gotToken != "secret" {
		t.Fatalf("unexpected request: %s %s token=%q", gotMethod, gotPath, gotToken)
	}
	if body["instance_id"] != float64(12) || body["mode"] != "full" {
		t.Fatalf("unexpected body: %#v", body)
	}
}

func TestRuntimeAgentClientConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no free port", http.StatusConflict)
	}))
	defer server.Close()

	client := NewRuntimeAgentClient("secret")
	_, err := client.CreateGateway(context.Background(), server.URL, RuntimeAgentCreateGatewayRequest{})
	if err == nil || !strings.Contains(err.Error(), "runtime agent conflict") || !strings.Contains(err.Error(), "no free port") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestRuntimeAgentClientNonConflictErrorIncludesStatusAndBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "agent exploded", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewRuntimeAgentClient("secret")
	_, err := client.CreateGateway(context.Background(), server.URL, RuntimeAgentCreateGatewayRequest{})
	if err == nil || !strings.Contains(err.Error(), "runtime agent status 500") || !strings.Contains(err.Error(), "agent exploded") {
		t.Fatalf("unexpected error %v", err)
	}
}

func runtimeAgentIntPtr(v int) *int {
	return &v
}
