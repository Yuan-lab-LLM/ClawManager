package services

import (
	"fmt"
	"time"

	"clawreef/internal/repository"
)

// RepairSeededAdminPassword upgrades shipped bad admin seed hashes to the
// expected admin123 hash without touching user-changed passwords.
func RepairSeededAdminPassword(userRepo repository.UserRepository) (bool, error) {
	admin, err := userRepo.GetByUsername("admin")
	if err != nil {
		return false, fmt.Errorf("failed to load admin user: %w", err)
	}
	if admin == nil || !IsKnownBrokenAdminSeedHash(admin.PasswordHash) {
		return false, nil
	}

	admin.PasswordHash = DefaultAdminPasswordHash
	admin.UpdatedAt = time.Now()

	if err := userRepo.Update(admin); err != nil {
		return false, fmt.Errorf("failed to repair seeded admin password: %w", err)
	}

	return true, nil
}
