package handlers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"clawreef/internal/runtimeauth"
)

type RuntimeServiceOperation struct {
	Check func(context.Context) error
	Serve func(http.ResponseWriter, *http.Request)
	Close func() error
}
type RuntimeServiceResolver func(context.Context, runtimeauth.Identity, *http.Request) (RuntimeServiceOperation, error)

// RuntimeServicesHandler authenticates only the services audience and owns the
// active capability lifetime. Each resolver must authorize one fixed operation
// against trusted storage before returning a handler; caller identity is ignored.
type RuntimeServicesHandler struct {
	verifier RuntimeCapabilityVerifier
	resolve  RuntimeServiceResolver
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	closing  bool
	workers  sync.WaitGroup
	limit    chan struct{}
}

func NewRuntimeServicesHandler(verifier RuntimeCapabilityVerifier, resolve RuntimeServiceResolver) (*RuntimeServicesHandler, error) {
	if verifier == nil || resolve == nil {
		return nil, errors.New("invalid managed service configuration")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &RuntimeServicesHandler{verifier: verifier, resolve: resolve, ctx: ctx, cancel: cancel, limit: make(chan struct{}, 256)}, nil
}

func (h *RuntimeServicesHandler) Close() {
	h.mu.Lock()
	h.closing = true
	h.cancel()
	h.mu.Unlock()
	h.workers.Wait()
}

func rejectRuntimeService(w http.ResponseWriter, r *http.Request, status int) {
	w.Header().Set("Connection", "close")
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if r.Body != nil {
		_ = r.Body.Close()
	}
	http.Error(w, "managed service unavailable", status)
}

func (h *RuntimeServicesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	h.mu.Lock()
	if h.closing {
		h.mu.Unlock()
		rejectRuntimeService(w, r, 503)
		return
	}
	select {
	case h.limit <- struct{}{}:
	default:
		h.mu.Unlock()
		rejectRuntimeService(w, r, 503)
		return
	}
	h.workers.Add(1)
	h.mu.Unlock()
	defer h.workers.Done()
	defer func() { <-h.limit }()
	size := 0
	for key, values := range r.Header {
		size += len(key)
		for _, value := range values {
			size += len(value)
		}
	}
	if size > 32<<10 {
		rejectRuntimeService(w, r, 431)
		return
	}
	if r.URL == nil || r.URL.IsAbs() || r.URL.Opaque != "" || r.URL.User != nil || r.URL.RawPath != "" || path.Clean(r.URL.Path) != r.URL.Path || strings.ContainsAny(r.URL.Path, "\\\x00") || !strings.HasPrefix(r.URL.Path, "/api/v1/runtime/services/") {
		rejectRuntimeService(w, r, 400)
		return
	}
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		rejectRuntimeService(w, r, 401)
		return
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) > 4096 {
		rejectRuntimeService(w, r, 401)
		return
	}
	checkCtx, checkCancel := context.WithTimeout(r.Context(), 2*time.Second)
	identity, err := h.verifier.Verify(checkCtx, parts[1], runtimeauth.AudienceServices)
	if err != nil || identity.InstanceID < 1 || identity.UserID < 1 || identity.Generation < 1 || identity.PodUID == "" {
		checkCancel()
		rejectRuntimeService(w, r, 403)
		return
	}
	operation, err := h.resolve(checkCtx, identity, r)
	defer func() {
		if operation.Close != nil {
			if err := operation.Close(); err != nil {
				log.Print("managed service cleanup failed")
			}
		}
	}()
	if err != nil || operation.Serve == nil || checkCtx.Err() != nil {
		checkCancel()
		rejectRuntimeService(w, r, 403)
		return
	}
	checkCancel()
	ctx, cancel := context.WithCancel(r.Context())
	stopLifetime := context.AfterFunc(h.ctx, cancel)
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
				checkCtx, checkCancel := context.WithTimeout(ctx, 2*time.Second)
				current, err := h.verifier.Verify(checkCtx, parts[1], runtimeauth.AudienceServices)
				if err == nil && current == identity && operation.Check != nil {
					err = operation.Check(checkCtx)
				}
				failed := err != nil || current != identity || checkCtx.Err() != nil
				checkCancel()
				if failed {
					cancel()
					return
				}
			}
		}
	}()
	controller := http.NewResponseController(w)
	ioDone := make(chan struct{})
	stopIO := context.AfterFunc(ctx, func() {
		defer close(ioDone)
		_ = controller.SetReadDeadline(time.Now())
		_ = controller.SetWriteDeadline(time.Now())
	})
	defer func() {
		if !stopIO() {
			<-ioDone
		}
		cancel()
		stopLifetime()
		<-watchDone
	}()
	operation.Serve(w, r.WithContext(ctx))
}
