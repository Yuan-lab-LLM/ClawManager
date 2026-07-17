package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type RuntimeAgentClient interface {
	Health(ctx context.Context, endpoint string) error
	CreateGateway(ctx context.Context, endpoint string, req RuntimeAgentCreateGatewayRequest) (*RuntimeAgentCreateGatewayResponse, error)
	DeleteGateway(ctx context.Context, endpoint, gatewayID string) error
	Drain(ctx context.Context, endpoint string) error
	ResyncInstanceSkills(ctx context.Context, endpoint string, instanceID int, mode string) error
}

type RuntimeAgentPortRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type RuntimeAgentCreateGatewayRequest struct {
	InstanceID    int                   `json:"instance_id"`
	UserID        int                   `json:"user_id"`
	AgentType     string                `json:"agent_type"`
	WorkspacePath string                `json:"workspace_path"`
	PortRange     RuntimeAgentPortRange `json:"port_range"`
	UID           int                   `json:"uid"`
	GID           int                   `json:"gid"`
	CPUCores      float64               `json:"cpu_cores"`
	MemoryMB      int                   `json:"memory_mb"`
	DiskQuotaMB   int                   `json:"disk_quota_mb"`
	Generation    int                   `json:"generation"`
	Environment   map[string]string     `json:"environment,omitempty"`
}

type RuntimeAgentCreateGatewayResponse struct {
	GatewayID string `json:"gateway_id"`
	Port      int    `json:"port"`
	PID       *int   `json:"pid,omitempty"`
	Status    string `json:"status"`
}

type runtimeAgentHTTPClient struct {
	controlToken string
	httpClient   *http.Client
}

func NewRuntimeAgentClient(controlToken string) RuntimeAgentClient {
	return NewRuntimeAgentClientWithHTTPClient(controlToken, nil)
}

func NewRuntimeAgentClientWithHTTPClient(controlToken string, httpClient *http.Client) RuntimeAgentClient {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}
	clientCopy := *httpClient
	clientCopy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &runtimeAgentHTTPClient{
		controlToken: controlToken,
		httpClient:   &clientCopy,
	}
}

func (c *runtimeAgentHTTPClient) Health(ctx context.Context, endpoint string) error {
	return c.do(ctx, http.MethodGet, endpoint, "/v1/health", nil, nil)
}

func (c *runtimeAgentHTTPClient) CreateGateway(ctx context.Context, endpoint string, req RuntimeAgentCreateGatewayRequest) (*RuntimeAgentCreateGatewayResponse, error) {
	var resp RuntimeAgentCreateGatewayResponse
	if err := c.do(ctx, http.MethodPost, endpoint, "/v1/gateways", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *runtimeAgentHTTPClient) DeleteGateway(ctx context.Context, endpoint, gatewayID string) error {
	return c.do(ctx, http.MethodDelete, endpoint, "/v1/gateways/"+url.PathEscape(gatewayID), nil, nil)
}

func (c *runtimeAgentHTTPClient) Drain(ctx context.Context, endpoint string) error {
	return c.do(ctx, http.MethodPost, endpoint, "/v1/drain", map[string]bool{"draining": true}, nil)
}

func (c *runtimeAgentHTTPClient) ResyncInstanceSkills(ctx context.Context, endpoint string, instanceID int, mode string) error {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "full"
	}
	body := map[string]any{
		"instance_id": instanceID,
		"mode":        mode,
		"trigger":     "manual",
	}
	return c.do(ctx, http.MethodPost, endpoint, "/v1/skills/resync", body, nil)
}

func (c *runtimeAgentHTTPClient) do(ctx context.Context, method, endpoint, path string, body any, out any) error {
	endpoint = strings.TrimRight(endpoint, "/")
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("X-ClawManager-Control-Token", c.controlToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if resp.StatusCode == http.StatusConflict {
			return fmt.Errorf("runtime agent conflict: %s", string(msg))
		}
		return fmt.Errorf("runtime agent status %d: %s", resp.StatusCode, string(msg))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
