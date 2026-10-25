package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

var (
	ErrExternalAccessNotEnabled         = errors.New("external access is not enabled")
	ErrExternalAccessPasswordNotEnabled = errors.New("external access password is not enabled")
)

const (
	ExternalAccessModeShareLink = "share_link"
	ExternalAccessModePassword  = "password"

	ExternalWorkspaceAccessNone  = "none"
	ExternalWorkspaceAccessRead  = "read"
	ExternalWorkspaceAccessWrite = "write"

	ExternalAccessExpirationPreset    = "preset"
	ExternalAccessExpirationCustom    = "custom"
	ExternalAccessExpirationPermanent = "permanent"

	ExternalAccessPreset1Hour   = "1h"
	ExternalAccessPreset24Hours = "24h"
	ExternalAccessPreset7Days   = "7d"
	ExternalAccessPreset30Days  = "30d"
)

type ExternalAccessExpirationRequest struct {
	Mode            string     `json:"expires_mode,omitempty"`
	Preset          string     `json:"expires_preset,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	WorkspaceAccess string     `json:"workspace_access,omitempty"`
}

type EnableShareLinkResult struct {
	Access   *models.InstanceExternalAccess `json:"access"`
	ShareURL string                         `json:"share_url,omitempty"`
}

type PasswordExternalAccessResult struct {
	Access   *models.InstanceExternalAccess `json:"access"`
	Password string                         `json:"password"`
	ShareURL string                         `json:"share_url,omitempty"`
}

type InstanceExternalAccessService interface {
	Get(ctx context.Context, instanceID int) (*models.InstanceExternalAccess, error)
	EnableShareLink(ctx context.Context, instanceID, createdBy int, expiration ExternalAccessExpirationRequest) (*EnableShareLinkResult, error)
	CreatePassword(ctx context.Context, instanceID, createdBy int, expiration ExternalAccessExpirationRequest) (*PasswordExternalAccessResult, error)
	ResetURL(ctx context.Context, instanceID, createdBy int) (*EnableShareLinkResult, error)
	ResetPassword(ctx context.Context, instanceID, createdBy int) (*PasswordExternalAccessResult, error)
	Disable(ctx context.Context, instanceID int) error
	ResolveShortLink(ctx context.Context, code string) (*models.InstanceExternalAccess, error)
	ValidateShortLink(ctx context.Context, code, password string) (*models.InstanceExternalAccess, error)
}

func (s *instanceExternalAccessService) ResetURL(ctx context.Context, instanceID, createdBy int) (*EnableShareLinkResult, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("instance external access repository is not configured")
	}
	existing, err := s.repo.GetByInstanceID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if existing == nil || !existing.Enabled {
		return nil, ErrExternalAccessNotEnabled
	}
	code, codeHash, err := generateShortCode()
	if err != nil {
		return nil, err
	}
	updated, err := s.repo.ResetURL(ctx, instanceID, createdBy, code, codeHash)
	if err != nil {
		return nil, err
	}
	if !updated {
		return nil, ErrExternalAccessNotEnabled
	}
	saved, err := s.repo.GetByInstanceID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return &EnableShareLinkResult{Access: saved, ShareURL: shortExternalAccessURL(code)}, nil
}

func (s *instanceExternalAccessService) ResetPassword(ctx context.Context, instanceID, createdBy int) (*PasswordExternalAccessResult, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("instance external access repository is not configured")
	}
	existing, err := s.repo.GetByInstanceID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if existing == nil || !existing.Enabled {
		return nil, ErrExternalAccessNotEnabled
	}
	if existing.AuthMode != ExternalAccessModePassword {
		return nil, ErrExternalAccessPasswordNotEnabled
	}
	password, err := randomToken("pwd", 16)
	if err != nil {
		return nil, err
	}
	passwordHash := hashExternalSecret(password)
	hint := password
	if len(hint) > 12 {
		hint = hint[:12]
	}
	updated, err := s.repo.ResetPassword(ctx, instanceID, createdBy, passwordHash, password, hint)
	if err != nil {
		return nil, err
	}
	if !updated {
		return nil, ErrExternalAccessPasswordNotEnabled
	}
	saved, err := s.repo.GetByInstanceID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return &PasswordExternalAccessResult{
		Access: saved, Password: password, ShareURL: ExternalAccessShareURL(saved),
	}, nil
}

type instanceExternalAccessService struct {
	repo repository.InstanceExternalAccessRepository
}

func NewInstanceExternalAccessService(repo repository.InstanceExternalAccessRepository) InstanceExternalAccessService {
	return &instanceExternalAccessService{repo: repo}
}

func (s *instanceExternalAccessService) Get(ctx context.Context, instanceID int) (*models.InstanceExternalAccess, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("instance external access repository is not configured")
	}
	return s.repo.GetByInstanceID(ctx, instanceID)
}

func (s *instanceExternalAccessService) EnableShareLink(ctx context.Context, instanceID, createdBy int, expiration ExternalAccessExpirationRequest) (*EnableShareLinkResult, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("instance external access repository is not configured")
	}
	code, codeHash, err := generateShortCode()
	if err != nil {
		return nil, err
	}
	expiresAt, err := resolveExternalAccessExpiresAt(expiration)
	if err != nil {
		return nil, err
	}
	workspaceAccess, err := NormalizeExternalWorkspaceAccess(expiration.WorkspaceAccess)
	if err != nil {
		return nil, err
	}
	access := &models.InstanceExternalAccess{
		InstanceID:      instanceID,
		Enabled:         true,
		AuthMode:        ExternalAccessModeShareLink,
		WorkspaceAccess: workspaceAccess,
		ShortCodeHash:   &codeHash,
		PublicSlug:      &code,
		PublicTokenHash: nil,
		ExpiresAt:       expiresAt,
		CreatedBy:       &createdBy,
		PasswordHash:    nil,
		PasswordValue:   nil,
		PasswordHint:    nil,
		LastUsedAt:      nil,
	}
	if err := s.repo.Upsert(ctx, access); err != nil {
		return nil, err
	}
	saved, err := s.repo.GetByInstanceID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return &EnableShareLinkResult{
		Access:   saved,
		ShareURL: shortExternalAccessURL(code),
	}, nil
}

func (s *instanceExternalAccessService) CreatePassword(ctx context.Context, instanceID, createdBy int, expiration ExternalAccessExpirationRequest) (*PasswordExternalAccessResult, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("instance external access repository is not configured")
	}
	code, codeHash, err := generateShortCode()
	if err != nil {
		return nil, err
	}
	password, err := randomToken("pwd", 16)
	if err != nil {
		return nil, err
	}
	expiresAt, err := resolveExternalAccessExpiresAt(expiration)
	if err != nil {
		return nil, err
	}
	workspaceAccess, err := NormalizeExternalWorkspaceAccess(expiration.WorkspaceAccess)
	if err != nil {
		return nil, err
	}
	passwordHash := hashExternalSecret(password)
	hint := password
	if len(hint) > 12 {
		hint = hint[:12]
	}
	access := &models.InstanceExternalAccess{
		InstanceID:      instanceID,
		Enabled:         true,
		AuthMode:        ExternalAccessModePassword,
		WorkspaceAccess: workspaceAccess,
		PublicSlug:      &code,
		ShortCodeHash:   &codeHash,
		PasswordHash:    &passwordHash,
		PasswordValue:   &password,
		PasswordHint:    &hint,
		ExpiresAt:       expiresAt,
		CreatedBy:       &createdBy,
		PublicTokenHash: nil,
		LastUsedAt:      nil,
	}
	if err := s.repo.Upsert(ctx, access); err != nil {
		return nil, err
	}
	saved, err := s.repo.GetByInstanceID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return &PasswordExternalAccessResult{Access: saved, Password: password, ShareURL: shortExternalAccessURL(code)}, nil
}

func NormalizeExternalWorkspaceAccess(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", ExternalWorkspaceAccessNone:
		return ExternalWorkspaceAccessNone, nil
	case ExternalWorkspaceAccessRead:
		return ExternalWorkspaceAccessRead, nil
	case ExternalWorkspaceAccessWrite:
		return ExternalWorkspaceAccessWrite, nil
	default:
		return "", fmt.Errorf("unsupported external workspace access")
	}
}

// ExternalAccessSessionBinding returns a non-secret fingerprint of the current
// share credential. Tokens carrying this binding can be rejected immediately
// after a link is disabled, reset, expired, or has its password rotated.
func ExternalAccessSessionBinding(code string, accesses ...*models.InstanceExternalAccess) string {
	code = strings.Trim(strings.TrimSpace(code), "/")
	if code == "" {
		return ""
	}
	credentialVersion := ""
	if len(accesses) > 0 && accesses[0] != nil && accesses[0].AuthMode == ExternalAccessModePassword && accesses[0].PasswordHash != nil {
		credentialVersion = strings.TrimSpace(*accesses[0].PasswordHash)
	}
	payload := "shared-instance-session\x00" + code
	if credentialVersion != "" {
		payload += "\x00" + credentialVersion
	}
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func (s *instanceExternalAccessService) Disable(ctx context.Context, instanceID int) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("instance external access repository is not configured")
	}
	return s.repo.Disable(ctx, instanceID)
}

func (s *instanceExternalAccessService) ResolveShortLink(ctx context.Context, code string) (*models.InstanceExternalAccess, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("instance external access repository is not configured")
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, fmt.Errorf("short link code is required")
	}
	access, err := s.repo.GetByShortCodeHash(ctx, hashExternalSecret(code))
	if err != nil {
		return nil, err
	}
	if access == nil || !access.Enabled {
		return nil, fmt.Errorf("external access is not enabled")
	}
	if access.ExpiresAt != nil && time.Now().UTC().After(*access.ExpiresAt) {
		return nil, fmt.Errorf("external access has expired")
	}
	return access, nil
}

func (s *instanceExternalAccessService) ValidateShortLink(ctx context.Context, code, password string) (*models.InstanceExternalAccess, error) {
	access, err := s.ResolveShortLink(ctx, code)
	if err != nil {
		return nil, err
	}
	switch access.AuthMode {
	case ExternalAccessModeShareLink:
		// The short code itself is the share-link capability. A matching hash is enough.
	case ExternalAccessModePassword:
		if access.PasswordHash == nil || *access.PasswordHash != hashExternalSecret(strings.TrimSpace(password)) {
			return nil, fmt.Errorf("invalid external access password")
		}
	default:
		return nil, fmt.Errorf("unsupported external access mode")
	}
	if err := s.repo.MarkUsed(ctx, access.ID); err != nil {
		return nil, err
	}
	return access, nil
}

func resolveExternalAccessExpiresAt(expiration ExternalAccessExpirationRequest) (*time.Time, error) {
	mode := strings.TrimSpace(expiration.Mode)
	if mode == "" {
		mode = ExternalAccessExpirationPreset
	}
	switch mode {
	case ExternalAccessExpirationPermanent:
		return nil, nil
	case ExternalAccessExpirationCustom:
		if expiration.ExpiresAt == nil {
			return nil, fmt.Errorf("custom external access expiration requires expires_at")
		}
		value := expiration.ExpiresAt.UTC()
		return &value, nil
	case ExternalAccessExpirationPreset:
		preset := strings.TrimSpace(expiration.Preset)
		if preset == "" {
			preset = ExternalAccessPreset24Hours
		}
		duration, err := externalAccessPresetDuration(preset)
		if err != nil {
			return nil, err
		}
		value := time.Now().UTC().Add(duration)
		return &value, nil
	default:
		return nil, fmt.Errorf("unsupported external access expiration mode")
	}
}

func externalAccessPresetDuration(preset string) (time.Duration, error) {
	switch strings.TrimSpace(preset) {
	case ExternalAccessPreset1Hour:
		return time.Hour, nil
	case ExternalAccessPreset24Hours:
		return 24 * time.Hour, nil
	case ExternalAccessPreset7Days:
		return 7 * 24 * time.Hour, nil
	case ExternalAccessPreset30Days:
		return 30 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported external access expiration preset")
	}
}

func generateShortCode() (code, codeHash string, err error) {
	code, err = randomToken("sl", 12)
	if err != nil {
		return "", "", err
	}
	return code, hashExternalSecret(code), nil
}

func shortExternalAccessURL(code string) string {
	return fmt.Sprintf("/s/%s/", strings.Trim(strings.TrimSpace(code), "/"))
}

func ExternalAccessShareURL(access *models.InstanceExternalAccess) string {
	if access == nil || !access.Enabled || access.PublicSlug == nil {
		return ""
	}
	return shortExternalAccessURL(*access.PublicSlug)
}

func ExternalAccessPassword(access *models.InstanceExternalAccess) string {
	if access == nil || !access.Enabled || access.AuthMode != ExternalAccessModePassword || access.PasswordValue == nil {
		return ""
	}
	return *access.PasswordValue
}

func randomToken(prefix string, bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(buf), nil
}

func hashExternalSecret(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}
