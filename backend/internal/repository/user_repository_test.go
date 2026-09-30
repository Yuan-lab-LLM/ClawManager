package repository

import (
	"errors"
	"testing"
)

func TestUsernameUniqueConstraintRecognition(t *testing.T) {
	for _, key := range []string{"uk_users_username", "uk_users_local_username"} {
		if !isUsernameConflict(errors.New("Error 1062: Duplicate entry 'alice' for key 'users." + key + "'")) {
			t.Fatalf("unrecognized constraint: %s", key)
		}
	}
	for _, err := range []error{errors.New("connection refused"), errors.New("Duplicate entry 'mail@example.com' for key 'email'"), errors.New("Duplicate entry 'dn' for key 'uk_users_provider_external_id'")} {
		if isUsernameConflict(err) {
			t.Fatalf("unrelated error treated as username conflict: %v", err)
		}
	}
}
