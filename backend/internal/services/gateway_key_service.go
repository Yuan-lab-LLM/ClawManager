package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type GatewayKeyService interface {
	Create(CreateGatewayKeyRequest) (*models.GatewayKey, string, error)
	List() ([]models.GatewayKey, error)
	Revoke(context.Context, int, int, string) error
	Rotate(context.Context, int, int) (*models.GatewayKey, string, error)
	UpdateLimits(context.Context, int, int, int, int, int) error
	UpdateBudget(context.Context, int, int, float64, string, bool) error
	BudgetUsage(context.Context, int) (*repository.GatewayKeyBudgetUsage, error)
	Resolve(string) (*models.GatewayKey, error)
	EnsureManagedInstanceKey(context.Context, *models.Instance) error
	BackfillManagedInstanceKeys(context.Context) error
}

func (s *gatewayKeyService) UpdateBudget(ctx context.Context, id, actorID int, limit float64, period string, resetUsage bool) error {
	if limit < 0 || limit > 1_000_000_000 {
		return fmt.Errorf("gateway key budget limit is invalid")
	}
	period = strings.ToLower(strings.TrimSpace(period))
	if period == "" {
		period = models.GatewayKeyBudgetPeriodLifetime
	}
	switch period {
	case models.GatewayKeyBudgetPeriodLifetime, models.GatewayKeyBudgetPeriodDaily, models.GatewayKeyBudgetPeriodWeekly, models.GatewayKeyBudgetPeriodMonthly:
	default:
		return fmt.Errorf("gateway key budget period is invalid")
	}
	key, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if key == nil {
		return fmt.Errorf("gateway key not found")
	}
	if err := s.repo.UpdateBudgetPolicy(ctx, id, limit, period, resetUsage); err != nil {
		return err
	}
	s.record(key, "gateway.key.budget.updated", fmt.Sprintf("Gateway key %s budget updated", key.KeyID))
	return nil
}

func (s *gatewayKeyService) BudgetUsage(ctx context.Context, id int) (*repository.GatewayKeyBudgetUsage, error) {
	key, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, fmt.Errorf("gateway key not found")
	}
	return s.repo.GetBudgetUsage(ctx, id)
}

func (s *gatewayKeyService) UpdateLimits(ctx context.Context, id, actorID, rpm, tpm, concurrent int) error {
	if rpm < 0 || tpm < 0 || concurrent < 0 || rpm > 100000 || tpm > 100000000 || concurrent > 10000 {
		return fmt.Errorf("gateway key limits are invalid")
	}
	key, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if key == nil {
		return fmt.Errorf("gateway key not found")
	}
	if err := s.repo.UpdateLimits(ctx, id, rpm, tpm, concurrent); err != nil {
		return err
	}
	s.record(key, "gateway.key.limits.updated", fmt.Sprintf("Gateway key %s limits updated", key.KeyID))
	return nil
}

type CreateGatewayKeyRequest struct {
	Name               string
	Description        string
	OwnerUserID        int
	CreatedByUserID    int
	AllowedModelGroups []string
	ExpiresAt          *time.Time
	KeyType            string
	RPMLimit           int
	TPMLimit           int
	ConcurrentLimit    int
	BudgetLimit        float64
	BudgetPeriod       string
}

type gatewayKeyService struct {
	repo         repository.GatewayKeyRepository
	instanceRepo repository.InstanceRepository
	auditService AuditEventService
}

func NewGatewayKeyService(repo repository.GatewayKeyRepository, instanceRepo repository.InstanceRepository, auditService AuditEventService) GatewayKeyService {
	return &gatewayKeyService{repo: repo, instanceRepo: instanceRepo, auditService: auditService}
}

func (s *gatewayKeyService) Create(req CreateGatewayKeyRequest) (*models.GatewayKey, string, error) {
	if s == nil || s.repo == nil {
		return nil, "", fmt.Errorf("gateway key repository is not configured")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 160 {
		return nil, "", fmt.Errorf("gateway key name is required and must be at most 160 characters")
	}
	if req.OwnerUserID <= 0 {
		return nil, "", fmt.Errorf("gateway key owner is required")
	}
	keyType := strings.TrimSpace(req.KeyType)
	if keyType == "" {
		keyType = models.GatewayKeyTypeUserCreated
	}
	if keyType != models.GatewayKeyTypeUserCreated && keyType != models.GatewayKeyTypeService {
		return nil, "", fmt.Errorf("gateway key type is not supported")
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now().UTC()) {
		return nil, "", fmt.Errorf("gateway key expiration must be in the future")
	}
	if req.RPMLimit < 0 || req.TPMLimit < 0 || req.ConcurrentLimit < 0 || req.BudgetLimit < 0 {
		return nil, "", fmt.Errorf("gateway key limits must be non-negative")
	}
	budgetPeriod := strings.ToLower(strings.TrimSpace(req.BudgetPeriod))
	if budgetPeriod == "" {
		budgetPeriod = models.GatewayKeyBudgetPeriodLifetime
	}
	switch budgetPeriod {
	case models.GatewayKeyBudgetPeriodLifetime, models.GatewayKeyBudgetPeriodDaily, models.GatewayKeyBudgetPeriodWeekly, models.GatewayKeyBudgetPeriodMonthly:
	default:
		return nil, "", fmt.Errorf("gateway key budget period is invalid")
	}
	groups, err := normalizeGatewayKeyGroups(req.AllowedModelGroups)
	if err != nil {
		return nil, "", err
	}
	raw, keyID, err := generateGatewayKey()
	if err != nil {
		return nil, "", err
	}
	var groupJSON *string
	if len(groups) > 0 {
		encoded, _ := json.Marshal(groups)
		text := string(encoded)
		groupJSON = &text
	}
	now := time.Now().UTC()
	description := strings.TrimSpace(req.Description)
	key := &models.GatewayKey{KeyID: keyID, KeyPrefix: repository.GatewayKeyTokenPrefix(raw), TokenHash: repository.GatewayKeyTokenHash(raw), KeyType: keyType, Name: name, OwnerUserID: &req.OwnerUserID, AllowedModelGroups: groupJSON, RPMLimit: req.RPMLimit, TPMLimit: req.TPMLimit, ConcurrentRequestsLimit: req.ConcurrentLimit, BudgetLimit: req.BudgetLimit, BudgetPeriod: budgetPeriod, Status: models.GatewayKeyStatusActive, ExpiresAt: req.ExpiresAt, CreatedByUserID: &req.CreatedByUserID, CreatedAt: now, UpdatedAt: now}
	if description != "" {
		key.Description = &description
	}
	if err := s.repo.Create(key); err != nil {
		return nil, "", fmt.Errorf("failed to create gateway key: %w", err)
	}
	s.record(key, "gateway.key.created", fmt.Sprintf("Gateway key %s created", key.KeyID))
	return key, raw, nil
}

func (s *gatewayKeyService) List() ([]models.GatewayKey, error) { return s.repo.List() }

func (s *gatewayKeyService) Revoke(ctx context.Context, id, actorID int, reason string) error {
	key, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if key == nil {
		return fmt.Errorf("gateway key not found")
	}
	if key.KeyType == models.GatewayKeyTypeManagedInstance {
		return fmt.Errorf("managed instance gateway keys cannot be revoked manually")
	}
	if key.Status == models.GatewayKeyStatusRevoked {
		return nil
	}
	if err := s.repo.Revoke(ctx, id, actorID, reason); err != nil {
		return fmt.Errorf("failed to revoke gateway key: %w", err)
	}
	s.record(key, "gateway.key.revoked", fmt.Sprintf("Gateway key %s revoked", key.KeyID))
	return nil
}

// Rotate replaces only the credential material. Keeping the same database row
// preserves budget, usage attribution, limits and the stable public key ID.
func (s *gatewayKeyService) Rotate(ctx context.Context, id, actorID int) (*models.GatewayKey, string, error) {
	old, err := s.repo.GetByID(id)
	if err != nil {
		return nil, "", err
	}
	if old == nil {
		return nil, "", fmt.Errorf("gateway key not found")
	}
	if old.KeyType == models.GatewayKeyTypeManagedInstance {
		return nil, "", fmt.Errorf("managed instance gateway keys require coordinated rotation")
	}
	if old.Status != models.GatewayKeyStatusActive {
		return nil, "", fmt.Errorf("only active gateway keys can be rotated")
	}
	raw, err := generateGatewayKeySecret(old.KeyID)
	if err != nil {
		return nil, "", err
	}
	if err := s.repo.RotateCredential(ctx, old.ID, repository.GatewayKeyTokenPrefix(raw), repository.GatewayKeyTokenHash(raw)); err != nil {
		return nil, "", err
	}
	rotated, err := s.repo.GetByID(old.ID)
	if err != nil {
		return nil, "", err
	}
	if rotated == nil {
		return nil, "", fmt.Errorf("gateway key disappeared after credential rotation")
	}
	s.record(old, "gateway.key.rotated", fmt.Sprintf("Gateway key %s credential rotated", old.KeyID))
	return rotated, raw, nil
}

func (s *gatewayKeyService) Resolve(raw string) (*models.GatewayKey, error) {
	key, err := s.repo.GetByToken(raw)
	if err != nil || key == nil {
		return key, err
	}
	if !key.IsUsable(time.Now().UTC()) {
		return nil, nil
	}
	_ = s.repo.Touch(context.Background(), key.ID, time.Now().UTC())
	return key, nil
}

func (s *gatewayKeyService) EnsureManagedInstanceKey(ctx context.Context, instance *models.Instance) error {
	if instance == nil || instance.AccessToken == nil || strings.TrimSpace(*instance.AccessToken) == "" {
		return nil
	}
	_, err := s.repo.UpsertManagedInstance(ctx, instance.ID, instance.UserID, *instance.AccessToken)
	return err
}

func (s *gatewayKeyService) BackfillManagedInstanceKeys(ctx context.Context) error {
	if s == nil || s.instanceRepo == nil {
		return nil
	}
	for offset := 0; ; offset += 200 {
		items, err := s.instanceRepo.GetAll(offset, 200)
		if err != nil {
			return fmt.Errorf("list instances for gateway key backfill: %w", err)
		}
		for i := range items {
			if err := s.EnsureManagedInstanceKey(ctx, &items[i]); err != nil {
				return err
			}
		}
		if len(items) < 200 {
			return nil
		}
	}
}

func (s *gatewayKeyService) record(key *models.GatewayKey, eventType, message string) {
	if s.auditService == nil || key == nil {
		return
	}
	traceID := "gateway-key-" + key.KeyID
	_ = s.auditService.RecordEvent(&models.AuditEvent{TraceID: traceID, UserID: key.OwnerUserID, InstanceID: key.InstanceID, GatewayKeyID: &key.ID, EventType: eventType, TrafficClass: models.TrafficClassLLM, Severity: models.AuditSeverityInfo, Message: message})
}

func generateGatewayKey() (string, string, error) {
	idBytes, secretBytes := make([]byte, 8), make([]byte, 32)
	if _, err := rand.Read(idBytes); err != nil {
		return "", "", err
	}
	if _, err := rand.Read(secretBytes); err != nil {
		return "", "", err
	}
	keyID := "gk_" + hex.EncodeToString(idBytes)
	return "cmk_live_" + keyID + "_" + hex.EncodeToString(secretBytes), keyID, nil
}

func generateGatewayKeySecret(keyID string) (string, error) {
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return "", err
	}
	return "cmk_live_" + strings.TrimSpace(keyID) + "_" + hex.EncodeToString(secretBytes), nil
}

func normalizeGatewayKeyGroups(input []string) ([]string, error) {
	seen := map[string]struct{}{}
	groups := make([]string, 0, len(input))
	for _, item := range input {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		if len(value) > 100 {
			return nil, fmt.Errorf("model group name is too long")
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		groups = append(groups, value)
	}
	return groups, nil
}
