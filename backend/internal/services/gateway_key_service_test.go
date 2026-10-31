package services

import (
	"context"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type rotationGatewayKeyRepository struct {
	repository.GatewayKeyRepository
	key         *models.GatewayKey
	rotateCalls int
	prefix      string
	tokenHash   string
}

func (r *rotationGatewayKeyRepository) GetByID(id int) (*models.GatewayKey, error) {
	if r.key == nil || r.key.ID != id {
		return nil, nil
	}
	copy := *r.key
	return &copy, nil
}

func (r *rotationGatewayKeyRepository) RotateCredential(_ context.Context, id int, prefix, tokenHash string) error {
	r.rotateCalls++
	r.prefix = prefix
	r.tokenHash = tokenHash
	if r.key != nil && r.key.ID == id {
		r.key.KeyPrefix = prefix
		r.key.TokenHash = tokenHash
	}
	return nil
}

func TestRotateGatewayKeyPreservesIdentityPolicyAndBudget(t *testing.T) {
	ownerID := 7
	repo := &rotationGatewayKeyRepository{key: &models.GatewayKey{
		ID:                      12,
		KeyID:                   "gk_stable",
		KeyPrefix:               "old-prefix",
		TokenHash:               "old-hash",
		KeyType:                 models.GatewayKeyTypeUserCreated,
		OwnerUserID:             &ownerID,
		RPMLimit:                30,
		TPMLimit:                4000,
		ConcurrentRequestsLimit: 3,
		BudgetLimit:             25,
		BudgetSpent:             8.5,
		BudgetPeriod:            models.GatewayKeyBudgetPeriodMonthly,
		Status:                  models.GatewayKeyStatusActive,
	}}
	service := &gatewayKeyService{repo: repo}

	rotated, raw, err := service.Rotate(context.Background(), repo.key.ID, ownerID)
	if err != nil {
		t.Fatalf("rotate gateway key: %v", err)
	}
	if repo.rotateCalls != 1 {
		t.Fatalf("credential rotation calls = %d, want 1", repo.rotateCalls)
	}
	if rotated.ID != 12 || rotated.KeyID != "gk_stable" {
		t.Fatalf("rotated identity = %d/%q, want stable row and key ID", rotated.ID, rotated.KeyID)
	}
	if rotated.RPMLimit != 30 || rotated.TPMLimit != 4000 || rotated.ConcurrentRequestsLimit != 3 || rotated.BudgetLimit != 25 || rotated.BudgetSpent != 8.5 {
		t.Fatalf("rotation changed policy or budget: %#v", rotated)
	}
	if !strings.HasPrefix(raw, "cmk_live_gk_stable_") {
		t.Fatalf("rotated secret %q does not retain stable key ID", raw)
	}
	if repo.tokenHash != repository.GatewayKeyTokenHash(raw) || repo.prefix != repository.GatewayKeyTokenPrefix(raw) {
		t.Fatal("repository did not receive the generated credential material")
	}
	if repo.tokenHash == "old-hash" {
		t.Fatal("credential hash was not replaced")
	}
}
