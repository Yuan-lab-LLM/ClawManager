package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/runtimeauth"
	"github.com/gin-gonic/gin"
)

type lifecycleVerifier struct{ calls atomic.Int32 }

func (v *lifecycleVerifier) Verify(context.Context, string, string) (runtimeauth.Identity, error) {
	if v.calls.Add(1) > 1 {
		return runtimeauth.Identity{}, errors.New("revoked")
	}
	return runtimeauth.Identity{InstanceID: 8, UserID: 1, Generation: 3, PodUID: "pod-test"}, nil
}

func TestGatewayModelStreamCancelsAfterRevocation(t *testing.T) {
	repo, _, token, _ := gatewayCapabilityFixture(t)
	verifier := &lifecycleVerifier{}
	r := gin.New()
	r.Use(GatewayAuthWithRuntimeCapabilities(repo, verifier, func(context.Context, int) (bool, error) { return true, nil }))
	entered := make(chan struct{})
	exited := make(chan struct{})
	r.GET("/", func(c *gin.Context) { close(entered); <-c.Request.Context().Done(); close(exited) })
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token)
	done := make(chan struct{})
	go func() { r.ServeHTTP(httptest.NewRecorder(), req); close(done) }()
	<-entered
	select {
	case <-exited:
		if ctx.Err() != nil {
			t.Fatal("stream waited for caller timeout instead of revocation")
		}
	case <-time.After(8 * time.Second):
		cancel()
		<-done
		t.Fatal("revoked model stream still active")
	}
	<-done
}
