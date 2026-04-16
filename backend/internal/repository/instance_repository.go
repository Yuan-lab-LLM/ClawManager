package repository

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// InstanceRepository defines the interface for instance data operations
type InstanceRepository interface {
	Create(instance *models.Instance) error
	GetByID(id int) (*models.Instance, error)
	GetByAccessToken(accessToken string) (*models.Instance, error)
	GetByAgentBootstrapToken(bootstrapToken string) (*models.Instance, error)
	GetAll(offset, limit int) ([]models.Instance, error)
	CountAll() (int, error)
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

type instanceRow struct {
	ID                       int        `db:"id"`
	UserID                   int        `db:"user_id"`
	Name                     string     `db:"name"`
	Description              *string    `db:"description"`
	Type                     string     `db:"type"`
	Status                   string     `db:"status"`
	CPUCores                 string     `db:"cpu_cores"`
	MemoryGB                 int        `db:"memory_gb"`
	DiskGB                   int        `db:"disk_gb"`
	GPUEnabled               bool       `db:"gpu_enabled"`
	GPUType                  *string    `db:"gpu_type"`
	GPUCount                 int        `db:"gpu_count"`
	OSType                   string     `db:"os_type"`
	OSVersion                string     `db:"os_version"`
	ImageRegistry            *string    `db:"image_registry"`
	ImageTag                 *string    `db:"image_tag"`
	StorageClass             string     `db:"storage_class"`
	MountPath                string     `db:"mount_path"`
	PodName                  *string    `db:"pod_name"`
	PodNamespace             *string    `db:"pod_namespace"`
	PodIP                    *string    `db:"pod_ip"`
	AccessURL                *string    `db:"access_url"`
	AccessToken              *string    `db:"access_token"`
	AgentBootstrapToken      *string    `db:"agent_bootstrap_token"`
	OpenClawConfigSnapshotID *int       `db:"openclaw_config_snapshot_id"`
	CreatedAt                time.Time  `db:"created_at"`
	UpdatedAt                time.Time  `db:"updated_at"`
	StartedAt                *time.Time `db:"started_at"`
	StoppedAt                *time.Time `db:"stopped_at"`
}

func (r instanceRow) toModel() (*models.Instance, error) {
	cpuCores, err := decimalStringToInt(r.CPUCores)
	if err != nil {
		return nil, err
	}
	instance := &models.Instance{
		ID:                       r.ID,
		UserID:                   r.UserID,
		Name:                     r.Name,
		Description:              r.Description,
		Type:                     r.Type,
		Status:                   r.Status,
		CPUCores:                 cpuCores,
		MemoryGB:                 r.MemoryGB,
		DiskGB:                   r.DiskGB,
		GPUEnabled:               r.GPUEnabled,
		GPUType:                  r.GPUType,
		GPUCount:                 r.GPUCount,
		OSType:                   r.OSType,
		OSVersion:                r.OSVersion,
		ImageRegistry:            r.ImageRegistry,
		ImageTag:                 r.ImageTag,
		StorageClass:             r.StorageClass,
		MountPath:                r.MountPath,
		PodName:                  r.PodName,
		PodNamespace:             r.PodNamespace,
		PodIP:                    r.PodIP,
		AccessURL:                r.AccessURL,
		AccessToken:              r.AccessToken,
		AgentBootstrapToken:      r.AgentBootstrapToken,
		OpenClawConfigSnapshotID: r.OpenClawConfigSnapshotID,
		CreatedAt:                r.CreatedAt,
		UpdatedAt:                r.UpdatedAt,
	}
	if r.StartedAt != nil {
		startedAt := *r.StartedAt
		instance.StartedAt = &startedAt
	}
	if r.StoppedAt != nil {
		stoppedAt := *r.StoppedAt
		instance.StoppedAt = &stoppedAt
	}
	return instance, nil
}

func decimalStringToInt(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse decimal instance value %q: %w", raw, err)
	}
	return int(math.Round(parsed)), nil
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
	var row instanceRow
	err := r.sess.Collection("instances").Find(db.Cond{"id": id}).One(&row)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}
	return row.toModel()
}

// GetByAccessToken gets an instance by its lifecycle gateway token.
func (r *instanceRepository) GetByAccessToken(accessToken string) (*models.Instance, error) {
	var row instanceRow
	err := r.sess.Collection("instances").Find(db.Cond{"access_token": accessToken}).One(&row)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get instance by access token: %w", err)
	}
	return row.toModel()
}

func (r *instanceRepository) GetByAgentBootstrapToken(bootstrapToken string) (*models.Instance, error) {
	var row instanceRow
	err := r.sess.Collection("instances").Find(db.Cond{"agent_bootstrap_token": bootstrapToken}).One(&row)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get instance by agent bootstrap token: %w", err)
	}
	return row.toModel()
}

func (r *instanceRepository) GetAll(offset, limit int) ([]models.Instance, error) {
	var rows []instanceRow
	err := r.sess.Collection("instances").Find().Offset(offset).Limit(limit).All(&rows)
	if err != nil {
		return nil, fmt.Errorf("failed to get all instances: %w", err)
	}
	return instanceRowsToModels(rows)
}

func (r *instanceRepository) CountAll() (int, error) {
	count, err := r.sess.Collection("instances").Find().Count()
	if err != nil {
		return 0, fmt.Errorf("failed to count all instances: %w", err)
	}
	return int(count), nil
}

// GetByUserID gets instances by user ID with pagination
func (r *instanceRepository) GetByUserID(userID int, offset, limit int) ([]models.Instance, error) {
	var rows []instanceRow
	err := r.sess.Collection("instances").Find(db.Cond{"user_id": userID}).Offset(offset).Limit(limit).All(&rows)
	if err != nil {
		return nil, fmt.Errorf("failed to get instances: %w", err)
	}
	return instanceRowsToModels(rows)
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
	var rows []instanceRow
	err := r.sess.Collection("instances").Find(
		db.Or(
			db.Cond{"status": "running"},
			db.Cond{"status": "creating"},
			db.Cond{"status": "stopped"},
			db.Cond{"status": "error"},
		),
	).All(&rows)
	if err != nil {
		return nil, fmt.Errorf("failed to get running instances: %w", err)
	}
	return instanceRowsToModels(rows)
}

func instanceRowsToModels(rows []instanceRow) ([]models.Instance, error) {
	items := make([]models.Instance, 0, len(rows))
	for _, row := range rows {
		instance, err := row.toModel()
		if err != nil {
			return nil, err
		}
		items = append(items, *instance)
	}
	return items, nil
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
