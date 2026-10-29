package middleware

import (
	"context"
	"net/http"
	"time"

	"clawreef/internal/runtimeauth"
	"github.com/gin-gonic/gin"
)

// Model streams retain their authorization only while the assignment remains
// active. The verifier must honor context deadlines; production reads SQL and
// Kubernetes with that context rather than detaching a database goroutine.
func serveCapabilityModel(c *gin.Context, verifier RuntimeCapabilityVerifier, token string, identity runtimeauth.Identity) {
	ctx, cancel := context.WithCancel(c.Request.Context())
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				check, stop := context.WithTimeout(ctx, 2*time.Second)
				current, err := verifier.Verify(check, token, runtimeauth.AudienceModel)
				stop()
				if err != nil || current != identity {
					cancel()
					return
				}
			}
		}
	}()
	controller := http.NewResponseController(c.Writer)
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(closed)
		controller.SetReadDeadline(time.Now())
		controller.SetWriteDeadline(time.Now())
	})
	defer func() {
		// Unregister before normal cancellation so successful buffered responses
		// can still flush, and join callbacks before net/http reuses the writer.
		if !stop() {
			<-closed
		}
		cancel()
		<-watchDone
	}()
	c.Request = c.Request.WithContext(ctx)
	c.Next()
}
