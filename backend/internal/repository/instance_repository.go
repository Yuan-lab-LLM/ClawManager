package repository

import (
	"fmt"
	"strings"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// InstanceRepository defines the interface for instance data operations
type InstanceRepository interface {
	Create(instance *models.Instance) error
	GetByID(id int) (*models.Instance, error)
	GetByAccessToken(accessToken string) (*models.Instance, error)
	GetByUserID(userID int, offset, limit int) ([]models.Instance, error)
	CountByUserID(userID int) (int, error)
	ExistsByUserIDAndName(userID int, name string) (bool, error)
	GetAllRunning() ([]models.Instance, error)
	Update(instance *models.Instance) error
	Delete(id int) error
}

// instanceRepository implements InstanceRepository
type instanceRepository struct {
	sess db.Session
}

// NewInstanceRepository creates a new instance repository
func NewInstanceRepository(sess db.Session) InstanceRepository {
	return &instanceRepository{sess: sess}
}

// Create creates a new instance
func (r *instanceRepository) Create(instance *models.Instance) error {
	res, err := r.sess.Collection("instances").Insert(instance)
	if err != nil {
		return fmt.Errorf("failed to create instance: %w", err)
	}
	// Get the generated ID
	if id, ok := res.ID().(int64); ok {
		instance.ID = int(id)
	}
	return nil
}

// GetByID gets an instance by ID
func (r *instanceRepository) GetByID(id int) (*models.Instance, error) {
	var instance models.Instance
	err := r.sess.Collection("instances").Find(db.Cond{"id": id}).One(&instance)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}
	return &instance, nil
}

// GetByAccessToken gets an instance by its lifecycle gateway token.
func (r *instanceRepository) GetByAccessToken(accessToken string) (*models.Instance, error) {
	var instance models.Instance
	err := r.sess.Collection("instances").Find(db.Cond{"access_token": accessToken}).One(&instance)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get instance by access token: %w", err)
	}
	return &instance, nil
}

// GetByUserID gets instances by user ID with pagination
func (r *instanceRepository) GetByUserID(userID int, offset, limit int) ([]models.Instance, error) {
	var instances []models.Instance
	err := r.sess.Collection("instances").Find(db.Cond{"user_id": userID}).Offset(offset).Limit(limit).All(&instances)
	if err != nil {
		return nil, fmt.Errorf("failed to get instances: %w", err)
	}
	return instances, nil
}

// CountByUserID counts instances by user ID
func (r *instanceRepository) CountByUserID(userID int) (int, error) {
	count, err := r.sess.Collection("instances").Find(db.Cond{"user_id": userID}).Count()
	if err != nil {
		return 0, fmt.Errorf("failed to count instances: %w", err)
	}
	return int(count), nil
}

// ExistsByUserIDAndName checks whether the user already has an instance with the same display name.
func (r *instanceRepository) ExistsByUserIDAndName(userID int, name string) (bool, error) {
	instances, err := r.GetByUserID(userID, 0, 1000)
	if err != nil {
		return false, err
	}

	normalized := strings.TrimSpace(strings.ToLower(name))
	for _, instance := range instances {
		if strings.TrimSpace(strings.ToLower(instance.Name)) == normalized {
			return true, nil
		}
	}

	return false, nil
}

// GetAllRunning gets all instances that are not in stopped or error state (for sync)
func (r *instanceRepository) GetAllRunning() ([]models.Instance, error) {
	var instances []models.Instance
	err := r.sess.Collection("instances").Find(
		db.Or(
			db.Cond{"status": "running"},
			db.Cond{"status": "creating"},
			db.Cond{"status": "stopped"},
			db.Cond{"status": "error"},
		),
	).All(&instances)
	if err != nil {
		return nil, fmt.Errorf("failed to get running instances: %w", err)
	}
	return instances, nil
}

// Update updates an instance
func (r *instanceRepository) Update(instance *models.Instance) error {
	err := r.sess.Collection("instances").Find(db.Cond{"id": instance.ID}).Update(instance)
	if err != nil {
		return fmt.Errorf("failed to update instance: %w", err)
	}
	return nil
}

// Delete deletes an instance
func (r *instanceRepository) Delete(id int) error {
	err := r.sess.Collection("instances").Find(db.Cond{"id": id}).Delete()
	if err != nil {
		return fmt.Errorf("failed to delete instance: %w", err)
	}
	return nil
}
