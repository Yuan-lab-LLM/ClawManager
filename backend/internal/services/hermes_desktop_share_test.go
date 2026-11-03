package services

import (
	"clawreef/internal/models"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type desktopExternalAccess struct {
	InstanceExternalAccessService
	access *models.InstanceExternalAccess
}

func (s *desktopExternalAccess) Get(_ context.Context, instanceID int) (*models.InstanceExternalAccess, error) {
	if s.access == nil || s.access.InstanceID != instanceID {
		return nil, nil
	}
	return s.access, nil
}

func (s *desktopExternalAccess) ResolveShortLink(_ context.Context, code string) (*models.InstanceExternalAccess, error) {
	if s.access == nil || !s.access.Enabled || s.access.PublicSlug == nil || strings.TrimSpace(*s.access.PublicSlug) != strings.TrimSpace(code) {
		return nil, ErrExternalAccessNotEnabled
	}
	if s.access.ExpiresAt != nil && !time.Now().UTC().Before(*s.access.ExpiresAt) {
		return nil, errors.New("external access has expired")
	}
	return s.access, nil
}

func TestHermesDesktopSharedSessionTracksExternalCredentialLifecycle(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/password-login" {
			t.Fatalf("unexpected upstream path %s", r.URL.Path)
		}
		http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "managed-session", Path: "/", HttpOnly: true})
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	s := desktopFixture(t, upstream.URL)
	slug := "sl_test"
	passwordHash := "password-version-1"
	expiresAt := time.Now().UTC().Add(time.Hour)
	access := &models.InstanceExternalAccess{
		InstanceID:   123,
		Enabled:      true,
		AuthMode:     ExternalAccessModePassword,
		PublicSlug:   &slug,
		PasswordHash: &passwordHash,
		ExpiresAt:    &expiresAt,
	}
	s.config.ExternalAccess = &desktopExternalAccess{access: access}
	binding := ExternalAccessSessionBinding(slug, access)

	descriptor, raw, err := s.ActivateShared(context.Background(), 123, slug, binding)
	if err != nil || descriptor == nil || !descriptor.Available || raw == "" {
		t.Fatalf("ActivateShared() descriptor=%+v token=%t err=%v", descriptor, raw != "", err)
	}
	if claims, err := s.Authenticate(context.Background(), raw, 123); err != nil || claims.ExternalSessionBinding != binding {
		t.Fatalf("shared Desktop session was not bound: claims=%+v err=%v", claims, err)
	}

	passwordHash = "password-version-2"
	access.PasswordHash = &passwordHash
	if _, err := s.Authenticate(context.Background(), raw, 123); !errors.Is(err, ErrHermesDesktopUnauthorized) {
		t.Fatalf("password rotation did not revoke shared Desktop session: %v", err)
	}
	if _, _, err := s.ActivateShared(context.Background(), 123, slug, binding); !errors.Is(err, ErrHermesDesktopUnauthorized) {
		t.Fatalf("stale binding activated after password rotation: %v", err)
	}

	access.Enabled = false
	if _, err := s.Authenticate(context.Background(), raw, 123); !errors.Is(err, ErrHermesDesktopUnauthorized) {
		t.Fatalf("disabled share did not reject shared Desktop session: %v", err)
	}
}
