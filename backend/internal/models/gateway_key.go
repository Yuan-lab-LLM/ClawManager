package models

import "time"

const (
	GatewayKeyTypeManagedInstance = "managed_instance"
	GatewayKeyTypeUserCreated     = "user_created"
	GatewayKeyTypeService         = "service"

	GatewayKeyStatusActive   = "active"
	GatewayKeyStatusDisabled = "disabled"
	GatewayKeyStatusRevoked  = "revoked"

	GatewayKeyBudgetPeriodLifetime = "lifetime"
	GatewayKeyBudgetPeriodDaily    = "daily"
	GatewayKeyBudgetPeriodWeekly   = "weekly"
	GatewayKeyBudgetPeriodMonthly  = "monthly"
)

// GatewayKey is a governed credential accepted by the LLM Gateway. The raw
// token is never stored here: TokenHash is an HMAC digest of it.
type GatewayKey struct {
	ID                           int        `db:"id,primarykey,autoincrement" json:"id"`
	KeyID                        string     `db:"key_id" json:"key_id"`
	KeyPrefix                    string     `db:"key_prefix" json:"key_prefix"`
	TokenHash                    string     `db:"token_hash" json:"-"`
	KeyType                      string     `db:"key_type" json:"key_type"`
	Name                         string     `db:"name" json:"name"`
	Description                  *string    `db:"description" json:"description,omitempty"`
	OwnerUserID                  *int       `db:"owner_user_id" json:"owner_user_id,omitempty"`
	InstanceID                   *int       `db:"instance_id" json:"instance_id,omitempty"`
	AllowedModelGroups           *string    `db:"allowed_model_groups" json:"allowed_model_groups,omitempty"`
	RPMLimit                     int        `db:"rpm_limit" json:"rpm_limit"`
	TPMLimit                     int        `db:"tpm_limit" json:"tpm_limit"`
	ConcurrentRequestsLimit      int        `db:"concurrent_requests_limit" json:"concurrent_requests_limit"`
	BudgetLimit                  float64    `db:"budget_limit" json:"budget_limit"`
	BudgetPeriod                 string     `db:"budget_period" json:"budget_period"`
	BudgetSpent                  float64    `db:"budget_spent" json:"budget_spent"`
	BudgetCurrencyReviewRequired bool       `db:"budget_currency_review_required" json:"budget_currency_review_required"`
	BudgetCurrencyReviewedAt     *time.Time `db:"budget_currency_reviewed_at" json:"budget_currency_reviewed_at,omitempty"`
	BudgetResetAt                *time.Time `db:"budget_reset_at" json:"budget_reset_at,omitempty"`
	Status                       string     `db:"status" json:"status"`
	ExpiresAt                    *time.Time `db:"expires_at" json:"expires_at,omitempty"`
	LastUsedAt                   *time.Time `db:"last_used_at" json:"last_used_at,omitempty"`
	CreatedByUserID              *int       `db:"created_by_user_id" json:"created_by_user_id,omitempty"`
	RevokedAt                    *time.Time `db:"revoked_at" json:"revoked_at,omitempty"`
	RevokedByUserID              *int       `db:"revoked_by_user_id" json:"revoked_by_user_id,omitempty"`
	RevocationReason             *string    `db:"revocation_reason" json:"revocation_reason,omitempty"`
	CreatedAt                    time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt                    time.Time  `db:"updated_at" json:"updated_at"`
}

func (GatewayKey) TableName() string { return "gateway_keys" }

func (k GatewayKey) IsUsable(now time.Time) bool {
	return k.Status == GatewayKeyStatusActive && (k.ExpiresAt == nil || k.ExpiresAt.After(now))
}
