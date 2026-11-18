package handlers

import (
	"context"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Cancellation interrupts transport independently of a non-context-aware file
// check. The caller retains its admission until this function joins all checks.
func serveTeamPreviewContent(c *gin.Context, file *os.File, info os.FileInfo, mode string, expires time.Time, check func(context.Context) error, interrupt func() error) {
	ctx, cancel := context.WithCancel(c.Request.Context())
	if !expires.IsZero() {
		deadline, finish := context.WithDeadline(ctx, expires)
		defer finish()
		ctx = deadline
	}
	controller := http.NewResponseController(c.Writer)
	ioDone := make(chan struct{})
	stopIO := context.AfterFunc(ctx, func() {
		defer close(ioDone)
		_ = controller.SetWriteDeadline(time.Now())
		_ = controller.SetReadDeadline(time.Now())
		_ = interrupt()
	})
	checked := func() error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		bounded, finish := context.WithTimeout(ctx, 2*time.Second)
		defer finish()
		deadlineDone := make(chan struct{})
		stopDeadline := context.AfterFunc(bounded, func() { defer close(deadlineDone); cancel() })
		err := check(bounded)
		if !stopDeadline() {
			<-deadlineDone
		}
		if bounded.Err() != nil {
			return bounded.Err()
		}
		return err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if checked() != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		if !stopIO() {
			<-ioDone
		}
		cancel()
		<-done
	}()
	if checked() != nil {
		c.Status(http.StatusForbidden)
		return
	}
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(info.Name())))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	setTeamPreviewHeaders(c.Writer.Header(), contentType, mode)
	// Only v3 grants have an expiry. Preserve the legacy mode contract while
	// migrated static HTML stays inert on its shared bootstrap origin.
	if mode == "static" && !expires.IsZero() {
		c.Header("Content-Security-Policy", "sandbox; default-src 'none'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	}
	http.ServeContent(c.Writer, c.Request.WithContext(ctx), info.Name(), info.ModTime(), file)
}
