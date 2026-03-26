package repository

import (
	"fmt"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

// RiskHitRepository defines repository operations for sensitive-data detection records.
type RiskHitRepository interface {
	Create(hit *models.RiskHit) error
	ListByTraceID(traceID string) ([]models.RiskHit, error)
}

type riskHitRepository struct {
	sess db.Session
}

// NewRiskHitRepository creates a new risk hit repository and ensures its table exists.
func NewRiskHitRepository(sess db.Session) RiskHitRepository {
	repo := &riskHitRepository{sess: sess}
	repo.ensureTable()
	return repo
}

func (r *riskHitRepository) ensureTable() {
	const query = `
CREATE TABLE IF NOT EXISTS risk_hits (
  id INT AUTO_INCREMENT PRIMARY KEY,
  trace_id VARCHAR(100) NOT NULL,
  session_id VARCHAR(100) NULL,
  request_id VARCHAR(100) NULL,
  user_id INT NULL,
  instance_id INT NULL,
  invocation_id INT NULL,
  rule_id VARCHAR(100) NOT NULL,
  rule_name VARCHAR(255) NOT NULL,
  severity VARCHAR(20) NOT NULL,
  action VARCHAR(50) NOT NULL,
  match_summary VARCHAR(500) NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_risk_hits_trace_id (trace_id),
  INDEX idx_risk_hits_request_id (request_id),
  INDEX idx_risk_hits_user_id (user_id),
  INDEX idx_risk_hits_invocation_id (invocation_id),
  INDEX idx_risk_hits_severity (severity),
  INDEX idx_risk_hits_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

	if _, err := r.sess.SQL().Exec(query); err != nil {
		panic(fmt.Errorf("failed to ensure risk_hits table: %w", err))
	}
}

func (r *riskHitRepository) Create(hit *models.RiskHit) error {
	if hit.CreatedAt.IsZero() {
		hit.CreatedAt = time.Now()
	}
	res, err := r.sess.Collection("risk_hits").Insert(hit)
	if err != nil {
		return fmt.Errorf("failed to create risk hit: %w", err)
	}
	hit.ID = int(res.ID().(int64))
	return nil
}

func (r *riskHitRepository) ListByTraceID(traceID string) ([]models.RiskHit, error) {
	var items []models.RiskHit
	if err := r.sess.Collection("risk_hits").Find(db.Cond{"trace_id": traceID}).OrderBy("id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list risk hits by trace: %w", err)
	}
	return items, nil
}
