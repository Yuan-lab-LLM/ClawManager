package services

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"

	"clawreef/internal/runtimeauth"
)

// RuntimeBrowserEgressVerifier is installed only on the dedicated worker proxy.
// A worker credential cannot authenticate model/services/control endpoints.
type RuntimeBrowserEgressVerifier struct {
	current runtimeauth.Lookup
	adapter *RuntimeBrowserAdapter
}

func NewRuntimeBrowserEgressVerifier(current runtimeauth.Lookup, adapter *RuntimeBrowserAdapter) (*RuntimeBrowserEgressVerifier, error) {
	if current == nil || adapter == nil {
		return nil, errRuntimeBrowser
	}
	return &RuntimeBrowserEgressVerifier{current, adapter}, nil
}

func (v *RuntimeBrowserEgressVerifier) Verify(ctx context.Context, token, audience string) (runtimeauth.Identity, error) {
	if v == nil || v.current == nil || v.adapter == nil || audience != runtimeauth.AudienceEgress || len(token) > 4096 {
		return runtimeauth.Identity{}, errRuntimeBrowser
	}
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != "bw1" || len(parts[2]) == 0 || len(parts[2]) > 128 || len(parts[3]) < 32 {
		return runtimeauth.Identity{}, errRuntimeBrowser
	}
	encoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(encoded) > 1024 || base64.RawURLEncoding.EncodeToString(encoded) != parts[1] {
		return runtimeauth.Identity{}, errRuntimeBrowser
	}
	var scope runtimeBrowserScope
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&scope) != nil || decoder.Decode(new(any)) != io.EOF || scope.InstanceID < 1 || scope.UserID < 1 || scope.Generation < 1 || scope.PodUID == "" || len(scope.PodUID) > 128 {
		return runtimeauth.Identity{}, errRuntimeBrowser
	}
	expected := runtimeauth.Identity{InstanceID: scope.InstanceID, UserID: scope.UserID, Generation: scope.Generation, PodUID: scope.PodUID}
	current, err := v.current(ctx, expected.InstanceID)
	if err != nil || current != expected {
		return runtimeauth.Identity{}, errRuntimeBrowser
	}
	snapshot, err := v.adapter.current(ctx, current)
	if err != nil || snapshot.podUID != parts[2] || subtle.ConstantTimeCompare([]byte(snapshot.credential), []byte(parts[3])) != 1 || ctx.Err() != nil {
		return runtimeauth.Identity{}, errRuntimeBrowser
	}
	return current, nil
}
