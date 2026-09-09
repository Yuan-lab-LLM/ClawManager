package services

import "testing"

func TestRegistrationInitializesOrdinaryQuota(t *testing.T) {
	users := newFakeUserRepo()
	quotas := newQuotaRoleTestRepo()
	auth := NewAuthService(users, testJWTConfig(), nil, WithQuotaRepository(quotas))
	user, err := auth.Register("desktop-user", "desktop@example.invalid", "test-password-only")
	if err != nil {
		t.Fatal(err)
	}
	quota, err := quotas.GetByUserID(user.ID)
	if err != nil || quota == nil || !quota.IsDefaultForRole("user") {
		t.Fatalf("registered user has no ordinary quota: quota=%+v err=%v", quota, err)
	}
	if user.Role != "user" {
		t.Fatal("registration changed ordinary role")
	}
}
