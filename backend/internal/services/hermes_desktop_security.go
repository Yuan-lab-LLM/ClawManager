package services

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"clawreef/internal/runtimeauth"
)

func (t *hermesDesktopTarget) platformCredential() string {
	if t.instance.AccessToken == nil {
		return ""
	}
	return *t.instance.AccessToken
}

func (t *hermesDesktopTarget) gatewayCredential() string {
	if t.instance.RuntimeSecurityProfile != "" {
		return t.nativeCredential
	}
	return t.platformCredential()
}

// Bind the target to both the authority's current assignment and the exact
// owner/generation/Pod observed by the BFF. A racing reassignment cannot issue
// a new owner's credential to an older authorization result.
func (s *HermesDesktopService) protectedHermesCredential(ctx context.Context, t *hermesDesktopTarget) (string, error) {
	a := s.config.RuntimeSecurity
	if a == nil || t.instance.RuntimeSecurityProfile != RuntimeSecurityProfile || t.pod.PodUID == nil || *t.pod.PodUID == "" {
		return "", ErrHermesDesktopUnavailable
	}
	credential, err := a.NativeGatewayCredentialForTarget(ctx, t.instance.ID, t.url)
	if err != nil {
		return "", ErrHermesDesktopUnavailable
	}
	expected := a.nativeCredential(runtimeauth.Identity{InstanceID: t.instance.ID, UserID: t.instance.UserID, Generation: t.binding.Generation, PodUID: *t.pod.PodUID})
	if subtle.ConstantTimeCompare([]byte(credential), []byte(expected)) != 1 {
		return "", ErrHermesDesktopUnavailable
	}
	return credential, nil
}

type hermesNativeIngressContextKey struct{}
type hermesNativeIngress struct {
	host, credential string
}

// Only backend-verified data may enter this private context. Every login,
// ticket and data request still uses the same literal native target; neither
// browser headers nor URLs carry the ingress credential.
func (s *HermesDesktopService) hermesUpstreamContext(ctx context.Context, t *hermesDesktopTarget) (context.Context, error) {
	if t.instance.RuntimeSecurityProfile == "" {
		return ctx, nil
	}
	credential, err := s.protectedHermesCredential(ctx, t)
	if err != nil || subtle.ConstantTimeCompare([]byte(credential), []byte(t.nativeCredential)) != 1 {
		return nil, ErrHermesDesktopUnavailable
	}
	return context.WithValue(ctx, hermesNativeIngressContextKey{}, hermesNativeIngress{host: t.url.Host, credential: credential}), nil
}

func setHermesNativeIngress(req *http.Request) {
	req.Header.Del(NativeIngressTokenHeader)
	bound, ok := req.Context().Value(hermesNativeIngressContextKey{}).(hermesNativeIngress)
	if !ok || req.URL == nil || req.URL.Host != bound.host || req.URL.User != nil ||
		(req.URL.Scheme != "http" && req.URL.Scheme != "ws") || !strings.HasPrefix(bound.credential, "ui1_") {
		return
	}
	req.Header.Set(NativeIngressTokenHeader, bound.credential)
}

// A context deadline alone cannot interrupt a repository implementation that
// ignores its context. Close I/O independently, then retain the caller until
// that check and the deadline callback have both returned.
func hermesDeadlineCheck(ctx context.Context, check func(context.Context) error, revoke func()) error {
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	callbackDone := make(chan struct{})
	stop := context.AfterFunc(checkCtx, func() { defer close(callbackDone); revoke() })
	err := check(checkCtx)
	if !stop() {
		<-callbackDone
	}
	if err == nil {
		err = checkCtx.Err()
	}
	if err != nil {
		revoke()
	}
	return err
}

type hermesOperationContextKey struct{}

// Protect login, response-body reads and ticket handshakes as one operation.
// Nested login calls share the outer guard. Cancellation interrupts HTTP I/O;
// a non-context-aware authority check remains joined before returning.
func (s *HermesDesktopService) protectedHermesOperation(ctx context.Context, t *hermesDesktopTarget, operation func(context.Context) error) (err error) {
	if t.instance.RuntimeSecurityProfile == "" || ctx.Value(hermesOperationContextKey{}) == t {
		return operation(ctx)
	}
	ioCtx, cancelIO := context.WithCancel(ctx)
	defer cancelIO()
	ioCtx = context.WithValue(ioCtx, hermesOperationContextKey{}, t)
	stop, joined := make(chan struct{}), make(chan struct{})
	var revoked atomic.Bool
	revoke := func() { revoked.Store(true); cancelIO() }
	check := func(c context.Context) error { _, e := s.hermesUpstreamContext(c, t); return e }
	go func() {
		defer close(joined)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ioCtx.Done():
				return
			case <-ticker.C:
				if hermesDeadlineCheck(ioCtx, check, revoke) != nil {
					return
				}
			}
		}
	}()
	defer func() {
		close(stop)
		<-joined
		if revoked.Load() || ctx.Err() != nil {
			err = ErrHermesDesktopUpstream
		}
	}()
	if err = operation(ioCtx); err != nil {
		return err
	}
	return hermesDeadlineCheck(ioCtx, check, revoke)
}
