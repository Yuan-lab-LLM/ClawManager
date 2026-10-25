package models

import (
	"strings"
	"testing"
)

func TestSanitizeInstanceError(t *testing.T) {
	got := SanitizeInstanceError("startup failed token=abc password='xyz' https://user:pass@host/path Authorization: Bearer abc.def")
	for _, secret := range []string{"abc", "xyz", "user:pass"} {
		if strings.Contains(got, secret) {
			t.Fatalf("secret leaked: %s", got)
		}
	}
	if !strings.Contains(got, "startup failed") {
		t.Fatal("lost cause")
	}
}
