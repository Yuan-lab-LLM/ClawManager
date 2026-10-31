package repository

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

type GatewayKeyRepository interface {
	Create(*models.GatewayKey) error
	GetByID(int) (*models.GatewayKey, error)
	GetByKeyID(string) (*models.GatewayKey, error)
	GetByToken(string) (*models.GatewayKey, error)
	List() ([]models.GatewayKey, error)
	UpsertManagedInstance(context.Context, int, int, string) (*models.GatewayKey, error)
	Revoke(context.Context, int, int, string) error
	RotateCredential(context.Context, int, string, string) error
	Touch(context.Context, int, time.Time) error
	UpdateLimits(context.Context, int, int, int, int) error
	UpdateBudgetPolicy(context.Context, int, float64, string, bool) error
	ReserveBudget(context.Context, int, string, float64, time.Time) error
	FinalizeBudget(context.Context, string, int, float64) error
	ReleaseBudget(context.Context, string) error
	GetBudgetUsage(context.Context, int) (*GatewayKeyBudgetUsage, error)
}

var ErrGatewayKeyBudgetExceeded = errors.New("gateway key budget exceeded")
var ErrGatewayBudgetCurrencyReview = errors.New("gateway budget currency requires administrator review and explicit usage reset")

type GatewayKeyBudgetUsage struct {
	GatewayKeyID             int                `json:"gateway_key_id"`
	Limit                    float64            `json:"limit"`
	Spent                    float64            `json:"spent"`
	Reserved                 float64            `json:"reserved"`
	Remaining                *float64           `json:"remaining,omitempty"`
	Period                   string             `json:"period"`
	ResetAt                  *time.Time         `json:"reset_at,omitempty"`
	InvocationCount          int                `json:"invocation_count"`
	SuccessfulRequests       int                `json:"successful_requests"`
	FailedRequests           int                `json:"failed_requests"`
	PromptTokens             int                `json:"prompt_tokens"`
	CompletionTokens         int                `json:"completion_tokens"`
	TotalTokens              int                `json:"total_tokens"`
	AllTimeEstimatedCost     float64            `json:"all_time_estimated_cost"`
	Currency                 string             `json:"currency"`
	LastInvocationAt         *time.Time         `json:"last_invocation_at,omitempty"`
	CurrencyReviewRequired   bool               `json:"currency_review_required"`
	UnverifiedLegacySpent    *float64           `json:"unverified_legacy_spent,omitempty"`
	UnverifiedLegacyReserved *float64           `json:"unverified_legacy_reserved,omitempty"`
	CostsByCurrency          map[string]float64 `json:"costs_by_currency"`
}

func (r *gatewayKeyRepository) UpdateLimits(ctx context.Context, id, rpm, tpm, concurrent int) error {
	_, err := r.sess.SQL().ExecContext(ctx, `UPDATE gateway_keys SET rpm_limit = ?, tpm_limit = ?, concurrent_requests_limit = ?, updated_at = ? WHERE id = ?`, rpm, tpm, concurrent, time.Now().UTC(), id)
	return err
}

func (r *gatewayKeyRepository) UpdateBudgetPolicy(ctx context.Context, id int, limit float64, period string, resetUsage bool) error {
	return r.sess.Tx(func(sess db.Session) error {
		var currentPeriod string
		var reviewRequired bool
		row, err := sess.SQL().QueryRow(`SELECT budget_period, budget_currency_review_required FROM gateway_keys WHERE id = ? FOR UPDATE`, id)
		if err != nil {
			return err
		}
		if err := row.Scan(&currentPeriod, &reviewRequired); err != nil {
			return err
		}
		if reviewRequired && !resetUsage {
			return ErrGatewayBudgetCurrencyReview
		}
		now := time.Now().UTC()
		if _, err := reconcilePendingGatewayBudget(ctx, sess, id, now); err != nil {
			return err
		}
		if resetUsage || currentPeriod != period {
			if _, err := sess.SQL().ExecContext(ctx, `DELETE FROM gateway_key_budget_reservations WHERE gateway_key_id = ? AND settlement_status <> 'image_pending'`, id); err != nil {
				return err
			}
			_, err = sess.SQL().ExecContext(ctx, `UPDATE gateway_keys SET budget_limit = ?, budget_period = ?, budget_spent = 0, budget_reset_at = ?, budget_currency_review_required = IF(?, FALSE, budget_currency_review_required), budget_currency_reviewed_at = IF(?, ?, budget_currency_reviewed_at), updated_at = ? WHERE id = ?`, limit, period, nextGatewayBudgetReset(period, now), resetUsage, resetUsage, now, now, id)
			return err
		}
		_, err = sess.SQL().ExecContext(ctx, `UPDATE gateway_keys SET budget_limit = ?, updated_at = ? WHERE id = ?`, limit, now, id)
		return err
	})
}

func (r *gatewayKeyRepository) ReserveBudget(ctx context.Context, keyID int, reservationID string, estimatedCost float64, expiresAt time.Time) error {
	if estimatedCost < 0 {
		estimatedCost = 0
	}
	return r.sess.Tx(func(sess db.Session) error {
		usage, err := loadGatewayKeyBudgetForUpdate(ctx, sess, keyID, time.Now().UTC())
		if err != nil {
			return err
		}
		if usage.CurrencyReviewRequired {
			return ErrGatewayBudgetCurrencyReview
		}
		if usage.Limit > 0 && usage.Spent+usage.Reserved+estimatedCost > usage.Limit+1e-9 {
			return ErrGatewayKeyBudgetExceeded
		}
		_, err = sess.SQL().ExecContext(ctx, `INSERT INTO gateway_key_budget_reservations (reservation_id, gateway_key_id, reserved_cost, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`, reservationID, keyID, estimatedCost, expiresAt.UTC(), time.Now().UTC())
		return err
	})
}

func (r *gatewayKeyRepository) FinalizeBudget(ctx context.Context, reservationID string, keyID int, actualCost float64) error {
	if strings.TrimSpace(reservationID) == "" {
		return nil
	}
	if actualCost < 0 {
		actualCost = 0
	}
	// Persist the actual amount before moving it to the ledger. If the ledger
	// transaction fails, the next request/usage read reconciles this row.
	if _, err := r.sess.SQL().ExecContext(ctx, `UPDATE gateway_key_budget_reservations SET actual_cost = ?, settlement_status = 'pending', expires_at = ? WHERE reservation_id = ? AND gateway_key_id = ?`, actualCost, time.Now().UTC().Add(24*time.Hour), reservationID, keyID); err != nil {
		return err
	}
	return r.sess.Tx(func(sess db.Session) error {
		if _, err := loadGatewayKeyBudgetForUpdate(ctx, sess, keyID, time.Now().UTC()); err != nil {
			return err
		}
		var alreadyRecorded int
		row, err := sess.SQL().QueryRow(`SELECT COUNT(*) FROM gateway_key_budget_ledger WHERE reservation_id = ?`, reservationID)
		if err != nil {
			return err
		}
		if err := row.Scan(&alreadyRecorded); err != nil {
			return err
		}
		if alreadyRecorded > 0 {
			return nil
		}
		if _, err := sess.SQL().ExecContext(ctx, `DELETE FROM gateway_key_budget_reservations WHERE reservation_id = ? AND gateway_key_id = ?`, reservationID, keyID); err != nil {
			return err
		}
		if _, err := sess.SQL().ExecContext(ctx, `INSERT INTO gateway_key_budget_ledger (reservation_id, gateway_key_id, actual_cost, recorded_at) VALUES (?, ?, ?, ?)`, reservationID, keyID, actualCost, time.Now().UTC()); err != nil {
			return err
		}
		_, err = sess.SQL().ExecContext(ctx, `UPDATE gateway_keys SET budget_spent = budget_spent + ?, updated_at = ? WHERE id = ?`, actualCost, time.Now().UTC(), keyID)
		return err
	})
}

func (r *gatewayKeyRepository) ReleaseBudget(ctx context.Context, reservationID string) error {
	if strings.TrimSpace(reservationID) == "" {
		return nil
	}
	_, err := r.sess.SQL().ExecContext(ctx, `DELETE FROM gateway_key_budget_reservations WHERE reservation_id = ? AND settlement_status = 'reserved'`, reservationID)
	return err
}

func (r *gatewayKeyRepository) GetBudgetUsage(ctx context.Context, keyID int) (*GatewayKeyBudgetUsage, error) {
	var usage *GatewayKeyBudgetUsage
	err := r.sess.Tx(func(sess db.Session) error {
		var err error
		usage, err = loadGatewayKeyBudgetForUpdate(ctx, sess, keyID, time.Now().UTC())
		return err
	})
	if err != nil {
		return nil, err
	}
	invocationRow, queryErr := r.sess.SQL().QueryRow(`
SELECT COUNT(*),
       COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN status != ? THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(prompt_tokens), 0),
       COALESCE(SUM(completion_tokens), 0),
       COALESCE(SUM(total_tokens), 0),
       MAX(created_at)
FROM model_invocations
WHERE gateway_key_id = ?`, models.ModelInvocationStatusCompleted, models.ModelInvocationStatusCompleted, keyID)
	if queryErr != nil {
		return nil, queryErr
	}
	if scanErr := invocationRow.Scan(&usage.InvocationCount, &usage.SuccessfulRequests, &usage.FailedRequests, &usage.PromptTokens, &usage.CompletionTokens, &usage.TotalTokens, &usage.LastInvocationAt); scanErr != nil {
		return nil, scanErr
	}
	costRows, queryErr := r.sess.SQL().Query(`SELECT UPPER(COALESCE(NULLIF(TRIM(currency), ''), 'USD')), COALESCE(SUM(estimated_cost), 0) FROM cost_records WHERE gateway_key_id = ? GROUP BY UPPER(COALESCE(NULLIF(TRIM(currency), ''), 'USD'))`, keyID)
	if queryErr != nil {
		return nil, queryErr
	}
	defer costRows.Close()
	usage.CostsByCurrency = map[string]float64{}
	for costRows.Next() {
		var currency string
		var amount float64
		if err := costRows.Scan(&currency, &amount); err != nil {
			return nil, err
		}
		usage.CostsByCurrency[currency] = amount
	}
	if err := costRows.Err(); err != nil {
		return nil, err
	}
	finalizeBudgetUsageCurrencies(usage)
	return usage, nil
}

func finalizeBudgetUsageCurrencies(usage *GatewayKeyBudgetUsage) {
	usage.Currency = "USD"
	usage.AllTimeEstimatedCost = usage.CostsByCurrency["USD"]
	if usage.CurrencyReviewRequired {
		legacy := usage.Spent
		usage.UnverifiedLegacySpent = &legacy
		reserved := usage.Reserved
		usage.UnverifiedLegacyReserved = &reserved
		usage.Reserved = 0
		usage.Spent = 0
		usage.Remaining = nil
	}
}

func loadGatewayKeyBudgetForUpdate(ctx context.Context, sess db.Session, keyID int, now time.Time) (*GatewayKeyBudgetUsage, error) {
	usage := &GatewayKeyBudgetUsage{GatewayKeyID: keyID, Currency: "USD"}
	row, err := sess.SQL().QueryRow(`SELECT budget_limit, budget_spent, budget_period, budget_reset_at, budget_currency_review_required FROM gateway_keys WHERE id = ? FOR UPDATE`, keyID)
	if err != nil {
		return nil, err
	}
	if err := row.Scan(&usage.Limit, &usage.Spent, &usage.Period, &usage.ResetAt, &usage.CurrencyReviewRequired); err != nil {
		return nil, err
	}
	settledCost, err := reconcilePendingGatewayBudget(ctx, sess, keyID, now)
	if err != nil {
		return nil, err
	}
	usage.Spent += settledCost
	if !usage.CurrencyReviewRequired && usage.ResetAt != nil && !usage.ResetAt.After(now) {
		usage.Spent = 0
		usage.ResetAt = nextGatewayBudgetReset(usage.Period, now)
		if _, err := sess.SQL().ExecContext(ctx, `DELETE FROM gateway_key_budget_reservations WHERE gateway_key_id = ? AND settlement_status <> 'image_pending'`, keyID); err != nil {
			return nil, err
		}
		if _, err := sess.SQL().ExecContext(ctx, `UPDATE gateway_keys SET budget_spent = 0, budget_reset_at = ?, updated_at = ? WHERE id = ?`, usage.ResetAt, now, keyID); err != nil {
			return nil, err
		}
	}
	if _, err := sess.SQL().ExecContext(ctx, `DELETE FROM gateway_key_budget_reservations WHERE gateway_key_id = ? AND settlement_status = 'reserved' AND expires_at <= ?`, keyID, now); err != nil {
		return nil, err
	}
	reservedRow, err := sess.SQL().QueryRow(`SELECT COALESCE(SUM(reserved_cost), 0) FROM gateway_key_budget_reservations WHERE gateway_key_id = ? AND (expires_at > ? OR settlement_status = 'image_pending')`, keyID, now)
	if err != nil {
		return nil, err
	}
	if err := reservedRow.Scan(&usage.Reserved); err != nil {
		return nil, err
	}
	if usage.Limit > 0 {
		remaining := usage.Limit - usage.Spent - usage.Reserved
		if remaining < 0 {
			remaining = 0
		}
		usage.Remaining = &remaining
	}
	return usage, nil
}

func reconcilePendingGatewayBudget(ctx context.Context, sess db.Session, keyID int, now time.Time) (float64, error) {
	rows, err := sess.SQL().Query(`SELECT reservation_id, actual_cost FROM gateway_key_budget_reservations WHERE gateway_key_id = ? AND settlement_status = 'pending' FOR UPDATE`, keyID)
	if err != nil {
		return 0, err
	}
	type pendingSettlement struct {
		id   string
		cost float64
	}
	items := make([]pendingSettlement, 0)
	for rows.Next() {
		var item pendingSettlement
		if err := rows.Scan(&item.id, &item.cost); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	settled := 0.0
	for _, item := range items {
		result, err := sess.SQL().ExecContext(ctx, `INSERT IGNORE INTO gateway_key_budget_ledger (reservation_id, gateway_key_id, actual_cost, recorded_at) VALUES (?, ?, ?, ?)`, item.id, keyID, item.cost, now)
		if err != nil {
			return 0, err
		}
		if affected, err := result.RowsAffected(); err == nil && affected > 0 {
			settled += item.cost
		}
		if _, err := sess.SQL().ExecContext(ctx, `DELETE FROM gateway_key_budget_reservations WHERE reservation_id = ?`, item.id); err != nil {
			return 0, err
		}
	}
	if settled > 0 {
		if _, err := sess.SQL().ExecContext(ctx, `UPDATE gateway_keys SET budget_spent = budget_spent + ?, updated_at = ? WHERE id = ?`, settled, now, keyID); err != nil {
			return 0, err
		}
	}
	return settled, nil
}

func nextGatewayBudgetReset(period string, now time.Time) *time.Time {
	now = now.UTC()
	var next time.Time
	switch period {
	case models.GatewayKeyBudgetPeriodDaily:
		next = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	case models.GatewayKeyBudgetPeriodWeekly:
		days := (int(time.Monday) - int(now.Weekday()) + 7) % 7
		if days == 0 {
			days = 7
		}
		next = time.Date(now.Year(), now.Month(), now.Day()+days, 0, 0, 0, 0, time.UTC)
	case models.GatewayKeyBudgetPeriodMonthly:
		next = time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	default:
		return nil
	}
	return &next
}

type gatewayKeyRepository struct{ sess db.Session }

func NewGatewayKeyRepository(sess db.Session) GatewayKeyRepository {
	return &gatewayKeyRepository{sess: sess}
}

func (r *gatewayKeyRepository) Create(key *models.GatewayKey) error {
	if strings.TrimSpace(key.BudgetPeriod) == "" {
		key.BudgetPeriod = models.GatewayKeyBudgetPeriodLifetime
	}
	if key.BudgetResetAt == nil {
		key.BudgetResetAt = nextGatewayBudgetReset(key.BudgetPeriod, time.Now().UTC())
	}
	_, err := r.sess.Collection("gateway_keys").Insert(key)
	return err
}

func (r *gatewayKeyRepository) GetByID(id int) (*models.GatewayKey, error) {
	var key models.GatewayKey
	err := r.sess.Collection("gateway_keys").Find(db.Cond{"id": id}).One(&key)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	return &key, err
}

func (r *gatewayKeyRepository) GetByKeyID(keyID string) (*models.GatewayKey, error) {
	var key models.GatewayKey
	err := r.sess.Collection("gateway_keys").Find(db.Cond{"key_id": strings.TrimSpace(keyID)}).One(&key)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	return &key, err
}

func (r *gatewayKeyRepository) GetByToken(token string) (*models.GatewayKey, error) {
	var key models.GatewayKey
	err := r.sess.Collection("gateway_keys").Find(db.Cond{"token_hash": GatewayKeyTokenHash(token)}).One(&key)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	return &key, err
}

func (r *gatewayKeyRepository) List() ([]models.GatewayKey, error) {
	items := []models.GatewayKey{}
	err := r.sess.Collection("gateway_keys").Find().OrderBy("-created_at", "-id").All(&items)
	return items, err
}

func (r *gatewayKeyRepository) UpsertManagedInstance(ctx context.Context, instanceID, ownerUserID int, token string) (*models.GatewayKey, error) {
	if instanceID <= 0 || ownerUserID <= 0 || strings.TrimSpace(token) == "" {
		return nil, nil
	}
	key, err := r.findManagedInstance(instanceID)
	if err != nil {
		return nil, err
	}
	if key != nil {
		tokenHash := GatewayKeyTokenHash(token)
		if key.TokenHash != tokenHash || key.OwnerUserID == nil || *key.OwnerUserID != ownerUserID {
			now := time.Now().UTC()
			if _, err := r.sess.SQL().ExecContext(ctx, `UPDATE gateway_keys SET token_hash = ?, key_prefix = ?, owner_user_id = ?, updated_at = ? WHERE id = ?`, tokenHash, maskGatewayKey(token), ownerUserID, now, key.ID); err != nil {
				return nil, err
			}
			key.TokenHash = tokenHash
			key.KeyPrefix = maskGatewayKey(token)
			key.OwnerUserID = &ownerUserID
			key.UpdatedAt = now
		}
		return key, nil
	}
	now := time.Now().UTC()
	key = &models.GatewayKey{KeyID: "legacy-instance-" + itoa(instanceID), KeyPrefix: maskGatewayKey(token), TokenHash: GatewayKeyTokenHash(token), KeyType: models.GatewayKeyTypeManagedInstance, Name: "Managed instance " + itoa(instanceID), OwnerUserID: &ownerUserID, InstanceID: &instanceID, BudgetPeriod: models.GatewayKeyBudgetPeriodLifetime, Status: models.GatewayKeyStatusActive, CreatedAt: now, UpdatedAt: now}
	_, err = r.sess.Collection("gateway_keys").Insert(key)
	if err != nil {
		return nil, err
	}
	return key, nil
}

func (r *gatewayKeyRepository) findManagedInstance(instanceID int) (*models.GatewayKey, error) {
	var key models.GatewayKey
	err := r.sess.Collection("gateway_keys").Find(db.Cond{"instance_id": instanceID, "key_type": models.GatewayKeyTypeManagedInstance}).One(&key)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	return &key, err
}

func (r *gatewayKeyRepository) Revoke(ctx context.Context, id, actorID int, reason string) error {
	now := time.Now().UTC()
	_, err := r.sess.SQL().ExecContext(ctx, `UPDATE gateway_keys SET status = ?, revoked_at = ?, revoked_by_user_id = ?, revocation_reason = ?, updated_at = ? WHERE id = ?`, models.GatewayKeyStatusRevoked, now, actorID, strings.TrimSpace(reason), now, id)
	return err
}

func (r *gatewayKeyRepository) RotateCredential(ctx context.Context, id int, prefix, tokenHash string) error {
	result, err := r.sess.SQL().ExecContext(ctx, `UPDATE gateway_keys SET key_prefix = ?, token_hash = ?, last_used_at = NULL, updated_at = ? WHERE id = ? AND status = ?`, prefix, tokenHash, time.Now().UTC(), id, models.GatewayKeyStatusActive)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("gateway key is no longer active")
	}
	return nil
}

func (r *gatewayKeyRepository) Touch(ctx context.Context, id int, at time.Time) error {
	_, err := r.sess.SQL().ExecContext(ctx, `UPDATE gateway_keys SET last_used_at = ?, updated_at = ? WHERE id = ?`, at.UTC(), at.UTC(), id)
	return err
}

func GatewayKeyTokenHash(token string) string {
	secret := strings.TrimSpace(os.Getenv("GATEWAY_KEY_PEPPER"))
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("JWT_SECRET"))
	}
	if secret == "" {
		secret = "clawmanager-development-gateway-key-pepper"
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(mac.Sum(nil))
}

func maskGatewayKey(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 12 {
		return token
	}
	return token[:8] + "..." + token[len(token)-4:]
}
func GatewayKeyTokenPrefix(token string) string { return maskGatewayKey(token) }
func itoa(value int) string                     { return strconv.Itoa(value) }
