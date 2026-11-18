package middleware

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type mergeGatewayKeyRepo struct {
	repository.GatewayKeyRepository
	key           *models.GatewayKey
	err           error
	lookups       int
	policyErr     error
	policyLookups []string
}

func (r *mergeGatewayKeyRepo) GetByToken(token string) (*models.GatewayKey, error) {
	r.lookups++
	if r.err != nil {
		return nil, r.err
	}
	if token != "igt_merge-test" {
		return nil, nil
	}
	return r.key, nil
}

func (r *mergeGatewayKeyRepo) GetByKeyID(keyID string) (*models.GatewayKey, error) {
	r.policyLookups = append(r.policyLookups, keyID)
	return r.key, r.policyErr
}

func (*mergeGatewayKeyRepo) Touch(context.Context, int, time.Time) error { return nil }

func TestGatewayKeysPreserveCapabilityProtectionAndGovernance(t *testing.T) {
	for _, kind := range []string{"unprotected-instance", "protected-instance", "predicate-error", "user-key", "ownerless", "disabled", "revoked", "expired", "lookup-error"} {
		t.Run(kind, func(t *testing.T) {
			instances, verifier, _, _ := gatewayCapabilityFixture(t)
			ownerID, instanceID := 1, 8
			groups := `["openai"]`
			key := &models.GatewayKey{ID: 12, KeyID: "key-12", KeyType: models.GatewayKeyTypeManagedInstance, Status: models.GatewayKeyStatusActive, OwnerUserID: &ownerID, InstanceID: &instanceID, AllowedModelGroups: &groups, RPMLimit: 10, TPMLimit: 20, ConcurrentRequestsLimit: 3}
			keys := &mergeGatewayKeyRepo{key: key}
			want := 401
			switch kind {
			case "unprotected-instance":
				want = 204
			case "user-key":
				key.InstanceID = nil
				want = 204
			case "ownerless":
				key.InstanceID = nil
				key.OwnerUserID = nil
			case "disabled":
				key.Status = models.GatewayKeyStatusDisabled
			case "revoked":
				key.Status = models.GatewayKeyStatusRevoked
			case "expired":
				expiry := time.Now().Add(-time.Minute)
				key.ExpiresAt = &expiry
			case "lookup-error":
				keys.err = errors.New("database unavailable")
			}
			predicate := func(ctx context.Context, id int) (bool, error) {
				if kind == "user-key" || kind == "ownerless" {
					t.Error("user key reached runtime protection lookup")
				}
				if id != 8 {
					t.Errorf("protection lookup for %d, want 8", id)
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Error("protection lookup lacks deadline")
				}
				if kind == "predicate-error" {
					return false, errors.New("protection unavailable")
				}
				return kind == "protected-instance", nil
			}
			rec, metadata := gatewayCapabilityRequest(GatewayAuthWithRuntimeCapabilitiesAndGatewayKeys(instances, keys, verifier, predicate), "igt_merge-test")
			if rec.Code != want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, want, rec.Body.String())
			}
			if instances.rawCalls != 0 {
				t.Fatal("governed credential fell back to lifecycle token authentication")
			}
			if want != 204 {
				return
			}
			for name, expected := range map[string]any{"gatewayAuthType": "virtual_key", "userID": 1, "gatewayKeyID": "key-12", "gatewayKeyDBID": 12, "gatewayKeyRPMLimit": 10, "gatewayKeyTPMLimit": 20, "gatewayKeyConcurrencyLimit": 3} {
				if metadata[name] != expected {
					t.Errorf("%s=%v want=%v", name, metadata[name], expected)
				}
			}
			if !reflect.DeepEqual(metadata["gatewayAllowedModelGroups"], []string{"openai"}) {
				t.Errorf("allowed models lost: %v", metadata)
			}
			if key.InstanceID != nil && (metadata["instanceID"] != 8 || metadata["runtimeGeneration"] != 3) {
				t.Errorf("instance attribution lost: %v", metadata)
			}
			if key.InstanceID == nil && metadata["instanceID"] != nil {
				t.Errorf("user key acquired instance identity: %v", metadata)
			}
		})
	}
}

func TestGatewayCapabilityDoesNotFallBackToGatewayKey(t *testing.T) {
	instances, verifier, model, egress := gatewayCapabilityFixture(t)
	keys := &mergeGatewayKeyRepo{err: errors.New("must not consult key repository")}
	auth := GatewayAuthWithRuntimeCapabilitiesAndGatewayKeys(instances, keys, verifier, func(context.Context, int) (bool, error) { return true, nil })
	for _, tc := range []struct {
		token string
		want  int
	}{{model, 204}, {egress, 401}, {"v1.invalid", 401}} {
		rec, metadata := gatewayCapabilityRequest(auth, tc.token)
		if rec.Code != tc.want {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.want, rec.Body.String())
		}
		if tc.want == 204 && metadata["runtimeGeneration"] != 3 {
			t.Errorf("capability lost runtime generation: %v", metadata)
		}
	}
	if keys.lookups != 0 || instances.rawCalls != 0 {
		t.Fatalf("capability consulted legacy credentials: keys=%d raw=%d", keys.lookups, instances.rawCalls)
	}
	if !reflect.DeepEqual(keys.policyLookups, []string{"legacy-instance-8"}) {
		t.Fatalf("only verified model capability should resolve managed policy: %v", keys.policyLookups)
	}
}

func TestGatewayKeysPreservePublicLegacyAuthentication(t *testing.T) {
	instances, _, _, _ := gatewayCapabilityFixture(t)
	ownerID := 1
	keys := &mergeGatewayKeyRepo{key: &models.GatewayKey{ID: 12, KeyID: "key-12", OwnerUserID: &ownerID, Status: models.GatewayKeyStatusActive}}
	auth := GatewayAuthWithGatewayKeys(instances, keys)
	for _, tc := range []struct{ token, wantType string }{{"igt_merge-test", "virtual_key"}, {*instances.instance.AccessToken, "instance"}} {
		rec, metadata := gatewayCapabilityRequest(auth, tc.token)
		if rec.Code != 204 || metadata["gatewayAuthType"] != tc.wantType {
			t.Fatalf("status=%d metadata=%v body=%s", rec.Code, metadata, rec.Body.String())
		}
	}
}

func TestGatewayCapabilityAppliesVerifiedInstanceManagedPolicy(t *testing.T) {
	for _, kind := range []string{"active", "missing", "disabled", "revoked", "expired", "wrong-owner", "missing-owner", "wrong-instance", "missing-instance", "wrong-type", "lookup-error"} {
		t.Run(kind, func(t *testing.T) {
			instances, verifier, token, _ := gatewayCapabilityFixture(t)
			ownerID, instanceID := 1, 8
			groups := `["openai"]`
			key := &models.GatewayKey{ID: 12, KeyID: "legacy-instance-8", KeyType: models.GatewayKeyTypeManagedInstance, Status: models.GatewayKeyStatusActive, OwnerUserID: &ownerID, InstanceID: &instanceID, AllowedModelGroups: &groups, RPMLimit: 10, TPMLimit: 20, ConcurrentRequestsLimit: 3}
			keys := &mergeGatewayKeyRepo{key: key, err: errors.New("capability must not authenticate a legacy credential")}
			want := 401
			switch kind {
			case "active":
				want = 204
			case "missing":
				keys.key = nil
				want = 204
			case "disabled":
				key.Status = models.GatewayKeyStatusDisabled
			case "revoked":
				key.Status = models.GatewayKeyStatusRevoked
			case "expired":
				expiry := time.Now().Add(-time.Minute)
				key.ExpiresAt = &expiry
			case "wrong-owner":
				ownerID = 99
			case "missing-owner":
				key.OwnerUserID = nil
			case "wrong-instance":
				instanceID = 99
			case "missing-instance":
				key.InstanceID = nil
			case "wrong-type":
				key.KeyType = models.GatewayKeyTypeUserCreated
			case "lookup-error":
				keys.policyErr = errors.New("managed policy unavailable")
			}
			rec, metadata := gatewayCapabilityRequest(GatewayAuthWithRuntimeCapabilitiesAndGatewayKeys(instances, keys, verifier, func(context.Context, int) (bool, error) { return true, nil }), token)
			if rec.Code != want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, want, rec.Body.String())
			}
			if keys.lookups != 0 || instances.rawCalls != 0 {
				t.Fatalf("capability fell back to legacy authentication: keys=%d raw=%d", keys.lookups, instances.rawCalls)
			}
			if !reflect.DeepEqual(keys.policyLookups, []string{"legacy-instance-8"}) {
				t.Fatalf("policy not resolved from verified instance: %v", keys.policyLookups)
			}
			if want != 204 {
				return
			}
			for name, expected := range map[string]any{"gatewayAuthType": "instance", "userID": 1, "instanceID": 8, "runtimeGeneration": 3} {
				if metadata[name] != expected {
					t.Errorf("%s=%v want=%v", name, metadata[name], expected)
				}
			}
			if kind == "missing" {
				if metadata["gatewayKeyDBID"] != nil || metadata["gatewayKeyID"] != nil {
					t.Fatalf("missing migration key acquired policy: %v", metadata)
				}
				return
			}
			for name, expected := range map[string]any{"gatewayKeyID": "legacy-instance-8", "gatewayKeyDBID": 12, "gatewayKeyType": models.GatewayKeyTypeManagedInstance, "gatewayKeyRPMLimit": 10, "gatewayKeyTPMLimit": 20, "gatewayKeyConcurrencyLimit": 3} {
				if metadata[name] != expected {
					t.Errorf("%s=%v want=%v", name, metadata[name], expected)
				}
			}
			if !reflect.DeepEqual(metadata["gatewayAllowedModelGroups"], []string{"openai"}) {
				t.Errorf("managed model restriction lost: %v", metadata)
			}
		})
	}
}
