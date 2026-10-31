package backupgate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInternalHTTPBoundaryRejectsBeforeAnyBusinessHandler(t *testing.T) {
	restore := ConfigureWorkBoundary(func(context.Context) (func(), error) { return nil, ErrMaintenance })
	defer restore()
	called := 0
	handler := HTTPBoundary(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called++; w.WriteHeader(http.StatusOK) }), "northbound.core_http")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/teams", nil))
	if response.Code != http.StatusServiceUnavailable || called != 0 {
		t.Fatal("Core mutation bypassed maintenance", response.Code, called)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if response.Code != http.StatusOK || called != 1 {
		t.Fatal("maintenance blocked liveness", response.Code, called)
	}
}
