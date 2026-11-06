package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRuntimeAgentWorkbenchErrorContract(t *testing.T) {
	for _, tc := range []struct {
		name               string
		status             int
		body, code         string
		stale, unavailable bool
	}{
		{"busy", 423, `{"error":{"code":"instance_busy","message":"Instance is stopping or unavailable."}}`, "instance_busy", false, false},
		{"stop unconfirmed", 423, `{"error":{"code":"stop_unconfirmed","message":"Business process teardown is not confirmed."}}`, "stop_unconfirmed", false, false},
		{"cancelled", 409, `{"error":{"code":"cancelled","message":"Operation was cancelled."}}`, "cancelled", false, false},
		{"timeout", 504, `{"error":{"code":"timeout","message":"Operation timed out."}}`, "timeout", false, false},
		{"generation changed", 409, `{"error":{"code":"stale_generation","message":"Instance assignment has changed."}}`, "", true, false},
		{"explicit generation code", 400, `{"error":{"code":"stale_generation","message":"Instance assignment has changed."}}`, "", true, false},
		{"legacy conflict", 409, `conflict`, "", false, true},
		{"malformed locked", 423, `{"error":"locked"}`, "", false, true},
		{"unknown typed conflict", 409, `{"error":{"code":"custom_failure","message":"Safe worker message"}}`, "custom_failure", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := NewRuntimeAgentClient("test").(*runtimeAgentHTTPClient)
			_, err := client.Workbench(context.Background(), server.URL, WorkbenchRequest{Operation: "capabilities"})
			if errors.Is(err, ErrWorkbenchStale) != tc.stale {
				t.Fatalf("stale = %v, want %v: %v", errors.Is(err, ErrWorkbenchStale), tc.stale, err)
			}
			if errors.Is(err, ErrWorkbenchUnavailable) != tc.unavailable {
				t.Fatalf("unavailable = %v, want %v: %v", errors.Is(err, ErrWorkbenchUnavailable), tc.unavailable, err)
			}
			if tc.code != "" {
				var operation *WorkbenchOperationError
				if !errors.As(err, &operation) || operation.Code != tc.code {
					t.Fatalf("got %v, want operation %s", err, tc.code)
				}
				if operation.Message == "" {
					t.Fatal("worker message lost")
				}
			}
			if calls != 1 {
				t.Fatalf("operation replayed %d times", calls)
			}
		})
	}
}
