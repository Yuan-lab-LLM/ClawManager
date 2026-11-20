package repository

import (
	"context"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestGatewayKeyCreateReturnsUsableDatabaseID(t *testing.T) {
	repo := NewGatewayKeyRepository(auditFullFixture(t))
	now := time.Now().UTC()
	key := &models.GatewayKey{
		KeyID: "create-regression", KeyPrefix: "test", TokenHash: GatewayKeyTokenHash("test-secret"),
		KeyType: models.GatewayKeyTypeService, Name: "creation regression",
		Status: models.GatewayKeyStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.Create(key); err != nil {
		t.Fatal(err)
	}
	if key.ID <= 0 {
		t.Fatalf("Create returned database ID %d", key.ID)
	}
	if err := repo.Revoke(context.Background(), key.ID, 0, "test"); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetByKeyID(key.KeyID)
	if err != nil || stored == nil || stored.ID != key.ID || stored.Status != models.GatewayKeyStatusRevoked {
		t.Fatalf("created ID could not revoke stored key: %#v, %v", stored, err)
	}
}
