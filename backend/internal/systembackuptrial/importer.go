package systembackuptrial

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

const deliveryDeadline = 4 * time.Minute

func (s *Service) importSocket() string {
	socket := s.value("IMPORT_SOCKET")
	if !filepath.IsAbs(socket) || filepath.Clean(socket) != socket {
		return ""
	}
	return socket
}

func (s *Service) importSocketReady() bool {
	info, err := os.Lstat(s.importSocket())
	return err == nil && info.Mode()&os.ModeSocket != 0
}

func (s *Service) handleImport(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.Request.Header.Get("Content-Type") != transferMediaType ||
		c.Request.ContentLength <= 0 || c.Request.ContentLength > maxTransferSize {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_trial_artifact_package"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), deliveryDeadline)
	defer cancel()
	environment := s.environment(ctx, false)
	if environment.Profile == nil || environment.missing("target") != "" || !s.importSocketReady() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "trial_artifact_import_unavailable"})
		return
	}
	if missing := s.restoreReadiness(ctx); len(missing) != 0 {
		c.JSON(http.StatusServiceUnavailable, Readiness{RestoreMissing: missing})
		return
	}
	s.mu.Lock()
	if s.checking || (s.task != nil && s.task.Status == "running") {
		s.mu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"code": "trial_task_running"})
		return
	}
	s.checking = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.checking = false
		s.mu.Unlock()
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://trial-import/delivery", http.MaxBytesReader(c.Writer, c.Request.Body, maxTransferSize))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_trial_artifact_package"})
		return
	}
	req.ContentLength = c.Request.ContentLength
	req.Header.Set("Content-Type", transferMediaType)
	response, err := privateSocketClient(s.importSocket()).Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": "trial_artifact_import_unavailable"})
		return
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusBadRequest:
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_trial_artifact_package"})
	case http.StatusConflict:
		c.JSON(http.StatusConflict, gin.H{"code": "trial_artifact_import_conflict"})
	case http.StatusOK:
		raw, err := io.ReadAll(io.LimitReader(response.Body, 4097))
		var handoff deliveryHandoff
		if err != nil || len(raw) > 4096 || json.Unmarshal(raw, &handoff) != nil || !handoff.valid(*environment.Profile) {
			c.JSON(http.StatusBadGateway, gin.H{"code": "trial_artifact_import_unavailable"})
			return
		}
		c.JSON(http.StatusOK, handoff)
	default:
		c.JSON(http.StatusBadGateway, gin.H{"code": "trial_artifact_import_unavailable"})
	}
}

// RunArtifactImporter gives only this process the writable archive mount. Its
// socket is shared with the target app, which opens the archive read-only.
func (s *Service) RunArtifactImporter(ctx context.Context) error {
	profile, err := s.environmentProfile()
	path, directory := s.importSocket(), s.value("IMPORT_WRITE_DIR")
	if err != nil || profile.Side != "target" || path == "" || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return errConfig
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return errConfig
	}
	defer root.Close()
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return errConfig
		}
		if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
			conn.Close()
			return errConfig
		} else if !errors.Is(err, syscall.ECONNREFUSED) {
			return errConfig
		}
		if os.Remove(path) != nil {
			return errConfig
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errConfig
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return errConfig
	}
	defer listener.Close()
	if os.Chmod(path, 0660) != nil {
		return errConfig
	}
	server := &http.Server{
		Handler: artifactImporter(root, profile), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: deliveryDeadline, WriteTimeout: deliveryDeadline, MaxHeaderBytes: 4096,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			deadline, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if server.Shutdown(deadline) != nil {
				_ = server.Close()
			}
		case <-done:
		}
	}()
	err = server.Serve(listener)
	close(done)
	<-stopped
	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func artifactImporter(root *os.Root, profile trialEnvironment) http.Handler {
	mux := http.NewServeMux()
	var upload sync.Mutex
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !upload.TryLock() {
			w.WriteHeader(http.StatusConflict)
			return
		}
		defer upload.Unlock()
		if err := probeImporterWrite(root); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /delivery", func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Content-Type") != transferMediaType ||
			request.ContentLength <= 0 || request.ContentLength > maxTransferSize {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !upload.TryLock() {
			w.WriteHeader(http.StatusConflict)
			return
		}
		defer upload.Unlock()
		body := http.MaxBytesReader(w, request.Body, maxTransferSize)
		handoff, err := receiveDelivery(request.Context(), root, profile, body)
		switch {
		case errors.Is(err, errTransfer):
			w.WriteHeader(http.StatusBadRequest)
		case errors.Is(err, errTransferConflict):
			w.WriteHeader(http.StatusConflict)
		case err != nil:
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(handoff)
		}
	})
	return mux
}

func probeImporterWrite(root *os.Root) (err error) {
	marker := ".trial-import-probe-" + newID()
	file, err := root.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, root.Remove(marker)) }()
	_, writeErr := file.WriteString(marker)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	actual, err := root.ReadFile(marker)
	if err != nil || string(actual) != marker {
		return errReadback
	}
	return nil
}
