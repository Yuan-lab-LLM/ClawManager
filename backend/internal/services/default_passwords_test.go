package services

import (
	"testing"

	"clawreef/internal/utils"
)

func TestDefaultAdminPasswordHashMatchesDocumentedPassword(t *testing.T) {
	if !utils.VerifyPassword(DefaultAdminPassword, DefaultAdminPasswordHash) {
		t.Fatalf("default admin password hash does not match %q", DefaultAdminPassword)
	}
}

func TestKnownBrokenAdminSeedHashesDoNotMatchDefaultPassword(t *testing.T) {
	if utils.VerifyPassword(DefaultAdminPassword, legacyBrokenAdminSeedHash) {
		t.Fatalf("broken admin seed hash unexpectedly matches %q", DefaultAdminPassword)
	}
}
