package handlers

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"clawreef/internal/runtimeauth"
	"clawreef/internal/services"
)

func validRuntimeRedisUpgrade(r *http.Request) bool {
	if r.Method != http.MethodPost || r.URL.Path != "/api/v1/runtime/services/team/redis" || r.URL.RawQuery != "" || r.ContentLength != 0 || len(r.TransferEncoding) != 0 || len(r.Header.Values("Upgrade")) != 1 || !strings.EqualFold(r.Header.Get("Upgrade"), "clawmanager-redis") {
		return false
	}
	for _, line := range r.Header.Values("Connection") {
		for _, token := range strings.Split(line, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
				return true
			}
		}
	}
	return false
}

type runtimeRedisSession interface {
	Relay(context.Context, net.Conn, io.Reader) error
	Close() error
}

func runtimeRedisOperation(session runtimeRedisSession) RuntimeServiceOperation {
	return RuntimeServiceOperation{Close: session.Close, Serve: func(w http.ResponseWriter, r *http.Request) {
		conn, buffered, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		joined := make(chan struct{})
		stop := context.AfterFunc(r.Context(), func() { defer close(joined); conn.Close() })
		defer func() {
			if !stop() {
				<-joined
			}
		}()
		if r.Context().Err() != nil {
			return
		}
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := buffered.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: clawmanager-redis\r\nCache-Control: no-store\r\n\r\n"); err != nil {
			return
		}
		if buffered.Flush() != nil || conn.SetDeadline(time.Time{}) != nil {
			return
		}
		_ = session.Relay(r.Context(), conn, buffered.Reader)
	}}
}

func NewRuntimeManagedServicesResolver(redis *services.ManagedTeamRedis, browser *services.RuntimeBrowserAdapter, preview *services.ManagedTeamPreview) (RuntimeServiceResolver, error) {
	if redis == nil || browser == nil || preview == nil {
		return nil, errors.New("invalid managed runtime providers")
	}
	return func(ctx context.Context, id runtimeauth.Identity, r *http.Request) (RuntimeServiceOperation, error) {
		if r.URL.Path == "/api/v1/runtime/services/team/preview-sign" {
			return NewRuntimeTeamPreviewOperation(preview, id, r)
		}
		if r.URL.Path == "/api/v1/runtime/services/team/redis" {
			if !validRuntimeRedisUpgrade(r) {
				return RuntimeServiceOperation{}, errors.New("invalid managed Redis upgrade")
			}
			session, err := redis.Open(ctx, id)
			if err != nil {
				return RuntimeServiceOperation{}, err
			}
			return runtimeRedisOperation(session), nil
		}
		if !runtimeBrowserRoute(r.Method, strings.TrimPrefix(r.URL.Path, "/api/v1/runtime/services")) || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			return RuntimeServiceOperation{}, errors.New("unknown managed runtime operation")
		}
		binding, err := browser.Resolve(ctx, id)
		if err != nil {
			return RuntimeServiceOperation{}, err
		}
		return NewRuntimeBrowserOperation(binding)
	}, nil
}
