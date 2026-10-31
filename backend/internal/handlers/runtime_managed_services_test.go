package handlers

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/runtimeauth"
)

type managedServiceVerifier func(context.Context, string, string) (runtimeauth.Identity, error)

func (f managedServiceVerifier) Verify(ctx context.Context, token, audience string) (runtimeauth.Identity, error) {
	return f(ctx, token, audience)
}

func TestRuntimeServicesRequireServicesAudienceAndTrustedIdentity(t *testing.T) {
	id := runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod"}
	verifier := managedServiceVerifier(func(ctx context.Context, token, audience string) (runtimeauth.Identity, error) {
		if token != "v1.services" || audience != runtimeauth.AudienceServices {
			return runtimeauth.Identity{}, errors.New("denied")
		}
		return id, nil
	})
	calls := 0
	h, err := NewRuntimeServicesHandler(verifier, func(ctx context.Context, identity runtimeauth.Identity, r *http.Request) (RuntimeServiceOperation, error) {
		calls++
		if identity != id {
			t.Error("caller header granted identity")
		}
		return RuntimeServiceOperation{Serve: func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "own-service") }}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	for _, token := range []string{"", "v1.model", "v1.egress", "v1.services"} {
		r := httptest.NewRequest("GET", "/api/v1/runtime/services/json/version", nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		r.Header.Set("X-ClawManager-Instance-ID", "999")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if token == "v1.services" {
			if w.Code != 200 || w.Body.String() != "own-service" {
				t.Fatal("validservicefailed")
			}
		} else if w.Code < 400 {
			t.Fatal("wrongpurposeaccepted")
		}
	}
	if calls != 1 {
		t.Fatal("unauthorizedrequestresolvedservice")
	}
}

func TestRuntimeServicesMembershipRevocationInterruptsBlockedResponse(t *testing.T) {
	var revoked atomic.Bool
	done := make(chan struct{})
	verifier := managedServiceVerifier(func(context.Context, string, string) (runtimeauth.Identity, error) {
		return runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod"}, nil
	})
	h, err := NewRuntimeServicesHandler(verifier, func(context.Context, runtimeauth.Identity, *http.Request) (RuntimeServiceOperation, error) {
		return RuntimeServiceOperation{
			Check: func(context.Context) error {
				if revoked.Load() {
					return errors.New("removed")
				}
				return nil
			},
			Serve: func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				_, _ = io.Copy(w, bytes.NewReader(make([]byte, 16<<20)))
			},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	server := httptest.NewServer(h)
	defer server.Close()
	c, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(9 * time.Second))
	_, _ = io.WriteString(c, "GET /api/v1/runtime/services/json/list HTTP/1.1\r\nHost: local\r\nAuthorization: Bearer v1.services\r\n\r\n")
	if response, err := http.ReadResponse(bufio.NewReader(c), nil); err != nil || response.StatusCode != 200 {
		t.Fatal("servicenotready")
	}
	revoked.Store(true)
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("revokedserviceblockedwriting")
	}
}
