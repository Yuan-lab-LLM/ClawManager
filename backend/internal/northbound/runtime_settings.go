package northbound

import (
	"sync"
	"time"

	"clawreef/internal/config"
	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type RuntimeSettingsProvider interface {
	Current() *models.NorthboundAdminSettings
	CallerPolicy(userID int) (*models.NorthboundCallerPolicy, error)
}

type DatabaseRuntimeSettings struct {
	repo     *repository.NorthboundRepository
	fallback *models.NorthboundAdminSettings
	mu       sync.Mutex
	cached   *models.NorthboundAdminSettings
	loadedAt time.Time
}

func NewDatabaseRuntimeSettings(repo *repository.NorthboundRepository, cfg config.NorthboundConfig) *DatabaseRuntimeSettings {
	return &DatabaseRuntimeSettings{repo: repo, fallback: defaultRuntimeSettings(cfg)}
}

func defaultRuntimeSettings(cfg config.NorthboundConfig) *models.NorthboundAdminSettings {
	return &models.NorthboundAdminSettings{
		ID: 1, APIEnabled: true, ChallengeTTLSeconds: durationSeconds(cfg.ChallengeTTL, 60),
		AccessTokenTTLSeconds: durationSeconds(cfg.AccessTokenTTL, 1800), RefreshTokenTTLSeconds: durationSeconds(cfg.RefreshTokenTTL, 604800),
		CoreRequestTimeoutSeconds: 30, ChallengeRatePerMinute: 10, LoginRatePerMinute: 5,
		AccountLoginRatePerMinute: 5, CreateRatePerMinute: 10, QueryRatePerMinute: 120,
		ShareRatePerMinute: 10, MaxPendingOperations: 5, OperationTickMilliseconds: durationMilliseconds(cfg.OperationTick, 1000),
		OperationLeaseSeconds: durationSeconds(cfg.OperationLease, 30), OperationMaxAttempts: positiveOr(cfg.OperationMaxAttempts, 5),
		AllowedLiteTypes: []string{"openclaw", "hermes", "opencode", "deepseek-harness"},
		AllowedProTypes:  []string{"openclaw", "hermes", "opencode", "deepseek-harness"},
		LiteCPUCores:     2, LiteMemoryGB: 4, LiteDiskGB: 5, ProCPUCores: 4, ProMemoryGB: 8, ProDiskGB: 50,
		WorkBuddyProCPUCores: 4, WorkBuddyProMemoryGB: 8, WorkBuddyProDiskGB: 40,
	}
}

func durationSeconds(value time.Duration, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return int(value.Seconds())
}
func durationMilliseconds(value time.Duration, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return int(value.Milliseconds())
}
func positiveOr(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func (p *DatabaseRuntimeSettings) Current() *models.NorthboundAdminSettings {
	if p == nil {
		return defaultRuntimeSettings(config.NorthboundConfig{})
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cached != nil && time.Since(p.loadedAt) < 2*time.Second {
		copy := *p.cached
		return &copy
	}
	if p.repo != nil {
		if item, err := p.repo.GetAdminSettings(); err == nil {
			p.cached = item
			p.loadedAt = time.Now()
			copy := *item
			return &copy
		}
	}
	if p.cached != nil {
		copy := *p.cached
		return &copy
	}
	copy := *p.fallback
	return &copy
}

func (p *DatabaseRuntimeSettings) CallerPolicy(userID int) (*models.NorthboundCallerPolicy, error) {
	if p == nil || p.repo == nil {
		return nil, nil
	}
	return p.repo.GetCallerPolicy(userID)
}

func allowedType(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
