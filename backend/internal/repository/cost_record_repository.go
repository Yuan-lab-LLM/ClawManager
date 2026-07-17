package repository

import (
	"fmt"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

// InstanceSessionCostAggregate summarizes cost usage for one session on an instance.
type InstanceSessionCostAggregate struct {
	SessionID        string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	EstimatedCost    float64
	Currency         string
}

// CostRecordRepository defines repository operations for token and money accounting.
type CostRecordRepository interface {
	Create(record *models.CostRecord) error
	ListByTraceID(traceID string) ([]models.CostRecord, error)
	ListByUserID(userID, limit int) ([]models.CostRecord, error)
	ListRecent(limit int) ([]models.CostRecord, error)
	AggregateCostByInstanceSession(instanceID int, filter SessionUsageFilter) ([]InstanceSessionCostAggregate, error)
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
  instance_mode VARCHAR(16) NULL,
  runtime_type VARCHAR(32) NULL,
  gateway_id VARCHAR(128) NULL,
  runtime_pod_id BIGINT NULL,
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
  INDEX idx_cost_records_gateway_id (gateway_id),
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

func (r *costRecordRepository) AggregateCostByInstanceSession(instanceID int, filter SessionUsageFilter) ([]InstanceSessionCostAggregate, error) {
	query := `
SELECT cr.session_id,
       COALESCE(SUM(cr.prompt_tokens), 0),
       COALESCE(SUM(cr.completion_tokens), 0),
       COALESCE(SUM(cr.total_tokens), 0),
       COALESCE(SUM(cr.estimated_cost), 0),
       COALESCE(MAX(cr.currency), 'USD')
FROM cost_records cr
INNER JOIN model_invocations mi
  ON mi.trace_id = cr.trace_id
 AND mi.instance_id = cr.instance_id
 AND mi.status != ?
WHERE cr.instance_id = ?
  AND cr.session_id IS NOT NULL
  AND cr.session_id != ''`
	args := []interface{}{models.ModelInvocationStatusBlocked, instanceID}
	query, args = appendTimeFilter(query, args, filter, "mi.created_at")
	query += `
GROUP BY cr.session_id`
	rows, err := r.sess.SQL().Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate cost records by instance session: %w", err)
	}
	defer rows.Close()

	items := make([]InstanceSessionCostAggregate, 0)
	for rows.Next() {
		var item InstanceSessionCostAggregate
		if err := rows.Scan(
			&item.SessionID,
			&item.PromptTokens,
			&item.CompletionTokens,
			&item.TotalTokens,
			&item.EstimatedCost,
			&item.Currency,
		); err != nil {
			return nil, fmt.Errorf("failed to scan session cost aggregate: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate session cost aggregates: %w", err)
	}
	return items, nil
}
