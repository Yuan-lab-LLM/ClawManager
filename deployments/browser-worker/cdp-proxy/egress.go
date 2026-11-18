package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/iamlovingit/clawmanager-agent/netbroker"
)

// The immutable scope is supplied by the trusted backend, separate from the
// browser's filesystem and environment. This credential is never served locally.
func newWorkerEgress(ctx context.Context, proxyURL, scope, podUID, key string) (*netbroker.Broker, error) {
	errConfig := errors.New("invalid protected browser egress configuration")
	if len(scope) > 1024 || len(key) < 32 || len(key) > 4096 || len(podUID) == 0 || len(podUID) > 128 || strings.IndexFunc(podUID, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}) >= 0 {
		return nil, errConfig
	}
	var identity struct {
		InstanceID int    `json:"instance_id"`
		UserID     int    `json:"user_id"`
		Generation int    `json:"generation"`
		PodUID     string `json:"pod_uid"`
	}
	decoder := json.NewDecoder(strings.NewReader(scope))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&identity) != nil || decoder.Decode(new(any)) != io.EOF || identity.InstanceID < 1 || identity.UserID < 1 || identity.Generation < 1 || identity.PodUID == "" || len(identity.PodUID) > 128 {
		return nil, errConfig
	}
	credential := "bw1." + base64.RawURLEncoding.EncodeToString([]byte(scope)) + "." + podUID + "." + key
	return netbroker.NewFixedEgress(ctx, "127.0.0.1", proxyURL, credential)
}
