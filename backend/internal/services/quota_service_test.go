package services

import (
	"testing"
	"time"

	"clawreef/internal/models"
)

type quotaRoleTestRepo struct {
	quotas map[int]*models.UserQuota
	nextID int
}

func newQuotaRoleTestRepo() *quotaRoleTestRepo {
	return &quotaRoleTestRepo{quotas: make(map[int]*models.UserQuota), nextID: 1}
}

func (r *quotaRoleTestRepo) Create(quota *models.UserQuota) error {
	clone := *quota
	if clone.ID == 0 {
		clone.ID = r.nextID
		r.nextID++
	}
	if clone.CreatedAt.IsZero() {
		clone.CreatedAt = time.Now()
	}
	if clone.UpdatedAt.IsZero() {
		clone.UpdatedAt = clone.CreatedAt
	}
	r.quotas[clone.UserID] = &clone
	*quota = clone
	return nil
}

func (r *quotaRoleTestRepo) GetByUserID(userID int) (*models.UserQuota, error) {
	quota := r.quotas[userID]
	if quota == nil {
		return nil, nil
	}
	clone := *quota
	return &clone, nil
}

func (r *quotaRoleTestRepo) Update(quota *models.UserQuota) error {
	clone := *quota
	r.quotas[clone.UserID] = &clone
	return nil
}

func (r *quotaRoleTestRepo) DeleteByUserID(userID int) error {
	delete(r.quotas, userID)
	return nil
}

func (r *quotaRoleTestRepo) CreateDefaultQuota(userID int) (*models.UserQuota, error) {
	defaults := models.DefaultQuotaForRole("user")
	quota := &models.UserQuota{UserID: userID}
	quota.ApplyResourceValues(defaults)
	if err := r.Create(quota); err != nil {
		return nil, err
	}
	return quota, nil
}

func TestDefaultQuotaForRoleUsesExpectedValues(t *testing.T) {
	userQuota := models.DefaultQuotaForRole("user")
	adminQuota := models.DefaultQuotaForRole("admin")

	if userQuota.MaxInstances != 10 || userQuota.MaxCPUCores != 40 || userQuota.MaxMemoryGB != 100 || userQuota.MaxStorageGB != 500 || userQuota.MaxGPUCount != 2 {
		t.Fatalf("user defaults = %#v", userQuota)
	}
	if adminQuota.MaxInstances != 100 || adminQuota.MaxCPUCores != 200 || adminQuota.MaxMemoryGB != 1000 || adminQuota.MaxStorageGB != 5000 || adminQuota.MaxGPUCount != 10 {
		t.Fatalf("admin defaults = %#v", adminQuota)
	}
}

func TestSyncDefaultQuotaForRoleUpgradesOnlyOrdinaryDefaults(t *testing.T) {
	repo := newQuotaRoleTestRepo()
	if _, err := repo.CreateDefaultQuota(1); err != nil {
		t.Fatalf("create default quota: %v", err)
	}
	if err := SyncDefaultQuotaForRole(repo, 1, "admin"); err != nil {
		t.Fatalf("sync admin default quota: %v", err)
	}
	quota, _ := repo.GetByUserID(1)
	if !quota.IsDefaultForRole("admin") {
		t.Fatalf("quota after promotion = %#v, want admin defaults", quota)
	}

	custom := &models.UserQuota{UserID: 2}
	custom.ApplyResourceValues(models.DefaultQuotaForRole("user"))
	custom.MaxInstances = 11
	if err := repo.Create(custom); err != nil {
		t.Fatalf("create custom quota: %v", err)
	}
	if err := SyncDefaultQuotaForRole(repo, 2, "admin"); err != nil {
		t.Fatalf("sync custom quota: %v", err)
	}
	quota, _ = repo.GetByUserID(2)
	if quota.MaxInstances != 11 || quota.MaxCPUCores != 40 || quota.MaxMemoryGB != 100 || quota.MaxStorageGB != 500 || quota.MaxGPUCount != 2 {
		t.Fatalf("custom quota changed after promotion = %#v", quota)
	}
}

func TestCreateUserUsesRoleSpecificDefaultQuota(t *testing.T) {
	repo := newFakeUserRepo()
	quotaRepo := newQuotaRoleTestRepo()
	service := NewUserService(repo, quotaRepo)

	admin, err := service.CreateUserWithProvider("alice", "alice@example.com", "password", "admin", AuthProviderLocal)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	user, err := service.CreateUserWithProvider("bob", "bob@example.com", "password", "user", AuthProviderLocal)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	adminQuota, _ := quotaRepo.GetByUserID(admin.ID)
	userQuota, _ := quotaRepo.GetByUserID(user.ID)
	if !adminQuota.IsDefaultForRole("admin") {
		t.Fatalf("admin quota = %#v, want admin defaults", adminQuota)
	}
	if !userQuota.IsDefaultForRole("user") {
		t.Fatalf("user quota = %#v, want user defaults", userQuota)
	}
}

func TestUpdateUserRoleUpgradesOrdinaryQuota(t *testing.T) {
	userRepo := newFakeUserRepo()
	quotaRepo := newQuotaRoleTestRepo()
	user := &models.User{Username: "alice", Email: "alice@example.com", Role: "user", AuthProvider: AuthProviderLocal, IsActive: true}
	if err := userRepo.Create(user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := quotaRepo.CreateDefaultQuota(user.ID); err != nil {
		t.Fatalf("create default quota: %v", err)
	}

	service := NewUserService(userRepo, quotaRepo)
	if err := service.UpdateUserRole(user.ID, "admin"); err != nil {
		t.Fatalf("update role: %v", err)
	}
	quota, _ := quotaRepo.GetByUserID(user.ID)
	if !quota.IsDefaultForRole("admin") {
		t.Fatalf("quota after manual promotion = %#v, want admin defaults", quota)
	}
}
