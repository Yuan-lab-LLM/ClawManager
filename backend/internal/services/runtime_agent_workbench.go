package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type WorkbenchOperationError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *WorkbenchOperationError) Error() string { return e.Message }
func (c *runtimeAgentHTTPClient) Workbench(ctx context.Context, endpoint string, request WorkbenchRequest) (json.RawMessage, error) {
	payload, err := json.Marshal(request)
	if err != nil || len(payload) > 1<<20 {
		return nil, errors.New("invalid workbench request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/v1/workbench", bytes.NewReader(payload))
	if err != nil {
		return nil, ErrWorkbenchUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClawManager-Control-Token", c.controlToken)
	client := *c.httpClient
	client.Timeout = 125 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		return nil, ErrWorkbenchUnavailable
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 {
		return nil, ErrWorkbenchUnavailable
	}
	if resp.StatusCode == 404 || resp.StatusCode == 405 || resp.StatusCode == 501 {
		return nil, ErrRuntimeAgentUnsupported
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure struct {
			Error WorkbenchOperationError `json:"error"`
		}
		if json.Unmarshal(body, &failure) == nil && failure.Error.Code != "" && len(failure.Error.Message) <= 512 {
			if failure.Error.Code == "stale_generation" {
				return nil, ErrWorkbenchStale
			}
			return nil, &failure.Error
		}
		return nil, ErrWorkbenchUnavailable
	}
	if !json.Valid(body) {
		return nil, ErrWorkbenchUnavailable
	}
	return json.RawMessage(body), nil
}
