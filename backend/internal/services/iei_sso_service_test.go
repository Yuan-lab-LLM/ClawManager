package services

import (
	"errors"
	"testing"
	"time"

	"clawreef/internal/config"
)

func testIEISystemConfig() config.IEISystemConfig {
	return config.IEISystemConfig{
		Enabled:       true,
		AESKey:        "TESTKEY123456789",
		AESIV:         "0123456789ABCDEF",
		TokenTTL:      24 * time.Hour,
		SessionTTL:    30 * time.Minute,
		SessionSecret: "test-only-iei-session-secret-at-least-32-bytes",
		Timezone:      "Asia/Shanghai",
	}
}

func TestIEISSOServiceExchangesDocumentCompatibleAESCBCToken(t *testing.T) {
	service, err := NewIEISSOService(testIEISystemConfig())
	if err != nil {
		t.Fatalf("NewIEISSOService() error = %v", err)
	}
	// Independently generated with Node/OpenSSL-compatible AES-128-CBC and
	// PKCS padding for Owner+Tag@Example.COM+2026-08-18 12:34:56.
	const externalToken = "xzOmdmY7dW9qI52/OrpGAcQFG1IPcJjc32hUjq/FfHCEiOdeJOJu01UGKEcw46pb"
	location, _ := time.LoadLocation("Asia/Shanghai")
	service.now = func() time.Time {
		return time.Date(2026, 8, 18, 12, 35, 10, 0, location)
	}

	session, err := service.ExchangeExternalToken(externalToken)
	if err != nil {
		t.Fatalf("ExchangeExternalToken() error = %v", err)
	}
	if session.Email != "owner+tag@example.com" {
		t.Fatalf("session email = %q", session.Email)
	}
	validated, err := service.ValidateSession(session.Token)
	if err != nil {
		t.Fatalf("ValidateSession() error = %v", err)
	}
	if validated.Email != session.Email || validated.SessionID != session.SessionID {
		t.Fatalf("validated session = %#v, issued = %#v", validated, session)
	}
}

func TestIEISSOServiceAcceptsExternalTokenAt24HourBoundary(t *testing.T) {
	service, err := NewIEISSOService(testIEISystemConfig())
	if err != nil {
		t.Fatalf("NewIEISSOService() error = %v", err)
	}
	const externalToken = "xzOmdmY7dW9qI52/OrpGAcQFG1IPcJjc32hUjq/FfHCEiOdeJOJu01UGKEcw46pb"
	location, _ := time.LoadLocation("Asia/Shanghai")
	service.now = func() time.Time {
		return time.Date(2026, 8, 19, 12, 34, 56, 0, location)
	}

	if _, err := service.ExchangeExternalToken(externalToken); err != nil {
		t.Fatalf("ExchangeExternalToken() rejected a token at the 24-hour boundary: %v", err)
	}
}

func TestIEISSOServiceRenewsLocalSessionWithoutChangingBinding(t *testing.T) {
	service, err := NewIEISSOService(testIEISystemConfig())
	if err != nil {
		t.Fatalf("NewIEISSOService() error = %v", err)
	}
	base := time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return base }
	initial, err := service.issueSession("owner@example.com")
	if err != nil {
		t.Fatalf("issueSession() error = %v", err)
	}

	service.now = func() time.Time { return base.Add(10 * time.Minute) }
	renewed, err := service.RenewSession(initial.Token)
	if err != nil {
		t.Fatalf("RenewSession() error = %v", err)
	}
	if renewed.Email != initial.Email || renewed.SessionID != initial.SessionID {
		t.Fatalf("renewed identity/binding = %#v, initial = %#v", renewed, initial)
	}
	if want := initial.ExpiresAt.Add(10 * time.Minute); !renewed.ExpiresAt.Equal(want) {
		t.Fatalf("renewed expiry = %s, want %s", renewed.ExpiresAt, want)
	}
}

func TestIEISSOServiceRejectsExpiredOrTamperedExternalToken(t *testing.T) {
	service, err := NewIEISSOService(testIEISystemConfig())
	if err != nil {
		t.Fatalf("NewIEISSOService() error = %v", err)
	}
	const externalToken = "xzOmdmY7dW9qI52/OrpGAcQFG1IPcJjc32hUjq/FfHCEiOdeJOJu01UGKEcw46pb"
	location, _ := time.LoadLocation("Asia/Shanghai")
	service.now = func() time.Time {
		return time.Date(2026, 8, 19, 12, 34, 57, 0, location)
	}
	if _, err := service.ExchangeExternalToken(externalToken); !errors.Is(err, ErrInvalidIEISystemToken) {
		t.Fatalf("expired token error = %v", err)
	}
	service.now = func() time.Time {
		return time.Date(2026, 8, 18, 12, 35, 10, 0, location)
	}
	if _, err := service.ExchangeExternalToken(externalToken[:len(externalToken)-1] + "A"); !errors.Is(err, ErrInvalidIEISystemToken) {
		t.Fatalf("tampered token error = %v", err)
	}
}

func TestIEISSOServiceRejectsInvalidConfiguration(t *testing.T) {
	cfg := testIEISystemConfig()
	cfg.AESIV = "short"
	if _, err := NewIEISSOService(cfg); err == nil {
		t.Fatal("NewIEISSOService() accepted a non-16-byte IV")
	}
	cfg = testIEISystemConfig()
	cfg.TokenTTL = 24*time.Hour + time.Second
	if _, err := NewIEISSOService(cfg); err == nil {
		t.Fatal("NewIEISSOService() accepted a token TTL above 24 hours")
	}
	cfg = testIEISystemConfig()
	cfg.SessionSecret = "short"
	if _, err := NewIEISSOService(cfg); err == nil {
		t.Fatal("NewIEISSOService() accepted a short session secret")
	}
}
