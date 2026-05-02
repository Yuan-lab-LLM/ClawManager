package repository

import (
	"fmt"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

// InstanceUsageRepository defines repository operations for instance resource
// usage records.
type InstanceUsageRepository interface {
	Create(record *models.InstanceUsage) error
	GetLatestByInstanceID(instanceID int) (*models.InstanceUsage, error)
	ListByInstanceID(instanceID int, since time.Time, limit int) ([]models.InstanceUsage, error)
	ListLatestPerInstance() ([]models.InstanceUsage, error)
	DeleteOlderThan(cutoff time.Time) (int64, error)
}

type instanceUsageRepository struct {
	sess db.Session
}

// NewInstanceUsageRepository creates a new instance usage repository and
// ensures its table exists. The table is created by the init migration, so
// ensureTable is a no-op safety net.
func NewInstanceUsageRepository(sess db.Session) InstanceUsageRepository {
	repo := &instanceUsageRepository{sess: sess}
	repo.ensureTable()
	return repo
}

func (r *instanceUsageRepository) ensureTable() {
	const query = `
CREATE TABLE IF NOT EXISTS instance_usage (
  id INT AUTO_INCREMENT PRIMARY KEY,
  instance_id INT NOT NULL,
  cpu_usage_percent DECIMAL(5,2),
  memory_usage_gb DECIMAL(10,2),
  disk_usage_gb DECIMAL(10,2),
  gpu_usage_percent DECIMAL(5,2),
  uptime_seconds INT,
  recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
  INDEX idx_instance_recorded (instance_id, recorded_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`
	if _, err := r.sess.SQL().Exec(query); err != nil {
		panic(fmt.Errorf("failed to ensure instance_usage table: %w", err))
	}
}

func (r *instanceUsageRepository) Create(record *models.InstanceUsage) error {
	if record.RecordedAt.IsZero() {
		record.RecordedAt = time.Now()
	}
	res, err := r.sess.Collection("instance_usage").Insert(record)
	if err != nil {
		return fmt.Errorf("failed to create instance usage record: %w", err)
	}
	record.ID = int(res.ID().(int64))
	return nil
}

func (r *instanceUsageRepository) GetLatestByInstanceID(instanceID int) (*models.InstanceUsage, error) {
	var item models.InstanceUsage
	err := r.sess.Collection("instance_usage").
		Find(db.Cond{"instance_id": instanceID}).
		OrderBy("-recorded_at").
		Limit(1).
		One(&item)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get latest usage for instance %d: %w", instanceID, err)
	}
	return &item, nil
}

func (r *instanceUsageRepository) ListByInstanceID(instanceID int, since time.Time, limit int) ([]models.InstanceUsage, error) {
	if limit <= 0 {
		limit = 500
	}
	var items []models.InstanceUsage
	err := r.sess.Collection("instance_usage").
		Find(db.And(
			db.Cond{"instance_id": instanceID},
			db.Cond{"recorded_at >=": since},
		)).
		OrderBy("-recorded_at").
		Limit(limit).
		All(&items)
	if err != nil {
		return nil, fmt.Errorf("failed to list usage for instance %d: %w", instanceID, err)
	}
	return items, nil
}

// ListLatestPerInstance returns the most recent usage record for every
// instance that has at least one record. Uses a correlated subquery to
// pick the row with the maximum recorded_at per instance_id.
func (r *instanceUsageRepository) ListLatestPerInstance() ([]models.InstanceUsage, error) {
	var items []models.InstanceUsage
	rows, err := r.sess.SQL().Query(`
SELECT u.id, u.instance_id, u.cpu_usage_percent, u.memory_usage_gb,
       u.disk_usage_gb, u.gpu_usage_percent, u.uptime_seconds, u.recorded_at
FROM instance_usage u
INNER JOIN (
  SELECT instance_id, MAX(recorded_at) AS max_recorded
  FROM instance_usage
  GROUP BY instance_id
) latest ON u.instance_id = latest.instance_id AND u.recorded_at = latest.max_recorded
ORDER BY u.instance_id
`)
	if err != nil {
		return nil, fmt.Errorf("failed to list latest usage per instance: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item models.InstanceUsage
		if err := rows.Scan(
			&item.ID, &item.InstanceID, &item.CPUUsagePercent, &item.MemoryUsageGB,
			&item.DiskUsageGB, &item.GPUUsagePercent, &item.UptimeSeconds, &item.RecordedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan usage row: %w", err)
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *instanceUsageRepository) DeleteOlderThan(cutoff time.Time) (int64, error) {
	result, err := r.sess.SQL().Exec(
		"DELETE FROM instance_usage WHERE recorded_at < ?", cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to delete expired usage records: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected, nil
}

