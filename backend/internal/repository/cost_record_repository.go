package repository

import (
	"fmt"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

// CostRecordRepository defines repository operations for token and money accounting.
type CostRecordRepository interface {
	Create(record *models.CostRecord) error
	ListByTraceID(traceID string) ([]models.CostRecord, error)
	ListByUserID(userID, limit int) ([]models.CostRecord, error)
	ListRecent(limit int) ([]models.CostRecord, error)
}

type costRecordRepository struct {
	sess db.Session
}

// NewCostRecordRepository creates a new cost record repository and ensures its table exists.
func NewCostRecordRepository(sess db.Session) CostRecordRepository {
	repo := &costRecordRepository{sess: sess}
	repo.ensureTable()
	return repo
}

func (r *costRecordRepository) ensureTable() {
	const query = `
CREATE TABLE IF NOT EXISTS cost_records (
  id INT AUTO_INCREMENT PRIMARY KEY,
  trace_id VARCHAR(100) NOT NULL,
  session_id VARCHAR(100) NULL,
  request_id VARCHAR(100) NULL,
  user_id INT NULL,
  instance_id INT NULL,
  invocation_id INT NULL,
  model_id INT NULL,
  provider_type VARCHAR(100) NOT NULL,
  model_name VARCHAR(255) NOT NULL,
  currency VARCHAR(16) NOT NULL DEFAULT 'USD',
  prompt_tokens INT NOT NULL DEFAULT 0,
  completion_tokens INT NOT NULL DEFAULT 0,
  total_tokens INT NOT NULL DEFAULT 0,
  input_unit_price DECIMAL(18,8) NOT NULL DEFAULT 0,
  output_unit_price DECIMAL(18,8) NOT NULL DEFAULT 0,
  estimated_cost DECIMAL(18,8) NOT NULL DEFAULT 0,
  actual_cost DECIMAL(18,8) NULL,
  internal_cost DECIMAL(18,8) NOT NULL DEFAULT 0,
  recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_cost_records_trace_id (trace_id),
  INDEX idx_cost_records_user_id (user_id),
  INDEX idx_cost_records_model_id (model_id),
  INDEX idx_cost_records_provider_type (provider_type),
  INDEX idx_cost_records_recorded_at (recorded_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

	if _, err := r.sess.SQL().Exec(query); err != nil {
		panic(fmt.Errorf("failed to ensure cost_records table: %w", err))
	}
}

func (r *costRecordRepository) Create(record *models.CostRecord) error {
	if record.RecordedAt.IsZero() {
		record.RecordedAt = time.Now()
	}
	res, err := r.sess.Collection("cost_records").Insert(record)
	if err != nil {
		return fmt.Errorf("failed to create cost record: %w", err)
	}
	record.ID = int(res.ID().(int64))
	return nil
}

func (r *costRecordRepository) ListByTraceID(traceID string) ([]models.CostRecord, error) {
	var items []models.CostRecord
	if err := r.sess.Collection("cost_records").Find(db.Cond{"trace_id": traceID}).OrderBy("id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list cost records by trace: %w", err)
	}
	return items, nil
}

func (r *costRecordRepository) ListByUserID(userID, limit int) ([]models.CostRecord, error) {
	var items []models.CostRecord
	if limit <= 0 {
		limit = 50
	}
	if err := r.sess.Collection("cost_records").Find(db.Cond{"user_id": userID}).OrderBy("-recorded_at").Limit(limit).All(&items); err != nil {
		return nil, fmt.Errorf("failed to list cost records by user: %w", err)
	}
	return items, nil
}

func (r *costRecordRepository) ListRecent(limit int) ([]models.CostRecord, error) {
	var items []models.CostRecord
	if limit <= 0 {
		limit = 100
	}
	if err := r.sess.Collection("cost_records").Find().OrderBy("-recorded_at").Limit(limit).All(&items); err != nil {
		return nil, fmt.Errorf("failed to list recent cost records: %w", err)
	}
	return items, nil
}
