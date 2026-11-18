package handlers

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestPreviewBlockedCheckCancelsStreamButRetainsWorker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.txt")
	if err := os.WriteFile(path, []byte("body"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	check := func(context.Context) error {
		if calls.Add(1) == 1 {
			return nil
		}
		close(entered)
		<-release
		return nil
	}
	writer := &blockedEgressWriter{ResponseRecorder: httptest.NewRecorder(), writing: make(chan struct{}), release: make(chan struct{})}
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest("GET", "http://preview.test/file", nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		serveTeamPreviewContent(c, file, info, "static", time.Now().Add(time.Minute), check, file.Close)
	}()
	defer func() { close(release); writer.SetWriteDeadline(time.Now()); <-done }()
	select {
	case <-writer.writing:
	case <-time.After(time.Second):
		t.Fatal("preview did not start writing")
	}
	select {
	case <-entered:
	case <-time.After(6 * time.Second):
		t.Fatal("periodic check did not start")
	}
	select {
	case <-writer.release:
	case <-time.After(3 * time.Second):
		t.Fatal("non-context-aware check suppressed two-second stream cancellation")
	}
	select {
	case <-done:
		t.Fatal("preview returned before blocked check joined")
	default:
	}
}
