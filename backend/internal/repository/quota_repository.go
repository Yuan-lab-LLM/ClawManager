package repository

import (
	"fmt"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// QuotaRepository defines the interface for quota data operations
type QuotaRepository interface {
	Create(quota *models.UserQuota) error
	GetByUserID(userID int) (*models.UserQuota, error)
	Update(quota *models.UserQuota) error
	DeleteByUserID(userID int) error
	CreateDefaultQuota(userID int) (*models.UserQuota, error)
}

// quotaRepository implements QuotaRepository
type quotaRepository struct {
	sess db.Session
}

// NewQuotaRepository creates a new quota repository
func NewQuotaRepository(sess db.Session) QuotaRepository {
	return &quotaRepository{sess: sess}
}

// Create creates a new quota
func (r *quotaRepository) Create(quota *models.UserQuota) error {
	_, err := r.sess.Collection("user_quotas").Insert(quota)
	if err != nil {
		return fmt.Errorf("failed to create quota: %w", err)
	}
	return nil
}

// GetByUserID gets quota by user ID
func (r *quotaRepository) GetByUserID(userID int) (*models.UserQuota, error) {
	var quota models.UserQuota
	err := r.sess.Collection("user_quotas").Find(db.Cond{"user_id": userID}).One(&quota)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get quota: %w", err)
	}
	return &quota, nil
}

// Update updates a quota
func (r *quotaRepository) Update(quota *models.UserQuota) error {
	err := r.sess.Collection("user_quotas").Find(db.Cond{"id": quota.ID}).Update(quota)
	if err != nil {
		return fmt.Errorf("failed to update quota: %w", err)
	}
	return nil
}

// DeleteByUserID deletes quota by user ID
func (r *quotaRepository) DeleteByUserID(userID int) error {
	err := r.sess.Collection("user_quotas").Find(db.Cond{"user_id": userID}).Delete()
	if err != nil {
		return fmt.Errorf("failed to delete quota: %w", err)
	}
	return nil
}

// CreateDefaultQuota creates default quota for a user
func (r *quotaRepository) CreateDefaultQuota(userID int) (*models.UserQuota, error) {
	now := time.Now()
	defaults := models.DefaultQuotaForRole("user")
	quota := &models.UserQuota{
		UserID:       userID,
		MaxInstances: defaults.MaxInstances,
		MaxCPUCores:  defaults.MaxCPUCores,
		MaxMemoryGB:  defaults.MaxMemoryGB,
		MaxStorageGB: defaults.MaxStorageGB,
		MaxGPUCount:  defaults.MaxGPUCount,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	_, err := r.sess.Collection("user_quotas").Insert(quota)
	if err != nil {
		return nil, fmt.Errorf("failed to create default quota: %w", err)
	}

	return quota, nil
}
